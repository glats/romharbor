package query

import (
	"context"
	"errors"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type SearchGames struct {
	Text     string
	Platform string
	Offset   int
	Limit    int
}

type SearchGamesHandler decorator.QueryHandler[SearchGames, rom.GamePage]

type searchGamesQuery struct {
	listings adapter.RomListings
}

func NewSearchGamesHandler(listings adapter.RomListings, metric decorator.MetricsClient) SearchGamesHandler {
	return decorator.ApplyQueryDecorators[SearchGames, rom.GamePage](searchGamesQuery{listings: listings}, metric)
}

// Handle merges the platform listing with the games the searching sources
// found. The search fails only when neither side produced games and one of
// them failed.
func (q searchGamesQuery) Handle(ctx context.Context, query SearchGames) (rom.GamePage, error) {
	listing, listingErr := q.listings.ObtainPlatformListing(ctx, query.Platform)
	if errors.Is(listingErr, rom.ErrUnknownPlatform) {
		return rom.GamePage{}, listingErr
	}
	found, searchErr := q.listings.SearchSources(ctx, query.Text, query.Platform)
	if listingErr != nil && len(found.Games) == 0 {
		return rom.GamePage{}, listingErr
	}
	if searchErr != nil && len(listing.Games) == 0 {
		return rom.GamePage{}, searchErr
	}
	games := rom.FilterByTitle(rom.MergeGames(listing.Games, found.Games), query.Text)
	failed := append(append([]string(nil), listing.FailedSources...), found.FailedSources...)
	return rom.GamePage{Games: rom.Page(games, query.Offset, query.Limit), FailedSources: failed}, nil
}
