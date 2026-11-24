package romsource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestDlArchiveSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("action") {
		case "list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"items": []map[string]any{
					{
						"id":           "abc123",
						"filename":     "Super Mario World (U) [!].zip",
						"size":         347370,
						"mime":         "application/zip",
						"download_url": "https://example.invalid/smw.zip",
						"folder":       "Upload_Kazu/Beastrom/Super_Nintendo",
					},
					{
						"id":       "def456",
						"filename": "Some Game.nsp",
						"size":     1024,
						"folder":   "Upload_Kazu/Beastrom/Game_ROMS",
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	games, err := source.Search(context.Background(), rom.Query{Platform: "snes"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1: %+v", len(games), games)
	}
	game := games[0]
	if game.Title != "Super Mario World (U) [!]" {
		t.Errorf("title = %q", game.Title)
	}
	if game.Platform != "snes" {
		t.Errorf("platform = %q", game.Platform)
	}
	if game.Files[0].URL != "https://example.invalid/smw.zip" {
		t.Errorf("url = %q", game.Files[0].URL)
	}
	if !game.Files[0].Archive {
		t.Error("zip must be marked as archive")
	}
}

func TestDlArchiveRefusedPageIsAnErrorAndIsNeverCached(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"provider refused","items":[` +
			`{"id":"a1","filename":"Refused (USA).zip","size":1,"folder":"Mirror/Nintendo_Nes"}` +
			`],"total":1}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	for range 2 {
		games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
		if err == nil {
			t.Fatalf("Search() = %+v, want the refusal of the provider", games)
		}
		if len(games) != 0 {
			t.Fatalf("Search() = %+v, want no game from a refused page", games)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("the refused page was asked %d times, want 2", got)
	}
}

func TestDlArchiveConcurrentSearchesShareOneIndexFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[` +
			`{"id":"a1","filename":"Example One (USA).zip","size":1,"folder":"Mirror/Nintendo_Nes"}` +
			`],"total":1}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	const callers = 8
	games := make([][]rom.Game, callers)
	errs := make([]error, callers)
	ready := &sync.WaitGroup{}
	ready.Add(callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			games[i], errs[i] = source.Search(context.Background(), rom.Query{Platform: "nes"})
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("the index was fetched %d times, want 1", got)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d error = %v", i, errs[i])
		}
		if len(games[i]) != 1 || games[i][0].ID != "dlarchive:a1" {
			t.Fatalf("caller %d = %+v, want the shared index", i, games[i])
		}
	}
}

func TestDlArchiveConcurrentSearchesShareOneFailedFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(200 * time.Millisecond)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	const callers = 8
	errs := make([]error, callers)
	ready := &sync.WaitGroup{}
	ready.Add(callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			_, errs[i] = source.Search(context.Background(), rom.Query{Platform: "nes"})
		}()
	}
	ready.Wait()
	close(start)
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Fatalf("the index was fetched %d times, want 1", got)
	}
	for i := range callers {
		if errs[i] == nil {
			t.Fatalf("caller %d error = nil, want the failure of the fetch", i)
		}
	}
}

func TestDlArchiveCallerWithACancelledContextDoesNotWaitForTheFetch(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	releaseFetch := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(arrived) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[],"total":0}`))
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseFetch)

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	fetching := make(chan struct{})
	go func() {
		defer close(fetching)
		_, _ = source.Search(context.Background(), rom.Query{Platform: "nes"})
	}()
	<-arrived

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := source.Search(ctx, rom.Query{Platform: "nes"})
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Search() error = %v, want the cancelled context", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled caller waited for the fetch to finish")
	}
	releaseFetch()
	<-fetching
}

func TestDlArchiveEmptyIndexIsNeverCached(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[],"total":0}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	for range 2 {
		games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(games) != 0 {
			t.Fatalf("Search() = %+v, want no game from an empty index", games)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("the empty index was asked %d times, want 2", got)
	}
}

func TestDlArchiveCancelledCallerNeverReachesTheProvider(t *testing.T) {
	var calls atomic.Int32
	source := NewDlArchive(doerFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, r.Context().Err()
	}))
	source.BaseURL = "https://provider.invalid/prefix/"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := source.Search(ctx, rom.Query{Platform: "nes"})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want the cancelled context", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("the provider was asked %d times, want 0", got)
	}
}

func TestDlArchiveCancelledCallerDoesNotStopTheOthers(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"items":[{"id":"a1","filename":"Example One (USA).zip","size":1,"folder":"Mirror/Nintendo_Nes"}],"total":1}`))
	}))
	defer server.Close()

	source := NewDlArchive(server.Client())
	source.BaseURL = server.URL

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Search(cancelled, rom.Query{Platform: "nes"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want the cancelled context", err)
	}

	games, err := source.Search(context.Background(), rom.Query{Platform: "nes"})
	if err != nil {
		t.Fatalf("Search() error = %v, want the index of the live caller", err)
	}
	if len(games) != 1 || games[0].ID != "dlarchive:a1" {
		t.Fatalf("Search() = %+v, want the index of the live caller", games)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the index was fetched %d times, want 1", got)
	}
}

func TestDlArchiveFolderPlatform(t *testing.T) {
	cases := map[string]string{
		"Upload_Kazu/Beastrom/Nintendo_DS":    "nds",
		"Upload_Kazu/Beastrom/Super_Nintendo": "snes",
		"Upload_Kazu/Beastrom/Game_ROMS":      "switch",
		"Upload_Kazu/Beastrom/2026/11/13":     "",
	}
	for folder, want := range cases {
		got, ok := dlArchiveFolderPlatform(folder)
		if want == "" {
			if ok {
				t.Errorf("folder %q must not match a platform, got %q", folder, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("folder %q = %q, want %q", folder, got, want)
		}
	}
}
