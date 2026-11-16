package romlistings

import (
	"context"
	"errors"

	"app.ddd/internal/domain/rom"
)

type flakySource struct {
	down bool
}

func (f *flakySource) Name() string        { return "flaky" }
func (f *flakySource) Platforms() []string { return nil }
func (f *flakySource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	if f.down {
		return nil, errors.New("upstream unavailable")
	}
	return []rom.Game{{ID: "old", Title: "Old"}}, nil
}

type memoryCache struct {
	listing rom.Listing
	ok      bool
}

func (m *memoryCache) Load(string) (rom.Listing, error) {
	if !m.ok {
		return rom.Listing{}, errors.New("missing")
	}
	return m.listing, nil
}

func (m *memoryCache) Store(string, rom.Listing) error { return nil }

type failingStoreCache struct{}

func (failingStoreCache) Load(string) (rom.Listing, error) {
	return rom.Listing{}, errors.New("missing")
}
func (failingStoreCache) Store(string, rom.Listing) error { return errors.New("disk full") }

type gameSource struct {
	games []rom.Game
}

func (g gameSource) Name() string        { return "fixture" }
func (g gameSource) Platforms() []string { return nil }
func (g gameSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return g.games, nil
}
