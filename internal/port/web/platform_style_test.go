package web

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"app.ddd/internal/domain/rom"
)

func TestEveryCatalogPlatformHasItsOwnIconMarkAndBrand(t *testing.T) {
	for _, platform := range rom.Platforms() {
		style := platformStyle(platform.Slug)
		assert.NotEqual(t, genericPlatformStyle, style, platform.Slug)
		assert.NotEmpty(t, style.Symbol, platform.Slug)
		assert.NotEmpty(t, style.Mark, platform.Slug)
		assert.NotEmpty(t, style.Brand, platform.Slug)
		assert.LessOrEqual(t, len(style.Mark), 5, platform.Slug)
	}
}

func TestUnknownPlatformFallsBackToTheGenericStyle(t *testing.T) {
	assert.Equal(t, genericPlatformStyle, platformStyle("does-not-exist"))
}

func TestPlatformStyleSymbolsAreDefinedInTheSpriteSet(t *testing.T) {
	known := map[string]bool{}
	for _, symbol := range platformSymbols {
		known[symbol] = true
	}
	for _, platform := range rom.Platforms() {
		assert.True(t, known[platformStyle(platform.Slug).Symbol], platform.Slug)
	}
	assert.True(t, known[genericPlatformStyle.Symbol])
}
