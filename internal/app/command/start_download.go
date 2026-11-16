package command

import (
	"context"

	"app.ddd/internal/adapter"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type StartDownload struct {
	NewJobID func() string
	GameID   string
}

type StartDownloadHandler decorator.CommandHandler[StartDownload]

type startDownloadCommand struct {
	listings  adapter.RomListings
	downloads adapter.RomDownloads
}

func NewStartDownloadHandler(listings adapter.RomListings, downloads adapter.RomDownloads, metric decorator.MetricsClient) StartDownloadHandler {
	return decorator.ApplyCommandDecorators[StartDownload](startDownloadCommand{listings: listings, downloads: downloads}, metric)
}

func (c startDownloadCommand) Handle(ctx context.Context, cmd StartDownload) error {
	game, ok, err := c.listings.Game(ctx, cmd.GameID)
	if err != nil || !ok {
		return rom.ErrGameNotFound
	}
	file, ok := game.Main()
	if !ok {
		return rom.ErrNoDownloadFile
	}
	platform, ok := rom.PlatformBySlug(game.Platform)
	if !ok {
		return rom.ErrUnknownPlatform
	}
	return c.downloads.Start(game, file, platform, cmd.NewJobID)
}
