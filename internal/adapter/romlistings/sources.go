package romlistings

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

// sourceListings answers from the configured sources and keeps nothing.
type sourceListings struct {
	registry *registry
}

type gamePlatformResolver interface {
	ResolvePlatform(ctx context.Context, id string) (string, bool, error)
}

func (s *sourceListings) fetch(ctx context.Context, platform string) (rom.GamePage, error) {
	page, err := s.registry.search(ctx, rom.Query{Platform: platform})
	if err != nil {
		return page, err
	}
	for i := range page.Games {
		withCover(&page.Games[i])
	}
	return page, nil
}

func (s *sourceListings) resolvePlatform(ctx context.Context, id string) (string, bool, error) {
	for _, source := range s.registry.sources {
		resolver, ok := source.(gamePlatformResolver)
		if !ok {
			continue
		}
		platform, found, err := resolver.ResolvePlatform(ctx, id)
		if err != nil {
			return "", false, err
		}
		if found {
			return platform, true, nil
		}
	}
	return "", false, nil
}

func (s *sourceListings) search(ctx context.Context, text, platform string) (rom.GamePage, error) {
	if _, ok := rom.PlatformBySlug(platform); platform != "" && !ok {
		return rom.GamePage{}, rom.ErrUnknownPlatform
	}
	found, err := s.registry.search(ctx, rom.Query{Platform: platform, Text: text})
	for i := range found.Games {
		withCover(&found.Games[i])
	}
	return found, err
}

func (s *sourceListings) resolve(id string) (rom.Game, bool) {
	for _, source := range s.registry.sources {
		resolver, ok := source.(adapter.RomGameResolver)
		if !ok {
			continue
		}
		if game, found := resolver.ResolveGame(id); found {
			withCover(&game)
			return game, true
		}
	}
	return rom.Game{}, false
}

func withCover(game *rom.Game) {
	if p, ok := rom.PlatformBySlug(game.Platform); ok {
		game.Cover = p.CoverURL(game.Title)
	}
}
