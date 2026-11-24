package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/config"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/adapter/romsource"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/app"
	"app.ddd/internal/common/logger"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

const (
	shutdownTimeout = 30 * time.Second
	sourceTimeout   = time.Minute
)

func main() {
	if err := godotenv.Load(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal().Err(err).Msg("load .env")
	}

	cfg, err := parseFlags(os.Args[1:], config.Get(), os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		os.Exit(2)
	}
	sources, err := buildSources(cfg.ROM.Sources)
	if err != nil {
		log.Fatal().Err(err).Str("sources", cfg.ROM.Sources).Msg("parse ROM sources")
	}
	if len(sources) == 0 {
		log.Fatal().Str("sources", cfg.ROM.Sources).Msg("no ROM sources enabled")
	}
	if err := logger.Configure(cfg.Log.Level, cfg.Log.Pretty, os.Stderr); err != nil {
		log.Fatal().Err(err).Str("level", cfg.Log.Level).Msg("parse LOG_LEVEL")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err = run(ctx, cfg, func(addr net.Addr) {
		log.Info().Str("addr", addr.String()).Str("library", cfg.ROM.Library).Msg("listening")
	})
	stop()
	if err != nil {
		log.Fatal().Err(err).Msg("web server stopped")
	}
}

func parseFlags(args []string, cfg config.Config, out io.Writer) (config.Config, error) {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(out)
	listen := fs.String("listen", cfg.HTTP.Addr, "HTTP listen address; overrides HTTP_ADDR")
	library := fs.String("library", cfg.ROM.Library, "ROM library root the downloads are written to; overrides ROM_LIBRARY_PATH")
	cache := fs.String("cache", cfg.ROM.Cache, "directory holding the cached ROM listings; overrides ROM_CACHE_PATH")
	sources := fs.String("sources", cfg.ROM.Sources, "comma separated ROM sources: archive.org, dlarchive, lolroms; overrides ROM_SOURCES")
	ttl := fs.Duration("ttl", cfg.ROM.CacheTTL, "how long a ROM listing stays fresh; overrides ROM_CACHE_TTL")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}

	cfg.HTTP.Addr = *listen
	cfg.ROM.Library = *library
	cfg.ROM.Cache = *cache
	cfg.ROM.Sources = *sources
	cfg.ROM.CacheTTL = *ttl
	return cfg, nil
}

func run(ctx context.Context, cfg config.Config, ready func(net.Addr)) error {
	if err := sqlite.Migrate(ctx, cfg.Database.Path); err != nil {
		return err
	}

	db, err := sqlite.Open(ctx, cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if cfg.Seed.Enabled {
		if err := sqlite.Seed(ctx, db); err != nil {
			return err
		}
	}

	sources, err := buildSources(cfg.ROM.Sources)
	if err != nil {
		return err
	}
	cache := romcache.New(cfg.ROM.Cache)
	library := &romlibrary.Library{Root: cfg.ROM.Library}
	jobs := sqlite.NewJobStore(db)
	listings := romlistings.NewListings(sources, cache, cfg.ROM.CacheTTL)
	downloads := romdownloads.NewDownloads(library, romdownloads.JobPorts{
		Producer:    jobs,
		Worker:      jobs,
		Maintenance: jobs,
	}, romdownloads.DefaultPolicy())
	if err := downloads.Resume(ctx); err != nil {
		return err
	}
	application := service.NewApp(sqlite.NewUserRepository(db), listings, downloads, jobs, library)
	stopDownloads := func() error {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		return downloads.Shutdown(shutdownCtx)
	}
	defer func() { _ = stopDownloads() }()
	log.Info().Strs("sources", sourceNames(sources)).Msg("ROM sources ready")

	listener, err := net.Listen("tcp", cfg.HTTP.Addr)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", cfg.HTTP.Addr, err)
	}

	server := newHTTPServer(application)

	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	if ready != nil {
		ready(listener.Addr())
	}

	select {
	case err := <-served:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}

	return stopDownloads()
}

func newHTTPServer(application *app.Application) *http.Server {
	server := &http.Server{
		Handler:           web.NewHandler(application),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	if application.Query.ListPlatforms != nil {
		server.WriteTimeout = 0
	}
	return server
}

func sourceNames(sources []adapter.RomSource) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name())
	}
	sort.Strings(names)
	return names
}

func buildSources(list string) ([]adapter.RomSource, error) {
	client := romfetch.NewClient(sourceTimeout)
	var sources []adapter.RomSource
	for _, name := range strings.Split(list, ",") {
		switch strings.TrimSpace(name) {
		case "":
			continue
		case "archive.org", "archiveorg":
			sources = append(sources, romsource.NewArchiveOrg(client))
		case "dlarchive":
			sources = append(sources, romsource.NewDlArchive(client))
		case "lolroms":
			sources = append(sources, romsource.NewLolRoms(client))
		default:
			return nil, errors.New("unknown source: " + name)
		}
	}
	return sources, nil
}
