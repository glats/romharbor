package romdownloads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/sqlite"
	"app.ddd/internal/domain/rom"
)

type fakeQueue struct {
	*sqlite.JobStore
	mu           sync.Mutex
	heartbeatErr error
	completeErr  error
	heartbeats   int
	completes    int
	retries      int
	fails        int
	releases     int
}

func (q *fakeQueue) Heartbeat(ctx context.Context, lease adapter.RomJobLease, downloaded, total int64, duration time.Duration) (adapter.RomJobLease, error) {
	q.mu.Lock()
	q.heartbeats++
	err := q.heartbeatErr
	q.mu.Unlock()
	if err != nil {
		return adapter.RomJobLease{}, err
	}
	return q.JobStore.Heartbeat(ctx, lease, downloaded, total, duration)
}

func (q *fakeQueue) Complete(ctx context.Context, lease adapter.RomJobLease, result rom.InstallResult) error {
	q.mu.Lock()
	q.completes++
	err := q.completeErr
	q.mu.Unlock()
	if err != nil {
		return err
	}
	return q.JobStore.Complete(ctx, lease, result)
}

func (q *fakeQueue) Retry(ctx context.Context, lease adapter.RomJobLease, cause error, runAt time.Time) error {
	q.mu.Lock()
	q.retries++
	q.mu.Unlock()
	return q.JobStore.Retry(ctx, lease, cause, runAt)
}

func (q *fakeQueue) Fail(ctx context.Context, lease adapter.RomJobLease, cause error) error {
	q.mu.Lock()
	q.fails++
	q.mu.Unlock()
	return q.JobStore.Fail(ctx, lease, cause)
}

func (q *fakeQueue) Release(ctx context.Context, lease adapter.RomJobLease) error {
	q.mu.Lock()
	q.releases++
	q.mu.Unlock()
	return q.JobStore.Release(ctx, lease)
}

func fakePorts(queue *fakeQueue) JobPorts {
	return JobPorts{Producer: queue, Worker: queue, Maintenance: queue}
}

func (q *fakeQueue) counts() (heartbeats, completes, retries, fails, releases int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.heartbeats, q.completes, q.retries, q.fails, q.releases
}

type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *logCapture) Handle(_ context.Context, record slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, record.Clone())
	return nil
}

func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler { return c }

func (c *logCapture) WithGroup(string) slog.Handler { return c }

func (c *logCapture) logger() *slog.Logger { return slog.New(c) }

func (c *logCapture) at(level slog.Level) []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, record := range c.records {
		if record.Level == level {
			out = append(out, record)
		}
	}
	return out
}

func TestALostLeaseCancelsTheInstallAndSkipsTheOutcome(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)
	queue := &fakeQueue{JobStore: store, heartbeatErr: fmt.Errorf("update job 1: %w", rom.ErrLeaseLost)}
	logs := &logCapture{}
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{})
	library := &fakeLibrary{install: func(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		close(cancelled)
		return rom.InstallResult{}, ctx.Err()
	}}
	runner := newTestRunner(t, library, fakePorts(queue), WithLogger(logs.logger()))
	require.NoError(t, runner.Resume(context.Background()))

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	<-started

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the install kept running after the lease was lost")
	}
	require.NoError(t, runner.Shutdown(context.Background()))

	heartbeats, completes, retries, fails, releases := queue.counts()
	assert.Positive(t, heartbeats, "the heartbeat that reported the lost lease was sent")
	assert.Zero(t, completes, "a job with a lost lease is not completed")
	assert.Zero(t, retries, "a job with a lost lease is not retried")
	assert.Zero(t, fails, "a job with a lost lease is not failed")
	assert.Zero(t, releases, "a job with a lost lease is not released")
	warnings := logs.at(slog.LevelWarn)
	require.Len(t, warnings, 1, "the lost lease is logged once")
	assert.Contains(t, warnings[0].Message, "lease")
	assert.Empty(t, logs.at(slog.LevelError))
	stored, _, err := store.Get(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobRunning, stored.Status, "the job keeps running until its lease expires")
}

func TestALostLeaseOnCompleteIsLoggedAndStopsTheWrites(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)
	queue := &fakeQueue{JobStore: store, completeErr: fmt.Errorf("update job 1: %w", rom.ErrLeaseLost)}
	logs := &logCapture{}
	runner := newTestRunner(t, &fakeLibrary{install: succeeds}, fakePorts(queue), WithLogger(logs.logger()))
	require.NoError(t, runner.Resume(context.Background()))

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	require.NotEmpty(t, job.ID)

	require.Eventually(t, func() bool {
		_, completes, _, _, _ := queue.counts()
		return completes == 1
	}, 5*time.Second, 5*time.Millisecond)
	require.NoError(t, runner.Shutdown(context.Background()))

	_, completes, retries, fails, releases := queue.counts()
	assert.Equal(t, 1, completes)
	assert.Zero(t, retries, "a lost lease stops the writes")
	assert.Zero(t, fails, "a lost lease stops the writes")
	assert.Zero(t, releases, "a lost lease stops the writes")
	warnings := logs.at(slog.LevelWarn)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "lease")
	assert.Empty(t, logs.at(slog.LevelError), "a lost lease is not a failed outcome")
}

func TestANotRetryableFailureFailsTheJobOnTheFirstAttempt(t *testing.T) {
	store, path := openStore(t)
	enqueuer := NewEnqueuer(store)
	queue := &fakeQueue{JobStore: store}
	library := &fakeLibrary{install: func(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
		return rom.InstallResult{}, fmt.Errorf("checksum mismatch: %w", rom.ErrNotRetryable)
	}}
	runner := newTestRunner(t, library, fakePorts(queue))
	require.NoError(t, runner.Resume(context.Background()))

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	failed := waitForJob(t, store, job.ID, status(rom.JobFailed))
	assert.Contains(t, failed.Error, "checksum mismatch")
	_, completes, retries, fails, _ := queue.counts()
	assert.Zero(t, completes)
	assert.Zero(t, retries, "a permanent failure is not retried")
	assert.Equal(t, 1, fails, "a permanent failure fails the job once")
	state, attempts, _ := storedAttempts(t, path, job.ID)
	assert.Equal(t, rom.JobFailed, state)
	assert.Equal(t, 1, attempts, "the permanent failure ends the first attempt")
}

func TestAFailedHeartbeatKeepsTheInstallRunning(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)
	queue := &fakeQueue{JobStore: store, heartbeatErr: errors.New("database is busy")}
	logs := &logCapture{}
	library := &fakeLibrary{install: func(ctx context.Context, req rom.InstallRequest, p func(int64, int64)) (rom.InstallResult, error) {
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return rom.InstallResult{}, ctx.Err()
		}
		return succeeds(ctx, req, p)
	}}
	runner := newTestRunner(t, library, fakePorts(queue), WithLogger(logs.logger()))
	require.NoError(t, runner.Resume(context.Background()))

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	done := waitForJob(t, store, job.ID, status(rom.JobDone))
	assert.Equal(t, "/library/a.sfc", done.Path)
	heartbeats, _, _, _, _ := queue.counts()
	assert.Positive(t, heartbeats, "the heartbeat that failed was sent")
	assert.NotEmpty(t, logs.at(slog.LevelWarn), "the failed heartbeat is logged")
}
