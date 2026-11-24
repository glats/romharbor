package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmock "app.ddd/internal/app/mock"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

type brokenListings struct {
	games    []rom.Game
	gamesErr error
	gameErr  error
}

func (l *brokenListings) PlatformCounts(context.Context) ([]rom.PlatformCount, error) {
	return nil, nil
}

func (l *brokenListings) SearchSources(ctx context.Context, _, platform string) (rom.GamePage, error) {
	return l.ObtainPlatformListing(ctx, platform)
}

func (l *brokenListings) ObtainPlatformListing(_ context.Context, platform string) (rom.GamePage, error) {
	if l.gamesErr != nil {
		return rom.GamePage{}, l.gamesErr
	}
	if _, ok := rom.PlatformBySlug(platform); platform != "" && !ok {
		return rom.GamePage{}, rom.ErrUnknownPlatform
	}
	return rom.GamePage{Games: l.games}, nil
}

func (l *brokenListings) Game(_ context.Context, id string) (rom.Game, bool, error) {
	if l.gameErr != nil {
		return rom.Game{}, false, l.gameErr
	}
	for _, game := range l.games {
		if game.ID == id {
			return game, true, nil
		}
	}
	return rom.Game{}, false, nil
}

type brokenJobs struct {
	getErr  error
	listErr error
}

func (j brokenJobs) Get(context.Context, string) (rom.Job, bool, error) {
	return rom.Job{}, false, j.getErr
}

func (j brokenJobs) List(context.Context) ([]rom.Job, error) { return nil, j.listErr }

type brokenLibrary struct {
	dirErr error
}

func (l brokenLibrary) PlatformDir(rom.Platform) (string, error) { return "", l.dirErr }

func (l brokenLibrary) Install(context.Context, rom.InstallRequest, func(int64, int64)) (rom.InstallResult, error) {
	return rom.InstallResult{}, errors.New("install not supported")
}

type brokenDownloads struct {
	err error
}

func (d brokenDownloads) Start(context.Context, rom.InstallRequest) (rom.Job, error) {
	return rom.Job{}, d.err
}

func newBrokenCatalogHandler(t *testing.T, listings *brokenListings, jobs brokenJobs, library brokenLibrary, downloads brokenDownloads) (http.Handler, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	application := service.NewApp(appmock.NewUserRepository(t), listings, downloads, jobs, library)
	return web.NewHandler(application, web.WithLogger(slog.New(slog.NewTextHandler(logs, nil)))), logs
}

func TestCatalogRoutesHideTheCauseOfAnInternalFailureAndLogIt(t *testing.T) {
	cause := errors.New("upstream credential leaked")
	game := rom.Game{ID: "fixture:one", Platform: "snes", Title: "One", Files: []rom.File{{Name: "One.sfc"}}}
	gameRoute := "/game/" + encodeGameID(game.ID)
	withGame := &brokenListings{games: []rom.Game{game}}

	tests := []struct {
		name       string
		target     string
		post       bool
		listings   *brokenListings
		jobs       brokenJobs
		library    brokenLibrary
		downloads  brokenDownloads
		wantStatus int
		jsonBody   bool
	}{
		{name: "platform listings", target: "/platform/snes", listings: &brokenListings{gamesErr: cause}, wantStatus: http.StatusBadGateway},
		{name: "platform destination", target: "/platform/snes", listings: &brokenListings{}, library: brokenLibrary{dirErr: cause}, wantStatus: http.StatusInternalServerError},
		{name: "platform jobs", target: "/platform/snes", listings: &brokenListings{}, jobs: brokenJobs{listErr: cause}, wantStatus: http.StatusInternalServerError},
		{name: "game lookup", target: gameRoute, listings: &brokenListings{gameErr: cause}, wantStatus: http.StatusInternalServerError},
		{name: "game destination", target: gameRoute, listings: withGame, library: brokenLibrary{dirErr: cause}, wantStatus: http.StatusInternalServerError},
		{name: "game jobs", target: gameRoute, listings: withGame, jobs: brokenJobs{listErr: cause}, wantStatus: http.StatusInternalServerError},
		{name: "api games", target: "/api/games", listings: &brokenListings{gamesErr: cause}, wantStatus: http.StatusBadGateway, jsonBody: true},
		{name: "api game", target: "/api/game?id=" + game.ID, listings: &brokenListings{gameErr: cause}, wantStatus: http.StatusInternalServerError, jsonBody: true},
		{name: "api jobs", target: "/api/jobs", listings: &brokenListings{}, jobs: brokenJobs{listErr: cause}, wantStatus: http.StatusInternalServerError, jsonBody: true},
		{name: "api job", target: "/api/job?id=1", listings: &brokenListings{}, jobs: brokenJobs{getErr: cause}, wantStatus: http.StatusInternalServerError, jsonBody: true},
		{name: "api download", target: "/api/download", post: true, listings: &brokenListings{gameErr: cause}, wantStatus: http.StatusInternalServerError, jsonBody: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, logs := newBrokenCatalogHandler(t, tt.listings, tt.jobs, tt.library, tt.downloads)

			var rec *httptest.ResponseRecorder
			if tt.post {
				rec = postDownload(t, handler, game.ID)
			} else {
				rec = do(t, handler, http.MethodGet, tt.target)
			}

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.NotContains(t, rec.Body.String(), cause.Error())
			assert.Contains(t, logs.String(), cause.Error())
			assert.Contains(t, logs.String(), "request failed")
			if tt.jsonBody {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
				assert.NotEmpty(t, payload["error"])
			}
		})
	}
}

