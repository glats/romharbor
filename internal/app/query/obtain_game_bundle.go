package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainGameBundle struct {
	ID string
}

type obtainGameBundleQuery struct {
	jobs      obtainJobQuery
	inspector adapter.RomGameInspector
}

func NewObtainGameBundleHandler(jobs adapter.RomJobStore, inspector adapter.RomGameInspector, metrics decorator.MetricsClient) decorator.QueryHandler[ObtainGameBundle, rom.GameBundle] {
	return decorator.ApplyQueryDecorators[ObtainGameBundle, rom.GameBundle](obtainGameBundleQuery{
		jobs: obtainJobQuery{jobs: jobs}, inspector: inspector,
	}, metrics)
}

func (q obtainGameBundleQuery) Handle(ctx context.Context, input ObtainGameBundle) (rom.GameBundle, error) {
	job, err := q.jobs.Handle(ctx, ObtainJob(input))
	if err != nil {
		return rom.GameBundle{}, err
	}
	return q.inspector.PrepareGameBundle(ctx, job)
}
