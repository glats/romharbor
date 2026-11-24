package web_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
)

func newJobs(t *testing.T) *sqlite.JobStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, sqlite.Migrate(t.Context(), path))
	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return sqlite.NewJobStore(db)
}

func newDownloads(t *testing.T, library adapter.RomLibrary, jobs *sqlite.JobStore, logger *slog.Logger) *romdownloads.Enqueuer {
	t.Helper()
	downloads, _ := newDownloadsWithRunner(t, library, jobs, logger)
	return downloads
}

func newDownloadsWithRunner(t *testing.T, library adapter.RomLibrary, jobs *sqlite.JobStore, logger *slog.Logger) (*romdownloads.Enqueuer, *romdownloads.Runner) {
	t.Helper()
	policy := romdownloads.DefaultPolicy()
	policy.Poll = 5 * time.Millisecond
	policy.Backoff = func(int) time.Duration { return time.Millisecond }
	runner := romdownloads.NewRunner(library, romdownloads.JobPorts{
		Producer:    jobs,
		Worker:      jobs,
		Maintenance: jobs,
	}, policy, romdownloads.WithLogger(logger))
	require.NoError(t, runner.Resume(context.Background()))
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	return romdownloads.NewEnqueuer(jobs), runner
}

func listJobs(t *testing.T, jobs *sqlite.JobStore) []rom.Job {
	t.Helper()
	list, err := jobs.List(context.Background())
	require.NoError(t, err)
	return list
}

func getJob(t *testing.T, jobs *sqlite.JobStore, id string) rom.Job {
	t.Helper()
	job, _, err := jobs.Get(context.Background(), id)
	require.NoError(t, err)
	return job
}
