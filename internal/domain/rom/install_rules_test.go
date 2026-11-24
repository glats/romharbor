package rom

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanInstallAppliesTheExtensionPolicy(t *testing.T) {
	snes := Platform{Slug: "snes", Extensions: []string{".sfc", ".smc"}}
	arcade := Platform{Slug: "arcade", Extensions: []string{".zip", ".7z"}}
	tests := []struct {
		name     string
		platform Platform
		file     File
		want     InstallPlan
		wantErr  string
	}{
		{
			name:     "a plain ROM keeps its extension",
			platform: snes,
			file:     File{Name: "Chrono Trigger (USA).sfc", Size: 16},
			want:     InstallPlan{Folder: "snes", Base: "Chrono Trigger (USA)", Ext: ".sfc", Size: 16},
		},
		{
			name:     "the case of the extension is kept",
			platform: snes,
			file:     File{Name: "Game.SFC"},
			want:     InstallPlan{Folder: "snes", Base: "Game", Ext: ".SFC"},
		},
		{
			name:     "an archive the platform does not accept is opened",
			platform: snes,
			file:     File{Name: "Bundle.zip", Archive: true},
			want:     InstallPlan{Folder: "snes", Base: "Bundle", Ext: ".zip", Archive: true},
		},
		{
			name:     "an archive the platform lists is the ROM itself",
			platform: arcade,
			file:     File{Name: "pacman.zip", Archive: true},
			want:     InstallPlan{Folder: "arcade", Base: "pacman", Ext: ".zip"},
		},
		{
			name:     "a file without an extension is refused",
			platform: snes,
			file:     File{Name: "Game"},
			wantErr:  "has no extension to install under",
		},
		{
			name:     "an extension the platform does not accept is refused",
			platform: snes,
			file:     File{Name: "Game.md"},
			wantErr:  "is not a ROM for the snes library",
		},
		{
			name:     "a file that must be opened but is not an archive is refused",
			platform: snes,
			file:     File{Name: "Game.sfc", Archive: true},
			wantErr:  "unsupported archive format for Game.sfc",
		},
		{
			name:     "a folder that leaves the library is refused",
			platform: Platform{Slug: "snes", Dir: "../escape", Extensions: []string{".sfc"}},
			file:     File{Name: "Game.sfc"},
			wantErr:  "is not a folder of the library",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.platform.PlanInstall(tt.file)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("PlanInstall() error = nil, want one containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlanInstall() error = %v, want one containing %q", err, tt.wantErr)
				}
				if !errors.Is(err, ErrNotRetryable) {
					t.Errorf("PlanInstall() error = %v, want a failure a retry cannot fix", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("PlanInstall() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("PlanInstall() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSkipInstallFollowsTheDomainPolicy(t *testing.T) {
	plan := InstallPlan{Folder: "snes", Base: "Game", Ext: ".sfc", Size: 16}
	tests := []struct {
		name     string
		plan     InstallPlan
		existing ExistingFile
		want     bool
	}{
		{
			name:     "the library holds the same file",
			plan:     plan,
			existing: ExistingFile{Exists: true, Regular: true, Size: 16},
			want:     true,
		},
		{
			name:     "the library holds a file of another size",
			plan:     plan,
			existing: ExistingFile{Exists: true, Regular: true, Size: 32},
		},
		{
			name: "the library holds nothing",
			plan: plan,
		},
		{
			name:     "the library holds a folder",
			plan:     plan,
			existing: ExistingFile{Exists: true, Size: 16},
		},
		{
			name:     "the published size is unknown",
			plan:     InstallPlan{Folder: "snes", Base: "Game", Ext: ".sfc"},
			existing: ExistingFile{Exists: true, Regular: true},
		},
		{
			name:     "an archive is downloaded again",
			plan:     InstallPlan{Folder: "snes", Base: "Bundle", Ext: ".zip", Archive: true, Size: 16},
			existing: ExistingFile{Exists: true, Regular: true, Size: 16},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SkipInstall(tt.plan, tt.existing); got != tt.want {
				t.Errorf("SkipInstall() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSkipSetFollowsTheDomainPolicy(t *testing.T) {
	if !SkipSet(ExistingFile{Exists: true, Regular: true}) {
		t.Error("SkipSet() = false, want the set present")
	}
	if SkipSet(ExistingFile{Exists: true}) {
		t.Error("SkipSet() = true, want a folder without an index to be no set")
	}
	if SkipSet(ExistingFile{}) {
		t.Error("SkipSet() = true, want an empty library to hold no set")
	}
}

func TestLibraryPathKeepsTheTargetInsideTheLibrary(t *testing.T) {
	tests := []struct {
		name    string
		folder  string
		file    string
		want    string
		wantErr bool
	}{
		{name: "a file of a platform folder", folder: "snes", file: "Game.sfc", want: "snes/Game.sfc"},
		{name: "a folder of a set", folder: "psx", file: "Game", want: "psx/Game"},
		{name: "the library root is not a folder", folder: ".", file: "Game.sfc", wantErr: true},
		{name: "a parent folder is not a folder", folder: "..", file: "Game.sfc", wantErr: true},
		{name: "a nested path is not a folder", folder: "a/b", file: "Game.sfc", wantErr: true},
		{name: "an absolute path is not a folder", folder: "/etc", file: "Game.sfc", wantErr: true},
		{name: "a name with a separator is refused", folder: "snes", file: "a/Game.sfc", wantErr: true},
		{name: "a name that is a parent reference is refused", folder: "snes", file: "..", wantErr: true},
		{name: "an empty name is refused", folder: "snes", file: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LibraryPath(tt.folder, tt.file)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LibraryPath(%q, %q) error = nil, want the path refused", tt.folder, tt.file)
				}
				if !errors.Is(err, ErrOutsideLibrary) {
					t.Errorf("LibraryPath(%q, %q) error = %v, want ErrOutsideLibrary", tt.folder, tt.file, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LibraryPath(%q, %q) error = %v", tt.folder, tt.file, err)
			}
			if got != tt.want {
				t.Errorf("LibraryPath(%q, %q) = %q, want %q", tt.folder, tt.file, got, tt.want)
			}
		})
	}
}

func TestAcceptsExtensionComparesTheExtensionExactly(t *testing.T) {
	if !AcceptsExtension([]string{".sfc", ".smc"}, ".SFC") {
		t.Error("AcceptsExtension() = false, want the case of the extension ignored")
	}
	if AcceptsExtension([]string{".gba"}, ".g") {
		t.Error("AcceptsExtension() = true, want a partial extension refused")
	}
	if AcceptsExtension(nil, "") {
		t.Error("AcceptsExtension() = true, want an empty extension refused")
	}
}

func TestPlatformDestinationRefusesAFolderOutsideTheLibrary(t *testing.T) {
	for _, dir := range []string{".", "..", "../escape", "a/b", "/etc"} {
		platform := Platform{Slug: "snes", Dir: dir}
		got, err := platform.Destination()
		if err == nil {
			t.Errorf("Destination() = %q, want the folder %q refused", got, dir)
			continue
		}
		if !errors.Is(err, ErrOutsideLibrary) {
			t.Errorf("Destination() error = %v, want ErrOutsideLibrary", err)
		}
	}
}
