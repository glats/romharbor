package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

const (
	DefaultCacheTTL = 24 * time.Hour
	downloadTimeout = 6 * time.Hour
)

var (
	ErrGameNotFound    = errors.New("game not found")
	ErrNoDownloadFile  = errors.New("game has no file")
	ErrUnknownPlatform = errors.New("unknown platform")
	ErrShuttingDown    = errors.New("catalog is shutting down")
)

type Dependencies struct {
	Sources []adapter.RomSource
	Cache   adapter.RomListingCache
	Library adapter.RomLibrary
	Jobs    adapter.RomJobStore
	TTL     time.Duration
	Logger  *slog.Logger
}

type PlatformCount struct {
	rom.Platform
	Count int
}

type StartDownload struct {
	NewJobID func() string
	GameID   string
}

type Service struct {
	registry *registry
	cache    adapter.RomListingCache
	library  adapter.RomLibrary
	jobs     adapter.RomJobStore
	ttl      time.Duration
	logger   *slog.Logger

	mu          sync.RWMutex
	listings    map[string]rom.Listing
	searchGames map[string]rom.Game

	lifetime context.Context
	cancel   context.CancelFunc
	running  sync.WaitGroup
}

func NewService(deps Dependencies) *Service {
	ttl := deps.TTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Service{
		registry:    newRegistry(deps.Sources),
		cache:       deps.Cache,
		library:     deps.Library,
		jobs:        deps.Jobs,
		ttl:         ttl,
		logger:      logger,
		listings:    map[string]rom.Listing{},
		searchGames: map[string]rom.Game{},
		lifetime:    lifetime,
		cancel:      cancel,
	}
}

func (s *Service) SourceNames() []string {
	return s.registry.names()
}

func (s *Service) PlatformCounts(ctx context.Context) []PlatformCount {
	out := []PlatformCount{}
	for _, p := range rom.Platforms() {
		count, err := s.Count(ctx, p.Slug)
		if err != nil {
			continue
		}
		out = append(out, PlatformCount{Platform: p, Count: count})
	}
	return out
}

func (s *Service) Games(ctx context.Context, platform, text string, offset, limit int) ([]rom.Game, error) {
	listing, err := s.listing(ctx, platform)
	if err != nil && text == "" {
		return nil, err
	}
	games := listing
	if text != "" {
		found, searchErr := s.registry.search(ctx, rom.Query{Platform: platform, Text: text})
		if err != nil && len(found) == 0 {
			return nil, err
		}
		if searchErr != nil && len(listing) == 0 {
			return nil, searchErr
		}
		s.mu.Lock()
		for i := range found {
			if p, ok := rom.PlatformBySlug(found[i].Platform); ok {
				found[i].Cover = p.CoverURL(found[i].Title)
			}
			s.searchGames[found[i].ID] = found[i]
		}
		s.mu.Unlock()
		games = append(append([]rom.Game(nil), listing...), found...)
		needle := strings.ToLower(text)
		filtered := make([]rom.Game, 0, len(games))
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Title), needle) {
				filtered = append(filtered, g)
			}
		}
		games = filtered
	}
	return rom.Page(games, offset, limit), nil
}

func (s *Service) Count(ctx context.Context, platform string) (int, error) {
	listing, err := s.listing(ctx, platform)
	if err != nil {
		return 0, err
	}
	return len(listing), nil
}

func (s *Service) Game(ctx context.Context, id string) (rom.Game, bool, error) {
	s.mu.RLock()
	game, ok := s.searchGames[id]
	s.mu.RUnlock()
	if ok {
		return game, true, nil
	}
	for _, p := range rom.Platforms() {
		listing, err := s.listing(ctx, p.Slug)
		if err != nil {
			continue
		}
		for _, g := range listing {
			if g.ID == id {
				return g, true, nil
			}
		}
	}
	return rom.Game{}, false, nil
}

func (s *Service) Destination(p rom.Platform) (string, error) {
	return s.library.PlatformDir(p)
}

func (s *Service) Jobs() []rom.Job {
	return s.jobs.List()
}

func (s *Service) Job(id string) (rom.Job, bool) {
	return s.jobs.Get(id)
}

func (s *Service) StartDownload(ctx context.Context, cmd StartDownload) (rom.Job, error) {
	game, ok, err := s.Game(ctx, cmd.GameID)
	if err != nil || !ok {
		return rom.Job{}, ErrGameNotFound
	}
	file, ok := game.Main()
	if !ok {
		return rom.Job{}, ErrNoDownloadFile
	}
	platform, ok := rom.PlatformBySlug(game.Platform)
	if !ok {
		return rom.Job{}, ErrUnknownPlatform
	}

	s.mu.Lock()
	if s.lifetime.Err() != nil {
		s.mu.Unlock()
		return rom.Job{}, ErrShuttingDown
	}
	job := s.jobs.Add(rom.Job{
		ID:      cmd.NewJobID(),
		GameID:  game.ID,
		Title:   game.Title,
		Status:  "queued",
		Total:   file.Size,
		Started: time.Now(),
	})
	s.running.Add(1)
	s.mu.Unlock()
	go s.runDownload(job.ID, game, file, platform)
	return job, nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for downloads: %w", ctx.Err())
	}
}

func (s *Service) runDownload(id string, game rom.Game, file rom.File, platform rom.Platform) {
	defer s.running.Done()
	ctx, cancel := context.WithTimeout(s.lifetime, downloadTimeout)
	defer cancel()

	s.jobs.Update(id, func(j *rom.Job) { j.Status = "running" })
	result, err := s.library.Install(ctx, rom.InstallRequest{
		Game:     game,
		File:     file,
		Platform: platform,
	}, func(downloaded, total int64) {
		s.jobs.Update(id, func(j *rom.Job) {
			j.Downloaded = downloaded
			if total > 0 {
				j.Total = total
			}
		})
	})
	if err != nil {
		s.logger.Error("download", "game", game.Title, "error", err)
		s.jobs.Update(id, func(j *rom.Job) {
			j.Status = "failed"
			j.Error = err.Error()
			j.Finished = time.Now()
		})
		return
	}
	s.jobs.Update(id, func(j *rom.Job) {
		j.Status = "done"
		j.Path = result.Path
		j.Total = result.Size
		j.Finished = time.Now()
	})
}

func (s *Service) listing(ctx context.Context, platform string) ([]rom.Game, error) {
	s.mu.RLock()
	cached, ok := s.listings[platform]
	s.mu.RUnlock()
	if ok && time.Since(cached.Fetched) < s.ttl {
		return cached.Games, nil
	}
	if disk, err := s.cache.Load(platform); err == nil && time.Since(disk.Fetched) < s.ttl {
		s.remember(platform, disk)
		return disk.Games, nil
	}

	games, err := s.registry.search(ctx, rom.Query{Platform: platform})
	if err != nil {
		if ok {
			return cached.Games, nil
		}
		return nil, err
	}
	fresh := rom.Listing{Fetched: time.Now(), Games: games}
	if p, ok := rom.PlatformBySlug(platform); ok {
		for i := range fresh.Games {
			fresh.Games[i].Cover = p.CoverURL(fresh.Games[i].Title)
		}
	}
	s.remember(platform, fresh)
	if err := s.cache.Store(platform, fresh); err != nil {
		return games, fmt.Errorf("write cache: %w", err)
	}
	return games, nil
}

func (s *Service) remember(platform string, listing rom.Listing) {
	s.mu.Lock()
	s.listings[platform] = listing
	s.mu.Unlock()
}
