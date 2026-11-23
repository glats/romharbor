package romsource

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"app.ddd/internal/domain/rom"
)

type requestLog struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (l *requestLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reqs = append(l.reqs, r.Clone(context.Background()))
}

func (l *requestLog) snapshot() []*http.Request {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*http.Request(nil), l.reqs...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func cannedJSON(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}
}

func TestArchiveOrgClientRequestsItemMetadata(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item one"}}

	if _, err := source.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	reqs := log.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	req := reqs[0]
	if req.Method != http.MethodGet {
		t.Errorf("method = %q", req.Method)
	}
	if got := req.URL.EscapedPath(); got != "/metadata/item%20one" {
		t.Errorf("path = %q", got)
	}
	if req.URL.RawQuery != "" {
		t.Errorf("query = %q, want none", req.URL.RawQuery)
	}
	if accept := req.Header.Get("Accept"); accept != "" {
		t.Errorf("accept = %q, want none", accept)
	}
}

func TestArchiveOrgClientReportsMetadataStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	want := "archive.org item-one: metadata item-one: 503 Service Unavailable"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestArchiveOrgClientTreatsEmptyNotFoundArrayAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"missing-item"}}

	_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	want := "archive.org missing-item: metadata missing-item: item not found (empty array response)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestArchiveOrgClientHonorsCancellation(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := source.Search(ctx, rom.Query{Platform: "nes"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := len(log.snapshot()); n != 0 {
		t.Errorf("server saw %d requests after cancellation, want 0", n)
	}
}

func TestArchiveOrgClientMapsFileSizeVariantsAndSkipsUnparsableSizes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[
			{"name":"Game One (USA).zip","size":10},
			{"name":"Game Two (USA).zip","size":"20"},
			{"name":"Game Three (USA).zip","size":null},
			{"name":"Game Four (USA).zip"},
			{"name":"Game Five (USA).zip","size":""},
			{"name":"Game Six (USA).zip","size":"unknown"}
		]}`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := map[string]int64{
		"Game One (USA)":   10,
		"Game Two (USA)":   20,
		"Game Three (USA)": 0,
		"Game Four (USA)":  0,
		"Game Five (USA)":  0,
	}
	if len(games) != len(want) {
		t.Fatalf("got %d games, want %d", len(games), len(want))
	}
	for _, g := range games {
		size, ok := want[g.Title]
		if !ok {
			t.Errorf("unexpected title %q", g.Title)
			continue
		}
		if g.Size != size || g.Files[0].Size != size {
			t.Errorf("%s size = %d/%d, want %d", g.Title, g.Size, g.Files[0].Size, size)
		}
	}
}

func TestArchiveOrgClientRejectsNonNumericSizeType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[{"name":"Game One (USA).zip","size":true}]}`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	prefix := "archive.org item-one: metadata item-one: "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("err = %v, want prefix %q", err, prefix)
	}
}

func TestArchiveOrgClientUsesInjectedHTTPClient(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return cannedJSON(r, `{"files":[{"name":"Example Game (USA).zip","size":5}]}`), nil
	})}

	source := NewArchiveOrg(client)
	source.BaseURL = "http://archive.invalid"
	source.Sets = map[string][]string{"nes": {"example-item"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if calls != 1 || len(games) != 1 || games[0].Files[0].URL != "http://archive.invalid/download/example-item/Example%20Game%20%28USA%29.zip" {
		t.Fatalf("calls = %d, games = %+v", calls, games)
	}
}

func TestDlArchiveClientPaginatesListWithLimitAndOffset(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = w.Write([]byte(`{"ok":true,"items":[` +
				`{"id":"a1","filename":"Example One (USA).zip","size":1,"download_url":"https://files.example.invalid/one.zip","folder":"Mirror/Nintendo_Nes"},` +
				`{"id":"a2","filename":"Example Two (USA).zip","size":2,"download_url":"https://files.example.invalid/two.zip","folder":"Mirror/Nintendo_Nes"}` +
				`],"total":3}`))
		case "2":
			_, _ = w.Write([]byte(`{"ok":true,"items":[` +
				`{"id":"a3","filename":"Example Three (USA).zip","size":3,"download_url":"https://files.example.invalid/three.zip","folder":"Mirror/Nintendo_Nes"}` +
				`],"total":3}`))
		}
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 3 || games[2].ID != "dlarchive:a3" {
		t.Fatalf("games = %+v", games)
	}
	reqs := log.snapshot()
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}
	for i, wantOffset := range []string{"0", "2"} {
		req := reqs[i]
		q := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Path != "/files_api.php" {
			t.Errorf("request %d = %s %s", i, req.Method, req.URL.Path)
		}
		if q.Get("action") != "list" || q.Get("limit") != "200" || q.Get("offset") != wantOffset {
			t.Errorf("request %d query = %q", i, req.URL.RawQuery)
		}
		if accept := req.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("request %d accept = %q", i, accept)
		}
	}
}

func TestDlArchiveClientReportsListStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	want := "dlarchive: list: 500 Internal Server Error"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDlArchiveClientCachesIndexWithinTTL(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"ok":true,"items":[` +
			`{"id":"a1","filename":"Example One (USA).zip","size":1,"download_url":"https://files.example.invalid/one.zip","folder":"Mirror/Nintendo_Nes"}` +
			`],"total":1}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	for range 2 {
		if _, err := source.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil {
			t.Fatalf("search: %v", err)
		}
	}
	if n := len(log.snapshot()); n != 1 {
		t.Fatalf("cached search made %d requests, want 1", n)
	}

	source.TTL = 0
	if _, err := source.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil {
		t.Fatalf("search after expiry: %v", err)
	}
	if n := len(log.snapshot()); n != 2 {
		t.Fatalf("expired search made %d total requests, want 2", n)
	}
}

func TestDlArchiveClientResolvesFileURL(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://files.example.invalid/one.zip"}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	got, err := source.FileURL(context.Background(), "abc/1")
	if err != nil {
		t.Fatalf("file url: %v", err)
	}
	if got != "https://files.example.invalid/one.zip" {
		t.Errorf("url = %q", got)
	}
	reqs := log.snapshot()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(reqs))
	}
	if raw := reqs[0].URL.RawQuery; raw != "action=file-url&id=abc%2F1" {
		t.Errorf("query = %q", raw)
	}
	if accept := reqs[0].Header.Get("Accept"); accept != "application/json" {
		t.Errorf("accept = %q", accept)
	}
}

func TestDlArchiveClientReportsMissingFileURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"url":""}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	_, err := source.FileURL(context.Background(), "abc/1")
	want := "dlarchive: no download URL for abc/1"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDlArchiveClientReportsFileURLStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	_, err := source.FileURL(context.Background(), "abc/1")
	want := "dlarchive: file-url: 404 Not Found"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestDlArchiveClientUsesInjectedHTTPClient(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return cannedJSON(r, `{"ok":true,"url":"https://files.example.invalid/one.zip"}`), nil
	})}

	source := NewDlArchive(client)
	source.BaseURL = "http://dlarchive.invalid"

	got, err := source.FileURL(context.Background(), "abc")
	if err != nil {
		t.Fatalf("file url: %v", err)
	}
	if calls != 1 || got != "https://files.example.invalid/one.zip" {
		t.Fatalf("calls = %d, url = %q", calls, got)
	}
}

func TestArchiveOrgClientTrailingSlashBaseURLSendsNoDoubleSlash(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL + "/"
	source.Sets = map[string][]string{"nes": {"item one"}}

	if _, err := source.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	reqs := log.snapshot()
	if len(reqs) != 1 || reqs[0].URL.EscapedPath() != "/metadata/item%20one" {
		t.Fatalf("requests = %+v, want path /metadata/item%%20one", reqs)
	}
}

func TestDlArchiveClientTrailingSlashBaseURLSendsNoDoubleSlash(t *testing.T) {
	var log requestLog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"ok":true,"url":"https://files.example.invalid/one.zip"}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL + "/"

	if _, err := source.FileURL(context.Background(), "abc"); err != nil {
		t.Fatalf("file url: %v", err)
	}
	reqs := log.snapshot()
	if len(reqs) != 1 || reqs[0].URL.EscapedPath() != "/files_api.php" {
		t.Fatalf("requests = %+v, want path /files_api.php", reqs)
	}
}

