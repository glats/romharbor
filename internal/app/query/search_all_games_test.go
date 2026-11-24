package query

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

type platformListings struct {
	pages    map[string]rom.GamePage
	failures map[string]error
}

func (p platformListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return nil, nil
}

func (p platformListings) ObtainPlatformListing(_ context.Context, platform string) (rom.GamePage, error) {
	if err, ok := p.failures[platform]; ok {
		return rom.GamePage{}, err
	}
	return p.pages[platform], nil
}

func (p platformListings) SearchSources(context.Context, string, string) (rom.GamePage, error) {
	return rom.GamePage{}, nil
}

func (p platformListings) Game(context.Context, string) (rom.Game, bool, error) {
	return rom.Game{}, false, nil
}

func gameTitles(games []rom.Game) []string {
	out := make([]string, 0, len(games))
	for _, game := range games {
		out = append(out, game.Title)
	}
	return out
}

func TestSearchAllGamesGroupsTheMatchesByPlatform(t *testing.T) {
	listings := platformListings{pages: map[string]rom.GamePage{
		"snes": {Games: []rom.Game{
			{ID: "1", Platform: "snes", Title: "Super Mario World"},
			{ID: "2", Platform: "snes", Title: "Zelda"},
		}},
		"nes": {Games: []rom.Game{
			{ID: "3", Platform: "nes", Title: "Super Mario Bros."},
		}},
	}}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "mario"})

	require.NoError(t, err)
	require.Len(t, result.Platforms, 2)
	assert.Equal(t, "nes", result.Platforms[0].Platform.Slug)
	assert.Equal(t, []string{"Super Mario Bros."}, gameTitles(result.Platforms[0].Games))
	assert.Equal(t, "snes", result.Platforms[1].Platform.Slug)
	assert.Equal(t, []string{"Super Mario World"}, gameTitles(result.Platforms[1].Games))
	assert.Empty(t, result.FailedPlatforms)
}

func TestSearchAllGamesFindsTheTextIgnoringCase(t *testing.T) {
	listings := platformListings{pages: map[string]rom.GamePage{
		"snes": {Games: []rom.Game{{ID: "1", Platform: "snes", Title: "Super Mario World"}}},
	}}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "MARIO"})

	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	assert.Equal(t, []string{"Super Mario World"}, gameTitles(result.Platforms[0].Games))
}

func TestSearchAllGamesReportsTheFailedSourcesOfEveryPlatform(t *testing.T) {
	listings := platformListings{pages: map[string]rom.GamePage{
		"snes": {
			Games:         []rom.Game{{ID: "1", Platform: "snes", Title: "Mario"}},
			FailedSources: []string{"archive.org"},
		},
	}}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "mario"})

	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	assert.Equal(t, []string{"archive.org"}, result.Platforms[0].FailedSources)
}

func TestSearchAllGamesKeepsThePlatformsThatAnsweredWhenOneFails(t *testing.T) {
	listings := platformListings{
		pages:    map[string]rom.GamePage{"snes": {Games: []rom.Game{{ID: "1", Platform: "snes", Title: "Mario"}}}},
		failures: map[string]error{"nes": errors.New("listing down")},
	}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "mario"})

	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	assert.Equal(t, "snes", result.Platforms[0].Platform.Slug)
	assert.Equal(t, []string{"nes"}, result.FailedPlatforms)
}

func TestSearchAllGamesFailsWhenNoGameWasFoundAndAPlatformFailed(t *testing.T) {
	listings := platformListings{failures: map[string]error{"nes": errors.New("listing down")}}

	_, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "mario"})

	assert.Error(t, err)
}

func TestSearchAllGamesWithAnEmptyTextFindsNothing(t *testing.T) {
	listings := platformListings{pages: map[string]rom.GamePage{
		"snes": {Games: []rom.Game{{ID: "1", Platform: "snes", Title: "Mario"}}},
	}}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{})

	require.NoError(t, err)
	assert.Empty(t, result.Platforms)
}

func TestSearchAllGamesLimitsTheGamesOfEveryPlatform(t *testing.T) {
	listings := platformListings{pages: map[string]rom.GamePage{
		"snes": {Games: []rom.Game{
			{ID: "1", Platform: "snes", Title: "Mario One"},
			{ID: "2", Platform: "snes", Title: "Mario Two"},
		}},
	}}

	result, err := NewSearchAllGamesHandler(listings, metric.NoOp{}).Handle(context.Background(), SearchAllGames{Text: "mario", Limit: 1})

	require.NoError(t, err)
	require.Len(t, result.Platforms, 1)
	assert.Len(t, result.Platforms[0].Games, 1)
}
