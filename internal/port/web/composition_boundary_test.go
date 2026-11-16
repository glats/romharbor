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
	"app.ddd/internal/adapter/romjobs"
	appmock "app.ddd/internal/app/mock"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/domain/user"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

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
	application, shutdown := service.NewApp(repo, service.CatalogDependencies{
		Cache: cache, Jobs: romjobs.New(), Logger: quiet,
	})
	t.Cleanup(func() { require.NoError(t, shutdown(ctx)) })
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
