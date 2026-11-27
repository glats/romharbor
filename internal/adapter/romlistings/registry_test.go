package romlistings

import (
	"context"
	"log/slog"
	"testing"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

func TestRegistrySkipsSourcesWithoutPlatform(t *testing.T) {
	r := newRegistry([]adapter.RomSource{
		&fakeSource{name: "all", games: []rom.Game{{ID: "a", Source: "all"}}},
		&fakeSource{name: "snes-only", platforms: []string{"snes"}, games: []rom.Game{{ID: "b", Source: "snes-only"}}},
	}, DefaultSourceTimeout, slog.Default())
	games, err := r.search(context.Background(), rom.Query{Platform: "gb"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games.Games) != 1 || games.Games[0].Source != "all" {
		t.Fatalf("got %+v", games)
	}
}

func TestRegistryKeepsFirstOccurrenceOfEachGameID(t *testing.T) {
	r := newRegistry([]adapter.RomSource{
		&fakeSource{name: "first", games: []rom.Game{{ID: "same", Source: "first"}}},
		&fakeSource{name: "second", games: []rom.Game{{ID: "same", Source: "second"}, {ID: "other", Source: "second"}}},
	}, DefaultSourceTimeout, slog.Default())
	games, err := r.search(context.Background(), rom.Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games.Games) != 2 || games.Games[0].Source != "first" || games.Games[1].ID != "other" {
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

type listerSource struct {
	fakeSource
	lists string
}

func (l *listerSource) SearchOnly() bool       { return true }
func (l *listerSource) Lists(slug string) bool { return slug == l.lists }

func TestRegistryAsksASearchOnlyListerForItsPlatformsWithoutText(t *testing.T) {
	lister := &listerSource{fakeSource: fakeSource{name: "lister", games: []rom.Game{{ID: "x", Source: "lister"}}}, lists: "lynx"}
	r := newRegistry([]adapter.RomSource{lister}, DefaultSourceTimeout, slog.Default())

	listed, err := r.search(context.Background(), rom.Query{Platform: "lynx"})
	if err != nil || len(listed.Games) != 1 {
		t.Fatalf("lynx listing = %+v, %v", listed, err)
	}
	other, err := r.search(context.Background(), rom.Query{Platform: "snes"})
	if err != nil || len(other.Games) != 0 {
		t.Fatalf("snes listing = %+v, %v", other, err)
	}
	searched, err := r.search(context.Background(), rom.Query{Platform: "snes", Text: "mario"})
	if err != nil || len(searched.Games) != 1 {
		t.Fatalf("search = %+v, %v", searched, err)
	}
}
