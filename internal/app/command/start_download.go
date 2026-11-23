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
	Queued func(rom.Job)
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
	if err != nil {
		return fmt.Errorf("look up game %q: %w", cmd.GameID, err)
	}
	if !ok {
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
	job, err := c.downloads.Start(ctx, rom.InstallRequest{Game: game, File: file, Platform: platform})
	if err != nil {
		return err
	}
	if cmd.Queued != nil {
		cmd.Queued(job)
	}
	return nil
}
