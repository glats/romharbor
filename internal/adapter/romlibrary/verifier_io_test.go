package romlibrary

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestVerifierProgress(t *testing.T) {
	s := serve(t, servedBody([]byte("abc")))
	l, _ := newLibrary(t)
	var done, total int64
	_, e := l.Install(context.Background(), rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc", URL: s.URL}}, func(d, n int64) { done, total = d, n })
	if e != nil || done != 3 || total != 3 {
		t.Fatalf("%d/%d %v", done, total, e)
	}
}

func TestVerifierInstallRollback(t *testing.T) {
	dir := t.TempDir()
	src, tmp, target := filepath.Join(dir, "part"), filepath.Join(dir, "tmp"), filepath.Join(dir, "target")
	_ = os.WriteFile(src, []byte("original"), 0o600)
	_ = os.Mkdir(target, 0o755)
	_ = os.WriteFile(filepath.Join(target, "child"), []byte("existing"), 0o600)
	if e := install(src, tmp, target, dir); e == nil {
		t.Fatal("expected rename failure")
	}
	got, e := os.ReadFile(src)
	if e != nil || string(got) != "original" {
		t.Fatalf("source lost %q %v", got, e)
	}
	got, e = os.ReadFile(filepath.Join(target, "child"))
	if e != nil || string(got) != "existing" {
		t.Fatal("target changed")
	}
	if _, e = os.Stat(tmp); !os.IsNotExist(e) {
		t.Fatal("staging leak")
	}
}
