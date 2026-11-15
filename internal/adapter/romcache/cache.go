package romcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"app.ddd/internal/domain/rom"
)

type Cache struct {
	dir string
}

type listingFile struct {
	Fetched time.Time  `json:"fetched"`
	Games   []rom.Game `json:"games"`
}

func New(dir string) *Cache {
	return &Cache{dir: dir}
}

func (c *Cache) Load(platform string) (rom.Listing, error) {
	data, err := os.ReadFile(c.path(platform))
	if err != nil {
		return rom.Listing{}, err
	}
	var file listingFile
	if err := json.Unmarshal(data, &file); err != nil {
		return rom.Listing{}, err
	}
	return rom.Listing{Fetched: file.Fetched, Games: file.Games}, nil
}

func (c *Cache) Store(platform string, listing rom.Listing) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(listingFile{Fetched: listing.Fetched, Games: listing.Games})
	if err != nil {
		return err
	}
	tmp := c.path(platform) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path(platform))
}

func (c *Cache) path(platform string) string {
	return filepath.Join(c.dir, platform+".json")
}
