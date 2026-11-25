package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type StartDownload struct {
	GameID string
}

type StartDownloadHandler decorator.CommandResultHandler[StartDownload, rom.Job]

type startDownloadCommand struct {
	listings  adapter.RomListings
	downloads adapter.RomDownloads
}

func NewStartDownloadHandler(listings adapter.RomListings, downloads adapter.RomDownloads, metric decorator.MetricsClient) StartDownloadHandler {
	return decorator.ApplyCommandResultDecorators[StartDownload, rom.Job](startDownloadCommand{listings: listings, downloads: downloads}, metric)
}

func (c startDownloadCommand) Handle(ctx context.Context, cmd StartDownload) (rom.Job, error) {
	game, ok, err := c.listings.Game(ctx, cmd.GameID)
	if err != nil {
		return rom.Job{}, fmt.Errorf("look up game %q: %w", cmd.GameID, err)
	}
	if !ok {
		return rom.Job{}, rom.ErrGameNotFound
	}
	file, ok := game.Main()
	if !ok {
		return rom.Job{}, rom.ErrNoDownloadFile
	}
	platform, ok := rom.PlatformBySlug(game.Platform)
	if !ok {
		return rom.Job{}, rom.ErrUnknownPlatform
	}
	job, err := c.downloads.Start(ctx, rom.InstallRequest{Game: game, File: file, Platform: platform})
	if err != nil {
		return rom.Job{}, err
	}
	if _, _, multiDisc := rom.DiscOf(game.Title); multiDisc {
		go c.startOtherDiscs(context.WithoutCancel(ctx), game, platform)
	}
	return job, nil
}

const siblingLookupTimeout = 2 * time.Minute

// startOtherDiscs queues the other discs of the release the game belongs to,
// so the set is complete and the library can write its playlist. A disc that
// cannot be found or queued is skipped: the requested download stands on its
// own.
func (c startDownloadCommand) startOtherDiscs(ctx context.Context, game rom.Game, platform rom.Platform) {
	ctx, cancel := context.WithTimeout(ctx, siblingLookupTimeout)
	defer cancel()
	group, disc, _ := rom.DiscOf(game.Title)
	candidates := c.candidateDiscs(ctx, game)
	queued := map[int]bool{disc: true}
	for _, other := range candidates {
		otherGroup, number, ok := rom.DiscOf(other.Title)
		if !ok || queued[number] || otherGroup != group || other.Source != game.Source || other.Platform != game.Platform {
			continue
		}
		file, ok := other.Main()
		if !ok {
			continue
		}
		if _, err := c.downloads.Start(ctx, rom.InstallRequest{Game: other, File: file, Platform: platform}); err == nil {
			queued[number] = true
		}
	}
}

// candidateDiscs gathers the games that may be other discs of the game: the
// hits of the search sources for its title and the platform listing of the
// listing sources.
func (c startDownloadCommand) candidateDiscs(ctx context.Context, game rom.Game) []rom.Game {
	var candidates []rom.Game
	if page, err := c.listings.SearchSources(ctx, discSearchText(game.Title), game.Platform); err == nil {
		candidates = append(candidates, page.Games...)
	}
	if page, err := c.listings.ObtainPlatformListing(ctx, game.Platform); err == nil {
		candidates = append(candidates, page.Games...)
	}
	return candidates
}

// discSearchText is the part of a title before its first tag, the words every
// disc of the game shares.
func discSearchText(title string) string {
	text, _, _ := strings.Cut(title, "(")
	return strings.TrimSpace(text)
}
