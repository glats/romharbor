package catalog

import (
	"context"
	"testing"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

func TestRegistrySkipsSourcesWithoutPlatform(t *testing.T) {
	r := newRegistry([]adapter.RomSource{
		&fakeSource{name: "all", games: []rom.Game{{ID: "a", Source: "all"}}},
		&fakeSource{name: "snes-only", platforms: []string{"snes"}, games: []rom.Game{{ID: "b", Source: "snes-only"}}},
	})
	games, err := r.search(context.Background(), rom.Query{Platform: "gb"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 || games[0].Source != "all" {
		t.Fatalf("got %+v", games)
	}
}

func TestRegistryKeepsFirstOccurrenceOfEachGameID(t *testing.T) {
	r := newRegistry([]adapter.RomSource{
		&fakeSource{name: "first", games: []rom.Game{{ID: "same", Source: "first"}}},
		&fakeSource{name: "second", games: []rom.Game{{ID: "same", Source: "second"}, {ID: "other", Source: "second"}}},
	})
	games, err := r.search(context.Background(), rom.Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 2 || games[0].Source != "first" || games[1].ID != "other" {
		t.Fatalf("got %+v", games)
	}
}

type fakeSource struct {
	name      string
	platforms []string
	games     []rom.Game
}

func (f *fakeSource) Name() string        { return f.name }
func (f *fakeSource) Platforms() []string { return f.platforms }
func (f *fakeSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return f.games, nil
}
