package web_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

type fixtureSource struct {
	games []rom.Game
}

func (f fixtureSource) Name() string        { return "fixture" }
func (f fixtureSource) Platforms() []string { return nil }

func (f fixtureSource) Search(_ context.Context, q rom.Query) ([]rom.Game, error) {
	var out []rom.Game
	for _, g := range f.games {
		if q.Platform == "" || g.Platform == q.Platform {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f fixtureSource) ResolvePlatform(_ context.Context, id string) (string, bool, error) {
	for _, g := range f.games {
		if g.ID == id {
			return g.Platform, true, nil
		}
	}
	return "", false, nil
}

func newCatalogHandler(t *testing.T, library string, games ...rom.Game) http.Handler {
	t.Helper()
	return newCatalogHandlerWithCache(t, t.TempDir(), library, games...)
}

func newCatalogHandlerWithCache(t *testing.T, cache, library string, games ...rom.Game) http.Handler {
	t.Helper()
	return newCatalogHandlerWithLibrary(t, cache, &romlibrary.Library{Root: library}, games...)
}

func newCatalogHandlerWithLibrary(t *testing.T, cache string, library adapter.RomLibrary, games ...rom.Game) http.Handler {
	t.Helper()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobs := newJobs(t)
	listings := romlistings.NewListings([]adapter.RomSource{fixtureSource{games: games}}, romcache.New(cache), time.Hour)
	downloads := newDownloads(t, library, jobs, quiet)
	application := service.NewApp(appmock.NewUserRepository(t), listings, downloads, jobs)
	t.Cleanup(func() { _ = downloads.Shutdown(context.Background()) })

	return web.NewHandler(application, web.WithLogger(quiet))
}

func TestGamesRejectsUnknownPlatformsWithoutCacheAccess(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	outside := filepath.Join(root, "neighbor.json")
	before := []byte(`{"fetched":"2099-01-01T00:00:00Z","games":[{"ID":"private"}]}`)
	require.NoError(t, os.WriteFile(outside, before, 0o600))
	h := newCatalogHandlerWithCache(t, cache, filepath.Join(root, "roms"))
	for _, platform := range []string{"../neighbor", "../created", "nested/../../neighbor", `..\neighbor`, "unknown"} {
		for _, query := range []string{"", "mario"} {
			t.Run(platform+"/"+query, func(t *testing.T) {
				values := url.Values{"platform": {platform}, "q": {query}}
				rec := do(t, h, http.MethodGet, "/api/games?"+values.Encode())
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.JSONEq(t, `{"error":"unknown platform"}`, rec.Body.String())
				after, err := os.ReadFile(outside)
				require.NoError(t, err)
				require.Equal(t, before, after)
				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				require.Len(t, entries, 1)
			})
		}
	}
	for _, platform := range []string{"", "snes"} {
		require.Equal(t, http.StatusOK, do(t, h, http.MethodGet, "/api/games?platform="+platform).Code)
	}
}

func snesGame(id, title string, file rom.File) rom.Game {
	return rom.Game{ID: id, Source: "fixture", Platform: "snes", Title: title, Files: []rom.File{file}}
}

func encodeGameID(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func postDownload(t *testing.T, h http.Handler, gameID string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]string{"id": gameID})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/download", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, mod := range mods {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func waitForJob(t *testing.T, h http.Handler, id string, want string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		job := decodeJSON(t, do(t, h, http.MethodGet, "/api/job?id="+id))
		status, _ := job["status"].(string)
		if status == want {
			return job
		}
		if status == "done" || status == "failed" || time.Now().After(deadline) {
			t.Fatalf("job %s status = %q, want %q: %v", id, status, want, job)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	return out
}

func TestCatalogRootRendersPlatformPicker(t *testing.T) {
	rec := do(t, newCatalogHandler(t, t.TempDir()), http.MethodGet, "/")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Download ROMs, organized by platform")
	assert.Contains(t, rec.Body.String(), "Super Nintendo Entertainment System")
}

func TestCatalogRoutesAreMounted(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir())

	for _, target := range []string{"/platform/snes", "/api/platforms", "/api/games", "/api/jobs"} {
		rec := do(t, handler, http.MethodGet, target)
		assert.Equal(t, http.StatusOK, rec.Code, "%s must be mounted", target)
	}
}

func TestCatalogPlatformPageShowsLibraryFolder(t *testing.T) {
	root := t.TempDir()
	game := rom.Game{ID: "fixture:genesis", Platform: "genesis", Title: "Fixture game"}
	handler := newCatalogHandler(t, root, game)

	for _, route := range []string{"/platform/genesis", "/game/" + encodeGameID(game.ID)} {
		rec := do(t, handler, http.MethodGet, route)
		require.Equal(t, http.StatusOK, rec.Code, route)
		assert.Contains(t, rec.Body.String(), "<code>megadrive</code>", route)
		assert.Contains(t, rec.Body.String(), game.Title, route)
	}
}

func TestCatalogGameRouteDecodesBase64IDs(t *testing.T) {
	game := rom.Game{ID: "fixture:snes/game", Platform: "snes", Title: "Path Game"}
	handler := newCatalogHandler(t, t.TempDir(), game)

	assert.Equal(t, http.StatusOK, do(t, handler, http.MethodGet, "/game/"+encodeGameID(game.ID)).Code)
	assert.Equal(t, http.StatusBadRequest, do(t, handler, http.MethodGet, "/game/!!!").Code)
}

func TestCatalogAPIPlatformsKeepsLegacyJSONShape(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir())

	platforms := decodeJSON(t, do(t, handler, http.MethodGet, "/api/platforms"))["platforms"].([]any)
	var snes map[string]any
	for _, p := range platforms {
		if m := p.(map[string]any); m["Slug"] == "snes" {
			snes = m
		}
	}
	require.NotNil(t, snes)
	assert.Equal(t, "Super Nintendo Entertainment System", snes["Name"])
	assert.Contains(t, snes, "Cores")
	assert.Contains(t, snes, "count")
}

