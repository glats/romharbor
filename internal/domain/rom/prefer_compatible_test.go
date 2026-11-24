package rom

import (
	"reflect"
	"testing"
)

func variantTitles(games []Game) []string {
	out := make([]string, 0, len(games))
	for _, g := range games {
		out = append(out, g.Title)
	}
	return out
}

func TestPreferCompatibleKeepsTheBestVariantOfEveryTitle(t *testing.T) {
	snes := platformOf(t, "snes")

	tests := []struct {
		name  string
		games []Game
		want  []string
	}{
		{
			name: "the united states release wins over the japanese and the european one",
			games: []Game{
				{Title: "Super Mario 64 (Japan) (Rev A)"},
				{Title: "Super Mario 64 (Europe)"},
				{Title: "Super Mario 64 (USA)"},
			},
			want: []string{"Super Mario 64 (USA)"},
		},
		{
			name: "the world release wins over the european one",
			games: []Game{
				{Title: "Game (Europe)"},
				{Title: "Game (World)"},
			},
			want: []string{"Game (World)"},
		},
		{
			name: "the clean release wins over the translation",
			games: []Game{
				{Title: "Game (USA) [T+Spa1.0_Trans]"},
				{Title: "Game (USA)"},
			},
			want: []string{"Game (USA)"},
		},
		{
			name: "a pal release wins over a translation of the united states release",
			games: []Game{
				{Title: "Game (USA) [T+Spa1.0_Trans]"},
				{Title: "Game (Europe)"},
			},
			want: []string{"Game (Europe)"},
		},
		{
			name: "the verified dump wins over the plain one",
			games: []Game{
				{Title: "Game (USA)"},
				{Title: "Game (USA) [!]"},
			},
			want: []string{"Game (USA) [!]"},
		},
		{
			name: "a different title is kept apart",
			games: []Game{
				{Title: "Game (USA)"},
				{Title: "Other Game (USA)"},
			},
			want: []string{"Game (USA)", "Other Game (USA)"},
		},
		{
			name: "the discs of a multi disc title are kept apart",
			games: []Game{
				{Title: "Final Fantasy IX (USA) (Disc 1)"},
				{Title: "Final Fantasy IX (USA) (Disc 2)"},
			},
			want: []string{"Final Fantasy IX (USA) (Disc 1)", "Final Fantasy IX (USA) (Disc 2)"},
		},
		{
			name: "every variant is kept when none of them is compatible",
			games: []Game{
				{Title: "Game (USA) (Beta)"},
				{Title: "Game (USA) (Proto)"},
			},
			want: []string{"Game (USA) (Beta)", "Game (USA) (Proto)"},
		},
		{
			name:  "an empty listing stays empty",
			games: nil,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PreferCompatible(tt.games, snes)
			if !reflect.DeepEqual(variantTitles(got), tt.want) {
				t.Errorf("PreferCompatible = %v, want %v", variantTitles(got), tt.want)
			}
		})
	}
}

func TestPreferCompatibleKeepsTheOrderOfTheListing(t *testing.T) {
	snes := platformOf(t, "snes")
	games := []Game{
		{Title: "Zelda (USA)"},
		{Title: "Mario (USA)"},
		{Title: "Mario (Europe)"},
	}

	got := PreferCompatible(games, snes)

	want := []string{"Zelda (USA)", "Mario (USA)"}
	if !reflect.DeepEqual(variantTitles(got), want) {
		t.Errorf("PreferCompatible = %v, want %v", variantTitles(got), want)
	}
}

func TestPreferCompatibleDoesNotModifyItsInput(t *testing.T) {
	snes := platformOf(t, "snes")
	games := []Game{
		{Title: "Mario (Europe)"},
		{Title: "Mario (USA)"},
	}

	_ = PreferCompatible(games, snes)

	want := []string{"Mario (Europe)", "Mario (USA)"}
	if !reflect.DeepEqual(variantTitles(games), want) {
		t.Errorf("input changed to %v, want %v", variantTitles(games), want)
	}
}
