package web_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romdownloads"
	"app.ddd/internal/adapter/romjobs"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

type independentSource struct{ calls atomic.Int32 }

func (*independentSource) Name() string        { return "synthetic" }
func (*independentSource) Platforms() []string { return nil }
func (s *independentSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	s.calls.Add(1)
	return []rom.Game{{ID: "ok", Platform: "snes", Title: "Synthetic", Files: []rom.File{{Name: "Synthetic.sfc", Size: 9}}}, {ID: "nofile", Platform: "snes", Title: "No file"}}, nil
}

type independentLibrary struct {
	entered chan struct{}
	block   bool
}

func (*independentLibrary) PlatformDir(rom.Platform) (string, error) { return "/synthetic", nil }
func (l *independentLibrary) Install(ctx context.Context, _ rom.InstallRequest, p func(int64, int64)) (rom.InstallResult, error) {
	p(3, 15)
	if l.entered != nil {
		close(l.entered)
	}
	if l.block {
		<-ctx.Done()
		return rom.InstallResult{}, ctx.Err()
	}
	return rom.InstallResult{Path: "/synthetic/game.sfc", Size: 15}, nil
}

func independentRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestIndependentSecurityAndLifecycle(t *testing.T) {
	root := t.TempDir()
	neighbor := filepath.Join(root, "neighbor.json")
	original := []byte(`{"fetched":"2099-01-01T00:00:00Z","games":[{"ID":"secret"}]}`)
	if err := os.WriteFile(neighbor, original, 0o600); err != nil {
		t.Fatal(err)
	}
	cache := romcache.New(filepath.Join(root, "cache"))
	source := &independentSource{}
	jobs := romjobs.New()
	library := &independentLibrary{}
	listings := romlistings.NewListings([]adapter.RomSource{source}, cache, 0)
	downloads := romdownloads.NewDownloads(library, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a := service.NewApp(nil, listings, downloads, jobs, library)
	stop := downloads.Shutdown
	t.Cleanup(func() { _ = stop(context.Background()) })
	h := web.NewHandler(a)
	for _, platform := range []string{"../neighbor", "nested/../../neighbor", `..\neighbor`, "SNES", "snes ", "%2e%2e%2fneighbor", "unknown"} {
		for _, text := range []string{"", "Synthetic"} {
			w := independentRequest(h, "GET", "/api/games?"+url.Values{"platform": {platform}, "q": {text}}.Encode(), "")
			if w.Code != 400 || w.Body.String() != "{\"error\":\"unknown platform\"}\n" {
				t.Fatalf("%q/%q: %d %s", platform, text, w.Code, w.Body.String())
			}
			if _, err := cache.Load(platform); err == nil || err.Error() != "unknown cache platform "+strconvQuote(platform) {
				t.Fatalf("Load %q: %v", platform, err)
			}
			if err := cache.Store(platform, rom.Listing{}); err == nil || err.Error() != "unknown cache platform "+strconvQuote(platform) {
				t.Fatalf("Store %q: %v", platform, err)
			}
			after, err := os.ReadFile(neighbor)
			if err != nil || sha256.Sum256(after) != sha256.Sum256(original) {
				t.Fatal("neighbor changed", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Fatal("rejection created files", entries, err)
			}
		}
	}
	if source.calls.Load() != 0 {
		t.Fatal("rejected requests reached source")
	}
	for _, platform := range []string{"", "snes"} {
		w := independentRequest(h, "GET", "/api/games?platform="+platform, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"ID":"ok"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, c := range []struct {
		body   string
		status int
		want   string
	}{{`{"id":"missing"}`, 404, "{\"error\":\"game not found\"}\n"}, {`{"id":"nofile"}`, 400, "{\"error\":\"game has no file\"}\n"}, {`{`, 400, "{\"error\":\"unexpected EOF\"}\n"}} {
		w := independentRequest(h, "POST", "/api/download", c.body)
		if w.Code != c.status || w.Body.String() != c.want {
			t.Fatal(w.Code, w.Body.String())
		}
		if len(jobs.List()) != 0 {
			t.Fatal("rejection stored job")
		}
	}
	for i := 0; i < 30; i++ {
		w := independentRequest(h, "POST", "/api/download", `{"id":"ok"}`)
		var queued rom.Job
		if err := json.Unmarshal(w.Body.Bytes(), &queued); err != nil {
			t.Fatal(err)
		}
		if w.Code != 202 || queued.Status != "queued" || queued.Downloaded != 0 || queued.Total != 9 || !queued.Finished.IsZero() || queued.Path != "" || queued.Error != "" {
			t.Fatalf("snapshot: %d %+v", w.Code, queued)
		}
		deadline := time.Now().Add(time.Second)
		for {
			j, _ := jobs.Get(queued.ID)
			if j.Status == "done" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("job never completed")
			}
			time.Sleep(time.Millisecond)
		}
		poll := independentRequest(h, "GET", "/api/job?id="+queued.ID, "")
		var done rom.Job
		_ = json.Unmarshal(poll.Body.Bytes(), &done)
		if done.Status != "done" || done.Total != 15 || done.Downloaded != 3 || done.Path != "/synthetic/game.sfc" {
			t.Fatal(poll.Body.String())
		}
	}
	if j, _ := jobs.Get("1"); j.GameID != "ok" {
		t.Fatal("rejections consumed IDs")
	}
	if err := stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	w := independentRequest(h, "POST", "/api/download", `{"id":"ok"}`)
	if w.Code != 503 || w.Body.String() != "{\"error\":\"service is shutting down\"}\n" || len(jobs.List()) != 30 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestIndependentShutdownCancellation(t *testing.T) {
	jobs := romjobs.New()
	library := &independentLibrary{entered: make(chan struct{}), block: true}
	listings := romlistings.NewListings([]adapter.RomSource{&independentSource{}}, romcache.New(t.TempDir()), 0)
	downloads := romdownloads.NewDownloads(library, jobs, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a := service.NewApp(nil, listings, downloads, jobs, library)
	stop := downloads.Shutdown
	h := web.NewHandler(a)
	w := independentRequest(h, "POST", "/api/download", `{"id":"ok"}`)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	<-library.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := stop(ctx); err != nil {
		t.Fatal(err)
	}
	j, _ := jobs.Get("1")
	q, _ := jobs.GetQueued("1")
	if j.Status != "failed" || j.Error != "context canceled" || j.Finished.IsZero() || q.Status != "queued" || q.Downloaded != 0 || q.Total != 9 {
		t.Fatal(j, q)
	}
}
