package romcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestCacheKeepsLegacyListingFormat(t *testing.T) {
	dir := t.TempDir()
	fetched := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	c := New(dir)

	if err := c.Store("snes", rom.Listing{Fetched: fetched, Games: []rom.Game{{ID: "a", Title: "A"}}}); err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "snes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["fetched"]; !ok {
		t.Fatalf("cache file has no fetched key: %s", data)
	}
	var games []map[string]any
	if err := json.Unmarshal(raw["games"], &games); err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0]["ID"] != "a" || games[0]["Title"] != "A" {
		t.Errorf("games = %v, want untagged ID and Title keys", games)
	}

	got, err := c.Load("snes")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.Fetched.Equal(fetched) || len(got.Games) != 1 || got.Games[0].ID != "a" {
		t.Errorf("Load() = %+v, want the stored listing", got)
	}
}

func TestCacheLoadMissingFileFails(t *testing.T) {
	if _, err := New(t.TempDir()).Load("snes"); err == nil {
		t.Fatal("Load() error = nil for a missing listing")
	}
}
