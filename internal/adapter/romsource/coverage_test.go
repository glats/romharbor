package romsource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveOrgCuratesAnOpenSetForEveryPlatformItCovers(t *testing.T) {
	sets := NewArchiveOrg(nil).Sets
	want := map[string][]string{
		"psx":       {"ps1lollo"},
		"psp":       {"psp-cso-collection"},
		"arcade":    {"mame-2003-plus-reference-set"},
		"switch":    {"switch-dumps-nsp-xci", "switch-dump-nsp-xci", "friday-the-13th-the-game-v-0-010092-a-00-c-4-b-6000"},
		"psvita":    {"psv-homebrew", "psvita-homebrew-collection-1-14-2024"},
		"ngage":     {"Nokia_N-Gage_Mega_Collection_Mirror"},
		"ps2":       {"playstation2_essentials", "playstation2_essentials_part2", "playstation-2-game-dumps"},
		"ngc":       {"zoocube", "GameCube_rvz-english", "gamecubegames"},
		"wii":       {"Wii_ISO"},
		"saturn":    {"sega_saturn"},
		"dc":        {"DreamcastSelfBoot", "ef_SEGA_GDI_USA", "DreamcastCHD2020_0-O"},
		"atari2600": {"nointro.atari-2600"},
		"n3ds":      {"nintendo3dscias"},
	}
	for slug, items := range want {
		assert.Equal(t, items, sets[slug], slug)
	}
}

func archiveItemServer(t *testing.T, items map[string]map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		body, ok := items[item]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestArchiveOrgRefusesAnAccessRestrictedItemInsteadOfListingGamesThatCannotBeDownloaded(t *testing.T) {
	server := archiveItemServer(t, map[string]map[string]any{
		"locked": {
			"metadata": map[string]any{"access-restricted-item": "true"},
			"files":    []map[string]any{{"name": "Game (USA).iso", "size": 10}},
		},
	})
	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"ps2": {"locked"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "ps2"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "access-restricted")
	assert.Empty(t, games)
}

func TestArchiveOrgKeepsTheGamesOfTheItemsThatAnsweredWhenAnotherItemFails(t *testing.T) {
	server := archiveItemServer(t, map[string]map[string]any{
		"locked": {
			"metadata": map[string]any{"access-restricted-item": "true"},
			"files":    []map[string]any{{"name": "Locked (USA).iso", "size": 10}},
		},
		"open": {
			"files": []map[string]any{{"name": "Open (USA).iso", "size": 20}},
		},
	})
	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"ps2": {"locked", "open", "missing"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "ps2"})

	require.NoError(t, err)
	require.Len(t, games, 1)
	assert.Equal(t, "Open (USA)", games[0].Title)
}

func TestArchiveOrgReportsAFailureWhenEveryItemOfThePlatformFails(t *testing.T) {
	server := archiveItemServer(t, map[string]map[string]any{})
	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"ps2": {"missing-a", "missing-b"}}

	_, err := source.Search(context.Background(), rom.Query{Platform: "ps2"})

	require.Error(t, err)
}

func TestLolRomsListsOnlyThePlatformsWhoseDirectoryItBrowses(t *testing.T) {
	source := NewLolRoms(nil)

	assert.True(t, source.Lists("lynx"))
	assert.False(t, source.Lists("snes"))
	assert.False(t, source.Lists(""))
}

func TestLolRomsListsAPlatformDirectoryWhenNoTextIsGiven(t *testing.T) {
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="/Atari/Lynx/Klax%20(USA).7z">Klax (USA).7z</a>
<a href="/Atari/Lynx/Rampart%20(USA).7z">Rampart (USA).7z</a>
<a href="/Atari/Lynx/Homebrew/Other.7z">Other.7z</a>
<a href="/Atari/Lynx/readme.txt">readme.txt</a>`))
	}))
	defer server.Close()
	source := NewLolRoms(server.Client())
	source.BaseURL = server.URL

	games, err := source.Search(context.Background(), rom.Query{Platform: "lynx"})

	require.NoError(t, err)
	assert.Equal(t, "/Atari/Lynx/", requested)
	require.Len(t, games, 2)
	assert.Equal(t, "lynx", games[0].Platform)
	assert.Equal(t, "lolroms", games[0].Source)
	assert.Equal(t, "Klax (USA)", games[0].Title)
	assert.True(t, games[0].Files[0].Archive)
}

func TestLolRomsStillRequiresATermForAPlatformItDoesNotBrowse(t *testing.T) {
	source := NewLolRoms(nil)

	_, err := source.Search(context.Background(), rom.Query{Platform: "snes"})

	require.Error(t, err)
}
