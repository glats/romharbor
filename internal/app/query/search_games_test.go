package query

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

func TestSearchGamesFiltersTheListingByTextIgnoringCase(t *testing.T) {
	handler := NewSearchGamesHandler(newListings(
		rom.Game{ID: "1", Platform: "snes", Title: "Super Mario World"},
		rom.Game{ID: "2", Platform: "snes", Title: "Zelda"},
		rom.Game{ID: "3", Platform: "snes", Title: "Mario Kart"},
	), metric.NoOp{})

	matches, err := handler.Handle(context.Background(), SearchGames{Platform: "snes", Text: "MARIO", Limit: 10})

	require.NoError(t, err)
	require.Len(t, matches.Games, 2)
	for _, game := range matches.Games {
		assert.True(t, strings.Contains(strings.ToLower(game.Title), "mario"), game.Title)
	}
}

func TestSearchGamesListsAGameOnceWhenTheListingAndTheSearchBothReturnIt(t *testing.T) {
	listed := fixtureSource{games: []rom.Game{{ID: "same", Platform: "snes", Title: "Mario Listed"}}}
	found := searchOnlySource{fixtureSource{games: []rom.Game{
		{ID: "same", Platform: "snes", Title: "Mario Found"},
		{ID: "other", Platform: "snes", Title: "Mario Other"},
	}}}
	listings := romlistings.NewListings([]adapter.RomSource{listed, found}, noCache{}, time.Hour)

	page, err := NewSearchGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchGames{Platform: "snes", Text: "mario", Limit: 10})

	require.NoError(t, err)
	var titles []string
	for _, game := range page.Games {
		titles = append(titles, game.Title)
	}
	assert.Equal(t, []string{"Mario Listed", "Mario Other"}, titles)
}

func TestSearchGamesPagesTheMatches(t *testing.T) {
	handler := NewSearchGamesHandler(newListings(
		rom.Game{ID: "1", Platform: "snes", Title: "Mario A"},
		rom.Game{ID: "2", Platform: "snes", Title: "Mario B"},
		rom.Game{ID: "3", Platform: "snes", Title: "Zelda"},
	), metric.NoOp{})

	page, err := handler.Handle(context.Background(), SearchGames{Platform: "snes", Text: "mario", Offset: 1, Limit: 1})

	require.NoError(t, err)
	require.Len(t, page.Games, 1)
	assert.Equal(t, "2", page.Games[0].ID)
}

func TestSearchGamesRejectsUnknownPlatforms(t *testing.T) {
	_, err := NewSearchGamesHandler(newListings(), metric.NoOp{}).Handle(context.Background(), SearchGames{Platform: "../neighbor", Text: "mario"})

	assert.ErrorIs(t, err, rom.ErrUnknownPlatform)
}

type scriptedListings struct {
	listing    rom.GamePage
	listingErr error
	found      rom.GamePage
	foundErr   error
}

func (s scriptedListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return nil, nil
}

func (s scriptedListings) ObtainPlatformListing(context.Context, string) (rom.GamePage, error) {
	return s.listing, s.listingErr
}

func (s scriptedListings) SearchSources(context.Context, string, string) (rom.GamePage, error) {
	return s.found, s.foundErr
}

func (s scriptedListings) Game(context.Context, string) (rom.Game, bool, error) {
	return rom.Game{}, false, nil
}

func TestSearchGamesKeepsTheListingWhenTheSearchFailsAndReportsFailedSources(t *testing.T) {
	listings := scriptedListings{
		listing:  rom.GamePage{Games: []rom.Game{{ID: "1", Title: "Mario"}}, FailedSources: []string{"a"}},
		found:    rom.GamePage{FailedSources: []string{"b"}},
		foundErr: errors.New("search down"),
	}

	page, err := NewSearchGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchGames{Text: "mario", Limit: 10})

	require.NoError(t, err)
	assert.Len(t, page.Games, 1)
	assert.Equal(t, []string{"a", "b"}, page.FailedSources)
}

func TestSearchGamesKeepsTheFoundGamesWhenTheListingFails(t *testing.T) {
	listings := scriptedListings{
		listingErr: errors.New("listing down"),
		found:      rom.GamePage{Games: []rom.Game{{ID: "9", Title: "Mario Found"}}},
	}

	page, err := NewSearchGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchGames{Text: "mario", Limit: 10})

	require.NoError(t, err)
	assert.Len(t, page.Games, 1)
}

func TestSearchGamesFailsWhenNothingWasFoundAndASideFailed(t *testing.T) {
	listingDown := errors.New("listing down")
	searchDown := errors.New("search down")

	_, err := NewSearchGamesHandler(scriptedListings{listingErr: listingDown}, metric.NoOp{}).Handle(context.Background(), SearchGames{Text: "x"})
	assert.ErrorIs(t, err, listingDown)

	_, err = NewSearchGamesHandler(scriptedListings{foundErr: searchDown}, metric.NoOp{}).Handle(context.Background(), SearchGames{Text: "x"})
	assert.ErrorIs(t, err, searchDown)
}
