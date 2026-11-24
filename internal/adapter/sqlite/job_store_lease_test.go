package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

func TestLeaseGuardedWritesRejectAnUnparsableJobID(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()
	lease := adapter.RomJobLease{JobID: "abc", Until: time.Now()}

	_, err := store.Heartbeat(ctx, lease, 1, 1, time.Minute)
	assert.ErrorIs(t, err, rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Complete(ctx, lease, rom.InstallResult{Path: "/x", Size: 1}), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Retry(ctx, lease, errors.New("late"), time.Now()), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Fail(ctx, lease, errors.New("late")), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Release(ctx, lease), rom.ErrJobNotFound)
}

func TestTheHolderOfAJobClaimedByAnotherWorkerLosesItsWrites(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	first, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	clk.Advance(2 * time.Minute)
	recovered, err := store.Recover(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)

	second, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.Job.ID, second.Job.ID)

	_, err = store.Heartbeat(ctx, first.Lease, 10, 100, time.Minute)
	assert.ErrorIs(t, err, rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Complete(ctx, first.Lease, rom.InstallResult{Path: "/lib/a.nes", Size: 10}), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Retry(ctx, first.Lease, errors.New("late"), clk.Now()), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Fail(ctx, first.Lease, errors.New("late")), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Release(ctx, first.Lease), rom.ErrLeaseLost)

	got, _, err := store.Get(ctx, first.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobRunning, got.Status, "the worker that claimed the job keeps it")

	require.NoError(t, store.Complete(ctx, second.Lease, rom.InstallResult{Path: "/lib/a.nes", Size: 10}))
	got, _, err = store.Get(ctx, second.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobDone, got.Status)
}

func TestAHolderWhoseLeaseExpiredCannotFinishTheJob(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	clk.Advance(2 * time.Minute)

	_, err = store.Heartbeat(ctx, claim.Lease, 10, 100, time.Minute)
	assert.ErrorIs(t, err, rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Complete(ctx, claim.Lease, rom.InstallResult{Path: "/lib/a.nes", Size: 10}), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Retry(ctx, claim.Lease, errors.New("late"), clk.Now()), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Fail(ctx, claim.Lease, errors.New("late")), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Release(ctx, claim.Lease), rom.ErrLeaseLost)

	got, _, err := store.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobRunning, got.Status, "the job keeps the state of its holder")
}

func TestTheHolderOfARecoveredJobLosesItsWrites(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	clk.Advance(2 * time.Minute)
	recovered, err := store.Recover(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)

	_, err = store.Heartbeat(ctx, claim.Lease, 10, 100, time.Minute)
	assert.ErrorIs(t, err, rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Complete(ctx, claim.Lease, rom.InstallResult{Path: "/lib/a.nes", Size: 10}), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Retry(ctx, claim.Lease, errors.New("late"), clk.Now()), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Release(ctx, claim.Lease), rom.ErrLeaseLost)

	got, _, err := store.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobQueued, got.Status)
	assert.Empty(t, got.Error)
}

func TestTheHolderRenewsItsLeaseWithEveryHeartbeat(t *testing.T) {
	store, clk, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "a")
	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	clk.Advance(time.Second)
	renewed, err := store.Heartbeat(ctx, claim.Lease, 10, 100, time.Minute)
	require.NoError(t, err)
	assert.NotEqual(t, claim.Lease.Until, renewed.Until, "the renewal replaces the lease")
	assert.Equal(t, claim.Job.ID, renewed.JobID)

	_, err = store.Heartbeat(ctx, claim.Lease, 20, 100, time.Minute)
	assert.ErrorIs(t, err, rom.ErrLeaseLost, "the replaced lease does not write any more")

	require.NoError(t, store.Complete(ctx, renewed, rom.InstallResult{Path: "/lib/a.nes", Size: 10}))
	got, _, err := store.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobDone, got.Status)
}
