package rom

import (
	"errors"
	"strings"
	"testing"
)

func TestSplitFileName(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		base    string
		ext     string
		wantErr bool
	}{
		{name: "keeps source extension", file: "Chrono Trigger (USA).sfc", base: "Chrono Trigger (USA)", ext: ".sfc"},
		{name: "strips directories and traversal", file: "../../etc/passwd", base: "passwd"},
		{name: "archive extension is part of the name", file: "Bundle.zip", base: "Bundle", ext: ".zip"},
		{name: "control characters removed and spacing collapsed", file: "  Spaced\t\tName\x00 .gba ", base: "Spaced Name", ext: ".gba"},
		{name: "base name capped at 120 bytes", file: strings.Repeat("a", 200) + ".nes", base: strings.Repeat("a", 120), ext: ".nes"},
		{name: "bare dot-dot rejected", file: "..", wantErr: true},
		{name: "empty name rejected", file: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, ext, err := SplitFileName(tt.file)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("SplitFileName(%q) error = nil, want error", tt.file)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitFileName(%q) error = %v", tt.file, err)
			}
			if base != tt.base || ext != tt.ext {
				t.Errorf("SplitFileName(%q) = (%q, %q), want (%q, %q)", tt.file, base, ext, tt.base, tt.ext)
			}
		})
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"No-Intro - Super Mario Bros. (USA).zip", "Super Mario Bros. (USA)"},
		{"007 - Everything or Nothing (Japan).zip", "007 - Everything or Nothing (Japan)"},
		{"Sonic The Hedgehog (USA, Europe).7z", "Sonic The Hedgehog (USA, Europe)"},
	}
	for _, tt := range cases {
		if got := CleanTitle(tt.name); got != tt.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPlatformAccepts(t *testing.T) {
	platform, ok := PlatformBySlug("snes")
	if !ok {
		t.Fatal("snes platform missing")
	}
	if !platform.Accepts("Super Mario World (USA).SFC") {
		t.Error("uppercase extension must be accepted")
	}
	if platform.Accepts("game.zip") {
		t.Error("zip is not a ROM")
	}
}

func TestGenesisInstallsIntoMegadriveFolder(t *testing.T) {
	genesis, ok := PlatformBySlug("genesis")
	if !ok {
		t.Fatal("genesis platform missing")
	}
	if got := genesis.LibraryFolder(); got != "megadrive" {
		t.Errorf("LibraryFolder() = %q, want megadrive", got)
	}
	snes, _ := PlatformBySlug("snes")
	if got := snes.LibraryFolder(); got != "snes" {
		t.Errorf("snes LibraryFolder() = %q, want snes", got)
	}
}

func TestPickROMPrefersArchiveRoot(t *testing.T) {
	names := []string{"disc/alt.sfc", "Game.sfc"}
	if i, ok := PickROM(names, []string{".sfc"}); !ok || i != 1 {
		t.Errorf("PickROM() = (%d, %v), want root file at index 1", i, ok)
	}
	if _, ok := PickROM([]string{"notes.txt"}, []string{".sfc"}); ok {
		t.Error("PickROM() found a ROM in an archive without one")
	}
}

func TestPlatformDestinationIsItsLibraryFolderOrAnError(t *testing.T) {
	genesis, _ := PlatformBySlug("genesis")
	if got, err := genesis.Destination(); err != nil || got != "megadrive" {
		t.Errorf("Destination() = (%q, %v), want megadrive", got, err)
	}
	if _, err := (Platform{}).Destination(); !errors.Is(err, ErrNoLibraryFolder) {
		t.Errorf("Destination() of a platform without a folder error = %v, want ErrNoLibraryFolder", err)
	}
}

func TestDiscOfReadsTheDiscTagOfAMultiDiscGame(t *testing.T) {
	cases := []struct {
		name  string
		title string
		group string
		disc  int
		ok    bool
	}{
		{"first disc", "Driver 2 (USA) (Disc 1)", "Driver 2 (USA)", 1, true},
		{"second disc with a revision", "Driver 2 (USA) (Disc 2) (Rev 1)", "Driver 2 (USA) (Rev 1)", 2, true},
		{"disk spelling", "Final Fantasy VII (Europe) (Disk 3)", "Final Fantasy VII (Europe)", 3, true},
		{"cd spelling", "Resident Evil 2 (USA) (CD 2)", "Resident Evil 2 (USA)", 2, true},
		{"disc of total", "Xenogears (USA) (Disc 1 of 2)", "Xenogears (USA)", 1, true},
		{"disc tag first among others", "Game (Disc 2) (En,Fr)", "Game (En,Fr)", 2, true},
		{"single disc game", "Gran Turismo (USA)", "", 0, false},
		{"demo is not a disc", "Driver 2 (USA) (Demo)", "", 0, false},
		{"discworld is not a disc tag", "Discworld (Europe)", "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			group, disc, ok := DiscOf(tc.title)
			if ok != tc.ok || group != tc.group || disc != tc.disc {
				t.Errorf("DiscOf(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.title, group, disc, ok, tc.group, tc.disc, tc.ok)
			}
		})
	}
}
