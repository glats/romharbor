package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

var (
	_ adapter.RomJobStore = (*JobStore)(nil)
	_ adapter.RomJobQueue = (*JobStore)(nil)
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newJobStore(t *testing.T) (*JobStore, *clock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, Migrate(path))
	db, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	clk := &clock{now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.Local)}
	store := NewJobStore(db)
	store.Now = clk.Now
	return store, clk, path
}

func installRequest(id string) rom.InstallRequest {
	file := rom.File{Name: id + ".zip", Size: 1000, MD5: "abc", URL: "https://files.invalid/" + id + ".zip", Archive: true}
	game := rom.Game{ID: id, Source: "fixture", Platform: "nes", Title: "Title " + id, Files: []rom.File{file}}
	platform, _ := rom.PlatformBySlug("nes")
	return rom.InstallRequest{Game: game, File: file, Platform: platform}
}

func mustEnqueue(t *testing.T, store *JobStore, id string) rom.Job {
	t.Helper()
	job, err := store.Enqueue(context.Background(), installRequest(id))
	require.NoError(t, err)
	return job
}

func TestEnqueueStoresAQueuedJobWithAStoreGeneratedID(t *testing.T) {
	store, clk, _ := newJobStore(t)

	first := mustEnqueue(t, store, "a")
	second := mustEnqueue(t, store, "b")

	assert.Equal(t, rom.Job{ID: "1", GameID: "a", Title: "Title a", Status: rom.JobQueued, Total: 1000, Started: clk.Now()}, first)
	assert.Equal(t, "2", second.ID)
	got, ok, err := store.Get(context.Background(), "1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, first, got)
}

func TestEnqueueReturnsTheActiveJobOfTheSameGame(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()
	first := mustEnqueue(t, store, "a")

	again := mustEnqueue(t, store, "a")
	assert.Equal(t, first.ID, again.ID)

	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	running := mustEnqueue(t, store, "a")
	assert.Equal(t, claim.Job.ID, running.ID)
	assert.Equal(t, rom.JobRunning, running.Status)

	require.NoError(t, store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/lib/a.nes", Size: 10}))
	next := mustEnqueue(t, store, "a")
	assert.NotEqual(t, first.ID, next.ID)
	assert.Equal(t, rom.JobQueued, next.Status)
	jobs, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, jobs, 2)
}

func TestClaimTakesTheOldestDueJobAndReturnsItsRequest(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	_, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "empty queue")
	mustEnqueue(t, store, "a")
	mustEnqueue(t, store, "b")

	claim, ok, err := store.Claim(ctx, time.Minute)

	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "1", claim.Job.ID)
	assert.Equal(t, rom.JobRunning, claim.Job.Status)
	assert.Equal(t, 1, claim.Attempt)
	assert.Equal(t, installRequest("a"), claim.Request)
	assert.Equal(t, clk.Now(), claim.Job.Started)
}

func TestClaimSkipsJobsScheduledInTheFuture(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, _, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.Retry(ctx, claim.Job.ID, errors.New("boom"), clk.Now().Add(time.Minute)))

	_, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	assert.False(t, ok, "retry is not due yet")

	clk.Advance(time.Minute)
	retried, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 2, retried.Attempt)
	assert.Empty(t, retried.Job.Error, "the new attempt clears the previous error")
}

