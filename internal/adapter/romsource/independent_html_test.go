package romsource_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"app.ddd/internal/adapter/romsource"
	"app.ddd/internal/domain/rom"
)

func TestIndependentHTMLMarkupAndBound(t *testing.T) {
	href := "/roms/Nintendo%20Entertainment%20System/A.zip"
	for _, tt := range []struct {
		name, html string
		want       int
	}{
		{"nested uppercase", "<A HREF='" + href + "'><span>A</span></A>", 1},
		{"unquoted", "<a href=" + href + ">A</a>", 1},
		{"duplicate", "<a href='" + href + "'>A</a><a href='" + href + "'>B</a>", 1},
		{"comment", "<!-- <a href='" + href + "'>A</a> -->", 0},
		{"script", "<script>const x=\"<a href='" + href + "'>A</a>\";</script>", 0},
		{"beyond bound", strings.Repeat(" ", 8<<20) + "<a href='" + href + "'>A</a>", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := &independentBody{Reader: strings.NewReader(tt.html)}
			client := &http.Client{Transport: independentTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Accept") != "text/html" || r.Header.Get("User-Agent") == "" {
					t.Fatal("headers lost")
				}
				return &http.Response{StatusCode: 200, Body: body}, nil
			})}
			source := romsource.NewLolRoms(client)
			source.BaseURL = "https://local.invalid"
			games, err := source.Search(context.Background(), rom.Query{Text: "A", Platform: "nes"})
			if err != nil || len(games) != tt.want || !body.closed {
				t.Fatalf("games=%+v err=%v closed=%t", games, err, body.closed)
			}
			if len(games) > 0 && games[0].Files[0].URL != source.BaseURL+href {
				t.Fatalf("URL=%s", games[0].Files[0].URL)
			}
		})
	}
}
