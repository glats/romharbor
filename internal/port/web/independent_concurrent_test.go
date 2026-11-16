package web_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestIndependentConcurrentStartShutdown(t *testing.T) {
	for round := 0; round < 10; round++ {
		jobs := romjobs.New()
		library := &independentLibrary{}
		listings := romlistings.NewListings([]adapter.RomSource{&independentSource{}}, romcache.New(t.TempDir()), 0)
		downloads := romdownloads.NewDownloads(library, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
		a := service.NewApp(nil, listings, downloads, jobs, library)
		h := web.NewHandler(a)
		if w := independentRequest(h, "GET", "/api/games?platform=snes", ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make(chan int, 40)
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes <- independentRequest(h, "POST", "/api/download", `{"id":"ok"}`).Code
			}()
		}
		close(start)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := downloads.Shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		close(codes)
		accepted := 0
		for code := range codes {
			switch code {
			case 202:
				accepted++
			case 503:
			default:
				t.Fatalf("unexpected status %d", code)
			}
		}
		if len(jobs.List()) != accepted {
			t.Fatalf("accepted=%d stored=%d", accepted, len(jobs.List()))
		}
		for _, job := range jobs.List() {
			if job.Status != "done" && job.Status != "failed" {
				t.Fatalf("unfinished after shutdown: %+v", job)
			}
		}
	}
}
