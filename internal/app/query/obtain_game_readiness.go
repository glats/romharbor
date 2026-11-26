package query

import (
	"context"
	"strings"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainGameReadiness struct {
	ID string
}

type obtainGameReadinessQuery struct {
	jobs      obtainJobQuery
	inspector adapter.RomGameInspector
}

func NewObtainGameReadinessHandler(jobs adapter.RomJobStore, inspector adapter.RomGameInspector, metrics decorator.MetricsClient) decorator.QueryHandler[ObtainGameReadiness, rom.GameReadiness] {
	return decorator.ApplyQueryDecorators[ObtainGameReadiness, rom.GameReadiness](obtainGameReadinessQuery{
		jobs: obtainJobQuery{jobs: jobs}, inspector: inspector,
	}, metrics)
}

func (q obtainGameReadinessQuery) Handle(ctx context.Context, input ObtainGameReadiness) (rom.GameReadiness, error) {
	job, err := q.jobs.Handle(ctx, ObtainJob(input))
	if err != nil {
		return rom.GameReadiness{}, err
	}
	report := q.inspector.InspectGame(ctx, job)
	if _, err := q.inspector.PrepareGameBundle(ctx, job); err == nil {
		report.BundleReady = true
	}
	if strings.HasPrefix(job.Path, "psx/") {
		report.Compatibility = rom.PlayStationCompatibility(report)
	}
	return report, nil
}
