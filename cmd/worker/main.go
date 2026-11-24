package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"

	"app.ddd/internal/adapter/config"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/common/logger"
)

const shutdownTimeout = 30 * time.Second

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
	if err := logger.Configure(cfg.Log.Level, cfg.Log.Pretty, os.Stderr); err != nil {
		log.Fatal().Err(err).Str("level", cfg.Log.Level).Msg("parse LOG_LEVEL")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	err = run(ctx, cfg)
	stop()
	if err != nil {
		log.Fatal().Err(err).Msg("download worker stopped")
	}
}

func parseFlags(args []string, cfg config.Config, out io.Writer) (config.Config, error) {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	fs.SetOutput(out)
	library := fs.String("library", cfg.ROM.Library, "ROM library root the downloads are written to; overrides ROM_LIBRARY_PATH")
	database := fs.String("database", cfg.Database.Path, "SQLite database file; overrides DATABASE_PATH")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, err
	}

	cfg.ROM.Library = *library
	cfg.Database.Path = *database
	return cfg, nil
}

// run claims the queued downloads and runs them until the context is done.
func run(ctx context.Context, cfg config.Config) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	if err := sqlite.EnsureSchema(ctx, cfg.Database.Path); err != nil {
		return stopped(ctx, err)
	}

	db, err := sqlite.Open(ctx, cfg.Database.Path)
	if err != nil {
		return stopped(ctx, err)
	}
	defer func() { _ = db.Close() }()

	if cfg.Seed.Enabled {
		if err := sqlite.Seed(ctx, db); err != nil {
			return stopped(ctx, err)
		}
	}

	jobs := sqlite.NewJobStore(db)
	library := &romlibrary.Library{Root: cfg.ROM.Library}
	runner := romdownloads.NewRunner(library, romdownloads.JobPorts{
		Producer:    jobs,
		Worker:      jobs,
		Maintenance: jobs,
	}, romdownloads.DefaultPolicy())
	if err := runner.Resume(ctx); err != nil {
		return stopped(ctx, err)
	}
	log.Info().Str("library", cfg.ROM.Library).Msg("download worker ready")

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	return runner.Shutdown(shutdownCtx)
}

// stopped reports a startup step that the shutdown interrupted: a cancelled
// context is a graceful stop, not a failure.
func stopped(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
