package romdownloads

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
)

func TestDefaultPolicyKeepsTheValuesTheRunnerUsedBefore(t *testing.T) {
	policy := DefaultPolicy()

	assert.Equal(t, 1, policy.Workers)
	assert.Equal(t, 30*time.Second, policy.Lease)
	assert.Equal(t, time.Second, policy.ProgressEvery)
	assert.Equal(t, 5*time.Second, policy.Poll)
	assert.Equal(t, 3, policy.MaxAttempts)
	assert.Equal(t, 30*24*time.Hour, policy.Retention)
	assert.Equal(t, 6*time.Hour, policy.Timeout)
	assert.Equal(t, 30*time.Second, policy.Backoff(1))
	assert.Equal(t, 2*time.Minute, policy.Backoff(2))
}

func TestThePolicyDecidesHowManyAttemptsAJobGets(t *testing.T) {
	store, _ := openStore(t)
	library := &fakeLibrary{install: func(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
		return rom.InstallResult{}, errors.New("connection reset")
	}}
	policy := DefaultPolicy()
	policy.MaxAttempts = 1
	downloads := NewDownloads(library, storePorts(store), policy)
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })
	require.NoError(t, downloads.Resume(context.Background()))

	job, err := downloads.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)

	failed := waitForJob(t, store, job.ID, status(rom.JobFailed))
	assert.Equal(t, "connection reset", failed.Error)
	assert.Len(t, library.installed(), 1, "the job gets a single attempt")
}

func TestThePolicyDecidesWhenTheFinishedJobsArePruned(t *testing.T) {
	store, _ := openStore(t)
	ctx := context.Background()
	old, err := store.Enqueue(ctx, gameRequest("old"))
	require.NoError(t, err)
	claim, _, err := store.Claim(ctx, time.Hour)
	require.NoError(t, err)
	require.NoError(t, store.Complete(ctx, claim.Lease, rom.InstallResult{Path: "/x", Size: 1}))

	policy := DefaultPolicy()
	policy.Retention = 0
	downloads := NewDownloads(&fakeLibrary{install: succeeds}, storePorts(store), policy)
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })
	require.NoError(t, downloads.Resume(ctx))

	_, found, err := store.Get(ctx, old.ID)
	require.NoError(t, err)
	assert.False(t, found, "a retention of zero prunes the finished jobs")
}

func TestTheDownloadRunnerHasNoExportedFields(t *testing.T) {
	runner := reflect.TypeFor[Downloads]()
	for i := range runner.NumField() {
		field := runner.Field(i)
		if !field.IsExported() {
			continue
		}
		assert.Fail(t, "the download runner keeps an exported field", field.Name)
	}
}
