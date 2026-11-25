package rom

import "testing"

func TestLibraryFoldersAreTheSystemNamesKnulliScans(t *testing.T) {
	want := map[string]string{
		"nes": "nes", "snes": "snes", "gb": "gb", "gbc": "gbc", "gba": "gba",
		"genesis": "megadrive", "n64": "n64", "nds": "nds",
		"psx": "psx", "ps2": "ps2", "psp": "psp",
		"ngc": "gamecube", "wii": "wii", "n3ds": "3ds",
		"saturn": "saturn", "dc": "dreamcast",
		"switch": "switch", "psvita": "psvita", "atari2600": "atari2600", "lynx": "lynx",
	}
	for slug, folder := range want {
		platform, ok := PlatformBySlug(slug)
		if !ok {
			t.Errorf("platform %q is missing", slug)
			continue
		}
		got, err := platform.Destination()
		if err != nil || got != folder {
			t.Errorf("%s installs into %q (%v), want %q", slug, got, err, folder)
		}
	}
}

func TestDiscPlatformsAcceptAPlaylistThatKnulliLists(t *testing.T) {
	for _, slug := range []string{"psx", "ps2", "saturn", "dc"} {
		platform, ok := PlatformBySlug(slug)
		if !ok {
			t.Fatalf("platform %q is missing", slug)
		}
		if !AcceptsExtension(platform.Extensions, ".m3u") {
			t.Errorf("%s does not accept .m3u", slug)
		}
	}
}
