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
	"unicode"

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

// Lists reports whether the source browses the platform's console directory
// instead of waiting for a search term.
func (l *LolRoms) Lists(slug string) bool {
	_, ok := lolRomsDirectories[slug]
	return ok
}

// lolRomsDirectories maps the platforms that no curated set covers to the
// console directory the source publishes as a plain file index.
var lolRomsDirectories = map[string]string{
	"lynx": "Atari/Lynx",
}

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
	"snes":      {"Super Famicom", "Super Nintendo Entertainment System"},
	"gb":        {"Game Boy"},
	"gbc":       {"Game Boy Color"},
	"gba":       {"Game Boy Advance"},
	"genesis":   {"Mega Drive - Genesis", "Sega Mega Drive - Genesis", "Sega Mega Drive", "Genesis"},
	"n64":       {"Nintendo 64"},
	"nds":       {"DS", "Nintendo DS"},
	"psx":       {"PlayStation", "Sony PlayStation"},
	"ps2":       {"PlayStation 2", "Sony PlayStation 2"},
	"psp":       {"PlayStation Portable", "Sony PlayStation Portable"},
	"n3ds":      {"3DS"},
	"lynx":      {"Lynx"},
	"saturn":    {"Saturn", "Sega Saturn"},
	"dc":        {"DreamCast", "Sega Dreamcast"},
	"ngc":       {"GameCube", "Nintendo GameCube"},
	"wii":       {"Wii", "Nintendo Wii"},
	"atari2600": {"2600", "Atari 2600"},
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

const lolRomsMaxResponseBytes = 8 << 20

// Search returns the games that match the query. A query without text lists
// the console directory of a platform the source browses.
func (l *LolRoms) Search(ctx context.Context, q rom.Query) ([]rom.Game, error) {
	directory, browse := lolRomsDirectories[q.Platform]
	browse = browse && q.Text == ""
	if q.Text == "" && !browse {
		return nil, fmt.Errorf("lolroms: a search term is required")
	}
	if l.HTTPClient == nil {
		return nil, fmt.Errorf("lolroms: no HTTP client configured")
	}
	client, err := lolromsapi.NewClient(
		strings.TrimRight(l.BaseURL, "/")+"/",
		lolromsapi.WithHTTPClient(l.HTTPClient),
		lolromsapi.WithRequestEditorFn(romfetch.Headers(map[string]string{"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) ROMHarbor/1.0", "Accept": "text/html"})),
	)
	if err != nil {
		return nil, err
	}
	var body []byte
	if browse {
		vendor, console, _ := strings.Cut(directory, "/")
		resp, err := client.ListLolRomsDirectory(ctx, vendor, console)
		if err != nil {
			return nil, err
		}
		body, err = readLolRomsPage(resp.StatusCode, resp.Status, resp.Body)
		if err != nil {
			return nil, err
		}
	} else {
		resp, err := client.SearchLolRoms(ctx, &lolromsapi.SearchLolRomsParams{Q: q.Text})
		if err != nil {
			return nil, err
		}
		body, err = readLolRomsPage(resp.StatusCode, resp.Status, resp.Body)
		if err != nil {
			return nil, err
		}
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
	entries := 0
	undecodable := 0
	var firstErr error
	for _, href := range hrefs {
		if !lolRomsArchive.MatchString(href) {
			continue
		}
		segments := strings.Split(strings.Trim(href, "/"), "/")
		if len(segments) < 3 {
			continue
		}
		entries++
		console, err := url.PathUnescape(segments[len(segments)-2])
		if err != nil {
			undecodable++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		slug, ok := lolRomsConsolePlatform(console)
		if !ok || (q.Platform != "" && slug != q.Platform) {
			continue
		}
		name, err := url.PathUnescape(path.Base(segments[len(segments)-1]))
		if err != nil {
			undecodable++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if seen[href] {
			continue
		}
		seen[href] = true
		games = append(games, l.game(href, slug, name))
	}
	if entries > 0 && undecodable == entries {
		return nil, fmt.Errorf("lolroms: %d of %d entries could not be decoded: %w", undecodable, entries, firstErr)
	}
	return rom.Page(games, q.Offset, q.Limit), nil
}

func (l *LolRoms) game(href, slug, name string) rom.Game {
	title := rom.CleanTitle(name)
	return rom.Game{
		ID:       lolRomsIDPrefix + href,
		Source:   "lolroms",
		Platform: slug,
		Title:    title,
		Region:   rom.RegionOf(title),
		PageURL:  l.BaseURL + href,
		Files: []rom.File{{
			Name:    name,
			URL:     l.BaseURL + href,
			Archive: true,
		}},
	}
}

const lolRomsIDPrefix = "lolroms:"

// ResolveGame rebuilds a game from its ID. The ID carries the link the game
// was found under, so no request is made; anything that is not a plain archive
// link under a known console is refused.
func (l *LolRoms) ResolveGame(id string) (rom.Game, bool) {
	href, ok := strings.CutPrefix(id, lolRomsIDPrefix)
	if !ok || !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") ||
		strings.ContainsAny(href, "?#\\") || hasControl(href) || !lolRomsArchive.MatchString(href) {
		return rom.Game{}, false
	}
	segments := strings.Split(strings.Trim(href, "/"), "/")
	if len(segments) < 3 {
		return rom.Game{}, false
	}
	for _, segment := range segments {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "" || decoded == "." || decoded == ".." || hasControl(decoded) || strings.ContainsAny(decoded, "/\\") {
			return rom.Game{}, false
		}
	}
	console, _ := url.PathUnescape(segments[len(segments)-2])
	slug, ok := lolRomsConsolePlatform(console)
	if !ok {
		return rom.Game{}, false
	}
	name, _ := url.PathUnescape(path.Base(segments[len(segments)-1]))
	return l.game(href, slug, name), true
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) }) >= 0
}

// readLolRomsPage closes the response body and returns the bounded page.
func readLolRomsPage(status int, statusText string, body io.ReadCloser) ([]byte, error) {
	defer func() { _ = body.Close() }()
	if status != 200 {
		return nil, fmt.Errorf("lolroms: %s", statusText)
	}
	page, err := io.ReadAll(boundBody(body, lolRomsMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("lolroms: %w", err)
	}
	return page, nil
}
