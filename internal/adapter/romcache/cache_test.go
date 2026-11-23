package romcache

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
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

func TestCacheLoadReportsCorruptAndUnreadableEntries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write func(t *testing.T, path string)
	}{
		{
			name: "corrupt content",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte(`{"fetched":"not-a-time","games":`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unreadable entry",
			write: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.write(t, filepath.Join(dir, "snes.json"))

			listing, err := New(dir).Load("snes")

			if listing.Games != nil {
				t.Errorf("Load() = %+v, want no listing", listing)
			}
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Load() error = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestCacheLoadMissingEntryIsNotCorrupt(t *testing.T) {
	_, err := New(t.TempDir()).Load("snes")
	if errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load() error = %v, a missing entry is not corrupt", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load() error = %v, want fs.ErrNotExist", err)
	}
}

func TestCacheRejectsPathsOutsideItsDirectory(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "neighbor.json")
	before := []byte(`{"fetched":"2026-11-14T10:00:00Z","games":[{"ID":"private"}]}`)
	if err := os.WriteFile(outside, before, 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(filepath.Join(root, "cache"))
	for _, key := range []string{"../neighbor", "nested/../../neighbor", `..\neighbor`, ".", ".."} {
		t.Run(key, func(t *testing.T) {
			if got, err := c.Load(key); err == nil {
				t.Errorf("Load(%q) = %+v without rejection", key, got)
			}
			if err := c.Store(key, rom.Listing{}); err == nil {
				t.Errorf("Store(%q) succeeded", key)
			}
			if got, err := os.ReadFile(outside); err != nil || !bytes.Equal(got, before) {
				t.Fatalf("neighbor changed: %q, %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(root, "cache")); !os.IsNotExist(err) {
				t.Errorf("rejection created cache state: %v", err)
			}
		})
	}
}
