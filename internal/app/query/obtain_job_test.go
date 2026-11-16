package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

func TestObtainJobReadsTheStoredJob(t *testing.T) {
	stored := rom.Job{ID: "1", Status: rom.JobDone, Total: 10, Downloaded: 10, Path: "/library/Game.sfc"}
	handler := NewObtainJobHandler(fakeJobs{jobs: []rom.Job{stored}}, metric.NoOp{})
	ctx := context.Background()

	got, err := handler.Handle(ctx, ObtainJob{ID: "1"})
	require.NoError(t, err)
	assert.Equal(t, stored, got)

	_, err = handler.Handle(ctx, ObtainJob{ID: "missing"})
	assert.ErrorIs(t, err, rom.ErrJobNotFound)
}
