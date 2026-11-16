package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ListJobs struct{}

type ListJobsHandler decorator.QueryHandler[ListJobs, []rom.Job]

type listJobsQuery struct {
	jobs adapter.RomJobStore
}

func NewListJobsHandler(jobs adapter.RomJobStore, metric decorator.MetricsClient) ListJobsHandler {
	return decorator.ApplyQueryDecorators[ListJobs, []rom.Job](listJobsQuery{jobs: jobs}, metric)
}

func (q listJobsQuery) Handle(_ context.Context, _ ListJobs) ([]rom.Job, error) {
	return q.jobs.List(), nil
}
