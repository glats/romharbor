package command

import (
	"context"

	"app.ddd/internal/app/catalog"
	"app.ddd/internal/common/decorator"
	"app.ddd/internal/domain/rom"
)

type StartDownload struct {
	NewJobID func() string
	GameID   string
}

type StartDownloadHandler decorator.CommandHandler[StartDownload]

type startDownloadCommand struct {
	listings  *catalog.Listings
	downloads *catalog.Downloads
}

func NewStartDownloadHandler(listings *catalog.Listings, downloads *catalog.Downloads, metric decorator.MetricsClient) StartDownloadHandler {
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
