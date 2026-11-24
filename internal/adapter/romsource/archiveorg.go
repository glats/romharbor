package romsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"app.ddd/internal/adapter/romsource/archiveorgapi"
	"app.ddd/internal/domain/rom"
)

const archiveOrgMaxMetadataBytes = 64 << 20

var errBodyTooLarge = errors.New("response body exceeds the size limit")

var errUnparsableSize = errors.New("file size is not a number")

type boundedReader struct {
	body      io.Reader
	remaining int64
	limit     int64
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var probe [1]byte
		n, err := b.body.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%w (%d bytes)", errBodyTooLarge, b.limit)
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func boundBody(body io.ReadCloser, limit int64) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{&boundedReader{body: body, remaining: limit, limit: limit}, body}
}

func forceJSONContentType(header map[string][]string) {
	if header != nil {
		header["Content-Type"] = []string{"application/json"}
	}
}

// ArchiveOrg serves games from Internet Archive items. The curated item list
// holds the per-game sets whose file listing is the catalog: every file in
// those items is one game with its own checksum and direct download URL.
type ArchiveOrg struct {
	HTTPClient archiveorgapi.HttpRequestDoer
	BaseURL    string
	// Sets maps a platform slug to the Internet Archive items that hold it.
	Sets map[string][]string

	logger *slog.Logger
}

// NewArchiveOrg returns the Internet Archive source with the curated sets.
func NewArchiveOrg(client archiveorgapi.HttpRequestDoer) *ArchiveOrg {
	return &ArchiveOrg{
		HTTPClient: client,
		BaseURL:    "https://archive.org",
		Sets: map[string][]string{
			"nes":     {"nes-collection-no-intro"},
			"snes":    {"nointro-snes"},
			"gb":      {"nointro-nintendo-gameboy"},
			"gbc":     {"ef_GBC_No-Intro"},
			"gba":     {"ef_gba_no-intro_2024-02-21"},
			"genesis": {"nointro.md"},
			"n64":     {"ef_nintendo_64_no-intro_2024-02-10"},
			"nds":     {"ni-n-ds-dec_202401"},
			"psx":     {"non-redump_sony_playstation"},
			"psp":     {"non-redump_sony_playstation_portable"},
			"arcade":  {"mame-merged"},
		},
		logger: slog.Default(),
	}
}

// Name identifies the source.
func (a *ArchiveOrg) Name() string { return "archive.org" }

// Platforms lists the platforms the curated sets cover.
func (a *ArchiveOrg) Platforms() []string {
	out := make([]string, 0, len(a.Sets))
	for slug := range a.Sets {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

// sizeValue reads the file size, which Internet Archive publishes as a number
// for most files and as a string for some of them.
type sizeValue int64

// UnmarshalJSON decodes both the number and the string form.
func (s *sizeValue) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		if text == "" {
			return nil
		}
		number, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("%w: %q", errUnparsableSize, text)
		}
		*s = sizeValue(number)
		return nil
	}
	var number int64
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*s = sizeValue(number)
	return nil
}

// Search returns the games of the curated sets that match the query.
func (a *ArchiveOrg) Search(ctx context.Context, q rom.Query) ([]rom.Game, error) {
	platforms := []string{q.Platform}
	if q.Platform == "" {
		platforms = a.Platforms()
	}

	var games []rom.Game
	for _, slug := range platforms {
		for _, item := range a.Sets[slug] {
			found, err := a.gamesOfItem(ctx, slug, item)
			if err != nil {
				return nil, fmt.Errorf("archive.org %s: %w", item, err)
			}
			games = append(games, found...)
		}
	}

	games = rom.FilterByTitle(games, q.Text)
	rom.SortByTitle(games)
	return rom.Page(games, q.Offset, q.Limit), nil
}

func (a *ArchiveOrg) ResolvePlatform(_ context.Context, id string) (string, bool, error) {
	rest, ok := strings.CutPrefix(id, "archive.org:")
	if !ok {
		return "", false, nil
	}
	item, _, _ := strings.Cut(rest, "/")
	for _, slug := range slices.Sorted(maps.Keys(a.Sets)) {
		if slices.Contains(a.Sets[slug], item) {
			return slug, true, nil
		}
	}
	return "", false, nil
}

func (a *ArchiveOrg) gamesOfItem(ctx context.Context, slug, item string) ([]rom.Game, error) {
	meta, err := a.metadata(ctx, item)
	if err != nil {
		return nil, err
	}
	base, ok := rom.PlatformBySlug(slug)
	if !ok {
		return nil, fmt.Errorf("unknown platform %q", slug)
	}

	var games []rom.Game
	for _, f := range meta.Files {
		var size sizeValue
		if err := size.UnmarshalJSON(f.Size); err != nil {
			if errors.Is(err, errUnparsableSize) {
				a.logger.Error("archive.org file skipped", "item", item, "file", f.Name, "error", err)
				continue
			}
			return nil, fmt.Errorf("metadata %s: size of %s: %w", item, f.Name, err)
		}
		if !rom.IsGameFile(f.Name) {
			continue
		}
		name := path.Base(f.Name)
		if !rom.IsArchive(name) && !base.Accepts(name) {
			continue
		}
		title := rom.CleanTitle(name)
		games = append(games, rom.Game{
			ID:       "archive.org:" + item + "/" + f.Name,
			Source:   "archive.org",
			Platform: slug,
			Title:    title,
			Region:   rom.RegionOf(title),
			Size:     int64(size),
			PageURL:  a.BaseURL + "/details/" + item,
			Files: []rom.File{{
				Name:    f.Name,
				Size:    int64(size),
				MD5:     f.Md5,
				SHA1:    f.Sha1,
				URL:     a.BaseURL + "/download/" + item + "/" + url.PathEscape(f.Name),
				Archive: rom.IsArchive(name),
			}},
		})
	}
	return games, nil
}

func (a *ArchiveOrg) metadata(ctx context.Context, item string) (*archiveorgapi.ItemMetadata, error) {
	if a.HTTPClient == nil {
		return nil, errors.New("no HTTP client is configured")
	}
	client, err := archiveorgapi.NewClient(a.BaseURL, archiveorgapi.WithHTTPClient(a.HTTPClient))
	if err != nil {
		return nil, err
	}
	resp, err := client.GetItemMetadata(ctx, item)
	if err != nil {
		return nil, fmt.Errorf("metadata %s: %w", item, err)
	}
	if resp.StatusCode != 200 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("metadata %s: %s", item, resp.Status)
	}
	resp.Body = boundBody(resp.Body, archiveOrgMaxMetadataBytes)
	forceJSONContentType(resp.Header)
	parsed, err := archiveorgapi.ParseGetItemMetadataResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("metadata %s: %w", item, err)
	}
	if parsed.JSON200 == nil {
		return nil, fmt.Errorf("metadata %s: response is not JSON", item)
	}
	if notFound, err := parsed.JSON200.AsItemNotFound(); err == nil && notFound != nil && len(notFound) == 0 {
		return nil, fmt.Errorf("metadata %s: item not found (empty array response)", item)
	}
	meta, err := parsed.JSON200.AsItemMetadata()
	if err != nil {
		return nil, fmt.Errorf("metadata %s: %w", item, err)
	}
	return &meta, nil
}