func TestConcurrentClaimsNeverShareAJob(t *testing.T) {
	store, _, _ := newJobStore(t)
	const jobs = 8
	for i := range jobs {
		mustEnqueue(t, store, string(rune('a'+i)))
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[string]int{}
	)
	for range 24 {
		wg.Go(func() {
			claim, ok, err := store.Claim(context.Background(), time.Minute)
			assert.NoError(t, err)
			if ok {
				mu.Lock()
				claimed[claim.Job.ID]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	assert.Len(t, claimed, jobs)
	for id, n := range claimed {
		assert.Equal(t, 1, n, "job %s claimed %d times", id, n)
	}
}

func TestHeartbeatRecordsProgressAndRenewsTheLease(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, _, err := store.Claim(ctx, 10*time.Second)
	require.NoError(t, err)

	clk.Advance(8 * time.Second)
	require.NoError(t, store.Heartbeat(ctx, claim.Job.ID, 400, 0, 10*time.Second))
	clk.Advance(8 * time.Second)

	recovered, err := store.Recover(ctx)
	require.NoError(t, err)
	assert.Zero(t, recovered, "the renewed lease is still valid")
	got, _, err := store.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 400, got.Downloaded)
	assert.EqualValues(t, 1000, got.Total, "a zero total keeps the known size")

	require.NoError(t, store.Heartbeat(ctx, claim.Job.ID, 600, 2000, 10*time.Second))
	got, _, _ = store.Get(ctx, claim.Job.ID)
	assert.EqualValues(t, 2000, got.Total)
}

func TestOutcomesCloseTheJob(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	for _, id := range []string{"done", "failed", "retry", "release"} {
		mustEnqueue(t, store, id)
	}
	claims := map[string]string{}
	for range 4 {
		claim, ok, err := store.Claim(ctx, time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		claims[claim.Job.GameID] = claim.Job.ID
	}
	clk.Advance(time.Second)

	require.NoError(t, store.Complete(ctx, claims["done"], rom.InstallResult{Path: "/lib/done.nes", Size: 4096}))
	require.NoError(t, store.Fail(ctx, claims["failed"], errors.New("checksum mismatch")))
	require.NoError(t, store.Retry(ctx, claims["retry"], errors.New("timeout"), clk.Now().Add(time.Hour)))
	require.NoError(t, store.Release(ctx, claims["release"]))

	done, _, _ := store.Get(ctx, claims["done"])
	assert.Equal(t, rom.JobDone, done.Status)
	assert.Equal(t, "/lib/done.nes", done.Path)
	assert.EqualValues(t, 4096, done.Total)
	assert.Equal(t, clk.Now(), done.Finished)
	failed, _, _ := store.Get(ctx, claims["failed"])
	assert.Equal(t, rom.JobFailed, failed.Status)
	assert.Equal(t, "checksum mismatch", failed.Error)
	assert.Equal(t, clk.Now(), failed.Finished)
	retry, _, _ := store.Get(ctx, claims["retry"])
	assert.Equal(t, rom.JobQueued, retry.Status)
	assert.Equal(t, "timeout", retry.Error)
	released, _, _ := store.Get(ctx, claims["release"])
	assert.Equal(t, rom.JobQueued, released.Status)

	again, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "the released job is immediately claimable")
	assert.Equal(t, claims["release"], again.Job.ID)
	assert.Equal(t, 1, again.Attempt, "a release does not count as an attempt")
}

func TestFinishedJobsRejectLateUpdatesWithALostLease(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, _, _ := store.Claim(ctx, time.Minute)
	require.NoError(t, store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/lib/a.nes", Size: 10}))

	assert.ErrorIs(t, store.Heartbeat(ctx, claim.Job.ID, 1, 1, time.Minute), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/lib/b.nes", Size: 20}), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Retry(ctx, claim.Job.ID, errors.New("late"), time.Now()), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Fail(ctx, claim.Job.ID, errors.New("late")), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Release(ctx, claim.Job.ID), rom.ErrLeaseLost)

	got, _, _ := store.Get(ctx, claim.Job.ID)
	assert.Equal(t, rom.JobDone, got.Status)
	assert.Empty(t, got.Error)
	assert.EqualValues(t, 10, got.Total)
	assert.Equal(t, "/lib/a.nes", got.Path)
}

func TestRecoverRequeuesExpiredLeasesOnly(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "stale")
	mustEnqueue(t, store, "live")
	stale, _, _ := store.Claim(ctx, 10*time.Second)
	live, _, _ := store.Claim(ctx, time.Hour)

	clk.Advance(time.Minute)
	recovered, err := store.Recover(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	got, _, _ := store.Get(ctx, stale.Job.ID)
	assert.Equal(t, rom.JobQueued, got.Status)
	other, _, _ := store.Get(ctx, live.Job.ID)
	assert.Equal(t, rom.JobRunning, other.Status)
	again, ok, _ := store.Claim(ctx, time.Minute)
	require.True(t, ok)
	assert.Equal(t, stale.Job.ID, again.Job.ID)
	assert.Equal(t, 2, again.Attempt)
}

func TestJobsSurviveReopeningTheDatabase(t *testing.T) {
	store, clk, path := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, _, _ := store.Claim(ctx, 10*time.Second)
	require.NoError(t, store.Heartbeat(ctx, claim.Job.ID, 300, 1000, 10*time.Second))

	db, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	reopened := NewJobStore(db)
	clk.Advance(time.Minute)
	reopened.Now = clk.Now
	recovered, err := reopened.Recover(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	got, ok, err := reopened.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, rom.JobQueued, got.Status)
	assert.EqualValues(t, 300, got.Downloaded)
	next := mustEnqueue(t, reopened, "b")
	assert.Equal(t, "2", next.ID, "IDs keep increasing after a restart")
}

func TestPruneDropsOldFinishedJobsOnly(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	for _, id := range []string{"old-done", "old-failed", "recent", "queued"} {
		mustEnqueue(t, store, id)
	}
	claim := func() rom.JobClaim {
		c, ok, err := store.Claim(ctx, time.Hour)
		require.NoError(t, err)
		require.True(t, ok)
		return c
	}
	oldDone, oldFailed, recent := claim(), claim(), claim()
	require.NoError(t, store.Complete(ctx, oldDone.Job.ID, rom.InstallResult{Path: "/x", Size: 1}))
	require.NoError(t, store.Fail(ctx, oldFailed.Job.ID, errors.New("x")))
	clk.Advance(31 * 24 * time.Hour)
	require.NoError(t, store.Complete(ctx, recent.Job.ID, rom.InstallResult{Path: "/y", Size: 1}))

	pruned, err := store.Prune(ctx, clk.Now().Add(-30*24*time.Hour))

	require.NoError(t, err)
	assert.Equal(t, 2, pruned)
	jobs, err := store.List(ctx)
	require.NoError(t, err)
	var games []string
	for _, job := range jobs {
		games = append(games, job.GameID)
	}
	assert.Equal(t, []string{"queued", "recent"}, games, "newest first")
}

func TestGetRejectsUnknownAndMalformedIDs(t *testing.T) {
	store, _, _ := newJobStore(t)
	for _, id := range []string{"", "0", "99", "abc", "1; DROP TABLE jobs"} {
		_, ok, err := store.Get(context.Background(), id)
		require.NoError(t, err, id)
		assert.False(t, ok, id)
	}
}

func TestJobTimestampsKeepNanosecondsAndTheLocalZone(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	clk.now = time.Date(2026, 11, 16, 12, 0, 0, 123456789, time.Local)
	queued := mustEnqueue(t, store, "a")
	claim, _, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	clk.Advance(1500 * time.Millisecond)
	require.NoError(t, store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/x", Size: 1}))

	got, _, err := store.Get(ctx, queued.ID)
	require.NoError(t, err)

	assert.Equal(t, "2026-11-16T12:00:00.123456789", got.Started.Format("2006-01-02T15:04:05.999999999"))
	assert.Equal(t, got.Started.Format(time.RFC3339Nano), queued.Started.Format(time.RFC3339Nano))
	assert.Equal(t, "2026-11-16T12:00:01.623456789", got.Finished.Format("2006-01-02T15:04:05.999999999"))
	assert.Equal(t, time.Local, got.Started.Location())
}
