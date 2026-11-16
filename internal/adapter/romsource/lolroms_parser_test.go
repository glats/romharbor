package romsource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

func newLolRomsSource(t *testing.T, handler http.HandlerFunc) *LolRoms {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	source := NewLolRoms(server.Client())
	source.BaseURL = server.URL
	return source
}

func serveLolRomsFixture(t *testing.T, name string) http.HandlerFunc {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "lolroms", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(body)
	}
}

func lolRomsRows(t *testing.T, source *LolRoms, games []rom.Game) []string {
	t.Helper()
	rows := make([]string, 0, len(games))
	for _, game := range games {
		href := strings.TrimPrefix(game.PageURL, source.BaseURL)
		if game.Source != "lolroms" || game.ID != "lolroms:"+href || len(game.Files) != 1 ||
			game.Files[0].URL != game.PageURL || !game.Files[0].Archive {
			t.Errorf("inconsistent game: %+v", game)
		}
		rows = append(rows, strings.Join([]string{game.Platform, game.Title, game.Region, href}, "|"))
	}
	return rows
}

func assertLolRomsRows(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("games mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestLolRomsParsesAnchorMarkupVariants(t *testing.T) {
	source := newLolRomsSource(t, serveLolRomsFixture(t, "markup_variants.html"))

	games, err := source.Search(context.Background(), rom.Query{Text: "super", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertLolRomsRows(t, lolRomsRows(t, source, games), []string{
		"snes|Super Mario World (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Super%20Mario%20World%20(USA).7z",
		"snes|Super Metroid (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Super%20Metroid%20(USA).zip",
		"snes|Donkey Kong Country (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Donkey%20Kong%20Country%20(USA).7z",
		"snes|Yoshis Island (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Yoshi%27s%20Island%20(USA).7z",
		"snes|Tom & Jerry (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Tom%20&%20Jerry%20(USA).7z",
		"snes|Star Fox (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Star%20Fox%20(USA).7z",
	})
}

func TestLolRomsIgnoresAnchorsOutsideMarkup(t *testing.T) {
	source := newLolRomsSource(t, serveLolRomsFixture(t, "non_anchor_markup.html"))

	games, err := source.Search(context.Background(), rom.Query{Text: "game", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertLolRomsRows(t, lolRomsRows(t, source, games), []string{
		"snes|Real Game (USA)|usa|/Nintendo/Super%20Nintendo%20Entertainment%20System/Real%20Game%20(USA).7z",
	})
}

func TestLolRomsFiltersAndPaginates(t *testing.T) {
	source := newLolRomsSource(t, serveLolRomsFixture(t, "listing_filters.html"))

	cases := []struct {
		name  string
		query rom.Query
		want  []string
	}{
		{
			name:  "all platforms in document order without duplicates",
			query: rom.Query{Text: "game", Limit: 10},
			want: []string{
				"gba|Metroid Fusion (USA)|usa|/Nintendo/Game%20Boy%20Advance/Metroid%20Fusion%20(USA).7z",
				"gba|Pokemon Emerald (Europe)|europe|/Nintendo/Game%20Boy%20Advance/Pokemon%20Emerald%20(Europe).zip",
				"n64|Mario 64 (USA).chd|usa|/Nintendo/Nintendo%2064/Mario%2064%20(USA).chd",
				"gb|Tetris (World)|world|/Nintendo/Game%20Boy/Tetris%20(World).ZIP",
				"gba|Mother 3 (Japan)|japan|/Nintendo/Game%20Boy%20Advance/Mother%203%20(Japan).7z",
			},
		},
		{
			name:  "platform filter",
			query: rom.Query{Text: "game", Platform: "gba", Limit: 10},
			want: []string{
				"gba|Metroid Fusion (USA)|usa|/Nintendo/Game%20Boy%20Advance/Metroid%20Fusion%20(USA).7z",
				"gba|Pokemon Emerald (Europe)|europe|/Nintendo/Game%20Boy%20Advance/Pokemon%20Emerald%20(Europe).zip",
				"gba|Mother 3 (Japan)|japan|/Nintendo/Game%20Boy%20Advance/Mother%203%20(Japan).7z",
			},
		},
		{
			name:  "offset and limit select a page",
			query: rom.Query{Text: "game", Platform: "gba", Offset: 1, Limit: 1},
			want: []string{
				"gba|Pokemon Emerald (Europe)|europe|/Nintendo/Game%20Boy%20Advance/Pokemon%20Emerald%20(Europe).zip",
			},
		},
		{
			name:  "offset past the last result is empty",
			query: rom.Query{Text: "game", Platform: "gba", Offset: 5, Limit: 10},
			want:  nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			games, err := source.Search(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			assertLolRomsRows(t, lolRomsRows(t, source, games), tc.want)
		})
	}
}

func TestLolRomsSendsSearchRequest(t *testing.T) {
	requests := make(chan *http.Request, 1)
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r
		w.Header().Set("Content-Type", "text/html")
	})

	if _, err := source.Search(context.Background(), rom.Query{Text: "super mario"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	var req *http.Request
	select {
	case req = <-requests:
	default:
		t.Fatal("search sent no HTTP request")
	}
	if req.Method != http.MethodGet || req.URL.Path != "/" {
		t.Errorf("request = %s %s, want GET /", req.Method, req.URL.Path)
	}
	if got := req.URL.Query().Get("q"); got != "super mario" {
		t.Errorf("q = %q, want %q", got, "super mario")
	}
	if got := req.Header.Get("User-Agent"); got != "Mozilla/5.0 (X11; Linux x86_64) romcatalog/1.0" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := req.Header.Get("Accept"); got != "text/html" {
		t.Errorf("Accept = %q, want text/html", got)
	}
}

func TestLolRomsRejectsEmptySearchWithoutRequest(t *testing.T) {
	requests := make(chan *http.Request, 1)
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r
	})

	games, err := source.Search(context.Background(), rom.Query{Platform: "snes"})
	if err == nil {
		t.Fatal("empty search term was accepted")
	}
	if games != nil {
		t.Errorf("games = %+v, want nil", games)
	}
	select {
	case <-requests:
		t.Fatal("empty search term sent an HTTP request")
	default:
	}
}

func TestLolRomsReportsErrorStatus(t *testing.T) {
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	games, err := source.Search(context.Background(), rom.Query{Text: "super mario"})
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want a 503 status error", err)
	}
	if games != nil {
		t.Errorf("games = %+v, want nil", games)
	}
}

func TestLolRomsHonorsCancellation(t *testing.T) {
	source := newLolRomsSource(t, serveLolRomsFixture(t, "markup_variants.html"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	games, err := source.Search(ctx, rom.Query{Text: "super mario"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if games != nil {
		t.Errorf("games = %+v, want nil", games)
	}
}

func TestLolRomsBoundsResponseBody(t *testing.T) {
	early := `<a href="/Nintendo/Game%20Boy/Early%20(USA).7z">Early (USA).7z</a>`
	late := `<a href="/Nintendo/Game%20Boy/Late%20(USA).7z">Late (USA).7z</a>`
	body := early + strings.Repeat(" ", 8<<20) + late
	source := newLolRomsSource(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})

	games, err := source.Search(context.Background(), rom.Query{Text: "game", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertLolRomsRows(t, lolRomsRows(t, source, games), []string{
		"gb|Early (USA)|usa|/Nintendo/Game%20Boy/Early%20(USA).7z",
	})
}
