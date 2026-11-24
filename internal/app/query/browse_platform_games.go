package query

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type BrowsePlatformGames struct {
	Platform string
	Offset   int
	Limit    int
}

type BrowsePlatformGamesHandler decorator.QueryHandler[BrowsePlatformGames, rom.GamePage]

type browsePlatformGamesQuery struct {
	listings adapter.RomListings
}

func NewBrowsePlatformGamesHandler(listings adapter.RomListings, metric decorator.MetricsClient) BrowsePlatformGamesHandler {
	return decorator.ApplyQueryDecorators[BrowsePlatformGames, rom.GamePage](browsePlatformGamesQuery{listings: listings}, metric)
}

func (q browsePlatformGamesQuery) Handle(ctx context.Context, query BrowsePlatformGames) (rom.GamePage, error) {
	listing, err := q.listings.ObtainPlatformListing(ctx, query.Platform)
	if err != nil {
		return rom.GamePage{}, err
	}
	return rom.GamePage{Games: rom.Page(preferCompatible(listing.Games, query.Platform), query.Offset, query.Limit), FailedSources: listing.FailedSources}, nil
}

func preferCompatible(games []rom.Game, slug string) []rom.Game {
	platform, ok := rom.PlatformBySlug(slug)
	if !ok {
		return games
	}
	return rom.PreferCompatible(games, platform)
}
