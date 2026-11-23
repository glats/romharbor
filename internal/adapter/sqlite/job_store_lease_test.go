package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
)

func TestLeaseGuardedWritesRejectAnUnparsableJobID(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()

	assert.ErrorIs(t, store.Heartbeat(ctx, "abc", 1, 1, time.Minute), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Complete(ctx, "abc", rom.InstallResult{Path: "/x", Size: 1}), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Retry(ctx, "abc", errors.New("late"), time.Now()), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Fail(ctx, "abc", errors.New("late")), rom.ErrJobNotFound)
	assert.ErrorIs(t, store.Release(ctx, "abc"), rom.ErrJobNotFound)
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

	assert.ErrorIs(t, store.Heartbeat(ctx, claim.Job.ID, 10, 100, time.Minute), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/lib/a.nes", Size: 10}), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Retry(ctx, claim.Job.ID, errors.New("late"), clk.Now()), rom.ErrLeaseLost)
	assert.ErrorIs(t, store.Release(ctx, claim.Job.ID), rom.ErrLeaseLost)

	got, _, err := store.Get(ctx, claim.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rom.JobQueued, got.Status)
	assert.Empty(t, got.Error)
}
