package romdownloads

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
)

func TestStartQueuesAJobThatTheWorkerCompletes(t *testing.T) {
	store, _ := openStore(t)
	library := &fakeLibrary{install: succeeds}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))

	job, err := downloads.Start(context.Background(), gameRequest("a"))

	require.NoError(t, err)
	assert.Equal(t, "1", job.ID)
	assert.Equal(t, rom.JobQueued, job.Status)
	done := waitForJob(t, store, job.ID, status(rom.JobDone))
	assert.Equal(t, "/library/a.sfc", done.Path)
	assert.False(t, done.Finished.IsZero())
}

func TestStartingAnActiveGameReturnsTheSameJob(t *testing.T) {
	store, _ := openStore(t)
	started := make(chan struct{}, 4)
	library := &fakeLibrary{install: blocksUntilCancelled(started)}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))

	first, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	<-started
	second, err := downloads.Start(context.Background(), gameRequest("a"))

	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, rom.JobRunning, second.Status)
	assert.Equal(t, []string{"a"}, library.installed())
}

func TestJobsRunOneAtATimeInQueueOrder(t *testing.T) {
	store, _ := openStore(t)
	var active, peak atomic.Int32
	library := &fakeLibrary{install: func(ctx context.Context, req rom.InstallRequest, p func(int64, int64)) (rom.InstallResult, error) {
		now := active.Add(1)
		peak.Store(max(peak.Load(), now))
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return succeeds(ctx, req, p)
	}}
	downloads := newTestDownloads(t, library, storePorts(store))
	ids := make([]string, 0, 3)
	for _, id := range []string{"a", "b", "c"} {
		job, err := downloads.Start(context.Background(), gameRequest(id))
		require.NoError(t, err)
		ids = append(ids, job.ID)
	}

	require.NoError(t, downloads.Resume(context.Background()))
	for _, id := range ids {
		waitForJob(t, store, id, status(rom.JobDone))
	}

	assert.EqualValues(t, 1, peak.Load())
	assert.Equal(t, []string{"a", "b", "c"}, library.installed())
}

func TestProgressIsPersistedWhileTheDownloadRuns(t *testing.T) {
	store, _ := openStore(t)
	release := make(chan struct{})
	library := &fakeLibrary{install: func(ctx context.Context, req rom.InstallRequest, progress func(int64, int64)) (rom.InstallResult, error) {
		progress(40, 100)
		<-release
		return succeeds(ctx, req, progress)
	}}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))

	job, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	running := waitForJob(t, store, job.ID, func(j rom.Job) bool { return j.Status == rom.JobRunning && j.Downloaded == 40 })
	assert.EqualValues(t, 100, running.Total)
	close(release)
	waitForJob(t, store, job.ID, status(rom.JobDone))
}

func TestFailedAttemptsRetryThenSucceed(t *testing.T) {
	store, _ := openStore(t)
	var calls atomic.Int32
	library := &fakeLibrary{install: func(ctx context.Context, req rom.InstallRequest, p func(int64, int64)) (rom.InstallResult, error) {
		if calls.Add(1) < 3 {
			return rom.InstallResult{}, errors.New("connection reset")
		}
		return succeeds(ctx, req, p)
	}}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))

	job, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	done := waitForJob(t, store, job.ID, status(rom.JobDone))
	assert.EqualValues(t, 3, calls.Load())
	assert.Empty(t, done.Error)
}

func TestTheJobFailsAfterTheLastAttempt(t *testing.T) {
	store, _ := openStore(t)
	library := &fakeLibrary{install: func(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
		return rom.InstallResult{}, errors.New("checksum mismatch")
	}}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))

	job, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	failed := waitForJob(t, store, job.ID, status(rom.JobFailed))
	assert.Equal(t, "checksum mismatch", failed.Error)
	assert.Len(t, library.installed(), 3)
}

func TestShutdownReleasesTheRunningJobAndRejectsNewOnes(t *testing.T) {
	store, _ := openStore(t)
	started := make(chan struct{}, 1)
	library := &fakeLibrary{install: blocksUntilCancelled(started)}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))
	job, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	<-started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, downloads.Shutdown(shutdownCtx))

	got, _, err := store.Get(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobQueued, got.Status, "an interrupted download is resumable, not failed")
	assert.Empty(t, got.Error)
	_, err = downloads.Start(context.Background(), gameRequest("b"))
	assert.ErrorIs(t, err, rom.ErrShuttingDown)
	jobs, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, jobs, 1, "the rejected start stored nothing")
}

func TestARestartResumesJobsLeftRunning(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	queued, err := store.Enqueue(ctx, gameRequest("a"))
	require.NoError(t, err)
	_, ok, err := store.Claim(ctx, time.Millisecond)
	require.NoError(t, err)
	require.True(t, ok)
	time.Sleep(5 * time.Millisecond)

	library := &fakeLibrary{install: succeeds}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(ctx))

	done := waitForJob(t, store, queued.ID, status(rom.JobDone))
	assert.Equal(t, "/library/a.sfc", done.Path)
	assert.Equal(t, []string{"a"}, library.installed())
}

func TestResumePrunesOldFinishedJobs(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	old, err := store.Enqueue(ctx, gameRequest("old"))
	require.NoError(t, err)
	claim, _, err := store.Claim(ctx, time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.Complete(ctx, claim.Lease, rom.InstallResult{Path: "/x", Size: 1}))

	downloads := newTestDownloads(t, &fakeLibrary{install: succeeds}, storePorts(store), WithClock(func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }))
	require.NoError(t, downloads.Resume(ctx))

	_, found, err := store.Get(ctx, old.ID)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestShutdownGivesUpWhenTheContextExpires(t *testing.T) {
	store, _ := openStore(t)
	started := make(chan struct{}, 1)
	stuck := make(chan struct{})
	library := &fakeLibrary{install: func(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
		started <- struct{}{}
		<-stuck
		return rom.InstallResult{}, errors.New("late")
	}}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(context.Background()))
	_, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = downloads.Shutdown(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	close(stuck)
}

func TestAJobWhoseLeaseOutlivesTheRestartIsRecoveredWhenItExpires(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	orphan, err := store.Enqueue(ctx, gameRequest("a"))
	require.NoError(t, err)
	_, ok, err := store.Claim(ctx, 100*time.Millisecond)
	require.NoError(t, err)
	require.True(t, ok)

	library := &fakeLibrary{install: succeeds}
	downloads := newTestDownloads(t, library, storePorts(store))
	require.NoError(t, downloads.Resume(ctx))

	done := waitForJob(t, store, orphan.ID, status(rom.JobDone))
	assert.Equal(t, "/library/a.sfc", done.Path)
}
