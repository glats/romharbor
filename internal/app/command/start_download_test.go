package command

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/app/catalog"
	"app.ddd/internal/common/metric"
	"app.ddd/internal/domain/rom"
)

type fixtureSource struct {
	games []rom.Game
}

func (f fixtureSource) Name() string        { return "fixture" }
func (f fixtureSource) Platforms() []string { return nil }
func (f fixtureSource) Search(_ context.Context, _ rom.Query) ([]rom.Game, error) {
	return f.games, nil
}

type noCache struct{}

func (noCache) Load(string) (rom.Listing, error) { return rom.Listing{}, context.Canceled }
func (noCache) Store(string, rom.Listing) error  { return nil }

type blockingLibrary struct {
	started chan struct{}
}

func (b *blockingLibrary) PlatformDir(p rom.Platform) (string, error) {
	return "/library/" + p.Slug, nil
}

func (b *blockingLibrary) Install(ctx context.Context, _ rom.InstallRequest, _ func(int64, int64)) (rom.InstallResult, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return rom.InstallResult{}, ctx.Err()
}

type recordingMetrics struct {
	keys []string
}

func (r *recordingMetrics) Inc(key string, _ int) {
	r.keys = append(r.keys, key)
}

type downloadFixture struct {
	listings  *catalog.Listings
	downloads *catalog.Downloads
	jobs      adapter.RomJobStore
	library   *blockingLibrary
}

func newDownloadFixture(t *testing.T, games ...rom.Game) downloadFixture {
	t.Helper()
	library := &blockingLibrary{started: make(chan struct{}, 1)}
	jobs := romjobs.New()
	downloads := catalog.NewDownloads(library, jobs, nil)
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })
	return downloadFixture{
		listings:  catalog.NewListings([]adapter.RomSource{fixtureSource{games: games}}, noCache{}, time.Hour),
		downloads: downloads,
		jobs:      jobs,
		library:   library,
	}
}

func awaitInstall(t *testing.T, library *blockingLibrary) {
	t.Helper()
	select {
	case <-library.started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not reach the library")
	}
}

func TestStartDownloadRejectsInvalidGamesBeforeAllocatingAJob(t *testing.T) {
	snes := rom.Game{ID: "ok", Platform: "snes", Title: "OK", Files: []rom.File{{Name: "OK.sfc", URL: "http://unused.invalid/ok"}}}
	noFile := rom.Game{ID: "nofile", Platform: "snes", Title: "No file"}
	tests := []struct {
		name    string
		gameID  string
		wantErr error
	}{
		{name: "unknown game", gameID: "missing", wantErr: rom.ErrGameNotFound},
		{name: "game without a file", gameID: "nofile", wantErr: rom.ErrNoDownloadFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newDownloadFixture(t, snes, noFile)
			ids := 0

			err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
				GameID:   tt.gameID,
				NewJobID: func() string { ids++; return "1" },
			})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, ids, "rejected request consumed a job ID")
			assert.Empty(t, fx.jobs.List())
		})
	}
}

func TestStartDownloadRejectsGamesOnUnknownPlatforms(t *testing.T) {
	game := rom.Game{ID: "odd", Platform: "nope", Title: "Nope Game", Files: []rom.File{{Name: "Nope.bin", URL: "http://unused.invalid/nope"}}}
	fx := newDownloadFixture(t, game)
	_, err := fx.listings.Games(context.Background(), "", "nope", 0, 10)
	require.NoError(t, err)
	ids := 0

	err = NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID:   "odd",
		NewJobID: func() string { ids++; return "1" },
	})

	require.ErrorIs(t, err, rom.ErrUnknownPlatform)
	assert.Zero(t, ids)
}

func TestStartDownloadQueuesAValidGame(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", Size: 7, URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	ids := 0

	err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID:   "g",
		NewJobID: func() string { ids++; return "1" },
	})

	require.NoError(t, err)
	assert.Equal(t, 1, ids)
	job, ok := fx.jobs.GetQueued("1")
	require.True(t, ok)
	assert.Equal(t, "queued", job.Status)
	assert.Equal(t, "g", job.GameID)
	assert.Equal(t, "G", job.Title)
	assert.Equal(t, int64(7), job.Total)
	awaitInstall(t, fx.library)
}

func TestStartDownloadRejectsNewDownloadsAfterShutdownWithoutAllocatingAJob(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	require.NoError(t, fx.downloads.Shutdown(context.Background()))
	ids := 0

	err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID:   "g",
		NewJobID: func() string { ids++; return "1" },
	})

	require.ErrorIs(t, err, rom.ErrShuttingDown)
	assert.Zero(t, ids)
}

func TestStartDownloadReportsDecoratorMetrics(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	metrics := &recordingMetrics{}
	handler := NewStartDownloadHandler(fx.listings, fx.downloads, metrics)

	require.ErrorIs(t, handler.Handle(context.Background(), StartDownload{GameID: "missing", NewJobID: func() string { return "x" }}), rom.ErrGameNotFound)
	require.NoError(t, handler.Handle(context.Background(), StartDownload{GameID: "g", NewJobID: func() string { return "1" }}))

	assert.Contains(t, metrics.keys, "commands.startdownload.failure")
	assert.Contains(t, metrics.keys, "commands.startdownload.success")
	awaitInstall(t, fx.library)
}
