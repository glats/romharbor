package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

type fixtureSource struct {
	games []rom.Game
}

type searchOnlySource struct{ fixtureSource }

func (searchOnlySource) SearchOnly() bool { return true }

func (f fixtureSource) Name() string        { return "fixture" }
func (f fixtureSource) Platforms() []string { return nil }
func (f fixtureSource) Search(_ context.Context, q rom.Query) ([]rom.Game, error) {
	var out []rom.Game
	for _, g := range f.games {
		if q.Platform == "" || g.Platform == q.Platform {
			out = append(out, g)
		}
	}
	return out, nil
}

type noCache struct{}

func (noCache) Load(string) (rom.Listing, error) { return rom.Listing{}, errors.New("missing") }
func (noCache) Store(string, rom.Listing) error  { return nil }

func newListings(games ...rom.Game) *romlistings.Listings {
	return romlistings.NewListings([]adapter.RomSource{fixtureSource{games: games}}, noCache{}, time.Hour)
}

func TestBrowsePlatformGamesPagesTheListing(t *testing.T) {
	handler := NewBrowsePlatformGamesHandler(newListings(
		rom.Game{ID: "1", Platform: "snes", Title: "Super Mario World"},
		rom.Game{ID: "2", Platform: "snes", Title: "Zelda"},
		rom.Game{ID: "3", Platform: "snes", Title: "Mario Kart"},
	), metric.NoOp{})

	page, err := handler.Handle(context.Background(), BrowsePlatformGames{Platform: "snes", Offset: 1, Limit: 1})

	require.NoError(t, err)
	require.Len(t, page.Games, 1)
	assert.Equal(t, "2", page.Games[0].ID)
}

func TestBrowsePlatformGamesRejectsUnknownPlatforms(t *testing.T) {
	_, err := NewBrowsePlatformGamesHandler(newListings(), metric.NoOp{}).Handle(context.Background(), BrowsePlatformGames{Platform: "../neighbor"})

	assert.ErrorIs(t, err, rom.ErrUnknownPlatform)
}
