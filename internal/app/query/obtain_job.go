package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainJob struct {
	ID     string
	Queued bool
}

type ObtainJobHandler decorator.QueryHandler[ObtainJob, rom.Job]

type obtainJobQuery struct {
	jobs adapter.RomJobStore
}

func NewObtainJobHandler(jobs adapter.RomJobStore, metric decorator.MetricsClient) ObtainJobHandler {
	return decorator.ApplyQueryDecorators[ObtainJob, rom.Job](obtainJobQuery{jobs: jobs}, metric)
}

func (q obtainJobQuery) Handle(_ context.Context, query ObtainJob) (rom.Job, error) {
	find := q.jobs.Get
	if query.Queued {
		find = q.jobs.GetQueued
	}
	job, ok := find(query.ID)
	if !ok {
		return rom.Job{}, rom.ErrJobNotFound
	}
	return job, nil
}