func TestDlArchiveClientReportsStatusWithoutTransportRequest(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("missing")),
		}, nil
	})}
	source := NewDlArchive(client)
	source.BaseURL = "http://dlarchive.invalid"

	_, err := source.FileURL(context.Background(), "abc")
	want := "dlarchive: file-url: 404 Not Found"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestArchiveOrgClientIgnoresUnreadMetadataFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":5,"errcode":"x","metadata":5,"files":[]}`))
	}))
	defer server.Close()

	source := NewArchiveOrg(server.Client())
	source.BaseURL = server.URL
	source.Sets = map[string][]string{"nes": {"item-one"}}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil || len(games) != 0 {
		t.Fatalf("games = %+v, err = %v; want no games and no error", games, err)
	}
}

type repeatReader byte

func (r repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(r)
	}
	return len(p), nil
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func paddedJSON(document string, total int64) io.Reader {
	return io.MultiReader(strings.NewReader(document), io.LimitReader(repeatReader(' '), total-int64(len(document))))
}

func bodyClient(status string, code int, body io.ReadCloser) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: code,
			Status:     status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       body,
			Request:    r,
		}, nil
	})}
}

func TestArchiveOrgClientBoundsMetadataBody(t *testing.T) {
	const document = `{"files":[{"name":"Example Game (USA).zip","size":5}]}`
	for _, tc := range []struct {
		name    string
		total   int64
		tooLong bool
	}{
		{"at the limit", archiveOrgMaxMetadataBytes, false},
		{"one byte over", archiveOrgMaxMetadataBytes + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: paddedJSON(document, tc.total)}
			source := NewArchiveOrg(bodyClient("200 OK", http.StatusOK, body))
			source.BaseURL = "http://archive.invalid"
			source.Sets = map[string][]string{"nes": {"item-one"}}

			games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
			if !body.closed {
				t.Error("body not closed")
			}
			if tc.tooLong {
				if !errors.Is(err, errBodyTooLarge) || !strings.HasPrefix(err.Error(), "archive.org item-one: metadata item-one: ") {
					t.Fatalf("err = %v, want wrapped errBodyTooLarge", err)
				}
				return
			}
			if err != nil || len(games) != 1 {
				t.Fatalf("games = %+v, err = %v", games, err)
			}
		})
	}
}

func TestDlArchiveClientBoundsResponseBody(t *testing.T) {
	const document = `{"ok":true,"items":[{"id":"a1","filename":"Example One (USA).zip","folder":"Mirror/Nintendo_Nes"}],"total":1}`
	const fileURL = `{"ok":true,"url":"https://files.example.invalid/one.zip"}`
	for _, tc := range []struct {
		name    string
		total   int64
		tooLong bool
	}{
		{"at the limit", dlArchiveMaxResponseBytes, false},
		{"one byte over", dlArchiveMaxResponseBytes + 1, true},
	} {
		t.Run("list "+tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: paddedJSON(document, tc.total)}
			source := NewDlArchive(bodyClient("200 OK", http.StatusOK, body))
			source.BaseURL = "http://dlarchive.invalid"

			games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
			if !body.closed {
				t.Error("body not closed")
			}
			if tc.tooLong {
				if !errors.Is(err, errBodyTooLarge) || !strings.HasPrefix(err.Error(), "dlarchive: list: ") {
					t.Fatalf("err = %v, want wrapped errBodyTooLarge", err)
				}
				return
			}
			if err != nil || len(games) != 1 {
				t.Fatalf("games = %+v, err = %v", games, err)
			}
		})
		t.Run("file url "+tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: paddedJSON(fileURL, tc.total)}
			source := NewDlArchive(bodyClient("200 OK", http.StatusOK, body))
			source.BaseURL = "http://dlarchive.invalid"

			got, err := source.FileURL(context.Background(), "abc")
			if !body.closed {
				t.Error("body not closed")
			}
			if tc.tooLong {
				if !errors.Is(err, errBodyTooLarge) || !strings.HasPrefix(err.Error(), "dlarchive: file-url: ") {
					t.Fatalf("err = %v, want wrapped errBodyTooLarge", err)
				}
				return
			}
			if err != nil || got != "https://files.example.invalid/one.zip" {
				t.Fatalf("url = %q, err = %v", got, err)
			}
		})
	}
}

