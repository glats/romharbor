package catalog

import (
	"context"
	"fmt"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type registry struct {
	sources []adapter.RomSource
}

func newRegistry(sources []adapter.RomSource) *registry {
	return &registry{sources: sources}
}

// search queries every source that serves the platform and merges the
// results, keeping the first occurrence of each game ID.
func (r *registry) search(ctx context.Context, q rom.Query) ([]rom.Game, error) {
	var (
		games []rom.Game
		errs  []error
	)
	seen := map[string]bool{}
	for _, s := range r.sources {
		if !serves(s, q.Platform) {
			continue
		}
		searchOnly, ok := s.(interface{ SearchOnly() bool })
		if (ok && searchOnly.SearchOnly()) != (q.Text != "") {
			continue
		}
		found, err := s.Search(ctx, q)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			continue
		}
		for _, g := range found {
			if seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			games = append(games, g)
		}
	}
	if len(games) == 0 && len(errs) > 0 {
		return nil, errs[0]
	}
	return games, nil
}

func serves(s adapter.RomSource, platform string) bool {
	plats := s.Platforms()
	if len(plats) == 0 {
		return true
	}
	for _, p := range plats {
		if p == platform {
			return true
		}
	}
	return false
}
