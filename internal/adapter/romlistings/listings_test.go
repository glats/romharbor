package romlistings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romsource"
	"app.ddd/internal/domain/rom"
)

func TestUnknownPlatformRejectedBeforeSourcesAndCache(t *testing.T) {
	listings := NewListings(nil, nil, 0)
	for _, platform := range []string{"../neighbor", "unknown"} {
		for _, text := range []string{"", "mario"} {
			if _, err := listings.Games(context.Background(), platform, text, 0, 10); !errors.Is(err, rom.ErrUnknownPlatform) {
				t.Errorf("Games(%q, %q) error = %v", platform, text, err)
			}
		}
		if _, err := listings.Count(context.Background(), platform); !errors.Is(err, rom.ErrUnknownPlatform) {
			t.Errorf("Count(%q) error = %v", platform, err)
		}
	}
}

func TestEmptyPlatformListsEverySource(t *testing.T) {
	listings := NewListings([]adapter.RomSource{gameSource{games: []rom.Game{{ID: "g", Title: "G"}}}}, &memoryCache{}, time.Hour)

	games, err := listings.Games(context.Background(), "", "", 0, 10)

	if err != nil || len(games) != 1 {
		t.Fatalf("Games() = %+v, %v", games, err)
	}
}

func TestSearchOnlySourceRetainsResultsWithoutPollutingListing(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed=%v", mixed), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("q") != "mario" {
					t.Errorf("query = %q", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`<a href="/Nintendo/Super%20Nintendo%20Entertainment%20System/Mario.7z">Mario.7z</a>`))
			}))
			defer upstream.Close()
			source := romsource.NewLolRoms(upstream.Client())
			source.BaseURL = upstream.URL
			sources := []adapter.RomSource{source}
			if mixed {
				sources = append(sources, gameSource{games: []rom.Game{{ID: "listed", Title: "Mario Listed"}}})
			}
			listings := NewListings(sources, &memoryCache{}, time.Hour)
			games, err := listings.Games(context.Background(), "snes", "mario", 0, 10)
			want := 1
			if mixed {
				want++
			}
			if err != nil || len(games) != want {
				t.Fatalf("Games() = %+v, %v", games, err)
			}
			count, err := listings.Count(context.Background(), "snes")
			if err != nil || count != want-1 {
				t.Fatalf("Count() = %d, %v; query polluted listing", count, err)
			}
			for _, game := range games {
				if game.Source != "lolroms" {
					continue
				}
				if got, ok, err := listings.Game(context.Background(), game.ID); err != nil || !ok || got.ID != game.ID {
					t.Fatalf("Game() = %+v, %v, %v", got, ok, err)
				}
			}
		})
	}
}

func TestStaleListingServedWhenSourcesFail(t *testing.T) {
	source := &flakySource{}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Nanosecond)
	if _, err := listings.Games(context.Background(), "snes", "", 0, 10); err != nil {
		t.Fatalf("first Games() error = %v", err)
	}

	source.down = true
	games, err := listings.Games(context.Background(), "snes", "", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v, want stale fallback", err)
	}
	if len(games) != 1 || games[0].ID != "old" {
		t.Fatalf("Games() = %+v, want the stale listing", games)
	}
}

func TestCacheWriteFailureIsReportedForEmptyText(t *testing.T) {
	listings := NewListings([]adapter.RomSource{gameSource{games: []rom.Game{{ID: "g", Title: "G"}}}}, failingStoreCache{}, time.Hour)

	games, err := listings.Games(context.Background(), "snes", "", 0, 10)

	if err == nil || !strings.Contains(err.Error(), "write cache") || games != nil {
		t.Fatalf("Games() = %+v, %v; want nil games and a write cache error", games, err)
	}
}
