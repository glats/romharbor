package romsource_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"app.ddd/internal/adapter/romsource"
	"app.ddd/internal/domain/rom"
)

type independentTransport func(*http.Request) (*http.Response, error)

type independentContextKey struct{}

func (f independentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type independentBody struct {
	io.Reader
	closed bool
}

func (b *independentBody) Close() error { b.closed = true; return nil }

func TestIndependentProviderTranscript(t *testing.T) {
	var transcript strings.Builder
	for _, provider := range []string{"archive", "dl", "file", "html"} {
		payloads := map[string][]string{
			"archive": {`{"files":[{"name":"Beta (USA).nes","size":"24","md5":"abc"},{"name":"Alpha.nes","size":0},{"name":"No.nes"},{"name":"readme.txt","size":1}]}`, `[]`, `{}`, `{"files":null}`, `{"files":[{"name":"A.nes","size":true}]}`, `{"files":[{"name":"A.nes","size":{},"md5":3}]}`, `{"files":[{"name":3,"size":{}}]}`, `{"files":[{"name":3},{"name":"A.nes","size":{}}]}`, `{"files":[{"size":"invalid","name":"A.nes"}]}`, `{`},
			"dl":      {`{"items":[{"id":"a b&+/?","filename":"Alpha.nes","folder":"Nintendo_Nes","size":12}],"total":1}`, `{"ok":false,"error":"missing"}`, `{"items":[],"total":100}`, `{"items":[{"size":"12"}]}`, `[]`, `{`},
			"file":    {`{"ok":true,"url":"https://files.invalid/a"}`, `{"ok":false,"error":"missing"}`, `{"ok":true}`, `{"ok":true,"url":5}`, `[]`, `{`},
			"html":    {`<a href="/roms/Nintendo%20Entertainment%20System/A.zip">A</a><a href="/roms/Nintendo%20Entertainment%20System/A.zip">again</a>`, ``},
		}
		for i, payload := range payloads[provider] {
			body := &independentBody{Reader: strings.NewReader(payload)}
			var request string
			ctx := context.WithValue(context.Background(), independentContextKey{}, "injection")
			client := &http.Client{Transport: independentTransport(func(r *http.Request) (*http.Response, error) {
				if r.Context().Value(independentContextKey{}) != "injection" {
					t.Fatal("context lost")
				}
				request = r.Method + " " + r.URL.String() + " " + r.Header.Get("Accept")
				return &http.Response{StatusCode: 200, Status: "200 OK", Body: body, Header: make(http.Header)}, nil
			})}
			var value any
			var err error
			switch provider {
			case "archive":
				s := romsource.NewArchiveOrg(client)
				s.BaseURL = "https://provider.invalid/prefix/"
				s.Sets = map[string][]string{"nes": {"set space&+"}}
				value, err = s.Search(ctx, rom.Query{Platform: "nes"})
			case "dl":
				s := romsource.NewDlArchive(client)
				s.BaseURL = "https://provider.invalid/prefix/"
				value, err = s.Search(ctx, rom.Query{Platform: "nes"})
			case "file":
				s := romsource.NewDlArchive(client)
				s.BaseURL = "https://provider.invalid/prefix/"
				value, err = s.FileURL(ctx, "a b&+/?")
			case "html":
				s := romsource.NewLolRoms(client)
				s.BaseURL = "https://provider.invalid/prefix/"
				value, err = s.Search(ctx, rom.Query{Text: "A &+/?", Platform: "nes"})
			}
			if !body.closed {
				t.Fatal("body not closed")
			}
			var typed *json.UnmarshalTypeError
			raw, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			fmt.Fprintf(&transcript, "PROBE %s/%d request=%q value=%s error=%v typed=%t\n", provider, i, request, raw, err, errors.As(err, &typed))
		}
	}
	want, err := os.ReadFile("testdata/independent-provider-transcript.txt")
	if err != nil {
		t.Fatal(err)
	}
	if transcript.String() != string(want) {
		t.Fatalf("provider transcript mismatch\ngot:\n%swant:\n%s", transcript.String(), want)
	}
}

func TestIndependentPagingCancellationAndStatus(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: independentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("offset") != fmt.Sprint(calls-1) {
			t.Fatalf("offset: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"items":[{"id":"%d","filename":"A.nes","folder":"Nes"}],"total":2}`, calls)))}, nil
	})}
	s := romsource.NewDlArchive(client)
	for range 2 {
		games, err := s.Search(context.Background(), rom.Query{Platform: "nes", Offset: 1, Limit: 1})
		if err != nil || len(games) != 1 || games[0].ID != "dlarchive:2" {
			t.Fatalf("page: %+v %v", games, err)
		}
	}
	if calls != 2 {
		t.Fatalf("cache made %d requests", calls)
	}
	for _, provider := range []string{"archive", "dl", "html"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c := &http.Client{Transport: independentTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}
		var err error
		switch provider {
		case "archive":
			a := romsource.NewArchiveOrg(c)
			a.Sets = map[string][]string{"nes": {"x"}}
			_, err = a.Search(ctx, rom.Query{Platform: "nes"})
		case "dl":
			_, err = romsource.NewDlArchive(c).Search(ctx, rom.Query{})
		case "html":
			_, err = romsource.NewLolRoms(c).Search(ctx, rom.Query{Text: "A"})
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancellation: %v", provider, err)
		}
		body := &independentBody{Reader: strings.NewReader("not JSON")}
		c.Transport = independentTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Body: body}, nil
		})
		switch provider {
		case "archive":
			a := romsource.NewArchiveOrg(c)
			a.Sets = map[string][]string{"nes": {"x"}}
			_, err = a.Search(context.Background(), rom.Query{Platform: "nes"})
		case "dl":
			_, err = romsource.NewDlArchive(c).FileURL(context.Background(), "a b&+/?")
		case "html":
			_, err = romsource.NewLolRoms(c).Search(context.Background(), rom.Query{Text: "A"})
		}
		if err == nil || !body.closed {
			t.Fatalf("status/closure %s: %v %t", provider, err, body.closed)
		}
		fmt.Printf("STATUS %s %v\n", provider, err)
	}
}
