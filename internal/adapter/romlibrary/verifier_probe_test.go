package romlibrary

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierNestedSet(t *testing.T) {
	a := zipArchive(t, []zipEntry{{name: "Game.cue", body: "FILE \"tracks/one.bin\" BINARY\n"}, {name: "tracks/one.bin", body: "track"}})
	s := serve(t, servedBody(a))
	l, _ := newLibrary(t)
	r, e := l.Install(context.Background(), rom.InstallRequest{Platform: rom.Platform{Dir: "psx", Extensions: []string{".cue", ".bin"}}, File: rom.File{Name: "Game.zip", URL: s.URL, Size: int64(len(a))}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(r.Path, "Game.cue"))
	if e != nil {
		t.Fatal(e)
	}
	ref := strings.Split(string(b), "\"")[1]
	if _, e = os.Stat(filepath.Join(r.Path, ref)); e != nil {
		t.Fatalf("installed index references absent piece %q: %v", ref, e)
	}
}

func TestVerifierIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name, body, md5 string
		size            int64
		ok              bool
	}{{"size", "abc", "", 4, false}, {"checksum", "abc", "bad", 3, false}, {"plain", "abc", "", 3, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := serve(t, servedBody([]byte(tc.body)))
			l, root := newLibrary(t)
			r, e := l.Install(context.Background(), rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc", URL: s.URL, Size: tc.size, MD5: tc.md5}}, nil)
			if (e == nil) != tc.ok {
				t.Fatalf("result %+v error %v", r, e)
			}
			if !tc.ok {
				if _, e := os.Stat(filepath.Join(root, "snes", "Game.sfc")); !os.IsNotExist(e) {
					t.Fatal("rejected file installed")
				}
				if _, e := os.Stat(filepath.Join(root, "snes", "Game.sfc.part.meta")); !os.IsNotExist(e) {
					t.Fatal("metadata retained")
				}
			}
		})
	}
}

func TestVerifierMalformedRange(t *testing.T) {
	l, root := newLibrary(t)
	dir := filepath.Join(root, "snes")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "Game.sfc.part"), []byte("abc"), 0o600)
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 3-1/6")
			w.WriteHeader(206)
			_, _ = w.Write([]byte("BAD"))
			return
		}
		_, _ = w.Write([]byte("abcdef"))
	})
	r, e := l.Install(context.Background(), rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc", URL: s.URL, Size: 6}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(r.Path)
	if string(b) != "abcdef" {
		t.Fatalf("malformed Content-Range accepted: %q", b)
	}
}

func TestVerifierMissingPiece(t *testing.T) {
	a := zipArchive(t, []zipEntry{{name: "Game.cue", body: "FILE \"gone.bin\" BINARY\n"}})
	s := serve(t, servedBody(a))
	l, root := newLibrary(t)
	_, e := l.Install(context.Background(), rom.InstallRequest{Platform: rom.Platform{Dir: "psx", Extensions: []string{".cue", ".bin"}}, File: rom.File{Name: "Game.zip", URL: s.URL}}, nil)
	if e == nil {
		t.Fatal("missing piece installed")
	}
	_ = errors.Is
	files := []string{}
	_ = filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) > 0 {
		t.Fatalf("failure leaked %v", files)
	}
}
