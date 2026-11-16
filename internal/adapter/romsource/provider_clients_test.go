package romsource

import (
	"context"
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
	prefix := "archive.org missing-item: metadata missing-item: "
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("err = %v, want prefix %q", err, prefix)
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

func TestArchiveOrgClientMapsFileSizeVariants(t *testing.T) {
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
		"Game Six (USA)":   0,
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
	want := "dlarchive: files_api.php?action=list&limit=200&offset=0: 500 Internal Server Error"
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
	want := "dlarchive: files_api.php?action=file-url&id=abc%2F1: 404 Not Found"
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

func TestArchiveOrgClientPreservesTrailingSlashBaseURL(t *testing.T) {
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
	if len(reqs) != 1 || reqs[0].URL.EscapedPath() != "//metadata/item%20one" {
		t.Fatalf("requests = %+v, want path //metadata/item%%20one", reqs)
	}
}

func TestDlArchiveClientPreservesTrailingSlashBaseURL(t *testing.T) {
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
	if len(reqs) != 1 || reqs[0].URL.EscapedPath() != "//files_api.php" {
		t.Fatalf("requests = %+v, want path //files_api.php", reqs)
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
	want := "dlarchive: files_api.php?action=file-url&id=abc: 404 Not Found"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestArchiveOrgClientKeepsLegacyDecodeLabels(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"boolean size", `{"files":[{"name":"A (USA).zip","size":true}]}`, "json: cannot unmarshal bool into Go struct field itemFile.files.size of type int64"},
		{"fractional size", `{"files":[{"name":"A (USA).zip","size":1.5}]}`, "json: cannot unmarshal number 1.5 into Go struct field itemFile.files.size of type int64"},
		{"empty array", `[]`, "json: cannot unmarshal array into Go value of type romsource.itemMetadata"},
		{"files string", `{"files":"x"}`, "json: cannot unmarshal string into Go struct field itemMetadata.files of type []romsource.itemFile"},
		{"file number", `{"files":[1]}`, "json: cannot unmarshal number into Go struct field itemMetadata.files of type romsource.itemFile"},
		{"name number", `{"files":[{"name":5}]}`, "json: cannot unmarshal number into Go struct field itemFile.files.name of type string"},
		{"md5 number", `{"files":[{"name":"A (USA).zip","md5":5}]}`, "json: cannot unmarshal number into Go struct field itemFile.files.md5 of type string"},
		{"size before md5", `{"files":[{"name":"A (USA).zip","size":true},{"name":"B (USA).zip","md5":5}]}`, "json: cannot unmarshal bool into Go struct field itemFile.files.size of type int64"},
		{"md5 before size", `{"files":[{"name":"A (USA).zip","md5":5},{"name":"B (USA).zip","size":true}]}`, "json: cannot unmarshal bool into Go struct field itemFile.files.size of type int64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			source := NewArchiveOrg(server.Client())
			source.BaseURL = server.URL
			source.Sets = map[string][]string{"nes": {"item-one"}}

			_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
			want := "archive.org item-one: metadata item-one: " + tc.want
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

func TestDlArchiveClientKeepsLegacyDecodeLabels(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"string size", `{"ok":true,"items":[{"id":"a","filename":"A (USA).zip","size":"1","folder":"X/Nintendo_Nes"}],"total":1}`, "json: cannot unmarshal string into Go struct field dlArchiveItem.items.size of type int64"},
		{"items object", `{"ok":true,"items":{},"total":1}`, "json: cannot unmarshal object into Go struct field .items of type []romsource.dlArchiveItem"},
		{"ok string", `{"ok":"yes","items":[],"total":0}`, "json: cannot unmarshal string into Go struct field .ok of type bool"},
		{"total string", `{"ok":true,"items":[],"total":"1"}`, "json: cannot unmarshal string into Go struct field .total of type int"},
		{"item id number", `{"items":[{"id":1,"filename":"A (USA).zip","size":1,"folder":"X/Nintendo_Nes"}],"total":1}`, "json: cannot unmarshal number into Go struct field dlArchiveItem.items.id of type string"},
		{"top array", `[]`, `json: cannot unmarshal array into Go value of type struct { OK bool "json:\"ok\""; Items []romsource.dlArchiveItem "json:\"items\""; Total int "json:\"total\"" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			source := NewDlArchive(server.Client())
			source.BaseURL = server.URL

			_, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDlArchiveClientKeepsLegacyFileURLDecodeLabels(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"url number", `{"ok":true,"url":5}`, "json: cannot unmarshal number into Go struct field .url of type string"},
		{"ok string", `{"ok":"true","url":"u"}`, "json: cannot unmarshal string into Go struct field .ok of type bool"},
		{"top array", `[]`, `json: cannot unmarshal array into Go value of type struct { OK bool "json:\"ok\""; URL string "json:\"url\"" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			source := NewDlArchive(server.Client())
			source.BaseURL = server.URL

			_, err := source.FileURL(context.Background(), "abc")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
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
