package romlibrary

import (
	"context"
	"testing"

	"app.ddd/internal/domain/rom"
)

// L-07: a file installs only under an extension the platform accepts.
func TestInstallRefusesAFileThePlatformDoesNotAccept(t *testing.T) {
	body := []byte("genesis-rom")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.md", Size: int64(len(body)), URL: srv.URL},
	}, nil)

	if err == nil {
		t.Fatal("Install() error = nil, want the file refused")
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
}

// L-18: the library root is not a folder of the library.
func TestInstallRefusesTheLibraryRootAsTheFolder(t *testing.T) {
	body := []byte("rom")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)
	inRoot := rom.Platform{Slug: "snes", Dir: ".", Extensions: []string{".sfc"}}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: inRoot,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)

	if err == nil {
		t.Fatal("Install() error = nil, want the library root refused as a folder")
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
}
