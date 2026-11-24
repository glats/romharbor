package command

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/adapter/sqlite"
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
	listings  *romlistings.Listings
	downloads *romdownloads.Downloads
	jobs      *sqlite.JobStore
	library   *blockingLibrary
}

func newDownloadFixture(t *testing.T, games ...rom.Game) downloadFixture {
	t.Helper()
	library := &blockingLibrary{started: make(chan struct{}, 1)}
	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, sqlite.Migrate(t.Context(), path))
	db, err := sqlite.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	jobs := sqlite.NewJobStore(db)
	downloads := romdownloads.NewDownloads(library, romdownloads.JobPorts{
		Producer:    jobs,
		Worker:      jobs,
		Maintenance: jobs,
	}, romdownloads.DefaultPolicy())
	require.NoError(t, downloads.Resume(context.Background()))
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })
	return downloadFixture{
		listings:  romlistings.NewListings([]adapter.RomSource{fixtureSource{games: games}}, noCache{}, time.Hour),
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

func TestStartDownloadRejectsInvalidGamesBeforeQueueingAJob(t *testing.T) {
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
			queued := 0

			err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
				GameID: tt.gameID,
				Queued: func(rom.Job) { queued++ },
			})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, queued, "rejected request reported a queued job")
			jobs, listErr := fx.jobs.List(context.Background())
			require.NoError(t, listErr)
			assert.Empty(t, jobs)
		})
	}
}

func TestStartDownloadRejectsGamesOnUnknownPlatforms(t *testing.T) {
	game := rom.Game{ID: "odd", Platform: "nope", Title: "Nope Game", Files: []rom.File{{Name: "Nope.bin", URL: "http://unused.invalid/nope"}}}
	fx := newDownloadFixture(t, game)
	_, err := fx.listings.SearchSources(context.Background(), "nope", "")
	require.NoError(t, err)
	queued := 0

	err = NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID: "odd",
		Queued: func(rom.Job) { queued++ },
	})

	require.ErrorIs(t, err, rom.ErrUnknownPlatform)
	assert.Zero(t, queued)
}

func TestStartDownloadQueuesAValidGame(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", Size: 7, URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	var job rom.Job

	err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID: "g",
		Queued: func(queued rom.Job) { job = queued },
	})

	require.NoError(t, err)
	assert.Equal(t, "1", job.ID)
	assert.Equal(t, rom.JobQueued, job.Status)
	assert.Equal(t, "g", job.GameID)
	assert.Equal(t, "G", job.Title)
	assert.Equal(t, int64(7), job.Total)
	awaitInstall(t, fx.library)
}

func TestStartDownloadRejectsNewDownloadsAfterShutdownWithoutQueueingAJob(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	require.NoError(t, fx.downloads.Shutdown(context.Background()))
	queued := 0

	err := NewStartDownloadHandler(fx.listings, fx.downloads, metric.NoOp{}).Handle(context.Background(), StartDownload{
		GameID: "g",
		Queued: func(rom.Job) { queued++ },
	})

	require.ErrorIs(t, err, rom.ErrShuttingDown)
	assert.Zero(t, queued)
}

type failingListings struct {
	err error
}

func (f failingListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return nil, f.err
}

func (f failingListings) ObtainPlatformListing(context.Context, string) (rom.GamePage, error) {
	return rom.GamePage{}, f.err
}

func (f failingListings) SearchSources(context.Context, string, string) (rom.GamePage, error) {
	return rom.GamePage{}, f.err
}

func (f failingListings) Game(context.Context, string) (rom.Game, bool, error) {
	return rom.Game{}, false, f.err
}

type failingDownloads struct {
	err error
}

func (f failingDownloads) Start(context.Context, rom.InstallRequest) (rom.Job, error) {
	return rom.Job{}, f.err
}

func TestStartDownloadReportsListingFailuresInsteadOfGameNotFound(t *testing.T) {
	cause := errors.New("source unavailable")
	handler := NewStartDownloadHandler(failingListings{err: cause}, failingDownloads{err: errors.New("downloads not reached")}, metric.NoOp{})
	queued := 0

	err := handler.Handle(context.Background(), StartDownload{GameID: "g", Queued: func(rom.Job) { queued++ }})

	require.ErrorIs(t, err, cause)
	assert.NotErrorIs(t, err, rom.ErrGameNotFound)
	assert.Zero(t, queued, "a failed lookup reported a queued job")
}

func TestStartDownloadReportsDecoratorMetrics(t *testing.T) {
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}}}
	fx := newDownloadFixture(t, game)
	metrics := &recordingMetrics{}
	handler := NewStartDownloadHandler(fx.listings, fx.downloads, metrics)

	require.ErrorIs(t, handler.Handle(context.Background(), StartDownload{GameID: "missing"}), rom.ErrGameNotFound)
	require.NoError(t, handler.Handle(context.Background(), StartDownload{GameID: "g"}))

	assert.Contains(t, metrics.keys, "commands.startdownload.failure")
	assert.Contains(t, metrics.keys, "commands.startdownload.success")
	awaitInstall(t, fx.library)
}
