package romsource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestArchiveOrgSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/metadata/item-one") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "NES/Super Mario Bros. (USA).zip", "size": 40960, "format": "ZIP", "md5": "abc123", "sha1": "def456"},
				{"name": "NES/Metroid (USA, Europe) (En).zip", "size": 20480, "format": "ZIP", "md5": "aaa111"},
				{"name": "BIOS/BIOS.zip", "size": 1024, "format": "ZIP", "md5": "bbb222"},
				{"name": "readme.txt", "size": 100, "format": "Text"},
			},
		})
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games, want 2: %+v", len(games), games)
	}
	if games[0].Title != "Metroid (USA, Europe) (En)" {
		t.Errorf("title = %q", games[0].Title)
	}
	if games[0].Region != "usa" {
		t.Errorf("region = %q", games[0].Region)
	}
	if games[0].Size != 20480 {
		t.Errorf("size = %d", games[0].Size)
	}
	if !games[0].Files[0].Archive {
		t.Error("zip file must be marked as archive")
	}
	if !strings.HasSuffix(games[0].Files[0].URL, "/download/item-one/NES%2FMetroid%20%28USA%2C%20Europe%29%20%28En%29.zip") {
		t.Errorf("url = %q", games[0].Files[0].URL)
	}
	if games[0].Files[0].MD5 != "aaa111" {
		t.Errorf("md5 = %q", games[0].Files[0].MD5)
	}
}

func TestArchiveOrgSearchFiltersByText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "Mario (USA).zip", "size": 1, "md5": "a"},
				{"name": "Zelda (USA).zip", "size": 2, "md5": "b"},
			},
		})
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes", Text: "zelda"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 || games[0].Title != "Zelda (USA)" {
		t.Fatalf("got %+v", games)
	}
}

func TestDlArchiveSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("action") {
		case "list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"items": []map[string]any{
					{
						"id":           "abc123",
						"filename":     "Super Mario World (U) [!].zip",
						"size":         347370,
						"mime":         "application/zip",
						"download_url": "https://example.invalid/smw.zip",
						"folder":       "Upload_Kazu/Beastrom/Super_Nintendo",
					},
					{
						"id":       "def456",
						"filename": "Some Game.nsp",
						"size":     1024,
						"folder":   "Upload_Kazu/Beastrom/Game_ROMS",
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	games, err := source.Search(context.Background(), rom.Query{Platform: "snes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1: %+v", len(games), games)
	}
	game := games[0]
	if game.Title != "Super Mario World (U) [!]" {
		t.Errorf("title = %q", game.Title)
	}
	if game.Platform != "snes" {
		t.Errorf("platform = %q", game.Platform)
	}
	if game.Files[0].URL != "https://example.invalid/smw.zip" {
		t.Errorf("url = %q", game.Files[0].URL)
	}
	if !game.Files[0].Archive {
		t.Error("zip must be marked as archive")
	}
}

func TestDlArchiveFolderPlatform(t *testing.T) {
	cases := map[string]string{
		"Upload_Kazu/Beastrom/Nintendo_DS":    "nds",
		"Upload_Kazu/Beastrom/Super_Nintendo": "snes",
		"Upload_Kazu/Beastrom/Game_ROMS":      "switch",
		"Upload_Kazu/Beastrom/2026/11/13":     "",
	}
	for folder, want := range cases {
		got, ok := dlArchiveFolderPlatform(folder)
		if want == "" {
			if ok {
				t.Errorf("folder %q must not match a platform, got %q", folder, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("folder %q = %q, want %q", folder, got, want)
		}
	}
}

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
