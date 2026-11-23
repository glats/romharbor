package romlibrary

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

var psxPlatform = rom.Platform{
	Slug:       "psx",
	Dir:        "psx",
	Name:       "Sony PlayStation",
	Extensions: []string{".chd", ".cue", ".bin", ".pbp", ".img", ".m3u"},
}

var dreamcastPlatform = rom.Platform{
	Slug:       "dc",
	Dir:        "dc",
	Name:       "Sega Dreamcast",
	Extensions: []string{".chd", ".gdi", ".cue", ".iso"},
}

const cueIndex = "FILE \"Game.bin\" BINARY\n  TRACK 01 MODE1/2352\n    INDEX 01 00:00:00\n"

func TestInstallExtractsACueSetIntoOneFolder(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := filepath.Join(root, "psx", "Game")
	if res.Path != folder {
		t.Errorf("Path = %q, want the folder %q", res.Path, folder)
	}
	if !res.Extracted {
		t.Errorf("result = %+v, want the set marked as extracted", res)
	}
	if got := readFile(t, filepath.Join(folder, "Game.cue")); string(got) != cueIndex {
		t.Errorf("installed index = %q, want %q", got, cueIndex)
	}
	if got := readFile(t, filepath.Join(folder, "Game.bin")); string(got) != "disc-image" {
		t.Errorf("installed piece = %q, want %q", got, "disc-image")
	}
	files := installedFiles(t, root)
	if len(files) != 2 {
		t.Errorf("library files = %v, want the index and its piece", files)
	}
}

