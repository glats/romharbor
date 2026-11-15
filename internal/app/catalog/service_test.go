package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/adapter/romsource"
	"app.ddd/internal/domain/rom"
)

type flakySource struct {
	down bool
}

func TestSearchOnlySourceRetainsResultsForDownload(t *testing.T) {
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
			library := &blockingLibrary{started: make(chan struct{})}
			s := NewService(Dependencies{Sources: sources, Cache: &memoryCache{}, Jobs: romjobs.New(), Library: library})
			defer func() { _ = s.Shutdown(context.Background()) }()
			games, err := s.Games(context.Background(), "snes", "mario", 0, 10)
			want := 1
			if mixed {
				want++
			}
			if err != nil || len(games) != want {
				t.Fatalf("Games() = %+v, %v", games, err)
			}
			count, err := s.Count(context.Background(), "snes")
			if err != nil || count != want-1 {
				t.Fatalf("Count() = %d, %v; query polluted listing", count, err)
			}
			for _, game := range games {
				if game.Source != "lolroms" {
					continue
				}
				if got, ok, err := s.Game(context.Background(), game.ID); err != nil || !ok || got.ID != game.ID {
					t.Fatalf("Game() = %+v, %v, %v", got, ok, err)
				}
				if _, err := s.StartDownload(context.Background(), StartDownload{GameID: game.ID, NewJobID: func() string { return "search" }}); err != nil {
					t.Fatal(err)
				}
				<-library.started
			}
		})
	}
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

type blockingLibrary struct {
	started chan struct{}
}

func (b *blockingLibrary) PlatformDir(p rom.Platform) (string, error) {
	return "/unused/" + p.Slug, nil
}

func (b *blockingLibrary) Install(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
	close(b.started)
	<-ctx.Done()
	return rom.InstallResult{}, ctx.Err()
}

type gameSource struct {
	games []rom.Game
}

func (g gameSource) Name() string        { return "fixture" }
func (g gameSource) Platforms() []string { return nil }
func (g gameSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return g.games, nil
}

func TestStaleListingServedWhenSourcesFail(t *testing.T) {
	source := &flakySource{}
	s := NewService(Dependencies{
		Sources: []adapter.RomSource{source},
		Cache:   &memoryCache{},
		Jobs:    romjobs.New(),
		TTL:     time.Nanosecond,
	})
	if _, err := s.Games(context.Background(), "snes", "", 0, 10); err != nil {
		t.Fatalf("first Games() error = %v", err)
	}

	source.down = true
	games, err := s.Games(context.Background(), "snes", "", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v, want stale fallback", err)
	}
	if len(games) != 1 || games[0].ID != "old" {
		t.Fatalf("Games() = %+v, want the stale listing", games)
	}
}

func TestShutdownCancelsInFlightDownloadsAndRejectsNewOnes(t *testing.T) {
	library := &blockingLibrary{started: make(chan struct{})}
	jobs := romjobs.New()
	s := NewService(Dependencies{
		Sources: []adapter.RomSource{gameSource{games: []rom.Game{{
			ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}},
		}}}},
		Cache:   &memoryCache{},
		Library: library,
		Jobs:    jobs,
	})

	job, err := s.StartDownload(context.Background(), StartDownload{NewJobID: func() string { return "1" }, GameID: "g"})
	if err != nil {
		t.Fatalf("StartDownload() error = %v", err)
	}
	if job.ID != "1" || job.Status != "queued" {
		t.Fatalf("job = %+v, want queued job 1", job)
	}
	<-library.started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	got, _ := jobs.Get("1")
	if got.Status != "failed" || got.Error != context.Canceled.Error() {
		t.Errorf("job after shutdown = %+v, want failed with context canceled", got)
	}
	_, err = s.StartDownload(context.Background(), StartDownload{NewJobID: func() string { return "2" }, GameID: "g"})
	if !errors.Is(err, ErrShuttingDown) {
		t.Errorf("StartDownload() after shutdown error = %v, want ErrShuttingDown", err)
	}
}
