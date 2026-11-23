package romlistings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"app.ddd/internal/domain/rom"
)

type flakySource struct {
	down bool
}

func (f *flakySource) Name() string        { return "flaky" }
func (f *flakySource) Platforms() []string { return nil }
func (f *flakySource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	if f.down {
		return nil, errors.New("upstream unavailable")
	}
	return []rom.Game{{ID: "old", Title: "Old"}}, nil
}

type memoryCache struct {
	listing rom.Listing
	ok      bool
}

func (m *memoryCache) Load(string) (rom.Listing, error) {
	if !m.ok {
		return rom.Listing{}, fmt.Errorf("cache: %w", fs.ErrNotExist)
	}
	return m.listing, nil
}

func (m *memoryCache) Store(string, rom.Listing) error { return nil }

type failingStoreCache struct{}

func (failingStoreCache) Load(string) (rom.Listing, error) {
	return rom.Listing{}, fmt.Errorf("cache: %w", fs.ErrNotExist)
}
func (failingStoreCache) Store(string, rom.Listing) error { return errors.New("disk full") }

type gameSource struct {
	games []rom.Game
}

func (g gameSource) Name() string        { return "fixture" }
func (g gameSource) Platforms() []string { return nil }
func (g gameSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return g.games, nil
}

type platformSource struct {
	platforms []string
	games     []rom.Game
	err       error
}

func (p platformSource) Name() string        { return "fixture" }
func (p platformSource) Platforms() []string { return p.platforms }
func (p platformSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.games, nil
}

type listingCache struct {
	mu       sync.Mutex
	listings map[string]rom.Listing
	loadErr  error
	storeErr error
}

func (c *listingCache) Load(platform string) (rom.Listing, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loadErr != nil {
		return rom.Listing{}, c.loadErr
	}
	listing, ok := c.listings[platform]
	if !ok {
		return rom.Listing{}, fmt.Errorf("cache %s: %w", platform, fs.ErrNotExist)
	}
	return listing, nil
}

func (c *listingCache) Store(platform string, listing rom.Listing) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.storeErr != nil {
		return c.storeErr
	}
	if c.listings == nil {
		c.listings = map[string]rom.Listing{}
	}
	c.listings[platform] = listing
	return nil
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
