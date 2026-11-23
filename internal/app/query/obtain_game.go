package query

import (
	"context"
	"fmt"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type ObtainGame struct {
	ID string
}

type ObtainGameHandler decorator.QueryHandler[ObtainGame, rom.Game]

type obtainGameQuery struct {
	listings adapter.RomListings
}

func NewObtainGameHandler(listings adapter.RomListings, metric decorator.MetricsClient) ObtainGameHandler {
	return decorator.ApplyQueryDecorators[ObtainGame, rom.Game](obtainGameQuery{listings: listings}, metric)
}

func (q obtainGameQuery) Handle(ctx context.Context, query ObtainGame) (rom.Game, error) {
	game, ok, err := q.listings.Game(ctx, query.ID)
	if err != nil {
		return rom.Game{}, fmt.Errorf("look up game %q: %w", query.ID, err)
	}
	if !ok {
		return rom.Game{}, rom.ErrGameNotFound
	}
	return game, nil
}
