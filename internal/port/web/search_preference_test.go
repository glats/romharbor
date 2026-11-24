package web

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"app.ddd/internal/domain/rom"
	"app.ddd/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type platformListings struct {
	uiListings
}

func (l platformListings) ObtainPlatformListing(_ context.Context, platform string) (rom.GamePage, error) {
	var games []rom.Game
	for _, g := range l.games {
		if g.Platform == platform {
			games = append(games, g)
		}
	}
	return rom.GamePage{Games: games}, nil
}

func TestGlobalSearchAppliesTheCompatibilityPreference(t *testing.T) {
	clean := uiGame("fixture:clean", "Game (USA)")
	beta := uiGame("fixture:beta", "Game (Beta)")
	beta.Files = []rom.File{{Name: "Game (Beta).sfc", Size: 2048}}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	listings := platformListings{uiListings{games: []rom.Game{clean, beta}}}
	h := NewHandler(service.NewApp(nil, listings, nil, uiJobs{}), WithLogger(quiet))

	rec := uiGet(t, h, "/search?q=Game")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Game (USA)")
	assert.NotContains(t, rec.Body.String(), "Game (Beta)")
}

type failingPlatformListings struct {
	platformListings
	failing string
}

func (l failingPlatformListings) ObtainPlatformListing(ctx context.Context, platform string) (rom.GamePage, error) {
	if platform == l.failing {
		return rom.GamePage{}, errors.New("upstream unavailable")
	}
	return l.platformListings.ObtainPlatformListing(ctx, platform)
}

func TestGlobalSearchNamesThePlatformsThatFailed(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	listings := failingPlatformListings{platformListings{uiListings{games: []rom.Game{uiGame("fixture:one", "Game (USA)")}}}, "nes"}
	h := NewHandler(service.NewApp(nil, listings, nil, uiJobs{}), WithLogger(quiet))

	rec := uiGet(t, h, "/search?q=Game")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Some sources did not respond: nes")
	assert.Contains(t, rec.Body.String(), "Game (USA)")
}
