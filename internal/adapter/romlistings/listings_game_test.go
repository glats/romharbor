package romlistings

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type platformNamingSource struct {
	platforms []string
	games     []rom.Game
	err       error

	mu      sync.Mutex
	queried []string
}

func (s *platformNamingSource) Name() string        { return "fixture" }
func (s *platformNamingSource) Platforms() []string { return s.platforms }
func (s *platformNamingSource) Search(_ context.Context, q rom.Query) ([]rom.Game, error) {
	s.mu.Lock()
	s.queried = append(s.queried, q.Platform)
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if q.Platform != "snes" {
		return nil, nil
	}
	return s.games, nil
}

func (s *platformNamingSource) ResolvePlatform(_ context.Context, id string) (string, bool, error) {
	if id != "g1" {
		return "", false, nil
	}
	return "snes", true, nil
}

func (s *platformNamingSource) queriedPlatforms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queried...)
}

func TestGameFetchesOnlyThePlatformNamedByTheIDResolver(t *testing.T) {
	source := &platformNamingSource{
		platforms: []string{"nes", "snes", "gb"},
		games:     []rom.Game{{ID: "g1", Platform: "snes", Title: "G"}},
	}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)

	game, ok, err := listings.Game(context.Background(), "g1")
	if err != nil {
		t.Fatalf("Game() error = %v", err)
	}
	if !ok || game.ID != "g1" {
		t.Fatalf("Game() = %+v, %v, want the game", game, ok)
	}
	queried := source.queriedPlatforms()
	if len(queried) != 1 || queried[0] != "snes" {
		t.Fatalf("the sources were asked for %v, want only the platform of the game", queried)
	}
}

func TestGameReportsTheErrorOfThePlatformItFetches(t *testing.T) {
	cause := errors.New("upstream unavailable")
	source := &platformNamingSource{
		platforms: []string{"nes", "snes", "gb"},
		games:     []rom.Game{{ID: "g1", Platform: "snes", Title: "G"}},
		err:       cause,
	}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)

	game, ok, err := listings.Game(context.Background(), "g1")

	if err == nil {
		t.Fatalf("Game() = %+v, %v, want the failure of the platform", game, ok)
	}
	if ok {
		t.Fatalf("Game() found a game: %+v", game)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("Game() error = %v, want the failure of the platform", err)
	}
}

func TestGameReportsAMissingGameWithoutAnError(t *testing.T) {
	source := &platformNamingSource{
		platforms: []string{"nes", "snes", "gb"},
		games:     []rom.Game{{ID: "g1", Platform: "snes", Title: "G"}},
	}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)

	game, ok, err := listings.Game(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Game() error = %v, want no error for a missing game", err)
	}
	if ok {
		t.Fatalf("Game() found a game: %+v", game)
	}
	if queried := source.queriedPlatforms(); len(queried) != 0 {
		t.Fatalf("the sources were asked for %v, want no fetch for a missing game", queried)
	}
}

func TestGameFindsAGameOfTheCachedListing(t *testing.T) {
	source := &platformNamingSource{
		platforms: []string{"nes", "snes", "gb"},
		games:     []rom.Game{{ID: "g1", Platform: "snes", Title: "G"}},
	}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)
	if _, err := listings.ObtainPlatformListing(context.Background(), "snes"); err != nil {
		t.Fatalf("ObtainPlatformListing() error = %v", err)
	}

	game, ok, err := listings.Game(context.Background(), "g1")

	if err != nil || !ok || game.ID != "g1" {
		t.Fatalf("Game() = %+v, %v, %v", game, ok, err)
	}
}

func TestGameFindsAGameOfTheStoredListing(t *testing.T) {
	cache := &listingCache{listings: map[string]rom.Listing{
		"snes": {Fetched: time.Now(), Games: []rom.Game{{ID: "g1", Platform: "snes", Title: "G"}}},
	}}
	source := &platformNamingSource{platforms: []string{"nes", "snes", "gb"}}
	listings := NewListings([]adapter.RomSource{source}, cache, time.Hour)

	game, ok, err := listings.Game(context.Background(), "g1")

	if err != nil || !ok || game.ID != "g1" {
		t.Fatalf("Game() = %+v, %v, %v", game, ok, err)
	}
	if queried := source.queriedPlatforms(); len(queried) != 0 {
		t.Fatalf("the sources were asked for %v, want no fetch for a stored game", queried)
	}
}
