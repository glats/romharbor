package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/config"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
)

func TestMain(m *testing.M) {
	log.Logger = zerolog.Nop()
	os.Exit(m.Run())
}

var romBytes = []byte("romharbor worker test rom")

func romServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(romBytes)
	}))
	t.Cleanup(server.Close)

	return server
}

func installRequest(url string) rom.InstallRequest {
	file := rom.File{Name: "Game.sfc", Size: int64(len(romBytes)), URL: url}
	game := rom.Game{ID: "g", Platform: "snes", Title: "Game", Files: []rom.File{file}}
	platform, _ := rom.PlatformBySlug("snes")
	return rom.InstallRequest{Game: game, File: file, Platform: platform}
}

func workerConfig(t *testing.T) config.Config {
	t.Helper()

	return config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "app.db")},
		ROM:      config.ROMConfig{Library: filepath.Join(t.TempDir(), "roms")},
	}
}

func startWorker(t *testing.T, cfg config.Config) func() error {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(30 * time.Second):
			t.Fatal("the worker did not stop")
			return nil
		}
	}
	t.Cleanup(func() { _ = stop() })

	return stop
}

func waitForJob(t *testing.T, store *sqlite.JobStore, id string) rom.Job {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for {
		job, found, err := store.Get(context.Background(), id)
		require.NoError(t, err)
		if found && job.Status == rom.JobDone {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job %s was never completed, last = %+v", id, job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheWorkerClaimsAndCompletesAQueuedJob(t *testing.T) {
	server := romServer(t)
	cfg := workerConfig(t)
	require.NoError(t, sqlite.EnsureSchema(t.Context(), cfg.Database.Path))
	db, err := sqlite.Open(t.Context(), cfg.Database.Path)
	require.NoError(t, err)
	jobs := sqlite.NewJobStore(db)
	queued, err := jobs.Enqueue(t.Context(), installRequest(server.URL+"/Game.sfc"))
	require.NoError(t, err)

	stop := startWorker(t, cfg)

	done := waitForJob(t, jobs, queued.ID)
	assert.Equal(t, "snes/Game.sfc", done.Path)
	installed, err := os.ReadFile(filepath.Join(cfg.ROM.Library, filepath.FromSlash(done.Path)))
	require.NoError(t, err)
	assert.Equal(t, romBytes, installed)

	require.NoError(t, stop(), "the worker stops gracefully")
}

func TestTheWorkerShutsDownGracefullyWhenTheContextIsCancelled(t *testing.T) {
	cfg := workerConfig(t)
	stop := startWorker(t, cfg)

	require.NoError(t, stop())
}

func TestTheWebAndTheWorkerShareTheDatabase(t *testing.T) {
	server := romServer(t)
	cfg := workerConfig(t)
	require.NoError(t, sqlite.EnsureSchema(t.Context(), cfg.Database.Path))

	web, err := sqlite.Open(t.Context(), cfg.Database.Path)
	require.NoError(t, err)
	defer func() { _ = web.Close() }()
	enqueuer := romdownloads.NewEnqueuer(sqlite.NewJobStore(web))
	queued, err := enqueuer.Start(context.Background(), installRequest(server.URL+"/Game.sfc"))
	require.NoError(t, err)

	stop := startWorker(t, cfg)

	worker, err := sqlite.Open(t.Context(), cfg.Database.Path)
	require.NoError(t, err)
	defer func() { _ = worker.Close() }()
	done := waitForJob(t, sqlite.NewJobStore(worker), queued.ID)
	assert.Equal(t, rom.JobDone, done.Status)
	assert.Equal(t, "snes/Game.sfc", done.Path)

	require.NoError(t, stop())
}

func TestParseFlagsOverridesTheEnvironment(t *testing.T) {
	base := config.Config{
		Database: config.DatabaseConfig{Path: "/env/app.db"},
		ROM:      config.ROMConfig{Library: "/env/roms"},
	}

	cfg, err := parseFlags([]string{"--library=/flag/roms", "--database=/flag/app.db"}, base, os.Stderr)

	require.NoError(t, err)
	assert.Equal(t, "/flag/roms", cfg.ROM.Library)
	assert.Equal(t, "/flag/app.db", cfg.Database.Path)
}

func TestParseFlagsKeepsTheEnvironmentWhenUnset(t *testing.T) {
	base := config.Config{
		Database: config.DatabaseConfig{Path: "/env/app.db"},
		ROM:      config.ROMConfig{Library: "/env/roms"},
	}

	cfg, err := parseFlags(nil, base, os.Stderr)

	require.NoError(t, err)
	assert.Equal(t, base, cfg)
}

func TestParseFlagsRejectsAnUnknownFlag(t *testing.T) {
	_, err := parseFlags([]string{"--no-such-flag"}, config.Config{}, os.Stderr)

	assert.Error(t, err)
}
