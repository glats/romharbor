package query

import (
	"context"

	"app.ddd/internal/app/catalog"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainGame struct {
	ID string
}

type ObtainGameHandler decorator.QueryHandler[ObtainGame, rom.Game]

type obtainGameQuery struct {
	listings *catalog.Listings
}

func NewObtainGameHandler(listings *catalog.Listings, metric decorator.MetricsClient) ObtainGameHandler {
	return decorator.ApplyQueryDecorators[ObtainGame, rom.Game](obtainGameQuery{listings: listings}, metric)
}

func (q obtainGameQuery) Handle(ctx context.Context, query ObtainGame) (rom.Game, error) {
	game, ok, err := q.listings.Game(ctx, query.ID)
	if err != nil {
		return rom.Game{}, err
	}
	if !ok {
		return rom.Game{}, rom.ErrGameNotFound
	}
	return game, nil
}
