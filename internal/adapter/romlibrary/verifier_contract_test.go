package romlibrary

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierArchiveContracts(t *testing.T) {
	for _, tc := range []struct {
		name, ext string
		platform  rom.Platform
		want      string
	}{{"single", "zip", snesPlatform, "rom"}, {"arcade", "zip", rom.Platform{Dir: "arcade", Extensions: []string{".zip"}}, "archive"}} {
		t.Run(tc.name, func(t *testing.T) {
			a := zipArchive(t, []zipEntry{{name: "Game.sfc", body: "rom"}})
			s := serve(t, servedBody(a))
			l, _ := newLibrary(t)
			r, e := l.Install(context.Background(), rom.InstallRequest{Platform: tc.platform, File: rom.File{Name: "Game." + tc.ext, URL: s.URL}}, nil)
			if e != nil {
				t.Fatal(e)
			}
			got, _ := os.ReadFile(r.Path)
			if tc.want == "rom" && string(got) != "rom" || tc.want == "archive" && string(got) != string(a) {
				t.Fatal("wrong bytes")
			}
		})
	}
}

func TestVerifierChecksumCancel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "part")
	_ = os.WriteFile(p, []byte("abc"), 0o600)
	ctx, c := context.WithCancel(context.Background())
	c()
	if e := newChecksums(rom.File{MD5: "bad"}).hashFile(ctx, p); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
