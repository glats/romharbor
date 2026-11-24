package web_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/adapter/romlistings"
	appmock "app.ddd/internal/app/mock"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

type partialSource struct {
	name  string
	games []rom.Game
	err   error
}

func (s partialSource) Name() string        { return s.name }
func (s partialSource) Platforms() []string { return nil }

func (s partialSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.games, nil
}

func newPartialCatalogHandler(t *testing.T, sources ...adapter.RomSource) http.Handler {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobs := newJobs(t)
	listings := romlistings.NewListings(sources, romcache.New(t.TempDir()), time.Hour)
	library := &romlibrary.Library{Root: t.TempDir()}
	downloads := newDownloads(t, library, jobs, quiet)
	application := service.NewApp(appmock.NewUserRepository(t), listings, downloads, jobs)
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })
	return web.NewHandler(application, web.WithLogger(quiet))
}

func answeredGame() partialSource {
	return partialSource{name: "healthy", games: []rom.Game{{ID: "fixture:1", Platform: "snes", Title: "Answered"}}}
}

func brokenSource() partialSource {
	return partialSource{name: "broken", err: errors.New("upstream unavailable")}
}

func TestCatalogShowsTheNoticeAndTheGamesOfTheSourcesThatAnswered(t *testing.T) {
	handler := newPartialCatalogHandler(t, brokenSource(), answeredGame())

	rec := do(t, handler, http.MethodGet, "/platform/snes")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Some sources did not respond: broken. Showing results from the others.")
	assert.Contains(t, rec.Body.String(), "Answered")
}

func TestCatalogAPIGamesNamesTheFailedSources(t *testing.T) {
	handler := newPartialCatalogHandler(t, brokenSource(), answeredGame())

	rec := do(t, handler, http.MethodGet, "/api/games?platform=snes")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeJSON(t, rec)
	require.Len(t, body["games"], 1)
	assert.Equal(t, []any{"broken"}, body["failedSources"])
}

func TestCatalogAPIGamesOmitsTheFailedSourcesWhenEverySourceAnswers(t *testing.T) {
	handler := newPartialCatalogHandler(t, answeredGame())

	rec := do(t, handler, http.MethodGet, "/api/games?platform=snes")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	body := decodeJSON(t, rec)
	require.Len(t, body, 1)
	require.Len(t, body["games"], 1)
	assert.NotContains(t, body, "failedSources")
}
