package romdownloads

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
)

func TestTheEnqueuerQueuesTheJobWithoutRunningIt(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))

	require.NoError(t, err)
	assert.Equal(t, "1", job.ID)
	assert.Equal(t, rom.JobQueued, job.Status)
	assert.Equal(t, "Title a", job.Title)
	assert.Equal(t, int64(100), job.Total)

	time.Sleep(100 * time.Millisecond)
	queued, found, err := store.Get(context.Background(), job.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, rom.JobQueued, queued.Status, "the web server does not run the download")
	assert.Zero(t, queued.Downloaded)
}

func TestTheEnqueuerRefusesToQueueAfterItsShutdown(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)
	require.NoError(t, enqueuer.Shutdown(context.Background()))

	job, err := enqueuer.Start(context.Background(), gameRequest("a"))

	require.ErrorIs(t, err, rom.ErrShuttingDown)
	assert.Empty(t, job)
	jobs, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, jobs, "the refused download stored nothing")
}

func TestTheEnqueuerKeepsTheJobOfAnActiveGame(t *testing.T) {
	store, _ := openStore(t)
	enqueuer := NewEnqueuer(store)

	first, err := enqueuer.Start(context.Background(), gameRequest("a"))
	require.NoError(t, err)
	second, err := enqueuer.Start(context.Background(), gameRequest("a"))

	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
}
