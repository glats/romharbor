package romcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestStoreRemovesTheTemporaryFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "snes.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := New(dir).Store("snes", rom.Listing{Fetched: time.Now(), Games: []rom.Game{{ID: "a"}}})

	if err == nil {
		t.Fatal("Store() error = nil, want the failed rename reported")
	}
	if got := cacheEntries(t, dir); len(got) != 1 || got[0] != "snes.json" {
		t.Errorf("cache directory holds %v, want only the target", got)
	}
}

func TestConcurrentStoreNeverPublishesAHalfWrittenListing(t *testing.T) {
	const (
		writers   = 8
		rounds    = 3
		gameCount = 400
	)

	dir := t.TempDir()
	cache := New(dir)
	path := filepath.Join(dir, "snes.json")

	title := strings.Repeat("t", 2048)
	games := make([]rom.Game, gameCount)
	for i := range games {
		games[i] = rom.Game{ID: fmt.Sprintf("game-%04d", i), Title: title}
	}

	stop := make(chan struct{})
	watched := make(chan error, 1)
	go func() { watched <- watchListing(path, gameCount+1, stop) }()

	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds)
	for w := range writers {
		wg.Go(func() {
			for r := range rounds {
				content := append([]rom.Game{{ID: fmt.Sprintf("writer-%d-%d", w, r)}}, games...)
				if err := cache.Store("snes", rom.Listing{Fetched: time.Now(), Games: content}); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	close(stop)

	for err := range errs {
		t.Errorf("Store() error = %v, want every concurrent store to succeed", err)
	}
	if err := <-watched; err != nil {
		t.Errorf("the cache file was published half-written: %v", err)
	}
	if got := cacheEntries(t, dir); len(got) != 1 || got[0] != "snes.json" {
		t.Errorf("cache directory holds %v, want only the target", got)
	}
}

func watchListing(path string, want int, stop <-chan struct{}) error {
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read the cache file: %w", err)
		}
		if len(data) == 0 {
			return errors.New("the cache file is empty")
		}
		var file listingFile
		if err := json.Unmarshal(data, &file); err != nil {
			return fmt.Errorf("the cache file is not a whole listing: %w", err)
		}
		if len(file.Games) != want {
			return fmt.Errorf("the cache file holds %d games, want %d", len(file.Games), want)
		}
	}
}

func cacheEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
