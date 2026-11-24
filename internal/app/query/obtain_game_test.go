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

func TestObtainGameFindsListedGamesOnly(t *testing.T) {
	handler := NewObtainGameHandler(newListings(rom.Game{ID: "1", Platform: "snes", Title: "Zelda"}), metric.NoOp{})

	game, err := handler.Handle(context.Background(), ObtainGame{ID: "1"})
	require.NoError(t, err)
	assert.Equal(t, "Zelda", game.Title)

	_, err = handler.Handle(context.Background(), ObtainGame{ID: "missing"})
	assert.ErrorIs(t, err, rom.ErrGameNotFound)
}

type failingListings struct {
	err error
}

func (f failingListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return nil, f.err
}

func (f failingListings) ObtainPlatformListing(context.Context, string) (rom.GamePage, error) {
	return rom.GamePage{}, f.err
}

func (f failingListings) SearchSources(context.Context, string, string) (rom.GamePage, error) {
	return rom.GamePage{}, f.err
}

func (f failingListings) Game(context.Context, string) (rom.Game, bool, error) {
	return rom.Game{}, false, f.err
}

func TestObtainGameReportsListingFailuresWithTheGameContext(t *testing.T) {
	cause := errors.New("source unavailable")
	handler := NewObtainGameHandler(failingListings{err: cause}, metric.NoOp{})

	_, err := handler.Handle(context.Background(), ObtainGame{ID: "fixture:broken"})

	require.ErrorIs(t, err, cause)
	assert.NotErrorIs(t, err, rom.ErrGameNotFound)
	assert.Contains(t, err.Error(), "fixture:broken")
}
