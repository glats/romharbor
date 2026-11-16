package query

import (
	"context"

	"app.ddd/internal/app/catalog"
	"app.ddd/internal/common/decorator"
)

type ListPlatforms struct{}

type ListPlatformsHandler decorator.QueryHandler[ListPlatforms, []catalog.PlatformCount]

type listPlatformsQuery struct {
	listings *catalog.Listings
}

func NewListPlatformsHandler(listings *catalog.Listings, metric decorator.MetricsClient) ListPlatformsHandler {
	return decorator.ApplyQueryDecorators[ListPlatforms, []catalog.PlatformCount](listPlatformsQuery{listings: listings}, metric)
}

func (q listPlatformsQuery) Handle(ctx context.Context, _ ListPlatforms) ([]catalog.PlatformCount, error) {
	return q.listings.PlatformCounts(ctx), nil
}
