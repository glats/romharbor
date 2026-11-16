package romcache

import (
	"encoding/json"
	"fmt"
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
	path, err := c.path(platform)
	if err != nil {
		return rom.Listing{}, err
	}
	data, err := os.ReadFile(path)
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
	path, err := c.path(platform)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(listingFile{Fetched: listing.Fetched, Games: listing.Games})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Cache) path(platform string) (string, error) {
	if _, ok := rom.PlatformBySlug(platform); platform != "" && !ok {
		return "", fmt.Errorf("unknown cache platform %q", platform)
	}
	return filepath.Join(c.dir, platform+".json"), nil
}
