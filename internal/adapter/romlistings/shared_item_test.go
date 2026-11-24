package romlistings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/adapter/romsource"
)

func TestAnItemSharedByTwoPlatformsResolvesByTheFileExtension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[{"name":"folder/Game.sfc","size":"3"}]}`))
	}))
	defer server.Close()
	source := romsource.NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"shared"}, "snes": {"shared"}}
	listings := romlistings.NewListings([]adapter.RomSource{source}, romcache.New(t.TempDir()), time.Hour)
	game, found, err := listings.Game(context.Background(), "archive.org:shared/folder/Game.sfc")
	if err != nil || !found || game.Platform != "snes" {
		t.Fatalf("game=%+v found=%v error=%v", game, found, err)
	}
}