func TestInstallExtractsAGDISetIntoOneFolder(t *testing.T) {
	gdi := "2\n1 0 2352 0 1_track01.bin\n2 0 2352 0 2_track02.bin\n"
	archive := sevenZipArchive(t, []zipEntry{
		{name: "Game.gdi", body: gdi},
		{name: "1_track01.bin", body: "track-one"},
		{name: "2_track02.bin", body: "track-two"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: dreamcastPlatform,
		File:     rom.File{Name: "Game.7z", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := filepath.Join(root, "dc", "Game")
	if res.Path != folder {
		t.Errorf("Path = %q, want the folder %q", res.Path, folder)
	}
	if got := readFile(t, filepath.Join(folder, "Game.gdi")); string(got) != gdi {
		t.Errorf("installed index = %q, want %q", got, gdi)
	}
	if got := readFile(t, filepath.Join(folder, "1_track01.bin")); string(got) != "track-one" {
		t.Errorf("installed piece = %q, want %q", got, "track-one")
	}
	if got := readFile(t, filepath.Join(folder, "2_track02.bin")); string(got) != "track-two" {
		t.Errorf("installed piece = %q, want %q", got, "track-two")
	}
	files := installedFiles(t, root)
	if len(files) != 3 {
		t.Errorf("library files = %v, want the index and its two tracks", files)
	}
}

func TestInstallExtractsAnM3UPlaylistIntoOneFolder(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.m3u", body: "#EXTM3U\nDisc 1.cue\nDisc 2.cue\n"},
		{name: "Disc 1.cue", body: "FILE \"Disc 1.bin\" BINARY\n"},
		{name: "Disc 1.bin", body: "disc-one"},
		{name: "Disc 2.cue", body: "FILE \"Disc 2.bin\" BINARY\n"},
		{name: "Disc 2.bin", body: "disc-two"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := filepath.Join(root, "psx", "Game")
	if res.Path != folder {
		t.Errorf("Path = %q, want the folder %q", res.Path, folder)
	}
	for name, want := range map[string]string{
		"Game.m3u":   "#EXTM3U\nDisc 1.cue\nDisc 2.cue\n",
		"Disc 1.cue": "FILE \"Disc 1.bin\" BINARY\n",
		"Disc 1.bin": "disc-one",
		"Disc 2.cue": "FILE \"Disc 2.bin\" BINARY\n",
		"Disc 2.bin": "disc-two",
	} {
		if got := readFile(t, filepath.Join(folder, name)); string(got) != want {
			t.Errorf("installed %s = %q, want %q", name, got, want)
		}
	}
	files := installedFiles(t, root)
	if len(files) != 5 {
		t.Errorf("library files = %v, want the playlist and both discs", files)
	}
}

func TestInstallFailsWhenAPieceOfTheSetIsMissing(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)

	if !errors.Is(err, rom.ErrNotRetryable) {
		t.Fatalf("Install() error = %v, want a failure a retry cannot fix", err)
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
	assertNoExtractDir(t, root)
	assertAbsent(t, filepath.Join(root, "psx", "Game.zip.part"))
}

func TestInstallRejectsAPieceThatEscapesTheArchive(t *testing.T) {
	tests := []struct {
		name      string
		reference string
	}{
		{name: "a parent directory", reference: "../Game.bin"},
		{name: "an absolute path", reference: "/etc/passwd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := zipArchive(t, []zipEntry{
				{name: "Game.cue", body: "FILE \"" + tt.reference + "\" BINARY\n"},
			})
			srv := serve(t, servedBody(archive))
			l, root := newLibrary(t)

			_, err := l.Install(context.Background(), rom.InstallRequest{
				Platform: psxPlatform,
				File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
			}, nil)

			if !errors.Is(err, rom.ErrNotRetryable) {
				t.Fatalf("Install() error = %v, want a failure a retry cannot fix", err)
			}
			if files := installedFiles(t, root); len(files) != 0 {
				t.Errorf("library files = %v, want none", files)
			}
			assertNoExtractDir(t, root)
			assertAbsent(t, filepath.Join(root, "psx", "Game.zip.part"))
		})
	}
}

func TestInstallRejectsAnIndexThatIsTooLarge(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: "FILE \"Game.bin\" BINARY\n" + strings.Repeat("a", maxIndexBytes)},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)

	if !errors.Is(err, rom.ErrNotRetryable) {
		t.Fatalf("Install() error = %v, want a failure a retry cannot fix", err)
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
	assertNoExtractDir(t, root)
	assertAbsent(t, filepath.Join(root, "psx", "Game.zip.part"))
}

func TestInstallKeepsTwoPiecesThatShareAName(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: "FILE \"a/Game.bin\" BINARY\nFILE \"b/Game.bin\" BINARY\n"},
		{name: "a/Game.bin", body: "one"},
		{name: "b/Game.bin", body: "two"},
	})
	srv := serve(t, servedBody(archive))
	l, _ := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := res.Path
	for name, want := range map[string]string{
		filepath.Join("a", "Game.bin"): "one",
		filepath.Join("b", "Game.bin"): "two",
	} {
		if got := readFile(t, filepath.Join(folder, name)); string(got) != want {
			t.Errorf("installed %s = %q, want %q", name, got, want)
		}
	}
}

func TestInstallKeepsTheArchiveLayoutOfTheSet(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "disc/Game.cue", body: "FILE \"tracks/one.bin\" BINARY\n"},
		{name: "disc/tracks/one.bin", body: "track"},
	})
	srv := serve(t, servedBody(archive))
	l, _ := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := res.Path
	if got := readFile(t, filepath.Join(folder, "Game.cue")); string(got) != "FILE \"tracks/one.bin\" BINARY\n" {
		t.Errorf("installed index = %q, want the untouched index text", got)
	}
	if got := readFile(t, filepath.Join(folder, "tracks", "one.bin")); string(got) != "track" {
		t.Errorf("installed piece = %q, want %q", got, "track")
	}
}

func TestInstallResolvesNestedIndexesInTheirOwnFolder(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.m3u", body: "disc1/a.cue\n"},
		{name: "disc1/a.cue", body: "FILE \"a.bin\" BINARY\n"},
		{name: "disc1/a.bin", body: "disc"},
	})
	srv := serve(t, servedBody(archive))
	l, _ := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := res.Path
	for name, want := range map[string]string{
		"Game.m3u":                      "disc1/a.cue\n",
		filepath.Join("disc1", "a.cue"): "FILE \"a.bin\" BINARY\n",
		filepath.Join("disc1", "a.bin"): "disc",
	} {
		if got := readFile(t, filepath.Join(folder, name)); string(got) != want {
			t.Errorf("installed %s = %q, want %q", name, got, want)
		}
	}
}

