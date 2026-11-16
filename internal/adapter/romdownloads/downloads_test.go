package romdownloads

import (
	"context"
	"errors"
	"testing"
	"time"

	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/domain/rom"
)

func TestShutdownCancelsInFlightDownloadsAndRejectsNewOnes(t *testing.T) {
	library := &blockingLibrary{started: make(chan struct{})}
	jobs := romjobs.New()
	downloads := NewDownloads(library, jobs, nil)
	game := rom.Game{ID: "g", Platform: "snes", Title: "G", Files: []rom.File{{Name: "G.sfc", URL: "http://unused.invalid/g"}}}
	platform, _ := rom.PlatformBySlug("snes")
	file, _ := game.Main()

	if err := downloads.Start(game, file, platform, func() string { return "1" }); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if job, ok := jobs.GetQueued("1"); !ok || job.Status != "queued" || job.GameID != "g" {
		t.Fatalf("queued job = %+v, %v", job, ok)
	}
	<-library.started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := downloads.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	got, _ := jobs.Get("1")
	if got.Status != "failed" || got.Error != context.Canceled.Error() {
		t.Errorf("job after shutdown = %+v, want failed with context canceled", got)
	}
	ids := 0
	err := downloads.Start(game, file, platform, func() string {
		ids++
		return "2"
	})
	if !errors.Is(err, rom.ErrShuttingDown) {
		t.Errorf("Start() after shutdown error = %v, want ErrShuttingDown", err)
	}
	if ids != 0 {
		t.Errorf("rejected Start() generated %d job IDs, want none", ids)
	}
	if _, ok := jobs.Get("2"); ok {
		t.Error("rejected Start() stored a job")
	}
}
