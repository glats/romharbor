package romlistings

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

const DefaultCacheTTL = 24 * time.Hour

type Listings struct {
	registry *registry
	cache    adapter.RomListingCache
	ttl      time.Duration
	logger   *slog.Logger

	mu          sync.RWMutex
	listings    map[string]rom.Listing
	searchGames map[string]rom.Game
}

func NewListings(sources []adapter.RomSource, cache adapter.RomListingCache, ttl time.Duration) *Listings {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	logger := slog.Default()
	return &Listings{
		registry:    newRegistry(sources, DefaultSourceTimeout, logger),
		cache:       cache,
		ttl:         ttl,
		logger:      logger,
		listings:    map[string]rom.Listing{},
		searchGames: map[string]rom.Game{},
	}
}

func (l *Listings) PlatformCounts(ctx context.Context) []rom.PlatformCount {
	out := []rom.PlatformCount{}
	for _, p := range rom.Platforms() {
		count, err := l.Count(ctx, p.Slug)
		if err != nil {
			l.logger.Error("platform count failed", "platform", p.Slug, "error", err)
			if last, ok := l.lastKnownCount(p.Slug); ok {
				out = append(out, rom.PlatformCount{Platform: p, Count: last})
			}
			continue
		}
		out = append(out, rom.PlatformCount{Platform: p, Count: count})
	}
	return out
}

func (l *Listings) Games(ctx context.Context, platform, text string, offset, limit int) (rom.GamePage, error) {
	listing, err := l.listing(ctx, platform)
	if err != nil && (text == "" || errors.Is(err, rom.ErrUnknownPlatform)) {
		return rom.GamePage{}, err
	}
	games := listing.Games
	failed := append([]string(nil), listing.FailedSources...)
	if text != "" {
		found, searchErr := l.registry.search(ctx, rom.Query{Platform: platform, Text: text})
		if err != nil && len(found.Games) == 0 {
			return rom.GamePage{}, err
		}
		if searchErr != nil && len(listing.Games) == 0 {
			return rom.GamePage{}, searchErr
		}
		l.mu.Lock()
		for i := range found.Games {
			if p, ok := rom.PlatformBySlug(found.Games[i].Platform); ok {
				found.Games[i].Cover = p.CoverURL(found.Games[i].Title)
			}
			l.searchGames[found.Games[i].ID] = found.Games[i]
		}
		l.mu.Unlock()
		games = append(append([]rom.Game(nil), listing.Games...), found.Games...)
		failed = append(failed, found.FailedSources...)
		needle := strings.ToLower(text)
		filtered := make([]rom.Game, 0, len(games))
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Title), needle) {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}
	return rom.GamePage{Games: rom.Page(games, offset, limit), FailedSources: failed}, nil
}

func (l *Listings) Count(ctx context.Context, platform string) (int, error) {
	listing, err := l.listing(ctx, platform)
	if err != nil {
		return 0, err
	}
	return len(listing.Games), nil
}

func (l *Listings) Game(ctx context.Context, id string) (rom.Game, bool, error) {
	l.mu.RLock()
	game, ok := l.searchGames[id]
	l.mu.RUnlock()
	if ok {
		return game, true, nil
	}
	for _, p := range rom.Platforms() {
		listing, err := l.listing(ctx, p.Slug)
		if err != nil {
			continue
		}
		for _, g := range listing.Games {
			if g.ID == id {
				return g, true, nil
			}
		}
	}
	return rom.Game{}, false, nil
}

func (l *Listings) listing(ctx context.Context, platform string) (rom.GamePage, error) {
	if _, ok := rom.PlatformBySlug(platform); platform != "" && !ok {
		return rom.GamePage{}, rom.ErrUnknownPlatform
	}
	l.mu.RLock()
	cached, ok := l.listings[platform]
	l.mu.RUnlock()
	if ok && time.Since(cached.Fetched) < l.ttl {
		return rom.GamePage{Games: cached.Games}, nil
	}
	disk, err := l.cache.Load(platform)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.logger.Error("listing cache entry unusable", "platform", platform, "error", err)
		}
	} else if time.Since(disk.Fetched) < l.ttl {
		l.remember(platform, disk)
		return rom.GamePage{Games: disk.Games}, nil
	}

	page, err := l.registry.search(ctx, rom.Query{Platform: platform})
	if err != nil {
		if ok {
			return rom.GamePage{Games: cached.Games, FailedSources: page.FailedSources}, nil
		}
		return page, err
	}
	fresh := rom.Listing{Fetched: time.Now(), Games: page.Games}
	if p, ok := rom.PlatformBySlug(platform); ok {
		for i := range fresh.Games {
			fresh.Games[i].Cover = p.CoverURL(fresh.Games[i].Title)
		}
	}
	if len(page.FailedSources) == 0 {
		l.remember(platform, fresh)
		if err := l.cache.Store(platform, fresh); err != nil {
			l.logger.Error("listing cache write failed", "platform", platform, "error", err)
		}
	}
	return page, nil
}

func (l *Listings) lastKnownCount(platform string) (int, bool) {
	l.mu.RLock()
	cached, ok := l.listings[platform]
	l.mu.RUnlock()
	if ok {
		return len(cached.Games), true
	}
	disk, err := l.cache.Load(platform)
	if err != nil {
		return 0, false
	}
	return len(disk.Games), true
}

func (l *Listings) remember(platform string, listing rom.Listing) {
	l.mu.Lock()
	l.listings[platform] = listing
	l.mu.Unlock()
}
