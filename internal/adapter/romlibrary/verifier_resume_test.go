package romlibrary

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierResume(t *testing.T) {
	for _, mode := range []string{"same", "changed", "absent", "complete"} {
		t.Run(mode, func(t *testing.T) {
			l, root := newLibrary(t)
			dir := filepath.Join(root, "snes")
			_ = os.MkdirAll(dir, 0o755)
			part := filepath.Join(dir, "Game.sfc.part")
			initial := "abc"
			if mode == "complete" {
				initial = "abcdef"
			}
			_ = os.WriteFile(part, []byte(initial), 0o600)
			if mode != "absent" {
				_ = os.WriteFile(part+".meta", []byte(`"old"`), 0o600)
			}
			s := serve(t, func(w http.ResponseWriter, r *http.Request) {
				want := `"old"`
				if mode == "absent" {
					want = ""
				}
				if r.Header.Get("If-Range") != want {
					t.Errorf("If-Range %q want %q", r.Header.Get("If-Range"), want)
				}
				switch mode {
				case "changed":
					w.Header().Set("ETag", `"new"`)
					_, _ = w.Write([]byte("uvwxyz"))
				case "complete":
					w.WriteHeader(416)
				default:
					w.Header().Set("Content-Range", "bytes 3-5/6")
					w.WriteHeader(206)
					_, _ = w.Write([]byte("def"))
				}
			})
			r, e := l.Install(context.Background(), rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc", URL: s.URL, Size: 6}}, nil)
			if e != nil {
				t.Fatal(e)
			}
			want := "abcdef"
			if mode == "changed" {
				want = "uvwxyz"
			}
			b, _ := os.ReadFile(r.Path)
			if string(b) != want {
				t.Fatalf("%q want %q", b, want)
			}
			for _, p := range []string{part, part + ".meta"} {
				if _, e = os.Stat(p); !os.IsNotExist(e) {
					t.Fatalf("leftover %s", p)
				}
			}
		})
	}
}

func TestVerifierSetCaps(t *testing.T) {
	for _, format := range []string{"zip", "7z"} {
		for _, cap := range []string{"entries", "bytes"} {
			t.Run(format+cap, func(t *testing.T) {
				entries := []zipEntry{{name: "Game.cue", body: "FILE \"Game.bin\" BINARY\n"}, {name: "Game.bin", body: "123456789"}}
				a := zipArchive(t, entries)
				if format == "7z" {
					a = sevenZipArchive(t, entries)
				}
				s := serve(t, servedBody(a))
				l, root := newLibrary(t)
				l.limits = extractLimits{entries: 10, bytes: 100}
				if cap == "entries" {
					l.limits.entries = 1
				} else {
					l.limits.bytes = 10
				}
				_, e := l.Install(context.Background(), rom.InstallRequest{Platform: rom.Platform{Dir: "psx", Extensions: []string{".cue", ".bin"}}, File: rom.File{Name: "Game." + format, URL: s.URL}}, nil)
				if e == nil {
					t.Fatal("cap bypassed")
				}
				_ = filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
					if e == nil && !i.IsDir() {
						t.Errorf("leaked %s", p)
					}
					return nil
				})
			})
		}
	}
}
