package web_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	appmock "app.ddd/internal/app/mock"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestUsersOnlyRootKeepsInheritedRedirect(t *testing.T) {
	handler := newHandler(t, appmock.NewUserRepository(t))

	rec := do(t, handler, http.MethodGet, "/")

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/users", rec.Header().Get("Location"))
	assert.Equal(t, "<a href=\"/users\">Found</a>.\n\n", rec.Body.String())
}

func TestCatalogErrorPagesKeepLegacyHeaders(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir())

	for _, tt := range []struct {
		target string
		status int
		body   string
	}{
		{target: "/game/!!!", status: http.StatusBadRequest, body: "bad game id"},
		{target: "/platform/not-real", status: http.StatusNotFound, body: "unknown platform not-real"},
	} {
		rec := do(t, handler, http.MethodGet, tt.target)

		assert.Equal(t, tt.status, rec.Code, tt.target)
		assert.Contains(t, rec.Body.String(), tt.body, tt.target)
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"), tt.target)
		assert.Empty(t, rec.Header().Values("Vary"), tt.target)
	}
}

func TestCatalogPageKeepsLegacyHeaders(t *testing.T) {
	rec := do(t, newCatalogHandler(t, t.TempDir()), http.MethodGet, "/")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Empty(t, rec.Header().Values("Vary"))
}

func TestUsersPagesKeepHTMXVary(t *testing.T) {
	repo := appmock.NewUserRepository(t)
	repo.EXPECT().List(mock.Anything).Return(nil, nil).Once()
	application := service.NewApp(repo, nil, nil, nil, nil)
	handler := web.NewHandler(application, web.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	rec := do(t, handler, http.MethodGet, "/users")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Values("Vary"), "HX-Request")
}

type failingWriter struct {
	header http.Header
}

func (f *failingWriter) Header() http.Header       { return f.header }
func (f *failingWriter) WriteHeader(int)           {}
func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("sink closed") }

type instantLibrary struct{}

func (instantLibrary) Install(_ context.Context, _ rom.InstallRequest, progress func(int64, int64)) (rom.InstallResult, error) {
	progress(42, 42)
	return rom.InstallResult{Path: "snes/Instant.sfc", Size: 42}, nil
}

func TestAcceptedDownloadReturnsTheQueuedSnapshotWhenTheWorkerFinishesFirst(t *testing.T) {
	game := snesGame("fixture:instant", "Instant", rom.File{Name: "Instant.sfc", Size: 42})
	handler := newCatalogHandlerWithLibrary(t, t.TempDir(), instantLibrary{}, game)

	rec := postDownload(t, handler, "fixture:instant")

	require.Equal(t, http.StatusAccepted, rec.Code)
	accepted := decodeJSON(t, rec)
	assert.Equal(t, "queued", accepted["status"])
	assert.Equal(t, float64(0), accepted["downloaded"])
	assert.Equal(t, float64(42), accepted["total"])
	assert.NotContains(t, accepted, "path")
	assert.NotContains(t, accepted, "error")
	assert.Equal(t, "0001-01-01T00:00:00Z", accepted["finished"])
	waitForJob(t, handler, "1", "done")
}

func TestCatalogJSONEncodeFailureIsLogged(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	handler := newCatalogHandler(t, t.TempDir())

	handler.ServeHTTP(&failingWriter{header: http.Header{}}, httptest.NewRequest(http.MethodGet, "/api/job?id=missing", nil))

	require.Contains(t, logs.String(), "write json")
	assert.Contains(t, logs.String(), "sink closed")
	assert.NotContains(t, logs.String(), "job not found")
}