func TestCatalogAPIGamesKeepsLegacyJSONShape(t *testing.T) {
	game := rom.Game{ID: "fixture:1", Platform: "snes", Title: "Shape"}
	handler := newCatalogHandler(t, t.TempDir(), game)

	games := decodeJSON(t, do(t, handler, http.MethodGet, "/api/games?platform=snes"))["games"].([]any)
	require.Len(t, games, 1)
	first := games[0].(map[string]any)
	assert.Equal(t, "fixture:1", first["ID"])
	assert.Equal(t, "Shape", first["Title"])
	assert.Contains(t, first, "Files")
}

func TestCatalogDownloadInstallsVerifiedROM(t *testing.T) {
	body := []byte("0123456789abcdef")
	sum := md5.Sum(body)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(upstream.Close)
	root := t.TempDir()
	handler := newCatalogHandler(t, root, snesGame("fixture:game", "Game", rom.File{
		Name: "Game.sfc", Size: int64(len(body)), MD5: hex.EncodeToString(sum[:]), URL: upstream.URL,
	}))

	rec := postDownload(t, handler, "fixture:game")
	require.Equal(t, http.StatusAccepted, rec.Code)
	accepted := decodeJSON(t, rec)
	assert.Equal(t, "1", accepted["id"])
	assert.Equal(t, "fixture:game", accepted["game_id"])

	job := waitForJob(t, handler, "1", "done")
	want := filepath.Join(root, "snes", "Game.sfc")
	assert.Equal(t, "snes/Game.sfc", job["path"])
	got, err := os.ReadFile(want)
	require.NoError(t, err)
	assert.Equal(t, body, got)
	assert.NoFileExists(t, want+".part")
}

func TestCatalogDownloadJobIDsStaySequentialAfterRejections(t *testing.T) {
	body := []byte("abc")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(upstream.Close)
	handler := newCatalogHandler(t, t.TempDir(), snesGame("fixture:ok", "OK", rom.File{Name: "OK.sfc", URL: upstream.URL}))

	assert.Equal(t, http.StatusNotFound, postDownload(t, handler, "missing").Code)
	assert.Equal(t, http.StatusAccepted, postDownload(t, handler, "fixture:ok").Code)
	waitForJob(t, handler, "1", "done")
}

func TestCatalogDownloadChecksumMismatchInstallsNothing(t *testing.T) {
	body := []byte("checksum-me")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
	t.Cleanup(upstream.Close)
	root := t.TempDir()
	handler := newCatalogHandler(t, root, snesGame("fixture:bad", "Bad", rom.File{
		Name: "Bad.sfc", Size: int64(len(body)), MD5: strings.Repeat("0", 32), URL: upstream.URL,
	}))

	require.Equal(t, http.StatusAccepted, postDownload(t, handler, "fixture:bad").Code)
	job := waitForJob(t, handler, "1", "failed")
	assert.Contains(t, job["error"], "md5 checksum mismatch")

	target := filepath.Join(root, "snes", "Bad.sfc")
	assert.NoFileExists(t, target)
	assert.NoFileExists(t, target+".part")
}

func TestCatalogDownloadRejectsCrossOriginWrites(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir(), snesGame("fixture:x", "X", rom.File{Name: "X.sfc", URL: "http://unused.invalid/x"}))

	rec := postDownload(t, handler, "fixture:x", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, decodeJSON(t, do(t, handler, http.MethodGet, "/api/jobs"))["jobs"])
}

func TestCatalogDownloadRejectsGameWithoutFile(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir(), rom.Game{ID: "fixture:nofile", Platform: "snes", Title: "No file"})

	rec := postDownload(t, handler, "fixture:nofile")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "game has no file", decodeJSON(t, rec)["error"])
}

func TestCatalogWriteRejectsUnknownBody(t *testing.T) {
	handler := newCatalogHandler(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader("{"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "error")
}
