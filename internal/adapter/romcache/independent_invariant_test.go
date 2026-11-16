package romcache_test

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/domain/rom"
)

func TestIndependentNeighborInvariant(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "neighbor.json")
	b := []byte(`{"fetched":"2099-01-01T00:00:00Z","games":[{"ID":"private"}]}`)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c := romcache.New(filepath.Join(root, "cache"))
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	loaded, err := c.Load("../neighbor")
	if err == nil {
		t.Errorf("rejected read leaked %v", loaded)
	}
	if err := c.Store("../neighbor", rom.Listing{}); err == nil {
		t.Error("rejected store succeeded")
	}
	after, err := os.ReadFile(p)
	if err != nil || sha256.Sum256(b) != sha256.Sum256(after) {
		t.Errorf("neighbor hash changed: %v", err)
	}
}
