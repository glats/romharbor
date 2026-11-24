package web_test

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/app/command"
	appmock "app.ddd/internal/app/mock"
	"app.ddd/internal/app/query"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/domain/user"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestCompositionReusesConfiguredCollaborators(t *testing.T) {
	ctx := context.Background()
	cache := romcache.New(t.TempDir())
	game := rom.Game{
		ID: "warm", Platform: "snes", Title: "Warm",
		Files: []rom.File{{Name: "Warm.sfc"}},
	}
	require.NoError(t, cache.Store("snes", rom.Listing{
		Fetched: time.Now(), Games: []rom.Game{game},
	}))
	listings := romlistings.NewListings(nil, cache, 0)
	_, err := listings.ObtainPlatformListing(ctx, "snes")
	require.NoError(t, err)
	require.NoError(t, cache.Store("snes", rom.Listing{Fetched: time.Now()}))
	jobs := newJobs(t)
	downloads := newDownloads(t, nil, jobs, nil)
	require.NoError(t, downloads.Shutdown(ctx))
	application := service.NewApp(nil, listings, downloads, jobs, nil)
	games, err := application.Query.BrowsePlatformGames.Handle(ctx, query.BrowsePlatformGames{
		Platform: "snes", Limit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, rom.GamePage{Games: []rom.Game{game}}, games)
	err = application.Command.StartDownload.Handle(ctx, command.StartDownload{
		GameID: "warm",
		Queued: func(rom.Job) { t.Fatal("closed downloads queued a job") },
	})
	require.ErrorIs(t, err, rom.ErrShuttingDown)
	require.Empty(t, listJobs(t, jobs))
}

func TestSharedCompositionServesUsersAndCachedCatalog(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cache := romcache.New(root)
	require.NoError(t, cache.Store("snes", rom.Listing{
		Fetched: time.Now(),
		Games: []rom.Game{{
			ID: "fixture:one", Platform: "snes", Title: "Fixture",
		}},
	}))
	repo := appmock.NewUserRepository(t)
	repo.On("List", ctx).Return([]user.User{}, nil)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	jobs := newJobs(t)
	listings := romlistings.NewListings(nil, cache, 0)
	downloads := newDownloads(t, nil, jobs, quiet)
	application := service.NewApp(repo, listings, downloads, jobs, nil)
	t.Cleanup(func() { require.NoError(t, downloads.Shutdown(ctx)) })
	handler := web.NewHandler(application, web.WithLogger(quiet))
	for _, path := range []string{
		"/users", "/api/jobs", "/api/games?platform=snes",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, path)
		if path == "/api/games?platform=snes" {
			require.Contains(t, response.Body.String(), `"ID":"fixture:one"`)
		}
	}
	file := filepath.Join(root, "snes.json")
	before, err := os.ReadFile(file)
	require.NoError(t, err)
	for _, platform := range []string{"unknown", "..%2Fneighbor"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/games?platform="+platform, nil))
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Equal(t, "{\"error\":\"unknown platform\"}\n", response.Body.String())
		after, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	}
}
