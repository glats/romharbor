package romlistings

import (
	"context"
	"errors"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

func TestPartialListingIsNotCached(t *testing.T) {
	captureLogs(t)
	cache := &listingCache{}
	listings := NewListings([]adapter.RomSource{
		scriptedSource{name: "broken", err: errors.New("upstream unavailable")},
		scriptedSource{name: "healthy", games: []rom.Game{{ID: "g", Title: "G"}}},
	}, cache, time.Hour)

	page, err := listings.ObtainPlatformListing(context.Background(), "snes")
	if err != nil {
		t.Fatalf("Games: %v", err)
	}
	if len(page.FailedSources) != 1 || page.FailedSources[0] != "broken" {
		t.Fatalf("failed sources = %+v, want [broken]", page.FailedSources)
	}
	if len(cache.listings) != 0 {
		t.Fatalf("cache holds %+v, want no entry for a partial listing", cache.listings)
	}
	if len(listings.listings) != 0 {
		t.Fatalf("memory holds %+v, want no entry for a partial listing", listings.listings)
	}
}
