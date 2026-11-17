package romsource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/adapter/romsource/lolromsapi"
	"app.ddd/internal/domain/rom"
)

// LolRoms serves games from the lolroms listing, which publishes No-Intro
// style archives per vendor and console. The search is a plain HTML query
// with a hard cap of about sixty results, and every link is a direct file.
type LolRoms struct {
	HTTPClient lolromsapi.HttpRequestDoer
	BaseURL    string
}

// NewLolRoms returns the lolroms source. A nil doer is not replaced by a
// default client; Search reports that no client is configured.
func NewLolRoms(doer lolromsapi.HttpRequestDoer) *LolRoms {
	return &LolRoms{HTTPClient: doer, BaseURL: "https://lolroms.com"}
}

// Name identifies the source.
func (l *LolRoms) Name() string { return "lolroms" }

func (l *LolRoms) SearchOnly() bool { return true }

// Platforms lists the platforms the source can serve.
func (l *LolRoms) Platforms() []string {
	out := make([]string, 0, len(lolRomsConsoles))
	for slug := range lolRomsConsoles {
		out = append(out, slug)
	}
	return out
}

// lolRomsConsoles maps a platform slug to the console folder names the source
// uses.
var lolRomsConsoles = map[string][]string{
	"nes":       {"Nintendo Entertainment System"},
	"snes":      {"Super Nintendo Entertainment System"},
	"gb":        {"Game Boy"},
	"gbc":       {"Game Boy Color"},
	"gba":       {"Game Boy Advance"},
	"genesis":   {"Sega Mega Drive - Genesis", "Sega Mega Drive", "Genesis"},
	"n64":       {"Nintendo 64"},
	"nds":       {"Nintendo DS"},
	"psx":       {"Sony PlayStation"},
	"ps2":       {"Sony PlayStation 2"},
	"psp":       {"Sony PlayStation Portable"},
	"saturn":    {"Sega Saturn"},
	"dc":        {"Sega Dreamcast"},
	"ngc":       {"Nintendo GameCube"},
	"wii":       {"Nintendo Wii"},
	"atari2600": {"Atari 2600"},
}

// lolRomsConsolePlatform returns the platform of a console folder name.
func lolRomsConsolePlatform(console string) (string, bool) {
	lowered := strings.ToLower(console)
	for slug, names := range lolRomsConsoles {
		for _, name := range names {
			if strings.EqualFold(lowered, name) {
				return slug, true
			}
		}
	}
	return "", false
}

// lolRomsArchive matches the file extensions the source publishes.
var lolRomsArchive = regexp.MustCompile(`(?i)\.(7z|chd|zip)$`)

// readAll reads a response body with a bounded size.
func readAll(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, 8<<20))
}

// Search returns the games that match the query.
func (l *LolRoms) Search(ctx context.Context, q rom.Query) ([]rom.Game, error) {
	if q.Text == "" {
		return nil, fmt.Errorf("lolroms: a search term is required")
	}
	if l.HTTPClient == nil {
		return nil, fmt.Errorf("lolroms: no HTTP client configured")
	}
	client, err := lolromsapi.NewClient(
		strings.TrimRight(l.BaseURL, "/")+"/",
		lolromsapi.WithHTTPClient(l.HTTPClient),
		lolromsapi.WithRequestEditorFn(romfetch.Headers(map[string]string{"User-Agent": "ROMHarbor", "Accept": "text/html"})),
	)
	if err != nil {
		return nil, err
	}
	resp, err := client.SearchLolRoms(ctx, &lolromsapi.SearchLolRomsParams{Q: q.Text})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lolroms: %s", resp.Status)
	}
	body, err := readAll(resp.Body)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hrefs := doc.Find("a[href]").Map(func(_ int, anchor *goquery.Selection) string {
		href, _ := anchor.Attr("href")
		return href
	})

	seen := map[string]bool{}
	var games []rom.Game
	for _, href := range hrefs {
		if !lolRomsArchive.MatchString(href) {
			continue
		}
		segments := strings.Split(strings.Trim(href, "/"), "/")
		if len(segments) < 3 {
			continue
		}
		console, err := url.PathUnescape(segments[len(segments)-2])
		if err != nil {
			continue
		}
		slug, ok := lolRomsConsolePlatform(console)
		if !ok || (q.Platform != "" && slug != q.Platform) {
			continue
		}
		name, err := url.PathUnescape(path.Base(segments[len(segments)-1]))
		if err != nil {
			continue
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		games = append(games, rom.Game{
			ID:       "lolroms:" + href,
			Source:   "lolroms",
			Platform: slug,
			Title:    rom.CleanTitle(name),
			Region:   rom.RegionOf(name),
			PageURL:  l.BaseURL + href,
			Files: []rom.File{{
				Name:    name,
				URL:     l.BaseURL + href,
				Archive: true,
			}},
		})
	}
	return rom.Page(games, q.Offset, q.Limit), nil
}