func TestInstallWritesAPieceThatTwoIndexesReferenceOnce(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: "FILE \"Game.bin\" BINARY\nFILE \"Game.bin\" BINARY\n"},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	folder := res.Path
	if got := readFile(t, filepath.Join(folder, "Game.bin")); string(got) != "disc-image" {
		t.Errorf("installed piece = %q, want %q", got, "disc-image")
	}
	if files := installedFiles(t, root); len(files) != 2 {
		t.Errorf("library files = %v, want the index and the piece once", files)
	}
}

func TestInstallKeepsTheSingleROMBehaviorWhenThePlatformDoesNotAcceptTheIndex(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	genesis := rom.Platform{Slug: "genesis", Dir: "megadrive", Extensions: []string{".md", ".gen", ".bin", ".smd"}}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: genesis,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "megadrive", "Game.bin")
	if res.Path != want || !res.Extracted {
		t.Errorf("result = %+v, want the single ROM at %s", res, want)
	}
	if got := readFile(t, want); string(got) != "disc-image" {
		t.Errorf("installed bytes = %q, want %q", got, "disc-image")
	}
}

func TestInstallHoldsTheSetUnderTheExtractionCaps(t *testing.T) {
	tests := []struct {
		name    string
		limits  extractLimits
		wantErr error
	}{
		{
			name:    "the entry cap",
			limits:  extractLimits{entries: 1, bytes: defaultExtractLimits.bytes},
			wantErr: errTooManyEntries,
		},
		{
			name:    "the byte cap",
			limits:  extractLimits{entries: defaultExtractLimits.entries, bytes: 8},
			wantErr: errExtractTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := zipArchive(t, []zipEntry{
				{name: "Game.cue", body: cueIndex},
				{name: "Game.bin", body: strings.Repeat("a", 64)},
			})
			srv := serve(t, servedBody(archive))
			l, root := newLibrary(t)
			l.limits = tt.limits

			_, err := l.Install(context.Background(), rom.InstallRequest{
				Platform: psxPlatform,
				File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
			}, nil)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Install() error = %v, want %v", err, tt.wantErr)
			}
			if files := installedFiles(t, root); len(files) != 0 {
				t.Errorf("library files = %v, want none", files)
			}
			assertNoExtractDir(t, root)
		})
	}
}

func TestInstallStopsTheSetWhenTheContextIsCanceled(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := l.Install(ctx, rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, func(int64, int64) {
		cancel()
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Install() error = %v, want context.Canceled", err)
	}
	assertAbsent(t, filepath.Join(root, "psx", "Game"))
	assertNoExtractDir(t, root)
}

func TestInstallRefusesAFolderThatIsNotASet(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	folder := filepath.Join(root, "psx", "Game")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "other.txt"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)

	if !errors.Is(err, rom.ErrNotRetryable) {
		t.Fatalf("Install() error = %v, want a failure a retry cannot fix", err)
	}
	if got := readFile(t, filepath.Join(folder, "other.txt")); string(got) != "other" {
		t.Errorf("the folder changed to %q", got)
	}
	assertNoExtractDir(t, root)
	assertAbsent(t, filepath.Join(root, "psx", "Game.zip.part"))
}

func TestInstallSkipsASetWhoseFolderAlreadyHoldsTheIndex(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "Game.cue", body: cueIndex},
		{name: "Game.bin", body: "disc-image"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	folder := filepath.Join(root, "psx", "Game")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Game.cue"), []byte("old-index"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: psxPlatform,
		File:     rom.File{Name: "Game.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !res.Skipped || res.Path != folder {
		t.Errorf("result = %+v, want the set skipped at %s", res, folder)
	}
	if got := readFile(t, filepath.Join(folder, "Game.cue")); string(got) != "old-index" {
		t.Errorf("the installed index changed to %q", got)
	}
	assertAbsent(t, filepath.Join(folder, "Game.bin"))
}
