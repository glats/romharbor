package rom

import "testing"

func TestCoverURLPointsAtTheLibretroThumbnailsRepository(t *testing.T) {
	tests := []struct {
		name  string
		slug  string
		title string
		want  string
	}{
		{"spaces and parentheses", "n64", "Killer Instinct Gold (USA)", "https://raw.githubusercontent.com/libretro-thumbnails/Nintendo_-_Nintendo_64/master/Named_Boxarts/Killer%20Instinct%20Gold%20%28USA%29.png"},
		{"libretro replaces reserved file name characters", "snes", "Mario & Luigi: Part/1?", "https://raw.githubusercontent.com/libretro-thumbnails/Nintendo_-_Super_Nintendo_Entertainment_System/master/Named_Boxarts/Mario%20_%20Luigi_%20Part_1_.png"},
		{"url characters in the title are escaped", "snes", "Game #1 100%", "https://raw.githubusercontent.com/libretro-thumbnails/Nintendo_-_Super_Nintendo_Entertainment_System/master/Named_Boxarts/Game%20%231%20100%25.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, ok := PlatformBySlug(tt.slug)
			if !ok {
				t.Fatalf("unknown platform %q", tt.slug)
			}
			if got := platform.CoverURL(tt.title); got != tt.want {
				t.Fatalf("CoverURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCoverURLIsEmptyWithoutAThumbnailSetOrATitle(t *testing.T) {
	platform, _ := PlatformBySlug("n64")
	if got := platform.CoverURL(""); got != "" {
		t.Fatalf("empty title = %q", got)
	}
	if got := (Platform{}).CoverURL("Game"); got != "" {
		t.Fatalf("no thumbnail set = %q", got)
	}
}
