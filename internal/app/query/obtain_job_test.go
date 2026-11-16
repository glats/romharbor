package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

func TestObtainJobReadsCurrentOrQueuedSnapshot(t *testing.T) {
	jobs := romjobs.New()
	jobs.Add(rom.Job{ID: "1", Status: "queued", Total: 10})
	jobs.Update("1", func(j *rom.Job) {
		j.Status = "done"
		j.Downloaded = 10
		j.Path = "/library/Game.sfc"
	})
	handler := NewObtainJobHandler(jobs, metric.NoOp{})
	ctx := context.Background()

	current, err := handler.Handle(ctx, ObtainJob{ID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "done", current.Status)

	queued, err := handler.Handle(ctx, ObtainJob{ID: "1", Queued: true})
	require.NoError(t, err)
	assert.Equal(t, "queued", queued.Status)
	assert.Zero(t, queued.Downloaded)
	assert.Empty(t, queued.Path)
	assert.Equal(t, int64(10), queued.Total)

	_, err = handler.Handle(ctx, ObtainJob{ID: "missing"})
	assert.ErrorIs(t, err, rom.ErrJobNotFound)
	_, err = handler.Handle(ctx, ObtainJob{ID: "missing", Queued: true})
	assert.ErrorIs(t, err, rom.ErrJobNotFound)
}
