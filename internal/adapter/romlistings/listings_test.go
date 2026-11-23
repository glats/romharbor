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

	page, err := listings.Games(context.Background(), "", "", 0, 10)

	if err != nil || len(page.Games) != 1 {
		t.Fatalf("Games() = %+v, %v", page, err)
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
			page, err := listings.Games(context.Background(), "snes", "mario", 0, 10)
			want := 1
			if mixed {
				want++
			}
			if err != nil || len(page.Games) != want {
				t.Fatalf("Games() = %+v, %v", page, err)
			}
			count, err := listings.Count(context.Background(), "snes")
			if err != nil || count != want-1 {
				t.Fatalf("Count() = %d, %v; query polluted listing", count, err)
			}
			for _, game := range page.Games {
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
	page, err := listings.Games(context.Background(), "snes", "", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v, want stale fallback", err)
	}
	if len(page.Games) != 1 || page.Games[0].ID != "old" {
		t.Fatalf("Games() = %+v, want the stale listing", page)
	}
}

func TestCacheWriteFailureKeepsTheFetchedGamesAndIsLogged(t *testing.T) {
	logs := captureLogs(t)
	listings := NewListings([]adapter.RomSource{gameSource{games: []rom.Game{{ID: "g", Title: "G"}}}}, failingStoreCache{}, time.Hour)

	games, err := listings.Games(context.Background(), "snes", "", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v, want the fetched games", err)
	}
	if len(games.Games) != 1 || games.Games[0].ID != "g" {
		t.Fatalf("Games() = %+v, want the fetched game", games)
	}
	count, err := listings.Count(context.Background(), "snes")
	if err != nil || count != 1 {
		t.Fatalf("Count() = %d, %v; want the fetched count", count, err)
	}
	if got := logLines(logs.String(), "disk full"); got != 1 {
		t.Fatalf("logged %d cache write failures, want 1: %s", got, logs.String())
	}
}

func TestPlatformCountsLogsAFailingPlatformAndKeepsTheCachedCount(t *testing.T) {
	logs := captureLogs(t)
	cache := &listingCache{listings: map[string]rom.Listing{
		"gb": {Fetched: time.Now().Add(-48 * time.Hour), Games: []rom.Game{{ID: "cached", Title: "Cached"}}},
	}}
	sources := []adapter.RomSource{
		platformSource{platforms: []string{"snes"}, games: []rom.Game{{ID: "live", Title: "Live"}}},
		platformSource{platforms: []string{"gb"}, err: errors.New("upstream unavailable")},
	}
	listings := NewListings(sources, cache, time.Hour)

	counts := listings.PlatformCounts(context.Background())

	got := map[string]int{}
	for _, c := range counts {
		got[c.Slug] = c.Count
	}
	if got["gb"] != 1 {
		t.Errorf("gb count = %d, want the cached count 1", got["gb"])
	}
	if got["snes"] != 1 {
		t.Errorf("snes count = %d, want the live count 1", got["snes"])
	}
	if failed := logLines(logs.String(), "platform count failed", "platform=gb"); failed != 1 {
		t.Errorf("logged %d failures for gb, want 1: %s", failed, logs.String())
	}
	if failed := logLines(logs.String(), "platform count failed", "platform=snes"); failed != 0 {
		t.Errorf("logged %d failures for snes, want 0: %s", failed, logs.String())
	}
}

func TestUnusableCacheEntryIsLoggedAndTreatedAsAMiss(t *testing.T) {
	logs := captureLogs(t)
	cache := &listingCache{loadErr: errors.New("cache entry is corrupt")}
	listings := NewListings([]adapter.RomSource{gameSource{games: []rom.Game{{ID: "g", Title: "G"}}}}, cache, time.Hour)

	games, err := listings.Games(context.Background(), "snes", "", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v, want the fetched games", err)
	}
	if len(games.Games) != 1 || games.Games[0].ID != "g" {
		t.Fatalf("Games() = %+v, want the fetched game", games)
	}
	if got := logLines(logs.String(), "cache entry is corrupt"); got != 1 {
		t.Fatalf("logged %d unusable cache entries, want 1: %s", got, logs.String())
	}
}

func TestMissingCacheEntryIsNotLogged(t *testing.T) {
	logs := captureLogs(t)
	listings := NewListings([]adapter.RomSource{gameSource{games: []rom.Game{{ID: "g", Title: "G"}}}}, &listingCache{}, time.Hour)

	games, err := listings.Games(context.Background(), "snes", "", 0, 10)

	if err != nil || len(games.Games) != 1 {
		t.Fatalf("Games() = %+v, %v; want the fetched game", games, err)
	}
	if strings.Contains(logs.String(), "cache") {
		t.Fatalf("a missing cache entry was logged: %s", logs.String())
	}
}
