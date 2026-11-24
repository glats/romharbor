package command

import (
	"context"
	"fmt"

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
	return c.downloads.Start(ctx, rom.InstallRequest{Game: game, File: file, Platform: platform})
}
