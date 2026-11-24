package rom

import "testing"

func TestUnknownCompatibilityTagsRemainCompatible(t *testing.T) {
	p, _ := PlatformBySlug("n64")
	for _, tag := range []string{"[bonus]", "[BIOS]", "[HD]", "[Homebrew]"} {
		if c := KnulliCompatibility(Game{Title: "Game " + tag}, p); !c.Compatible {
			t.Errorf("%s: %+v", tag, c)
		}
	}
}

func TestPreferredFormatWinsRegardlessOfInputOrder(t *testing.T) {
	for _, tc := range []struct{ slug, old, preferred string }{
		{"n64", "Game.n64", "Game.z64"},
		{"psx", "Game.cue", "Game.chd"},
	} {
		p, _ := PlatformBySlug(tc.slug)
		games := []Game{
			{ID: "old", Title: "Game (USA)", Files: []File{{Name: tc.old}}},
			{ID: "preferred", Title: "Game (USA)", Files: []File{{Name: tc.preferred}}},
		}
		got := PreferCompatible(games, p)
		if len(got) != 1 || got[0].ID != "preferred" {
			t.Errorf("%s: %+v", tc.slug, got)
		}
	}
}
