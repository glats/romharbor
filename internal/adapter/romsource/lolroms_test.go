package romsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"app.ddd/internal/domain/rom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLolRomsSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<div class="row">
<a href="/Nintendo/Super%20Nintendo%20Entertainment%20System/Super%20Mario%20World%20(USA).7z">Super Mario World (USA).7z</a><span class="file-meta">2026-02-26</span>
</div>
<div class="row">
<a href="/Nintendo/Super%20Nintendo%20Entertainment%20System/Speed%20Hacks/Fast%20Game.7z">Fast Game.7z</a>
</div>
<div class="row">
<a href="/Nintendo/Game%20Boy%20Advance/Metroid%20Fusion%20(USA).7z">Metroid Fusion (USA).7z</a>
</div>`))
	}))
	defer server.Close()

	source := NewLolRoms(server.Client())
	source.BaseURL = server.URL

	games, err := source.Search(context.Background(), rom.Query{Platform: "snes", Text: "super mario"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1: %+v", len(games), games)
	}
	game := games[0]
	if game.Title != "Super Mario World (USA)" {
		t.Errorf("title = %q", game.Title)
	}
	if game.Platform != "snes" {
		t.Errorf("platform = %q", game.Platform)
	}
	if game.Files[0].URL == "" || !game.Files[0].Archive {
		t.Errorf("file = %+v", game.Files[0])
	}
}

func TestLolRomsConsolePlatform(t *testing.T) {
	cases := map[string]string{
		"Super Nintendo Entertainment System": "snes",
		"Game Boy Advance":                    "gba",
		"Nintendo Entertainment System":       "nes",
		"Unknown Console":                     "",
	}
	for console, want := range cases {
		got, ok := lolRomsConsolePlatform(console)
		if want == "" {
			if ok {
				t.Errorf("console %q must not match, got %q", console, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("console %q = %q, want %q", console, got, want)
		}
	}
}

func TestLolRomsWithoutHTTPClientReportsErrorAndSendsNothing(t *testing.T) {
	for name, source := range map[string]*LolRoms{
		"constructor": NewLolRoms(nil),
		"field":       {BaseURL: "https://provider.invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			games, err := source.Search(context.Background(), rom.Query{Text: "mario"})
			if err == nil || err.Error() != "lolroms: no HTTP client configured" {
				t.Fatalf("err = %v, want the no-client error", err)
			}
			if games != nil {
				t.Errorf("games = %+v, want nil", games)
			}
		})
	}
}

func TestLolRomsBaseURLWithTrailingSlashSendsSingleSlash(t *testing.T) {
	for _, tt := range []struct {
		name, suffix, wantPath string
	}{
		{"no slash", "", "/"},
		{"trailing slash", "/", "/"},
		{"prefix no slash", "/prefix", "/prefix/"},
		{"prefix trailing slash", "/prefix/", "/prefix/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			paths := make(chan string, 1)
			source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
				paths <- r.URL.Path
			})
			source.BaseURL += tt.suffix
			if _, err := source.Search(context.Background(), rom.Query{Text: "mario"}); err != nil {
				t.Fatalf("search: %v", err)
			}
			if got := <-paths; got != tt.wantPath {
				t.Errorf("path = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestLolRomsMatchesArchiveExtensionsCaseInsensitively(t *testing.T) {
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="/Nintendo/Game%20Boy/Upper%20(USA).7Z">a</a><a href="/Nintendo/Game%20Boy/Text%20(USA).txt">b</a>`))
	})
	games, err := source.Search(context.Background(), rom.Query{Text: "a"})
	if err != nil || len(games) != 1 || games[0].Files[0].Name != "Upper (USA).7Z" {
		t.Fatalf("games = %+v err = %v", games, err)
	}
}

func TestLolRomsSkipsEntriesThatCannotBeDecoded(t *testing.T) {
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="/Nintendo/Game%20Boy/Good%20Game%20(USA).7z">Good</a>` +
			`<a href="/Nintendo/Game%20Boy/Bad%zz.7z">Bad</a>` +
			`<a href="/Nintendo/Game%zz/Other%20Game.7z">Other</a>`))
	})

	games, err := source.Search(context.Background(), rom.Query{Text: "game", Platform: "gb", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 || games[0].Title != "Good Game (USA)" {
		t.Fatalf("games = %+v, want only the decodable entry", games)
	}
}

func TestLolRomsReportsAnErrorWhenEveryEntryCannotBeDecoded(t *testing.T) {
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="/Nintendo/Game%20Boy/Bad%zz.7z">Bad</a>`))
	})

	games, err := source.Search(context.Background(), rom.Query{Text: "game", Platform: "gb", Limit: 10})

	if err == nil {
		t.Fatal("search reported no error for a listing with no decodable entry")
	}
	if games != nil {
		t.Errorf("games = %+v, want nil", games)
	}
}

func TestLolRomsKnowsTheConsoleFoldersTheSiteActuallyUses(t *testing.T) {
	cases := map[string]string{
		"PlayStation":          "psx",
		"PlayStation 2":        "ps2",
		"PlayStation Portable": "psp",
		"Super Famicom":        "snes",
		"Mega Drive - Genesis": "genesis",
		"Nintendo 64":          "n64",
		"DS":                   "nds",
		"Saturn":               "saturn",
		"DreamCast":            "dc",
		"GameCube":             "ngc",
		"Wii":                  "wii",
		"2600":                 "atari2600",
		"Discs":                "",
		"CDN":                  "",
		"T-En":                 "",
	}
	for console, want := range cases {
		got, ok := lolRomsConsolePlatform(console)
		if want == "" {
			assert.Falsef(t, ok, "console %q must not match, got %q", console, got)
			continue
		}
		assert.Truef(t, ok, "console %q must match", console)
		assert.Equalf(t, want, got, "console %q", console)
	}
}

func TestLolRomsFindsAPlayStationGameUnderTheSonyFolder(t *testing.T) {
	source := newLolRomsSource(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<a href="/SONY/PlayStation/Driver%202%20(USA)%20(Disc%201).7z">Driver 2 (USA) (Disc 1).7z</a>
<a href="/SONY/PlayStation%20Portable/Race%20Driver%202006%20(USA).7z">Race Driver 2006 (USA).7z</a>
<a href="/Microsoft/Xbox/TOCA%20Race%20Driver%202%20(USA).7z">TOCA Race Driver 2 (USA).7z</a>`))
	})

	games, err := source.Search(context.Background(), rom.Query{Platform: "psx", Text: "driver 2"})

	require.NoError(t, err)
	require.Len(t, games, 1)
	assert.Equal(t, "psx", games[0].Platform)
	assert.Equal(t, "Driver 2 (USA) (Disc 1)", games[0].Title)
}
