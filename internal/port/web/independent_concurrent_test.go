package web_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"

	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestIndependentConcurrentStartShutdown(t *testing.T) {
	for round := 0; round < 10; round++ {
		jobs := newJobs(t)
		library := &independentLibrary{}
		listings := romlistings.NewListings([]adapter.RomSource{&independentSource{}}, romcache.New(t.TempDir()), 0)
		downloads := newDownloads(t, library, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
		a := service.NewApp(nil, listings, downloads, jobs)
		h := web.NewHandler(a)
		if w := independentRequest(h, "GET", "/api/games?platform=snes", ""); w.Code != 200 {
			t.Fatal(w.Code)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		type response struct {
			code int
			id   string
		}
		responses := make(chan response, 40)
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				w := independentRequest(h, "POST", "/api/download", `{"id":"ok"}`)
				var job rom.Job
				_ = json.Unmarshal(w.Body.Bytes(), &job)
				responses <- response{code: w.Code, id: job.ID}
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
		close(responses)
		accepted := map[string]bool{}
		for r := range responses {
			switch r.code {
			case 202:
				accepted[r.id] = true
			case 503:
			default:
				t.Fatalf("unexpected status %d", r.code)
			}
		}
		stored := listJobs(t, jobs)
		if len(stored) != len(accepted) {
			t.Fatalf("accepted jobs=%d stored=%d", len(accepted), len(stored))
		}
		for _, job := range stored {
			if !accepted[job.ID] {
				t.Fatalf("stored job %s was never accepted", job.ID)
			}
			if job.Status != "done" && job.Status != "queued" {
				t.Fatalf("job left in %q after shutdown: %+v", job.Status, job)
			}
		}
	}
}
