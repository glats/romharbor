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

func TestListJobsReturnsEveryStoredJob(t *testing.T) {
	jobs := romjobs.New()
	jobs.Add(rom.Job{ID: "1", Status: "queued"})
	jobs.Add(rom.Job{ID: "2", Status: "done"})

	got, err := NewListJobsHandler(jobs, metric.NoOp{}).Handle(context.Background(), ListJobs{})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"1", "2"}, []string{got[0].ID, got[1].ID})
}
