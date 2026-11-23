package romsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierSourceErrors(t *testing.T) {
	for _, mode := range []string{"size", "escape", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "size":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"files":[{"name":"NES/Good.zip","size":"12"},{"name":"NES/Bad.zip","size":"bad"}]}`))
				case "escape":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<a href="/Nintendo/Nintendo%20Entertainment%20System/Bad%ZZ.zip">Bad.zip</a>`))
				case "overflow":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(strings.Repeat("x", (8<<20)+1)))
				}
			}))
			defer s.Close()
			if mode == "size" {
				a := NewArchiveOrg(s.Client())
				a.BaseURL = s.URL
				a.Sets = map[string][]string{"nes": {"fixture"}}
				g, e := a.Search(context.Background(), rom.Query{Platform: "nes"})
				if e != nil || len(g) != 1 || g[0].Size != 12 {
					t.Fatalf("%+v %v", g, e)
				}
			} else {
				a := NewLolRoms(s.Client())
				a.BaseURL = s.URL
				_, e := a.Search(context.Background(), rom.Query{Platform: "nes", Text: "bad"})
				if e == nil {
					t.Fatal("invalid response accepted")
				}
			}
		})
	}
}
