package query

import (
	"context"

	"app.ddd/internal/app/catalog"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ListGames struct {
	Platform string
	Text     string
	Offset   int
	Limit    int
}

type ListGamesHandler decorator.QueryHandler[ListGames, []rom.Game]

type listGamesQuery struct {
	listings *catalog.Listings
}

func NewListGamesHandler(listings *catalog.Listings, metric decorator.MetricsClient) ListGamesHandler {
	return decorator.ApplyQueryDecorators[ListGames, []rom.Game](listGamesQuery{listings: listings}, metric)
}

func (q listGamesQuery) Handle(ctx context.Context, query ListGames) ([]rom.Game, error) {
	return q.listings.Games(ctx, query.Platform, query.Text, query.Offset, query.Limit)
}
