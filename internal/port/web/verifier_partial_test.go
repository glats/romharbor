package web_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/adapter/romcache"
	"app.ddd/internal/adapter/romlistings"
	"app.ddd/internal/domain/rom"
	"app.ddd/internal/port/web"
	"app.ddd/internal/service"
)

type verifierBrokenSource struct{}

func (verifierBrokenSource) Name() string        { return "broken" }
func (verifierBrokenSource) Platforms() []string { return nil }
func (verifierBrokenSource) Search(context.Context, rom.Query) ([]rom.Game, error) {
	return nil, errors.New("PRIVATE-UPSTREAM")
}

func TestVerifierPartialHTMLAndPaging(t *testing.T) {
	j := newJobs(t)
	l := &independentLibrary{}
	d := newDownloads(t, l, j, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer func() { _ = d.Shutdown(context.Background()) }()
	h := web.NewHandler(service.NewApp(nil, romlistings.NewListings([]adapter.RomSource{&independentSource{}, verifierBrokenSource{}}, romcache.New(t.TempDir()), time.Hour), d, j, l))
	for _, p := range []string{"/platform/snes", "/api/games?platform=snes"} {
		w := independentRequest(h, "GET", p, "")
		want := "Some sources did not respond: broken. Showing results from the others."
		if strings.HasPrefix(p, "/api") {
			want = `"failedSources":["broken"]`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "PRIVATE-UPSTREAM") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	for _, q := range []string{"offset=-1", "offset=no", "offset=9999999999999999999999999999", "limit=-1", "limit=no"} {
		w := independentRequest(h, "GET", "/api/games?platform=snes&"+q, "")
		field := strings.Split(q, "=")[0]
		if w.Code != 400 || w.Body.String() != `{"error":"invalid `+field+`"}`+"\n" {
			t.Fatalf("%s %d %s", q, w.Code, w.Body.String())
		}
	}
}

type verifierBrokenListings struct{ adapter.RomListings }

func (verifierBrokenListings) Game(context.Context, string) (rom.Game, bool, error) {
	return rom.Game{}, false, errors.New("PRIVATE-DB")
}

func TestVerifierInfrastructureStatus(t *testing.T) {
	j := newJobs(t)
	l := &independentLibrary{}
	d := newDownloads(t, l, j, nil)
	defer func() { _ = d.Shutdown(context.Background()) }()
	h := web.NewHandler(service.NewApp(nil, verifierBrokenListings{}, d, j, l))
	for _, req := range []struct{ method, path, body string }{{"GET", "/api/game?id=x", ""}, {"POST", "/api/download", `{"id":"x"}`}} {
		w := independentRequest(h, req.method, req.path, req.body)
		if w.Code != 500 || strings.Contains(w.Body.String(), "PRIVATE-DB") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
