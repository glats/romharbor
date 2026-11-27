package web

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
)

func TestPlatformNavListsEveryPlatformWithItsIconOnEveryCatalogPage(t *testing.T) {
	game := uiGame("fixture:one", "One")
	h := newUIHandler(t, nil, game)
	targets := []string{"/", "/platform/snes", "/search?q=one", "/game/" + base64.RawURLEncoding.EncodeToString([]byte(game.ID))}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			body := uiGet(t, h, target).Body.String()

			assert.Contains(t, body, `id="platform-nav"`)
			assert.Contains(t, body, `id="sidebar-toggle"`)
			assert.Contains(t, body, `aria-controls="platform-nav"`)
			for _, platform := range rom.Platforms() {
				assert.Contains(t, body, `class="sidebar-link" href="/platform/`+platform.Slug+`"`, platform.Slug)
			}
			assert.Contains(t, body, `<use href="#pi-cartridge">`)
			assert.Contains(t, body, `id="pi-disc"`)
		})
	}
}

func TestPlatformNavMarksOnlyTheCurrentPlatform(t *testing.T) {
	game := uiGame("fixture:one", "One")
	h := newUIHandler(t, nil, game)

	for _, target := range []string{"/platform/snes", "/game/" + base64.RawURLEncoding.EncodeToString([]byte(game.ID))} {
		t.Run(target, func(t *testing.T) {
			body := uiGet(t, h, target).Body.String()

			require.Equal(t, 1, strings.Count(body, `aria-current="page"`))
			assert.Contains(t, body, `href="/platform/snes" aria-current="page"`)
		})
	}
	assert.NotContains(t, uiGet(t, h, "/").Body.String(), `aria-current="page"`)
}

func TestHomePlatformBlocksShowTheIconAndTheCoresAsChips(t *testing.T) {
	body := uiGet(t, newUIHandler(t, nil), "/").Body.String()

	assert.Contains(t, body, `<span class="core-chip">snes9x</span>`)
	assert.Contains(t, body, `<span class="core-chip">mednafen_psx_hw</span>`)
	assert.Contains(t, body, `<span class="platform-mark">PS1</span>`)
	assert.Contains(t, body, `data-brand="sony"`)
}

func TestPlatformPageHeadingCarriesThePlatformIcon(t *testing.T) {
	body := uiGet(t, newUIHandler(t, nil, uiGame("fixture:one", "One")), "/platform/snes").Body.String()

	assert.Contains(t, body, `class="platform-heading"`)
	assert.Contains(t, body, `data-brand="nintendo"`)
}
