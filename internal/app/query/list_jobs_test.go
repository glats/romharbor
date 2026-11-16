package query

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

func TestListJobsReturnsEveryStoredJob(t *testing.T) {
	jobs := fakeJobs{jobs: []rom.Job{{ID: "2", Status: rom.JobDone}, {ID: "1", Status: rom.JobQueued}}}

	got, err := NewListJobsHandler(jobs, metric.NoOp{}).Handle(context.Background(), ListJobs{})

	require.NoError(t, err)
	assert.Equal(t, jobs.jobs, got)
}
