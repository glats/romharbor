package romsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/adapter/romsource/dlarchiveapi"
	"app.ddd/internal/domain/rom"
)

type dlArchiveFlight[T any] struct {
	done  chan struct{}
	value T
	err   error
}

type singleflight[T any] struct {
	mu    sync.Mutex
	calls map[string]*dlArchiveFlight[T]
}

func (s *singleflight[T]) do(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[string]*dlArchiveFlight[T]{}
	}
	if call, ok := s.calls[key]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.value, call.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	call := &dlArchiveFlight[T]{done: make(chan struct{})}
	s.calls[key] = call
	s.mu.Unlock()

	call.value, call.err = fn(ctx)
	close(call.done)

	s.mu.Lock()
	delete(s.calls, key)
	s.mu.Unlock()
	return call.value, call.err
}

const dlArchiveMaxResponseBytes = 8 << 20

const dlArchiveIDPrefix = "dlarchive:"

// DlArchive serves games from the dlarchive index, a JSON listing of a
// personal file host that groups ROMs by platform folder.
type DlArchive struct {
	HTTPClient dlarchiveapi.HttpRequestDoer
	BaseURL    string
	// TTL is how long the index stays cached in memory.
	TTL time.Duration

	mu      sync.Mutex
	index   []dlarchiveapi.DlArchiveItem
	fetched time.Time

	flights singleflight[[]dlarchiveapi.DlArchiveItem]
}

// NewDlArchive returns the dlarchive source.
func NewDlArchive(client dlarchiveapi.HttpRequestDoer) *DlArchive {
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

	var games []rom.Game
	for _, item := range index {
		slug, ok := dlArchiveFolderPlatform(item.Folder)
		if !ok || (q.Platform != "" && slug != q.Platform) {
			continue
		}
		title := rom.CleanTitle(item.Filename)
		games = append(games, rom.Game{
			ID:       dlArchiveIDPrefix + item.Id,
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
	return rom.Page(rom.FilterByTitle(games, q.Text), q.Offset, q.Limit), nil
}

// items returns the index, fetching it when it is missing or stale.
func (d *DlArchive) items(ctx context.Context) ([]dlarchiveapi.DlArchiveItem, error) {
	if index, ok := d.cachedItems(); ok {
		return index, nil
	}
	return d.flights.do(ctx, "index", d.fetchItems)
}

func (d *DlArchive) cachedItems() ([]dlarchiveapi.DlArchiveItem, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.index != nil && time.Since(d.fetched) < d.TTL {
		return d.index, true
	}
	return nil, false
}

func (d *DlArchive) fetchItems(ctx context.Context) ([]dlarchiveapi.DlArchiveItem, error) {
	if index, ok := d.cachedItems(); ok {
		return index, nil
	}

	var (
		items  []dlarchiveapi.DlArchiveItem
		offset int
	)
	for {
		limit := 200
		params := &dlarchiveapi.FilesApiParams{Action: dlarchiveapi.List, Limit: &limit, Offset: &offset}
		body, err := d.filesAPI(ctx, params)
		if err != nil {
			return nil, err
		}
		page, err := body.AsDlArchiveListResponse()
		if err != nil {
			return nil, fmt.Errorf("dlarchive: %s: %w", params.Action, err)
		}
		refused, err := dlArchivePageRefused(body)
		if err != nil {
			return nil, fmt.Errorf("dlarchive: %s: %w", params.Action, err)
		}
		if refused {
			return nil, fmt.Errorf("dlarchive: %s: the provider answered ok=false", params.Action)
		}
		items = append(items, page.Items...)
		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}
	if len(items) == 0 {
		return items, nil
	}
	d.mu.Lock()
	d.index = items
	d.fetched = time.Now()
	d.mu.Unlock()
	return items, nil
}

func dlArchivePageRefused(body *dlarchiveapi.FilesApi200JSONResponseBody) (bool, error) {
	raw, err := body.MarshalJSON()
	if err != nil {
		return false, err
	}
	var page struct {
		Ok *bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return false, err
	}
	return page.Ok != nil && !*page.Ok, nil
}

func (d *DlArchive) ResolvePlatform(ctx context.Context, id string) (string, bool, error) {
	itemID, ok := strings.CutPrefix(id, dlArchiveIDPrefix)
	if !ok {
		return "", false, nil
	}
	index, err := d.items(ctx)
	if err != nil {
		return "", false, err
	}
	for _, item := range index {
		if item.Id != itemID {
			continue
		}
		platform, ok := dlArchiveFolderPlatform(item.Folder)
		return platform, ok, nil
	}
	return "", false, nil
}

// FileURL resolves the direct download URL of an item.
func (d *DlArchive) FileURL(ctx context.Context, id string) (string, error) {
	params := &dlarchiveapi.FilesApiParams{Action: dlarchiveapi.FileUrl, Id: &id}
	body, err := d.filesAPI(ctx, params)
	if err != nil {
		return "", err
	}
	answer, err := body.AsDlArchiveFileURLResponse()
	if err != nil {
		return "", fmt.Errorf("dlarchive: %s: %w", params.Action, err)
	}
	failure, err := body.AsDlArchiveErrorResponse()
	if err != nil {
		return "", fmt.Errorf("dlarchive: %s: %w", params.Action, err)
	}
	if !bool(failure.Ok) || answer.Url == "" {
		return "", fmt.Errorf("dlarchive: no download URL for %s", id)
	}
	return answer.Url, nil
}

func (d *DlArchive) filesAPI(ctx context.Context, params *dlarchiveapi.FilesApiParams) (*dlarchiveapi.FilesApi200JSONResponseBody, error) {
	if d.HTTPClient == nil {
		return nil, errors.New("dlarchive: no HTTP client is configured")
	}
	client, err := dlarchiveapi.NewClient(
		d.BaseURL,
		dlarchiveapi.WithHTTPClient(d.HTTPClient),
		dlarchiveapi.WithRequestEditorFn(romfetch.Headers(map[string]string{"Accept": "application/json"})),
	)
	if err != nil {
		return nil, fmt.Errorf("dlarchive: %s: %w", params.Action, err)
	}
	resp, err := client.FilesApi(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("dlarchive: %s: %w", params.Action, err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("dlarchive: %s: %s", params.Action, resp.Status)
	}
	resp.Body = boundBody(resp.Body, dlArchiveMaxResponseBytes)
	forceJSONContentType(resp.Header)
	parsed, err := dlarchiveapi.ParseFilesApiResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("dlarchive: %s: %w", params.Action, err)
	}
	if parsed.JSON200 == nil {
		return nil, fmt.Errorf("dlarchive: %s: response is not JSON", params.Action)
	}
	return parsed.JSON200, nil
}
