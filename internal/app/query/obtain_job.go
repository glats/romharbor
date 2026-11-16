package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainJob struct {
	ID string
}

type ObtainJobHandler decorator.QueryHandler[ObtainJob, rom.Job]

type obtainJobQuery struct {
	jobs adapter.RomJobStore
}

func NewObtainJobHandler(jobs adapter.RomJobStore, metric decorator.MetricsClient) ObtainJobHandler {
	return decorator.ApplyQueryDecorators[ObtainJob, rom.Job](obtainJobQuery{jobs: jobs}, metric)
}

func (q obtainJobQuery) Handle(ctx context.Context, query ObtainJob) (rom.Job, error) {
	job, ok, err := q.jobs.Get(ctx, query.ID)
	if err != nil {
		return rom.Job{}, err
	}
	if !ok {
		return rom.Job{}, rom.ErrJobNotFound
	}
	return job, nil
}
