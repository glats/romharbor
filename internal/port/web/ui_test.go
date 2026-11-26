package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
	"app.ddd/internal/service"
)

type uiListings struct {
	games []rom.Game
}

func (l uiListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return []rom.PlatformCount{{
		Platform: rom.Platform{Slug: "snes", Name: "Super Nintendo Entertainment System", Cores: []string{"snes9x"}},
		Count:    len(l.games),
	}}, nil
}

func (l uiListings) ObtainPlatformListing(context.Context, string) (rom.GamePage, error) {
	return rom.GamePage{Games: l.games}, nil
}

func (l uiListings) SearchSources(_ context.Context, text, _ string) (rom.GamePage, error) {
	var out []rom.Game
	for _, g := range l.games {
		if strings.Contains(strings.ToLower(g.Title), strings.ToLower(text)) {
			out = append(out, g)
		}
	}
	return rom.GamePage{Games: out}, nil
}

func (l uiListings) Game(_ context.Context, id string) (rom.Game, bool, error) {
	for _, g := range l.games {
		if g.ID == id {
			return g, true, nil
		}
	}
	return rom.Game{}, false, nil
}

type uiJobs struct {
	jobs []rom.Job
}

func (s uiJobs) Get(_ context.Context, id string) (rom.Job, bool, error) {
	for _, j := range s.jobs {
		if j.ID == id {
			return j, true, nil
		}
	}
	return rom.Job{}, false, nil
}

func (s uiJobs) List(context.Context) ([]rom.Job, error) {
	return s.jobs, nil
}

func uiGame(id, title string) rom.Game {
	return rom.Game{
		ID:       id,
		Source:   "fixture",
		Platform: "snes",
		Title:    title,
		Region:   "USA",
		Size:     2048,
		Files:    []rom.File{{Name: title + ".sfc", Size: 2048}},
	}
}

