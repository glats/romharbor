package query

import (
	"context"
	"errors"
	"fmt"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

// SearchAllGames asks every platform for the games that match a text.
type SearchAllGames struct {
	// Text is the free text search.
	Text string
	// Limit caps the games of every platform.
	Limit int
}

// SearchAllGamesHandler answers a global search.
type SearchAllGamesHandler decorator.QueryHandler[SearchAllGames, SearchAllGamesResult]

// PlatformGames holds the games a global search found for one platform.
type PlatformGames struct {
	// Platform is the system the games belong to.
	Platform rom.Platform
	// Games are the games that matched the text.
	Games []rom.Game
	// FailedSources are the names of the sources that did not answer.
	FailedSources []string
}

// SearchAllGamesResult groups the games a global search found by platform.
type SearchAllGamesResult struct {
	// Platforms holds one group per platform that found a game, in the order
	// of the catalog.
	Platforms []PlatformGames
	// FailedPlatforms holds the platforms whose listing could not be obtained.
	FailedPlatforms []string
}

type searchAllGamesQuery struct {
	listings adapter.RomListings
}

// NewSearchAllGamesHandler returns the handler of a global search.
func NewSearchAllGamesHandler(listings adapter.RomListings, metric decorator.MetricsClient) SearchAllGamesHandler {
	return decorator.ApplyQueryDecorators[SearchAllGames, SearchAllGamesResult](searchAllGamesQuery{listings: listings}, metric)
}

// Handle searches every platform and groups the matches by platform.
//
// The search reads the listing of every platform and keeps the games whose
// title holds the text, ignoring the case. A platform that fails does not
// stop the search: it is reported apart, with the sources that failed, the
// way SearchGames reports them. The search fails only when no game was found
// and a platform failed.
func (q searchAllGamesQuery) Handle(ctx context.Context, query SearchAllGames) (SearchAllGamesResult, error) {
	result := SearchAllGamesResult{Platforms: []PlatformGames{}}
	if query.Text == "" {
		return result, nil
	}
	var failures []error
	found := 0
	for _, platform := range rom.Platforms() {
		page, err := q.listings.ObtainPlatformListing(ctx, platform.Slug)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", platform.Slug, err))
			result.FailedPlatforms = append(result.FailedPlatforms, platform.Slug)
			continue
		}
		matches := rom.PreferCompatible(rom.FilterByTitle(page.Games, query.Text), platform)
		found += len(matches)
		if len(matches) == 0 && len(page.FailedSources) == 0 {
			continue
		}
		result.Platforms = append(result.Platforms, PlatformGames{
			Platform:      platform,
			Games:         rom.Page(matches, 0, query.Limit),
			FailedSources: page.FailedSources,
		})
	}
	if found == 0 && len(failures) > 0 {
		return SearchAllGamesResult{}, errors.Join(failures...)
	}
	return result, nil
}