func TestCatalogKeepsTheStatusAndTextOfEveryExpectedDomainError(t *testing.T) {
	game := rom.Game{ID: "fixture:one", Platform: "snes", Title: "One", Files: []rom.File{{Name: "One.sfc"}}}
	bare := rom.Game{ID: "fixture:bare", Platform: "snes", Title: "Bare"}

	tests := []struct {
		name       string
		target     string
		post       bool
		postID     string
		listings   *brokenListings
		downloads  brokenDownloads
		wantStatus int
		wantBody   string
	}{
		{name: "missing game page", target: "/game/" + encodeGameID("fixture:missing"), listings: &brokenListings{}, wantStatus: http.StatusNotFound, wantBody: "game not found"},
		{name: "missing game api", target: "/api/game?id=fixture:missing", listings: &brokenListings{}, wantStatus: http.StatusNotFound, wantBody: `{"error":"game not found"}`},
		{name: "missing job api", target: "/api/job?id=missing", listings: &brokenListings{}, wantStatus: http.StatusNotFound, wantBody: `{"error":"job not found"}`},
		{name: "unknown platform api", target: "/api/games?platform=nope", listings: &brokenListings{}, wantStatus: http.StatusBadRequest, wantBody: `{"error":"unknown platform"}`},
		{name: "download without file", post: true, postID: "fixture:bare", listings: &brokenListings{games: []rom.Game{bare}}, wantStatus: http.StatusBadRequest, wantBody: `{"error":"game has no file"}`},
		{name: "download of a missing game", post: true, postID: "fixture:missing", listings: &brokenListings{}, wantStatus: http.StatusNotFound, wantBody: `{"error":"game not found"}`},
		{name: "download while shutting down", post: true, listings: &brokenListings{games: []rom.Game{game}}, downloads: brokenDownloads{err: rom.ErrShuttingDown}, wantStatus: http.StatusServiceUnavailable, wantBody: `{"error":"service is shutting down"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newBrokenCatalogHandler(t, tt.listings, brokenJobs{}, brokenLibrary{}, tt.downloads)

			var rec *httptest.ResponseRecorder
			if tt.post {
				postID := tt.postID
				if postID == "" {
					postID = game.ID
				}
				rec = postDownload(t, handler, postID)
			} else {
				rec = do(t, handler, http.MethodGet, tt.target)
			}

			require.Equal(t, tt.wantStatus, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.wantBody)
		})
	}
}

func TestCatalogGamesRejectsInvalidPagingParameters(t *testing.T) {
	handler, _ := newBrokenCatalogHandler(t, &brokenListings{}, brokenJobs{}, brokenLibrary{}, brokenDownloads{})

	tests := []struct {
		target string
		want   string
	}{
		{target: "/api/games?offset=abc", want: `{"error":"invalid offset"}`},
		{target: "/api/games?offset=-1", want: `{"error":"invalid offset"}`},
		{target: "/api/games?offset=1.5", want: `{"error":"invalid offset"}`},
		{target: "/api/games?limit=abc", want: `{"error":"invalid limit"}`},
		{target: "/api/games?limit=-1", want: `{"error":"invalid limit"}`},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := do(t, handler, http.MethodGet, tt.target)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.JSONEq(t, tt.want, rec.Body.String())
		})
	}
}

func TestCatalogGamesCapsTheLimitAtTheSourcePageSize(t *testing.T) {
	var games []rom.Game
	for i := range 300 {
		games = append(games, rom.Game{ID: fmt.Sprintf("g%03d", i), Title: fmt.Sprintf("G%03d", i)})
	}
	handler, _ := newBrokenCatalogHandler(t, &brokenListings{games: games}, brokenJobs{}, brokenLibrary{}, brokenDownloads{})
	count := func(target string) (int, string) {
		rec := do(t, handler, http.MethodGet, target)
		require.Equal(t, http.StatusOK, rec.Code)
		var body struct{ Games []rom.Game }
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		first := ""
		if len(body.Games) > 0 {
			first = body.Games[0].ID
		}
		return len(body.Games), first
	}

	n, _ := count("/api/games?limit=1000")
	assert.Equal(t, 200, n)
	n, _ = count("/api/games?limit=10")
	assert.Equal(t, 10, n)
	n, _ = count("/api/games")
	assert.Equal(t, 50, n)
	_, first := count("/api/games?offset=5")
	assert.Equal(t, "g005", first)
}

func TestCatalogDownloadAnswersAMalformedBodyWithoutTheDecoderText(t *testing.T) {
	for _, body := range []string{`{`, `{"id":5}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			handler, logs := newBrokenCatalogHandler(t, &brokenListings{}, brokenJobs{}, brokenLibrary{}, brokenDownloads{})
			req := httptest.NewRequest(http.MethodPost, "/api/download", bytes.NewBufferString(body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, `{"error":"invalid request body"}`, rec.Body.String())
			assert.Contains(t, logs.String(), "request failed")
		})
	}
}
