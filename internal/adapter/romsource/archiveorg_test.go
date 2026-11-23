package romsource

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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

func TestArchiveOrgSkipsFilesWithAnUnparsableSizeAndKeepsTheRest(t *testing.T) {
	logs := captureLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{"name": "NES/Metroid (USA).zip", "size": 20480, "md5": "aaa111"},
				{"name": "NES/Mario (USA).zip", "size": "unknown", "md5": "bbb222"},
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
	if len(games) != 1 || games[0].Title != "Metroid (USA)" {
		t.Fatalf("games = %+v, want only the file with a parsable size", games)
	}
	if games[0].Size != 20480 {
		t.Errorf("size = %d, want 20480", games[0].Size)
	}
	if got := logLines(logs.String(), "Mario (USA).zip"); got != 1 {
		t.Fatalf("logged %d skipped files, want 1: %s", got, logs.String())
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

func logLines(logs string, needles ...string) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		matches := true
		for _, needle := range needles {
			if !strings.Contains(line, needle) {
				matches = false
				break
			}
		}
		if matches {
			count++
		}
	}
	return count
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}
