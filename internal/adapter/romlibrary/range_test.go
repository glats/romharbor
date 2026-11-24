package romlibrary

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestInstallRejectsAnImpossibleContentRange(t *testing.T) {
	tests := []struct {
		name   string
		header string
		body   string
	}{
		{name: "an end before the start", header: "bytes 3-1/6", body: "BAD"},
		{name: "an end beyond the total", header: "bytes 3-6/6", body: "defg"},
		{name: "an end that is not the last byte", header: "bytes 3-4/6", body: "de"},
		{name: "a body that does not match the range", header: "bytes 3-5/6", body: "de"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, root := newLibrary(t)
			dir := filepath.Join(root, "snes")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "Game.sfc.part"), []byte("abc"), 0o644); err != nil {
				t.Fatal(err)
			}
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Range") != "" {
					w.Header().Set("Content-Range", tt.header)
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write([]byte(tt.body))
					return
				}
				_, _ = w.Write([]byte("abcdef"))
			})

			res, err := l.Install(context.Background(), rom.InstallRequest{
				Platform: snesPlatform,
				File:     rom.File{Name: "Game.sfc", URL: srv.URL, Size: 6},
			}, nil)
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if got := readFile(t, l.abs(res.Path)); string(got) != "abcdef" {
				t.Errorf("installed bytes = %q, want the whole file %q", got, "abcdef")
			}
			assertAbsent(t, filepath.Join(dir, "Game.sfc.part"))
		})
	}
}

func TestInstallAcceptsAContentRangeWithoutATotal(t *testing.T) {
	l, root := newLibrary(t)
	dir := filepath.Join(root, "snes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Game.sfc.part"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 3-5/*")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("def"))
			return
		}
		_, _ = w.Write([]byte("abcdef"))
	})

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, l.abs(res.Path)); string(got) != "abcdef" {
		t.Errorf("installed bytes = %q, want %q", got, "abcdef")
	}
}
