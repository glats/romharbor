package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ListPlatforms struct{}

type ListPlatformsHandler decorator.QueryHandler[ListPlatforms, []rom.PlatformCount]

type listPlatformsQuery struct {
	listings adapter.RomListings
}

func NewListPlatformsHandler(listings adapter.RomListings, metric decorator.MetricsClient) ListPlatformsHandler {
	return decorator.ApplyQueryDecorators[ListPlatforms, []rom.PlatformCount](listPlatformsQuery{listings: listings}, metric)
}

func (q listPlatformsQuery) Handle(ctx context.Context, _ ListPlatforms) ([]rom.PlatformCount, error) {
	return q.listings.PlatformCounts(ctx)
}
