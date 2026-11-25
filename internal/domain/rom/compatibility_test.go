package rom

import (
	"reflect"
	"testing"
)

func platformOf(t *testing.T, slug string) Platform {
	t.Helper()
	platform, ok := PlatformBySlug(slug)
	if !ok {
		t.Fatalf("platform %s missing", slug)
	}
	return platform
}

func gameWithFiles(title string, names ...string) Game {
	game := Game{Title: title}
	for _, name := range names {
		game.Files = append(game.Files, File{Name: name})
	}
	return game
}

func TestKnulliCompatibilityReadsTheNoIntroAndRedumpTags(t *testing.T) {
	n64 := platformOf(t, "n64")
	snes := platformOf(t, "snes")
	psx := platformOf(t, "psx")

	tests := []struct {
		name     string
		game     Game
		platform Platform
		want     Compatibility
	}{
		{
			name:     "a spanish translation of a heavy n64 title does not run",
			game:     Game{Title: "Banjo-Kazooie (E) (M3) [T+Spa1.1_PacoChan]"},
			platform: n64,
			want:     Compatibility{Compatible: false, Labels: []string{LabelTranslation, LabelPAL, LabelHeavy}},
		},
		{
			name:     "a spanish translation of goldeneye does not run",
			game:     Game{Title: "GoldenEye 007 (E) [T+Spa2.0_Sogun & IlDucci]"},
			platform: n64,
			want:     Compatibility{Compatible: false, Labels: []string{LabelTranslation, LabelPAL, LabelHeavy}},
		},
		{
			name:     "a verified pal heavy title runs",
			game:     Game{Title: "Banjo-Tooie (E) (M4) [!]"},
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelGood, LabelPAL, LabelHeavy}},
		},
		{
			name:     "conker is heavy because the default n64 core crashes on it",
			game:     Game{Title: "Conker's Bad Fur Day (E) [!]"},
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelGood, LabelPAL, LabelHeavy}},
		},
		{
			name:     "a united states release runs without a label",
			game:     Game{Title: "Killer Instinct Gold (USA)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a beta does not run",
			game:     Game{Title: "Crash Bandicoot (Unknown) (Beta) (1996-02-29)"},
			platform: psx,
			want:     Compatibility{Compatible: false, Labels: []string{LabelBeta}},
		},
		{
			name:     "a beta disc does not run",
			game:     Game{Title: "Final Fantasy IX (USA) (Disc 1) (Beta) (2000-09-06)"},
			platform: psx,
			want:     Compatibility{Compatible: false, Labels: []string{LabelBeta}},
		},
		{
			name:     "a dated revision runs",
			game:     Game{Title: "Metal Gear Solid (USA) (Disc 1) (v1.1)"},
			platform: psx,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a united states n64 title runs",
			game:     Game{Title: "Super Mario 64 (USA)"},
			platform: n64,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a japanese revision runs",
			game:     Game{Title: "Super Mario 64 (Japan) (Rev A)"},
			platform: n64,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a hack does not run",
			game:     Game{Title: "Game [h1C]"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelHack}},
		},
		{
			name:     "a bad dump does not run",
			game:     Game{Title: "Game [b1]"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelBadDump}},
		},
		{
			name:     "a prototype does not run",
			game:     Game{Title: "Game (Proto)"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelPrototype}},
		},
		{
			name:     "a spelled out prototype does not run",
			game:     Game{Title: "Game (Prototype)"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelPrototype}},
		},
		{
			name:     "a demo does not run",
			game:     Game{Title: "Game (Demo)"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelDemo}},
		},
		{
			name:     "a sample does not run",
			game:     Game{Title: "Game (Sample)"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelSample}},
		},
		{
			name:     "an unlicensed release does not run",
			game:     Game{Title: "Game (Unl)"},
			platform: snes,
			want:     Compatibility{Compatible: false, Labels: []string{LabelUnlicensed}},
		},
		{
			name:     "a european release runs and is labeled pal",
			game:     Game{Title: "Game (Europe)"},
			platform: snes,
			want:     Compatibility{Compatible: true, Labels: []string{LabelPAL}},
		},
		{
			name:     "a european release with the short region runs and is labeled pal",
			game:     Game{Title: "Game (E)"},
			platform: snes,
			want:     Compatibility{Compatible: true, Labels: []string{LabelPAL}},
		},
		{
			name:     "an australian release runs and is labeled pal",
			game:     Game{Title: "Game (Australia)"},
			platform: snes,
			want:     Compatibility{Compatible: true, Labels: []string{LabelPAL}},
		},
		{
			name:     "an australian release with the short region runs and is labeled pal",
			game:     Game{Title: "Game (A)"},
			platform: snes,
			want:     Compatibility{Compatible: true, Labels: []string{LabelPAL}},
		},
		{
			name:     "a japanese release runs without a label",
			game:     Game{Title: "Game (Japan)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a japanese release with the short region runs without a label",
			game:     Game{Title: "Game (J)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a united states release with the short region runs without a label",
			game:     Game{Title: "Game (U)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a world release runs without a label",
			game:     Game{Title: "Game (World)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "an unknown tag never makes a game incompatible",
			game:     Game{Title: "Game (Rev B) (v1.2) (Disc 2) [a]"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a title that still carries a file name runs",
			game:     Game{Title: "No-Intro - Game (USA).zip"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KnulliCompatibility(tt.game, tt.platform)
			if got.Compatible != tt.want.Compatible {
				t.Errorf("KnulliCompatibility(%q).Compatible = %t, want %t", tt.game.Title, got.Compatible, tt.want.Compatible)
			}
			if !reflect.DeepEqual(got.Labels, tt.want.Labels) {
				t.Errorf("KnulliCompatibility(%q).Labels = %v, want %v", tt.game.Title, got.Labels, tt.want.Labels)
			}
		})
	}
}

func TestKnulliCompatibilityIgnoresTheCaseOfTheTitle(t *testing.T) {
	snes := platformOf(t, "snes")
	lower := KnulliCompatibility(Game{Title: "game (beta) [h1c] (europe)"}, snes)
	upper := KnulliCompatibility(Game{Title: "GAME (BETA) [H1C] (EUROPE)"}, snes)

	if !reflect.DeepEqual(lower, upper) {
		t.Errorf("KnulliCompatibility is case dependent: %+v and %+v", lower, upper)
	}
	if lower.Compatible {
		t.Error("a beta with a hack must not be compatible")
	}
	want := []string{LabelHack, LabelBeta, LabelPAL}
	if !reflect.DeepEqual(lower.Labels, want) {
		t.Errorf("labels = %v, want %v", lower.Labels, want)
	}
}

func TestKnulliCompatibilityReadsTheTagsOfTheFileNames(t *testing.T) {
	snes := platformOf(t, "snes")
	n64 := platformOf(t, "n64")

	got := KnulliCompatibility(gameWithFiles("Game", "Game (Europe).zip"), snes)
	if !got.Compatible || !reflect.DeepEqual(got.Labels, []string{LabelPAL}) {
		t.Errorf("a pal file name = %+v, want a compatible pal", got)
	}

	got = KnulliCompatibility(gameWithFiles("Game", "Game (Beta).zip"), snes)
	if got.Compatible || !reflect.DeepEqual(got.Labels, []string{LabelBeta}) {
		t.Errorf("a beta file name = %+v, want an incompatible beta", got)
	}

	got = KnulliCompatibility(gameWithFiles("Game", "Banjo-Kazooie (E) (M3) [T+Spa1.1_PacoChan].zip"), n64)
	if got.Compatible || !reflect.DeepEqual(got.Labels, []string{LabelTranslation, LabelPAL, LabelHeavy}) {
		t.Errorf("a translated file name = %+v, want an incompatible translation", got)
	}
}

func TestKnulliCompatibilityAppliesThePlatformRules(t *testing.T) {
	n64 := platformOf(t, "n64")
	snes := platformOf(t, "snes")

	tests := []struct {
		name     string
		game     Game
		platform Platform
		want     Compatibility
	}{
		{
			name:     "a zipped n64 rom is the preferred format",
			game:     gameWithFiles("Super Mario 64 (USA)", "Super Mario 64 (USA).z64"),
			platform: n64,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a big endian n64 rom is labeled",
			game:     gameWithFiles("Super Mario 64 (USA)", "Super Mario 64 (USA).n64"),
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelZ64}},
		},
		{
			name:     "a little endian n64 rom is labeled",
			game:     gameWithFiles("Super Mario 64 (USA)", "Super Mario 64 (USA).v64"),
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelZ64}},
		},
		{
			name:     "a format the platform does not accept does not run",
			game:     gameWithFiles("Super Mario 64 (USA)", "Super Mario 64 (USA).sfc"),
			platform: n64,
			want:     Compatibility{Compatible: false, Labels: []string{LabelFormat}},
		},
		{
			name:     "an archive holds the rom and is not a format",
			game:     gameWithFiles("Super Mario 64 (USA)", "Super Mario 64 (USA).zip"),
			platform: n64,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "a game without files has no format",
			game:     Game{Title: "Super Mario 64 (USA)"},
			platform: n64,
			want:     Compatibility{Compatible: true},
		},
		{
			name:     "donkey kong 64 is heavy",
			game:     Game{Title: "Donkey Kong 64 (USA)"},
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelHeavy}},
		},
		{
			name:     "majoras mask is heavy",
			game:     Game{Title: "Majoras Mask (USA)"},
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelHeavy}},
		},
		{
			name:     "majora's mask is heavy",
			game:     Game{Title: "Majora's Mask (USA)"},
			platform: n64,
			want:     Compatibility{Compatible: true, Labels: []string{LabelHeavy}},
		},
		{
			name:     "a heavy title is heavy only on n64",
			game:     Game{Title: "Donkey Kong 64 (USA)"},
			platform: snes,
			want:     Compatibility{Compatible: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KnulliCompatibility(tt.game, tt.platform)
			if got.Compatible != tt.want.Compatible {
				t.Errorf("Compatible = %t, want %t", got.Compatible, tt.want.Compatible)
			}
			if !reflect.DeepEqual(got.Labels, tt.want.Labels) {
				t.Errorf("Labels = %v, want %v", got.Labels, tt.want.Labels)
			}
		})
	}
}

func TestKnulliCompatibilityIsDeterministic(t *testing.T) {
	n64 := platformOf(t, "n64")
	game := Game{Title: "Banjo-Kazooie (E) (M3) [T+Spa1.1_PacoChan]"}

	first := KnulliCompatibility(game, n64)
	for range 5 {
		if got := KnulliCompatibility(game, n64); !reflect.DeepEqual(got, first) {
			t.Fatalf("KnulliCompatibility is not deterministic: %+v and %+v", first, got)
		}
	}
}
