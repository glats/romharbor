package romsource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"app.ddd/internal/adapter/romsource/dlarchiveapi"
	"app.ddd/internal/domain/rom"
)

// DlArchive serves games from the dlarchive index, a JSON listing of a
// personal file host that groups ROMs by platform folder.
type DlArchive struct {
	HTTPClient *http.Client
	BaseURL    string
	// TTL is how long the index stays cached in memory.
	TTL time.Duration

	mu      sync.Mutex
	index   []dlarchiveapi.DlArchiveItem
	fetched time.Time
}

// NewDlArchive returns the dlarchive source.
func NewDlArchive(client *http.Client) *DlArchive {
	if client == nil {
		client = http.DefaultClient
	}
	return &DlArchive{
		HTTPClient: client,
		BaseURL:    "https://uptobox.dlarchive.my.id",
		TTL:        12 * time.Hour,
	}
}

// Name identifies the source.
func (d *DlArchive) Name() string { return "dlarchive" }

// Platforms lists the platforms the index folders cover.
func (d *DlArchive) Platforms() []string {
	out := make([]string, 0, len(dlArchiveFolders))
	for slug := range dlArchiveFolders {
		out = append(out, slug)
	}
	return out
}

// dlArchiveFolders maps a platform slug to the folder tokens that hold it.
var dlArchiveFolders = map[string][]string{
	"nes":     {"Nintendo_Nes", "Nintendo_Entertainment_System", "Nes"},
	"snes":    {"Super_Nintendo", "Super_Nintendo_Entertainment_System", "Snes"},
	"gb":      {"Gameboy", "Game_Boy"},
	"gbc":     {"Gameboy_Color", "Game_Boy_Color"},
	"gba":     {"Gameboy_Advance", "Game_Boy_Advance"},
	"genesis": {"Sega_Mega_Drive", "Mega_Drive", "Genesis", "Sega_Genesis"},
	"n64":     {"Nintendo_64", "Nintend_64"},
	"nds":     {"Nintendo_DS", "Nds", "Nintendo_DS_Lite"},
	"psx":     {"Playstation", "Playstation_1"},
	"ps2":     {"Playstation_2", "Ps2"},
	"psp":     {"Playstation_Portable", "Psp"},
	"switch":  {"Game_ROMS", "Nintendo_Switch", "Switch"},
}

// dlArchiveFolderPlatform returns the platform of a folder, by its last
// segment, and whether the folder holds a game at all.
func dlArchiveFolderPlatform(folder string) (string, bool) {
	segments := strings.Split(folder, "/")
	last := strings.ToLower(segments[len(segments)-1])
	for slug, tokens := range dlArchiveFolders {
		for _, token := range tokens {
			if strings.EqualFold(last, token) {
				return slug, true
			}
		}
	}
	return "", false
}

// Search returns the games of a platform that match the query.
func (d *DlArchive) Search(ctx context.Context, q rom.Query) ([]rom.Game, error) {
	index, err := d.items(ctx)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(q.Text)

	var games []rom.Game
	for _, item := range index {
		slug, ok := dlArchiveFolderPlatform(item.Folder)
		if !ok || (q.Platform != "" && slug != q.Platform) {
			continue
		}
		title := rom.CleanTitle(item.Filename)
		if needle != "" && !strings.Contains(strings.ToLower(title), needle) {
			continue
		}
		games = append(games, rom.Game{
			ID:       "dlarchive:" + item.Id,
			Source:   "dlarchive",
			Platform: slug,
			Title:    title,
			Region:   rom.RegionOf(title),
			Size:     item.Size,
			PageURL:  d.BaseURL + "/?id=" + url.QueryEscape(item.Id),
			Files: []rom.File{{
				Name:    item.Filename,
				Size:    item.Size,
				URL:     item.DownloadUrl,
				Archive: rom.IsArchive(item.Filename),
			}},
		})
	}
	return rom.Page(games, q.Offset, q.Limit), nil
}

// items returns the index, fetching it when it is missing or stale.
func (d *DlArchive) items(ctx context.Context) ([]dlarchiveapi.DlArchiveItem, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.index != nil && time.Since(d.fetched) < d.TTL {
		return d.index, nil
	}

	var (
		items  []dlarchiveapi.DlArchiveItem
		offset int
	)
	for {
		limit := 200
		var page dlarchiveapi.DlArchiveListResponse
		params := &dlarchiveapi.FilesApiParams{Action: dlarchiveapi.List, Limit: &limit, Offset: &offset}
		if err := d.filesAPI(ctx, params, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}
	d.index = items
	d.fetched = time.Now()
	return items, nil
}

// FileURL resolves the direct download URL of an item.
func (d *DlArchive) FileURL(ctx context.Context, id string) (string, error) {
	var answer dlarchiveapi.DlArchiveFileURLResponse
	params := &dlarchiveapi.FilesApiParams{Action: dlarchiveapi.FileUrl, Id: &id}
	if err := d.filesAPI(ctx, params, &answer); err != nil {
		return "", err
	}
	if !answer.Ok || answer.Url == "" {
		return "", fmt.Errorf("dlarchive: no download URL for %s", id)
	}
	return answer.Url, nil
}

func (d *DlArchive) filesAPI(ctx context.Context, params *dlarchiveapi.FilesApiParams, target any) error {
	client, err := dlarchiveapi.NewClient(d.BaseURL+"/", dlarchiveapi.WithHTTPClient(d.HTTPClient))
	if err != nil {
		return err
	}
	var endpoint string
	capture := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Accept", "application/json")
		endpoint = req.URL.String()
		return nil
	}
	resp, err := client.FilesApi(ctx, params, capture)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dlarchive: %s: %s", path.Base(endpoint), resp.Status)
	}
	return legacyDecodeError(json.NewDecoder(resp.Body).Decode(target))
}
