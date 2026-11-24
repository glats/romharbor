package romlibrary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierSetInputs(t *testing.T) {
	for _, ref := range []string{"../one.bin", `..\one.bin`, "/one.bin", `C:\one.bin`} {
		t.Run(ref, func(t *testing.T) {
			a := zipArchive(t, []zipEntry{{name: "Game.cue", body: "FILE \"" + ref + "\" BINARY\n"}, {name: "one.bin", body: "track"}})
			s := serve(t, servedBody(a))
			l, _ := newLibrary(t)
			_, e := l.Install(context.Background(), rom.InstallRequest{Platform: rom.Platform{Dir: "psx", Extensions: []string{".cue", ".bin"}}, File: rom.File{Name: "Game.zip", URL: s.URL}}, nil)
			if e == nil {
				t.Fatal("unsafe reference accepted")
			}
		})
	}
	for _, format := range []string{"cue", "gdi", "m3u"} {
		t.Run(format, func(t *testing.T) {
			index := "FILE \"one.bin\" BINARY\n"
			entries := []zipEntry{{name: "one.bin", body: "track"}}
			if format == "gdi" {
				index = "1\n1 0 4 2352 one.bin 0\n"
			}
			if format == "m3u" {
				index = "Disc.cue\n"
				entries = append(entries, zipEntry{name: "Disc.cue", body: "FILE \"one.bin\" BINARY\n"})
			}
			entries = append(entries, zipEntry{name: "Game." + format, body: index})
			a := zipArchive(t, entries)
			ext := "zip"
			if format == "gdi" {
				ext = "7z"
				a = sevenZipArchive(t, entries)
			}
			s := serve(t, servedBody(a))
			l, _ := newLibrary(t)
			req := rom.InstallRequest{Platform: rom.Platform{Dir: "psx", Extensions: []string{".cue", ".gdi", ".m3u", ".bin"}}, File: rom.File{Name: "Game." + ext, URL: s.URL}}
			r, e := l.Install(context.Background(), req, nil)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				got, e := os.ReadFile(filepath.Join(l.abs(r.Path), entry.name))
				if e != nil || string(got) != entry.body {
					t.Fatalf("%s %q %v", entry.name, got, e)
				}
			}
			r, e = l.Install(context.Background(), req, nil)
			if e != nil || !r.Skipped {
				t.Fatalf("skip %+v %v", r, e)
			}
		})
	}
}
