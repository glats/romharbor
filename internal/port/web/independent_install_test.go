package web_test

import (
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlibrary"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

type payloadSource struct{ url string }

func (payloadSource) Name() string        { return "synthetic" }
func (payloadSource) Platforms() []string { return []string{"snes"} }
func (s payloadSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return []rom.Game{{ID: "payload", Platform: "snes", Title: "Payload", Files: []rom.File{{Name: "Payload.sfc", URL: s.url, Size: 3}}}}, nil
}

func TestIndependentRequestCancellationInstall(t *testing.T) {
	payload := []byte{0, 1, 2}
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
			_, _ = w.Write(payload)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	jobs := newJobs(t)
	root := t.TempDir()
	library := &romlibrary.Library{Root: root + "/roms"}
	listings := romlistings.NewListings([]adapter.RomSource{payloadSource{upstream.URL}}, romcache.New(root+"/cache"), 0)
	downloads := newDownloads(t, library, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a := service.NewApp(nil, listings, downloads, jobs)
	stop := downloads.Shutdown
	defer func() { _ = stop(context.Background()) }()
	h := web.NewHandler(a)
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("POST", "/api/download", strings.NewReader(`{"id":"payload"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	cancel()
	close(release)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		job := getJob(t, jobs, "1")
		if job.Status == "done" {
			b, err := os.ReadFile(filepath.Join(library.Root, filepath.FromSlash(job.Path)))
			if err != nil || sha256.Sum256(b) != sha256.Sum256(payload) {
				t.Fatal("installed payload mismatch", err)
			}
			break
		}
		if job.Status == "failed" || time.Now().After(deadline) {
			t.Fatal(job)
		}
		time.Sleep(time.Millisecond)
	}
}
