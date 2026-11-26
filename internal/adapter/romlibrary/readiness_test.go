package romlibrary

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestInspectGameIndexEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, entry, title, body, status string
		discs                            []int
	}{
		{"partial cue", "game.cue", "", "FILE \"one.bin\" BINARY\nFILE\n", rom.ReadinessUnknown, nil},
		{"playlist cycle", "game.m3u", "", "game.m3u\n", rom.ReadinessUnknown, nil},
		{"unknown count", "game.m3u", "Game", "Game (Disc 1).cue\nGame (Disc 3).cue\n", rom.ReadinessUnknown, nil},
		{"known count", "game.m3u", "Game (Disc 1 of 2)", "Game (Disc 1).cue\n", rom.ReadinessIncomplete, []int{2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			library := Library{Root: t.TempDir()}
			dir := filepath.Join(library.Root, "psx")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{
				tc.entry: tc.body, "one.bin": "data",
				"Game (Disc 1).cue": "FILE one.bin BINARY\n",
				"Game (Disc 3).cue": "FILE one.bin BINARY\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := library.InspectGame(t.Context(), rom.Job{Path: "psx/" + tc.entry, Title: tc.title, Status: rom.JobDone})
			if got.Status != tc.status || !reflect.DeepEqual(got.MissingDiscs, tc.discs) || len(got.MissingFiles) != 0 {
				t.Fatalf("inspect = %+v, want %s with missing discs %v and no missing files", got, tc.status, tc.discs)
			}
		})
	}
}

func TestInspectInstalledGameDoesNotFollowEscapingReferences(t *testing.T) {
	library := Library{Root: t.TempDir()}
	psx := filepath.Join(library.Root, "psx")
	if err := os.Mkdir(psx, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(psx, "escape.bin")); err != nil {
		t.Fatal(err)
	}
	body := "FILE \"escape.bin\" BINARY\nFILE \"../../private.bin\" BINARY\n"
	if err := os.WriteFile(filepath.Join(psx, "game.cue"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := library.InspectGame(t.Context(), rom.Job{Path: "psx/game.cue", Status: rom.JobDone})
	if got.Status != rom.ReadinessUnknown || len(got.Files) != 1 || len(got.MissingFiles) != 0 || !strings.Contains(got.Reason, "unsafe") {
		t.Fatalf("inspect = %+v, want an unknown result without escaped paths", got)
	}
}

func TestInspectInstalledGame(t *testing.T) {
	const (
		d1 = "psx/Driver 2 (USA) (Disc 1 of 2)"
		d2 = "psx/Driver 2 (USA) (Disc 2 of 2)"
		pl = "psx/Driver 2 (USA).m3u"
	)
	for _, tc := range []struct {
		name, title, playlist, status string
		track                         bool
		missing                       []string
		discs                         []int
	}{
		{"complete", filepath.Base(d1), filepath.Base(d1) + "/game.cue\n" + filepath.Base(d2) + "/game.cue\n", "complete", true, nil, nil},
		{"missing track", filepath.Base(d1), filepath.Base(d1) + "/game.cue\n" + filepath.Base(d2) + "/game.cue\n", "incomplete", false, []string{d2 + "/game.bin"}, nil},
		{"missing disc", filepath.Base(d1), "", "incomplete", false, nil, []int{2}},
		{"unknown count", "Driver 2 (USA) (Disc 1)", "", "unknown", false, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			library := Library{Root: t.TempDir()}
			put := func(name, body string) {
				t.Helper()
				target := filepath.Join(library.Root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			put(d1+"/game.cue", "FILE \"game.bin\" BINARY\n")
			put(d1+"/game.bin", "data")
			if tc.playlist != "" {
				put(pl, tc.playlist)
				put(d2+"/game.cue", "FILE \"game.bin\" BINARY\n")
			}
			if tc.track {
				put(d2+"/game.bin", "data")
			}
			got := library.InspectGame(t.Context(), rom.Job{Title: tc.title, Path: d1, Status: rom.JobDone})
			if got.Status != tc.status || !reflect.DeepEqual(got.MissingFiles, tc.missing) || !reflect.DeepEqual(got.MissingDiscs, tc.discs) {
				t.Fatalf("inspect = %+v, want %s with files %v and discs %v", got, tc.status, tc.missing, tc.discs)
			}
			if tc.status == "complete" && (got.EntryPoint != pl || len(got.Files) != 5) {
				t.Fatalf("complete set = %+v", got)
			}
		})
	}
}
