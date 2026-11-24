package romlistings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

// Listings serves the games of the configured sources and keeps what they
// answered: each platform listing is fresh for ttl, stays on disk, and is
// served stale when the sources fail.
type Listings struct {
	sources *sourceListings
	cache   adapter.RomListingCache
	ttl     time.Duration
	now     func() time.Time
	logger  *slog.Logger

	mu       sync.RWMutex
	listings map[string]rom.Listing

	flights singleflight[rom.GamePage]
}

// Option changes how Listings is built.
type Option func(*Listings)

// WithClock sets the clock that decides whether a cached listing is fresh.
func WithClock(now func() time.Time) Option {
	return func(l *Listings) { l.now = now }
}

func NewListings(sources []adapter.RomSource, cache adapter.RomListingCache, ttl time.Duration, options ...Option) *Listings {
	logger := slog.Default()
	l := &Listings{
		sources:  &sourceListings{registry: newRegistry(sources, DefaultSourceTimeout, logger)},
		cache:    cache,
		ttl:      ttl,
		now:      time.Now,
		logger:   logger,
		listings: map[string]rom.Listing{},
	}
	for _, option := range options {
		option(l)
	}
	return l
}

func (l *Listings) PlatformCounts(ctx context.Context) ([]rom.PlatformCount, error) {
	out := []rom.PlatformCount{}
	var errs []error
	for _, p := range rom.Platforms() {
		count, err := l.Count(ctx, p.Slug)
		if err != nil {
			l.logger.Error("platform count failed", "platform", p.Slug, "error", err)
			if last, ok := l.lastKnownCount(p.Slug); ok {
				out = append(out, rom.PlatformCount{Platform: p, Count: last})
			} else {
				errs = append(errs, fmt.Errorf("count %s: %w", p.Slug, err))
			}
			continue
		}
		out = append(out, rom.PlatformCount{Platform: p, Count: count})
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ObtainPlatformListing returns every game of a platform, or of all platforms
// when the platform is empty.
func (l *Listings) ObtainPlatformListing(ctx context.Context, platform string) (rom.GamePage, error) {
	return l.listing(ctx, platform)
}

// SearchSources asks the sources that can search for the text. Games found
// this way are remembered so they can be resolved by ID later.
func (l *Listings) SearchSources(ctx context.Context, text, platform string) (rom.GamePage, error) {
	return l.sources.search(ctx, text, platform)
}

func (l *Listings) Count(ctx context.Context, platform string) (int, error) {
	listing, err := l.listing(ctx, platform)
	if err != nil {
		return 0, err
	}
	return len(listing.Games), nil
}

func (l *Listings) Game(ctx context.Context, id string) (rom.Game, bool, error) {
	if game, ok := l.sources.resolve(id); ok {
		return game, true, nil
	}
	platform, ok, err := l.sources.resolvePlatform(ctx, id)
	if err != nil {
		return rom.Game{}, false, err
	}
	if ok {
		listing, err := l.listing(ctx, platform)
		if err != nil {
			return rom.Game{}, false, err
		}
		if game, found := gameOfID(listing.Games, id); found {
			return game, true, nil
		}
	}
	if game, ok := l.knownGame(id); ok {
		return game, true, nil
	}
	return rom.Game{}, false, nil
}

func (l *Listings) knownGame(id string) (rom.Game, bool) {
	for _, p := range rom.Platforms() {
		l.mu.RLock()
		listing, ok := l.listings[p.Slug]
		l.mu.RUnlock()
		if ok {
			if game, found := gameOfID(listing.Games, id); found {
				return game, true
			}
		}
		stored, err := l.cache.Load(p.Slug)
		if err != nil {
			continue
		}
		if game, found := gameOfID(stored.Games, id); found {
			return game, true
		}
	}
	return rom.Game{}, false
}

func gameOfID(games []rom.Game, id string) (rom.Game, bool) {
	for _, game := range games {
		if game.ID == id {
			return game, true
		}
	}
	return rom.Game{}, false
}

func (l *Listings) listing(ctx context.Context, platform string) (rom.GamePage, error) {
	if _, ok := rom.PlatformBySlug(platform); platform != "" && !ok {
		return rom.GamePage{}, rom.ErrUnknownPlatform
	}
	l.mu.RLock()
	cached, ok := l.listings[platform]
	l.mu.RUnlock()
	if ok && l.fresh(cached.Fetched) {
		return rom.GamePage{Games: cached.Games}, nil
	}
	return l.flights.do(ctx, platform, func(ctx context.Context) (rom.GamePage, error) {
		return l.refresh(ctx, platform)
	})
}

func (l *Listings) refresh(ctx context.Context, platform string) (rom.GamePage, error) {
	l.mu.RLock()
	cached, ok := l.listings[platform]
	l.mu.RUnlock()
	disk, err := l.cache.Load(platform)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			l.logger.Error("listing cache entry unusable", "platform", platform, "error", err)
		}
	} else if l.fresh(disk.Fetched) {
		l.remember(platform, disk)
		return rom.GamePage{Games: disk.Games}, nil
	}

	page, err := l.sources.fetch(ctx, platform)
	if err != nil {
		if ok {
			return rom.GamePage{Games: cached.Games, FailedSources: page.FailedSources}, nil
		}
		return page, err
	}
	fresh := rom.Listing{Fetched: l.now(), Games: page.Games}
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

func (l *Listings) fresh(fetched time.Time) bool {
	return l.now().Sub(fetched) < l.ttl
}
