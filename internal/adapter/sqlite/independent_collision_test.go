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

func TestHeartbeatInvalidatesPreviousTokenWithinMillisecond(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "rotation")

	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	renewed, err := store.Heartbeat(ctx, claim.Lease, 1, 10, time.Minute)
	require.NoError(t, err)
	assert.True(t, renewed.Until.After(claim.Lease.Until), "the renewal issues a new token")

	err = store.Complete(ctx, claim.Lease, rom.InstallResult{})
	assert.ErrorIs(t, err, rom.ErrLeaseLost, "the previous token completed the job")
}

func TestReleasedHolderCannotFinishReplacementClaim(t *testing.T) {
	store, _, _ := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "rotation")

	claim, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, store.Release(ctx, claim.Lease))

	replacement, ok, err := store.Claim(ctx, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, replacement.Lease.Until.After(claim.Lease.Until), "the replacement issues a new token")

	err = store.Complete(ctx, claim.Lease, rom.InstallResult{})
	assert.ErrorIs(t, err, rom.ErrLeaseLost, "the released holder completed the replacement job")
}

func TestARowWrittenByTheBaseStaysRecoverableAndClaimable(t *testing.T) {
	store, clk, path := newJobStore(t)
	ctx := context.Background()
	mustEnqueue(t, store, "legacy")
	first, ok, err := store.Claim(ctx, 10*time.Second)
	require.NoError(t, err)
	require.True(t, ok)

	db, err := Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, `UPDATE jobs SET status = 'running', lease_until = ? WHERE id = ?`, first.Lease.Until.UnixMilli(), first.Job.ID)
	require.NoError(t, err)

	clk.Advance(time.Minute)
	recovered, err := store.Recover(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)

	second, ok, err := store.Claim(ctx, 10*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, first.Job.ID, second.Job.ID)
	assert.True(t, second.Lease.Until.After(first.Lease.Until), "the recovered row keeps its lease sequence")

	err = store.Complete(ctx, first.Lease, rom.InstallResult{})
	assert.ErrorIs(t, err, rom.ErrLeaseLost, "the previous holder completed the recovered job")
}

func TestThePreviousTokenCannotWriteAfterANewTokenIsIssued(t *testing.T) {
	writes := []struct {
		name  string
		write func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error
	}{
		{"heartbeat", func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error {
			_, err := store.Heartbeat(ctx, lease, 1, 10, time.Minute)
			return err
		}},
		{"complete", func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error {
			return store.Complete(ctx, lease, rom.InstallResult{})
		}},
		{"retry", func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error {
			return store.Retry(ctx, lease, errors.New("late"), time.Now())
		}},
		{"fail", func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error {
			return store.Fail(ctx, lease, errors.New("late"))
		}},
		{"release", func(ctx context.Context, store *JobStore, lease adapter.RomJobLease) error {
			return store.Release(ctx, lease)
		}},
	}
	scenarios := []struct {
		name  string
		issue func(ctx context.Context, t *testing.T, store *JobStore) adapter.RomJobLease
	}{
		{"after a heartbeat", func(ctx context.Context, t *testing.T, store *JobStore) adapter.RomJobLease {
			claim, ok, err := store.Claim(ctx, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			renewed, err := store.Heartbeat(ctx, claim.Lease, 1, 10, time.Minute)
			require.NoError(t, err)
			require.True(t, renewed.Until.After(claim.Lease.Until))
			return claim.Lease
		}},
		{"after a release and a new claim", func(ctx context.Context, t *testing.T, store *JobStore) adapter.RomJobLease {
			claim, ok, err := store.Claim(ctx, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			require.NoError(t, store.Release(ctx, claim.Lease))
			replacement, ok, err := store.Claim(ctx, time.Minute)
			require.NoError(t, err)
			require.True(t, ok)
			require.True(t, replacement.Lease.Until.After(claim.Lease.Until))
			return claim.Lease
		}},
	}

	for _, scenario := range scenarios {
		for _, write := range writes {
			t.Run(scenario.name+"/"+write.name, func(t *testing.T) {
				store, _, _ := newJobStore(t)
				ctx := context.Background()
				mustEnqueue(t, store, "rotation")

				previous := scenario.issue(ctx, t, store)

				err := write.write(ctx, store, previous)
				assert.ErrorIs(t, err, rom.ErrLeaseLost, "the previous token wrote the job")
			})
		}
	}
}
