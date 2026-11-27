package web

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"

	"app.ddd/internal/domain/rom"
)

func TestCatalogPagesHaveNoPlatformSidebar(t *testing.T) {
	game := uiGame("fixture:one", "One")
	h := newUIHandler(t, nil, game)
	targets := []string{"/", "/platform/snes", "/search?q=one", "/game/" + base64.RawURLEncoding.EncodeToString([]byte(game.ID))}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			body := uiGet(t, h, target).Body.String()

			assert.NotContains(t, body, `id="platform-nav"`)
			assert.NotContains(t, body, `id="sidebar-toggle"`)
			assert.NotContains(t, body, `class="sidebar`)
			assert.Contains(t, body, `class="site-header"`)
		})
	}
}

func TestHomeListsEveryPlatformBlockWithItsIcon(t *testing.T) {
	body := uiGet(t, newUIHandler(t, nil), "/").Body.String()

	for _, platform := range rom.Platforms() {
		assert.Contains(t, body, `href="/platform/`+platform.Slug+`"`, platform.Slug)
	}
	assert.Contains(t, body, `<use href="#pi-cartridge">`)
	assert.Contains(t, body, `id="pi-disc"`)
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
