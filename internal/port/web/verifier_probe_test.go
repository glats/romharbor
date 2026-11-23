package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

func TestVerifierWebContract(t *testing.T) {
	j := newJobs(t)
	l := &independentLibrary{}
	d := newDownloads(t, l, j, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = d.Shutdown(context.Background()) }()
	a := service.NewApp(nil, romlistings.NewListings([]adapter.RomSource{&independentSource{}}, romcache.New(t.TempDir()), time.Hour), d, j, l)
	h := web.NewHandler(a)
	for _, p := range []string{"/api/games?platform=snes", "/api/games?platform=unknown", "/api/game?id=absent", "/api/job?id=absent"} {
		w := independentRequest(h, "GET", p, "")
		t.Logf("%s %d %v %s", p, w.Code, w.Header(), strings.TrimSpace(w.Body.String()))
		if strings.Contains(p, "platform=snes") && (w.Code != http.StatusOK || w.Header().Get("ETag") != "" || strings.Contains(w.Body.String(), "failedSources")) {
			t.Fatal("healthy contract changed")
		}
	}
	w := independentRequest(h, "POST", "/api/download", "{")
	t.Logf("malformed %d %s", w.Code, strings.TrimSpace(w.Body.String()))
}
