package web

import (
	"context"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
	"app.ddd/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type coverStub struct {
	cover rom.Cover
	err   error
	asked [][2]string
}

func (c *coverStub) Cover(_ context.Context, platform, title string) (rom.Cover, error) {
	c.asked = append(c.asked, [2]string{platform, title})
	return c.cover, c.err
}

func newCoverHandler(covers *coverStub) http.Handler {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	options := []service.Option{}
	if covers != nil {
		options = append(options, service.WithCovers(covers))
	}
	return NewHandler(service.NewApp(nil, uiListings{games: []rom.Game{coverGame()}}, nil, uiJobs{}, options...), WithLogger(quiet))
}

func coverGame() rom.Game {
	game := uiGame("fixture:cover", "Mario & Luigi (USA)")
	game.Cover = "https://raw.githubusercontent.com/libretro-thumbnails/x/master/Named_Boxarts/Mario.png"
	return game
}

func TestCoverIsServedWithALongCacheLifetime(t *testing.T) {
	covers := &coverStub{cover: rom.Cover{Data: []byte("png-bytes"), ContentType: "image/png"}}

	rec := uiGet(t, newCoverHandler(covers), "/cover?platform=snes&title=Mario+%26+Luigi+%28USA%29")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "png-bytes", rec.Body.String())
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=604800, immutable", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, [][2]string{{"snes", "Mario & Luigi (USA)"}}, covers.asked)
}

func TestAMissingCoverAnswersNotFoundAndMayBeCachedBriefly(t *testing.T) {
	for _, failure := range []error{rom.ErrCoverNotFound, rom.ErrUnknownPlatform} {
		rec := uiGet(t, newCoverHandler(&coverStub{err: failure}), "/cover?platform=snes&title=Nope")

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "public, max-age=3600", rec.Header().Get("Cache-Control"))
	}
}

func TestAnUpstreamCoverFailureAnswersBadGatewayWithoutCaching(t *testing.T) {
	rec := uiGet(t, newCoverHandler(&coverStub{err: errors.New("boom")}), "/cover?platform=snes&title=Slow")

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestACoverRequestNeedsAPlatformAndAShortTitle(t *testing.T) {
	covers := &coverStub{}
	h := newCoverHandler(covers)

	for _, target := range []string{"/cover", "/cover?platform=snes", "/cover?title=x", "/cover?platform=snes&title=" + strings.Repeat("a", 301)} {
		assert.Equal(t, http.StatusBadRequest, uiGet(t, h, target).Code, target)
	}
	assert.Empty(t, covers.asked)
}

func TestWithoutACoverStoreTheCoverRouteIsNotFound(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, uiGet(t, newCoverHandler(nil), "/cover?platform=snes&title=x").Code)
}

func TestCardsPointTheImageAtTheSameOriginCoverRoute(t *testing.T) {
	body := uiGet(t, newCoverHandler(nil), "/platform/snes").Body.String()

	assert.Contains(t, html.UnescapeString(body), `src="/cover?platform=snes&title=Mario+%26+Luigi+%28USA%29"`)
	assert.NotContains(t, body, "raw.githubusercontent.com")
	assert.Contains(t, body, `class="cover-frame"`)
	assert.Contains(t, body, `loading="lazy"`)
}

func TestTheJSONContractKeepsTheUpstreamCoverURL(t *testing.T) {
	body := uiGet(t, newCoverHandler(nil), "/api/games?platform=snes").Body.String()

	assert.Contains(t, body, "https://raw.githubusercontent.com/libretro-thumbnails/x/master/Named_Boxarts/Mario.png")
}
