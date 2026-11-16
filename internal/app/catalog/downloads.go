package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

const downloadTimeout = 6 * time.Hour

type Downloads struct {
	library adapter.RomLibrary
	jobs    adapter.RomJobStore
	logger  *slog.Logger

	mu       sync.Mutex
	lifetime context.Context
	cancel   context.CancelFunc
	running  sync.WaitGroup
}

func NewDownloads(library adapter.RomLibrary, jobs adapter.RomJobStore, logger *slog.Logger) *Downloads {
	if logger == nil {
		logger = slog.Default()
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &Downloads{
		library:  library,
		jobs:     jobs,
		logger:   logger,
		lifetime: lifetime,
		cancel:   cancel,
	}
}

func (d *Downloads) Start(game rom.Game, file rom.File, platform rom.Platform, newJobID func() string) error {
	d.mu.Lock()
	if d.lifetime.Err() != nil {
		d.mu.Unlock()
		return rom.ErrShuttingDown
	}
	job := d.jobs.Add(rom.Job{
		ID:      newJobID(),
		GameID:  game.ID,
		Title:   game.Title,
		Status:  "queued",
		Total:   file.Size,
		Started: time.Now(),
	})
	d.running.Add(1)
	d.mu.Unlock()
	go d.run(job.ID, game, file, platform)
	return nil
}

func (d *Downloads) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	d.cancel()
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for downloads: %w", ctx.Err())
	}
}

func (d *Downloads) run(id string, game rom.Game, file rom.File, platform rom.Platform) {
	defer d.running.Done()
	ctx, cancel := context.WithTimeout(d.lifetime, downloadTimeout)
	defer cancel()

	d.jobs.Update(id, func(j *rom.Job) { j.Status = "running" })
	result, err := d.library.Install(ctx, rom.InstallRequest{
		Game:     game,
		File:     file,
		Platform: platform,
	}, func(downloaded, total int64) {
		d.jobs.Update(id, func(j *rom.Job) {
			j.Downloaded = downloaded
			if total > 0 {
				j.Total = total
			}
		})
	})
	if err != nil {
		d.logger.Error("download", "game", game.Title, "error", err)
		d.jobs.Update(id, func(j *rom.Job) {
			j.Status = "failed"
			j.Error = err.Error()
			j.Finished = time.Now()
		})
		return
	}
	d.jobs.Update(id, func(j *rom.Job) {
		j.Status = "done"
		j.Path = result.Path
		j.Total = result.Size
		j.Finished = time.Now()
	})
}
