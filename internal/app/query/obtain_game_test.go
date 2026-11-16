package query

import (
	"context"
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
