package romdownloads

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
)

type fakeLibrary struct {
	mu       sync.Mutex
	installs []string
	install  func(ctx context.Context, req rom.InstallRequest, progress func(int64, int64)) (rom.InstallResult, error)
}

func (f *fakeLibrary) Install(ctx context.Context, req rom.InstallRequest, progress func(int64, int64)) (rom.InstallResult, error) {
	f.mu.Lock()
	f.installs = append(f.installs, req.Game.ID)
	f.mu.Unlock()
	return f.install(ctx, req, progress)
}

func (f *fakeLibrary) installed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.installs...)
}

func succeeds(ctx context.Context, req rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
	return rom.InstallResult{Path: "/library/" + req.File.Name, Size: req.File.Size}, nil
}

func blocksUntilCancelled(started chan<- struct{}) func(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
	return func(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return rom.InstallResult{}, ctx.Err()
	}
}

func openStore(t *testing.T) (*sqlite.JobStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, sqlite.Migrate(t.Context(), path))
	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return sqlite.NewJobStore(db), path
}

func storePorts(store *sqlite.JobStore) JobPorts {
	return JobPorts{Producer: store, Worker: store, Maintenance: store}
}

func newTestRunner(t *testing.T, library *fakeLibrary, jobs JobPorts, options ...Option) *Runner {
	t.Helper()
	policy := DefaultPolicy()
	policy.Poll = 10 * time.Millisecond
	policy.ProgressEvery = 10 * time.Millisecond
	policy.Backoff = func(int) time.Duration { return 20 * time.Millisecond }
	runner := NewRunner(library, jobs, policy, options...)
	t.Cleanup(func() { _ = runner.Shutdown(context.Background()) })
	return runner
}

func gameRequest(id string) rom.InstallRequest {
	file := rom.File{Name: id + ".sfc", Size: 100, URL: "http://unused.invalid/" + id}
	game := rom.Game{ID: id, Platform: "snes", Title: "Title " + id, Files: []rom.File{file}}
	platform, _ := rom.PlatformBySlug("snes")
	return rom.InstallRequest{Game: game, File: file, Platform: platform}
}

func waitForJob(t *testing.T, store *sqlite.JobStore, id string, ok func(rom.Job) bool) rom.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, found, err := store.Get(context.Background(), id)
		require.NoError(t, err)
		if found && ok(job) {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never reached the expected state, last = %+v", id, job)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func status(want string) func(rom.Job) bool {
	return func(j rom.Job) bool { return j.Status == want }
}