func TestProviderClientsCloseBodyOnStatusError(t *testing.T) {
	archiveBody := &trackedBody{Reader: strings.NewReader("busy")}
	archive := NewArchiveOrg(bodyClient("503 Service Unavailable", http.StatusServiceUnavailable, archiveBody))
	archive.BaseURL = "http://archive.invalid"
	archive.Sets = map[string][]string{"nes": {"item-one"}}
	if _, err := archive.Search(context.Background(), rom.Query{Platform: "nes"}); err == nil || !archiveBody.closed {
		t.Fatalf("archive.org err = %v, closed = %t", err, archiveBody.closed)
	}

	dlBody := &trackedBody{Reader: strings.NewReader("busy")}
	dl := NewDlArchive(bodyClient("503 Service Unavailable", http.StatusServiceUnavailable, dlBody))
	dl.BaseURL = "http://dlarchive.invalid"
	if _, err := dl.FileURL(context.Background(), "abc"); err == nil || !dlBody.closed {
		t.Fatalf("dlarchive err = %v, closed = %t", err, dlBody.closed)
	}
}

func TestProviderClientsRefuseMissingHTTPClient(t *testing.T) {
	archive := NewArchiveOrg(nil)
	archive.BaseURL = "http://archive.invalid"
	archive.Sets = map[string][]string{"nes": {"item-one"}}
	_, err := archive.Search(context.Background(), rom.Query{Platform: "nes"})
	if want := "archive.org item-one: no HTTP client is configured"; err == nil || err.Error() != want {
		t.Errorf("archive.org err = %v, want %q", err, want)
	}

	dl := NewDlArchive(nil)
	dl.BaseURL = "http://dlarchive.invalid"
	_, err = dl.Search(context.Background(), rom.Query{Platform: "nes"})
	if want := "dlarchive: no HTTP client is configured"; err == nil || err.Error() != want {
		t.Errorf("dlarchive search err = %v, want %q", err, want)
	}
	_, err = dl.FileURL(context.Background(), "abc")
	if want := "dlarchive: no HTTP client is configured"; err == nil || err.Error() != want {
		t.Errorf("dlarchive file url err = %v, want %q", err, want)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderClientsAcceptAnyRequestDoer(t *testing.T) {
	archive := NewArchiveOrg(doerFunc(func(r *http.Request) (*http.Response, error) {
		return cannedJSON(r, `{"files":[{"name":"Example Game (USA).zip","size":5}]}`), nil
	}))
	archive.BaseURL = "http://archive.invalid"
	archive.Sets = map[string][]string{"nes": {"example-item"}}
	if games, err := archive.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil || len(games) != 1 {
		t.Errorf("archive.org games = %+v, err = %v", games, err)
	}

	dl := NewDlArchive(doerFunc(func(r *http.Request) (*http.Response, error) {
		return cannedJSON(r, `{"ok":true,"url":"https://files.example.invalid/one.zip"}`), nil
	}))
	dl.BaseURL = "http://dlarchive.invalid"
	if got, err := dl.FileURL(context.Background(), "abc"); err != nil || got != "https://files.example.invalid/one.zip" {
		t.Errorf("dlarchive url = %q, err = %v", got, err)
	}
}

func TestArchiveOrgClientReportsContractMismatchesFromGeneratedTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"boolean size", `{"files":[{"name":"A (USA).zip","size":true}]}`},
		{"fractional size", `{"files":[{"name":"A (USA).zip","size":1.5}]}`},
		{"files string", `{"files":"x"}`},
		{"file number", `{"files":[1]}`},
		{"name number", `{"files":[{"name":5}]}`},
		{"md5 number", `{"files":[{"name":"A (USA).zip","md5":5}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := NewArchiveOrg(bodyClient("200 OK", http.StatusOK, io.NopCloser(strings.NewReader(tc.body))))
			source.BaseURL = "http://archive.invalid"
			source.Sets = map[string][]string{"nes": {"item-one"}}

			_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
			var typed *json.UnmarshalTypeError
			if !errors.As(err, &typed) || !strings.HasPrefix(err.Error(), "archive.org item-one: metadata item-one: ") {
				t.Fatalf("err = %v, want wrapped *json.UnmarshalTypeError", err)
			}
			for _, legacy := range []string{"romsource.", "itemFile", "itemMetadata"} {
				if strings.Contains(err.Error(), legacy) {
					t.Errorf("err = %v carries legacy label %q", err, legacy)
				}
			}
		})
	}
}

func TestDlArchiveClientReportsContractMismatchesFromGeneratedTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		list   bool
		prefix string
	}{
		{"string size", `{"ok":true,"items":[{"id":"a","filename":"A (USA).zip","size":"1","folder":"X/Nintendo_Nes"}],"total":1}`, true, "dlarchive: list: "},
		{"items object", `{"ok":true,"items":{},"total":1}`, true, "dlarchive: list: "},
		{"ok string", `{"ok":"yes","items":[],"total":0}`, true, "dlarchive: list: "},
		{"total string", `{"ok":true,"items":[],"total":"1"}`, true, "dlarchive: list: "},
		{"item id number", `{"items":[{"id":1,"filename":"A (USA).zip"}],"total":1}`, true, "dlarchive: list: "},
		{"list top array", `[]`, true, "dlarchive: list: "},
		{"url number", `{"ok":true,"url":5}`, false, "dlarchive: file-url: "},
		{"file url ok string", `{"ok":"true","url":"u"}`, false, "dlarchive: file-url: "},
		{"file url top array", `[]`, false, "dlarchive: file-url: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := NewDlArchive(bodyClient("200 OK", http.StatusOK, io.NopCloser(strings.NewReader(tc.body))))
			source.BaseURL = "http://dlarchive.invalid"

			var err error
			if tc.list {
				_, err = source.Search(context.Background(), rom.Query{Platform: "nes"})
			} else {
				_, err = source.FileURL(context.Background(), "abc")
			}
			var typed *json.UnmarshalTypeError
			if !errors.As(err, &typed) || !strings.HasPrefix(err.Error(), tc.prefix) {
				t.Fatalf("err = %v, want %q prefix wrapping *json.UnmarshalTypeError", err, tc.prefix)
			}
			for _, legacy := range []string{"romsource.", "dlArchiveItem", "struct {"} {
				if strings.Contains(err.Error(), legacy) {
					t.Errorf("err = %v carries legacy label %q", err, legacy)
				}
			}
		})
	}
}

func TestDlArchiveClientReportsErrorVariantAsMissingFileURL(t *testing.T) {
	for name, body := range map[string]string{
		"failure payload":            `{"ok":false,"error":"not found"}`,
		"failure payload with url":   `{"ok":false,"url":"https://files.example.invalid/one.zip"}`,
		"success without url":        `{"ok":true}`,
		"success with an empty url":  `{"ok":true,"url":""}`,
		"payload without ok present": `{"url":"https://files.example.invalid/one.zip"}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := NewDlArchive(bodyClient("200 OK", http.StatusOK, io.NopCloser(strings.NewReader(body))))
			source.BaseURL = "http://dlarchive.invalid"

			_, err := source.FileURL(context.Background(), "abc")
			if want := "dlarchive: no download URL for abc"; err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestProviderClientsDecodeJSONWithoutContentType(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"url":"https://files.example.invalid/one.zip","files":[]}`)),
			Request:    r,
		}, nil
	})}
	dl := NewDlArchive(client)
	dl.BaseURL = "http://dlarchive.invalid"
	if got, err := dl.FileURL(context.Background(), "abc"); err != nil || got != "https://files.example.invalid/one.zip" {
		t.Errorf("dlarchive url = %q, err = %v", got, err)
	}
	archive := NewArchiveOrg(client)
	archive.BaseURL = "http://archive.invalid"
	archive.Sets = map[string][]string{"nes": {"item-one"}}
	if _, err := archive.Search(context.Background(), rom.Query{Platform: "nes"}); err != nil {
		t.Errorf("archive.org err = %v", err)
	}
}