func uiJob(id, title, status string, downloaded, total int64, path, failure string) rom.Job {
	return rom.Job{
		ID:         id,
		GameID:     "fixture:" + id,
		Title:      title,
		Status:     status,
		Downloaded: downloaded,
		Total:      total,
		Path:       path,
		Error:      failure,
		Started:    time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func newUIHandler(t *testing.T, jobs []rom.Job, games ...rom.Game) http.Handler {
	t.Helper()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	application := service.NewApp(nil, uiListings{games: games}, nil, uiJobs{jobs: jobs})
	return NewHandler(application, WithLogger(quiet))
}

func uiGet(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestCatalogPageCarriesTheThemeToggleAndTheBootstrapScript(t *testing.T) {
	rec := uiGet(t, newUIHandler(t, nil), "/")
	body := rec.Body.String()

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, body, `href="/static/catalog.css"`)
	assert.Contains(t, body, `id="theme-toggle"`)
	assert.Contains(t, body, `aria-pressed="false"`)
	assert.Contains(t, body, `aria-label="Switch to dark theme"`)
	assert.Contains(t, body, `data-theme`)
	assert.Contains(t, body, `prefers-color-scheme`)
	assert.Contains(t, body, `romharbor-theme`)
	assert.NotContains(t, body, "<style>")
}

func TestStaticStylesheetAndScriptAreServedFromTheBinary(t *testing.T) {
	h := newUIHandler(t, nil)

	tests := []struct {
		path     string
		wantType string
		wants    []string
	}{
		{
			path:     "/static/catalog.css",
			wantType: "text/css",
			wants:    []string{"--bg:", `[data-theme="dark"]`, ".progress-bar", ".chip", ".badge"},
		},
		{
			path:     "/static/catalog.js",
			wantType: "javascript",
			wants:    []string{"/api/jobs", "If-None-Match", "jobsEndpoint", "visibilitychange", "/api/job/readiness", "/api/job/bundle", "Gameplay is not verified"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := uiGet(t, h, tt.path)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Header().Get("Content-Type"), tt.wantType)
			for _, want := range tt.wants {
				assert.Contains(t, rec.Body.String(), want)
			}
		})
	}
}

func TestDownloadsPanelIsOnEveryCatalogPageAndReusesTheJobsEndpoint(t *testing.T) {
	game := uiGame("fixture:one", "One")
	h := newUIHandler(t, []rom.Job{uiJob("1", "One", "done", 10, 10, "snes/One.sfc", "")}, game)

	targets := []string{"/", "/platform/snes", "/game/" + base64.RawURLEncoding.EncodeToString([]byte(game.ID))}

	for _, target := range targets {
		t.Run(target, func(t *testing.T) {
			body := uiGet(t, h, target).Body.String()

			assert.Contains(t, body, `id="downloads-panel"`)
			assert.Contains(t, body, `data-jobs-endpoint="/api/jobs"`)
			assert.Contains(t, body, `aria-live="polite"`)
			assert.Contains(t, body, `aria-controls="downloads-panel"`)
			assert.Contains(t, body, `id="downloads-count"`)
		})
	}
}

func TestDownloadsPanelShowsTheJobsWithTheirProgressAndOutcome(t *testing.T) {
	jobs := []rom.Job{
		uiJob("1", "Queued", "queued", 0, 100, "", ""),
		uiJob("2", "Running", "running", 40, 100, "", ""),
		uiJob("3", "Done", "done", 100, 100, "snes/Done.sfc", ""),
		uiJob("4", "Broken", "failed", 0, 100, "", "md5 checksum mismatch"),
	}
	body := uiGet(t, newUIHandler(t, jobs, uiGame("fixture:one", "One")), "/platform/snes").Body.String()

	assert.Contains(t, body, `data-job-id="1"`)
	assert.Contains(t, body, `data-job-id="4"`)
	assert.Contains(t, body, `chip-queued`)
	assert.Contains(t, body, `chip-running`)
	assert.Contains(t, body, `chip-done`)
	assert.Contains(t, body, `chip-failed`)
	assert.Contains(t, body, `role="progressbar"`)
	assert.Contains(t, body, `aria-valuenow="40"`)
	assert.Contains(t, body, `aria-valuenow="100"`)
	assert.Contains(t, body, "md5 checksum mismatch")
	assert.Contains(t, body, "<code>snes/Done.sfc</code>")
}

func TestDownloadsPanelListsTheNewestJobFirst(t *testing.T) {
	jobs := []rom.Job{
		uiJob("1", "Oldest", "done", 1, 1, "snes/Oldest.sfc", ""),
		uiJob("3", "Newest", "done", 1, 1, "snes/Newest.sfc", ""),
		uiJob("2", "Middle", "done", 1, 1, "snes/Middle.sfc", ""),
	}
	body := uiGet(t, newUIHandler(t, jobs), "/platform/snes").Body.String()

	newest := strings.Index(body, "Newest")
	middle := strings.Index(body, "Middle")
	oldest := strings.Index(body, "Oldest")
	require.Positive(t, newest)
	require.Positive(t, middle)
	require.Positive(t, oldest)
	assert.Less(t, newest, middle)
	assert.Less(t, middle, oldest)
}

func TestDownloadsPanelCountsTheActiveJobs(t *testing.T) {
	jobs := []rom.Job{
		uiJob("1", "Queued", "queued", 0, 100, "", ""),
		uiJob("2", "Running", "running", 40, 100, "", ""),
		uiJob("3", "Done", "done", 100, 100, "snes/Done.sfc", ""),
	}
	body := uiGet(t, newUIHandler(t, jobs), "/platform/snes").Body.String()

	assert.Contains(t, body, `id="downloads-count"`)
	assert.Regexp(t, `id="downloads-count"[^>]*>2<`, body)
}

func TestPlatformPageOffersTheFilterAndPaginationControls(t *testing.T) {
	games := []rom.Game{
		uiGame("fixture:one", "One"),
		uiGame("fixture:two", "Two"),
		uiGame("fixture:three", "Three"),
	}
	h := newUIHandler(t, nil, games...)

	body := uiGet(t, h, "/platform/snes").Body.String()
	assert.Contains(t, body, `name="q"`)
	assert.Contains(t, body, `name="limit"`)
	assert.Contains(t, body, `name="offset"`)
	assert.NotContains(t, body, `aria-label="Pagination"`)

	first := uiGet(t, h, "/platform/snes?limit=1").Body.String()
	assert.Contains(t, first, `aria-label="Pagination"`)
	assert.Contains(t, first, `rel="next"`)
	assert.NotContains(t, first, `rel="prev"`)

	middle := uiGet(t, h, "/platform/snes?limit=1&offset=1").Body.String()
	assert.Contains(t, middle, `rel="prev"`)
	assert.Contains(t, middle, `rel="next"`)
	assert.Contains(t, middle, "Two")
	assert.NotContains(t, middle, "Three")

	filtered := uiGet(t, h, "/platform/snes?q=two").Body.String()
	assert.Contains(t, filtered, "Two")
	assert.NotContains(t, filtered, "Three")

	limited := uiGet(t, h, "/platform/snes?limit=1").Body.String()
	assert.Equal(t, 1, strings.Count(limited, `class="card"`))
}

func TestCatalogPageKeepsTheLandmarksAndTheAccessibleNames(t *testing.T) {
	body := uiGet(t, newUIHandler(t, nil, uiGame("fixture:one", "One")), "/platform/snes").Body.String()

	assert.Contains(t, body, "<header")
	assert.Contains(t, body, "<main")
	assert.Contains(t, body, "<aside")
	assert.Contains(t, body, `for="catalog-search"`)
	assert.Contains(t, body, `id="catalog-search"`)
	assert.Contains(t, body, ">Search in Super Nintendo Entertainment System</label>")
	assert.Contains(t, body, `for="catalog-limit"`)
	assert.Contains(t, body, ">Results per page</label>")
	assert.Contains(t, body, `aria-label="Switch to dark theme"`)
	assert.Contains(t, body, `aria-label="Close downloads"`)
}

func TestCatalogPagesRenderOnlyTheirOwnSections(t *testing.T) {
	game := uiGame("fixture:one", "One")
	h := newUIHandler(t, nil, game)

	home := uiGet(t, h, "/").Body.String()
	assert.Contains(t, home, `action="/search"`)
	assert.NotContains(t, home, `class="card"`)
	assert.Contains(t, home, "Download ROMs, organized by platform")

	listing := uiGet(t, h, "/platform/snes").Body.String()
	assert.Contains(t, listing, `id="catalog-search"`)
	assert.Contains(t, listing, `class="card"`)

	detail := uiGet(t, h, "/game/"+base64.RawURLEncoding.EncodeToString([]byte(game.ID))).Body.String()
	assert.Contains(t, detail, `id="dl"`)
	assert.NotContains(t, detail, `class="card"`)
	assert.NotContains(t, detail, `name="limit"`)
	assert.NotContains(t, detail, "No games found for this platform.")
}

func TestGameCardRendersTheCompatibilityLabelsWhenTheViewModelCarriesThem(t *testing.T) {
	pages := parseCatalogPages()
	var body bytes.Buffer
	data := map[string]any{
		"ID":         "fixture:one",
		"Title":      "Labeled game",
		"Region":     "Europe",
		"Size":       int64(2048),
		"Cover":      "",
		"Labels":     []string{"pal", "heavy"},
		"Compatible": (*bool)(nil),
	}

	require.NoError(t, pages.ExecuteTemplate(&body, "game-card", data))

	assert.Contains(t, body.String(), `data-badge="pal"`)
	assert.Contains(t, body.String(), `data-badge="heavy"`)
	assert.Contains(t, body.String(), `data-compatibility-slot`)
}

func TestGameCardKeepsTheCompatibilitySlotEmptyWithoutLabels(t *testing.T) {
	pages := parseCatalogPages()
	var body bytes.Buffer
	data := map[string]any{
		"ID":         "fixture:one",
		"Title":      "Plain game",
		"Region":     "USA",
		"Size":       int64(2048),
		"Cover":      "",
		"Labels":     []string{},
		"Compatible": (*bool)(nil),
	}

	require.NoError(t, pages.ExecuteTemplate(&body, "game-card", data))

	assert.NotContains(t, body.String(), "data-badge=")
	assert.Contains(t, body.String(), `data-compatibility-slot`)
}

func TestCompatibilityBadgeRendersTheIncompatibleState(t *testing.T) {
	pages := parseCatalogPages()
	var body bytes.Buffer
	incompatible := false
	data := map[string]any{
		"Labels":     []string{},
		"Compatible": &incompatible,
	}

	require.NoError(t, pages.ExecuteTemplate(&body, "compatibility", data))

	assert.Contains(t, body.String(), `data-badge="incompatible"`)
}

func TestCompatibilityBadgeRendersTheCompatibleState(t *testing.T) {
	pages := parseCatalogPages()
	var body bytes.Buffer
	compatible := true
	data := map[string]any{
		"Labels":     []string{},
		"Compatible": &compatible,
	}

	require.NoError(t, pages.ExecuteTemplate(&body, "compatibility", data))

	assert.Contains(t, body.String(), `data-badge="compatible"`)
}

func TestCompatibilityBadgesComeFromTheClassifier(t *testing.T) {
	clean := uiGame("fixture:clean", "Clean Game (USA)")
	clean.Files = []rom.File{{Name: "Clean Game (USA).sfc", Size: 2048}}
	translated := uiGame("fixture:translated", "Other Game (Europe) [T+Spa1.0_Someone]")
	translated.Files = []rom.File{{Name: "Other Game (Europe) [T+Spa1.0_Someone].sfc", Size: 2048}}
	h := newUIHandler(t, nil, clean, translated)

	body := uiGet(t, h, "/platform/snes").Body.String()

	assert.Contains(t, body, `data-badge="compatible"`)
	assert.Contains(t, body, `data-badge="incompatible"`)
	assert.Contains(t, body, `data-badge="translation"`)
	assert.Contains(t, body, `data-badge="pal"`)
}

func TestTheGlobalSearchGroupsTheMatchesByPlatform(t *testing.T) {
	h := newUIHandler(t, nil, uiGame("fixture:one", "Mario World"), uiGame("fixture:two", "Zelda"))

	empty := uiGet(t, h, "/search")
	assert.Equal(t, http.StatusOK, empty.Code)
	assert.NotContains(t, empty.Body.String(), `class="card"`)

	found := uiGet(t, h, "/search?q=mario").Body.String()
	assert.Contains(t, found, "Mario World")
	assert.NotContains(t, found, "Zelda")
	assert.Contains(t, found, `href="/platform/snes?q=mario"`)

	none := uiGet(t, h, "/search?q=nothing").Body.String()
	assert.Contains(t, none, "No games matched.")
}

func TestHeavyLabelCarriesTheKnulliAdviceAsATooltip(t *testing.T) {
	pages := parseCatalogPages()
	var body bytes.Buffer
	data := map[string]any{
		"ID":         "fixture:one",
		"Title":      "Conker's Bad Fur Day (E) [!]",
		"Region":     "Europe",
		"Size":       int64(2048),
		"Cover":      "",
		"Labels":     []string{"heavy", "pal"},
		"Compatible": (*bool)(nil),
	}

	require.NoError(t, pages.ExecuteTemplate(&body, "game-card", data))

	assert.Contains(t, body.String(), `data-badge="heavy" title="Demanding on a handheld. On Knulli set the core to mupen64plus-next and keep the ROM as .z64; if a menu ignores the D-pad, map the D-pad to the analog stick."`)
	assert.NotContains(t, body.String(), `data-badge="pal" title=`)
}
