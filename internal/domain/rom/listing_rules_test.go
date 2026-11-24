package rom

import (
	"reflect"
	"strings"
	"testing"
)

func titles(games []Game) []string {
	out := make([]string, 0, len(games))
	for _, g := range games {
		out = append(out, g.Title)
	}
	return out
}

func TestFilterByTitleKeepsGamesContainingTheTextIgnoringCase(t *testing.T) {
	games := []Game{{ID: "a", Title: "Super Mario Land"}, {ID: "b", Title: "Tetris"}, {ID: "c", Title: "MARIO Kart"}}
	got := FilterByTitle(games, "mario")
	if want := []string{"Super Mario Land", "MARIO Kart"}; !reflect.DeepEqual(titles(got), want) {
		t.Fatalf("FilterByTitle = %v, want %v", titles(got), want)
	}
}

func TestFilterByTitleWithoutTextKeepsEveryGame(t *testing.T) {
	games := []Game{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}
	if got := FilterByTitle(games, ""); len(got) != 2 {
		t.Fatalf("FilterByTitle with empty text = %d games, want 2", len(got))
	}
}

func TestFilterByTitleDoesNotModifyItsInput(t *testing.T) {
	games := []Game{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}, {ID: "c", Title: "Alpine"}}
	_ = FilterByTitle(games, "alp")
	if want := []string{"Alpha", "Beta", "Alpine"}; !reflect.DeepEqual(titles(games), want) {
		t.Fatalf("input changed to %v, want %v", titles(games), want)
	}
}

func TestSortByTitleOrdersByTitleKeepingEqualTitlesInOrder(t *testing.T) {
	games := []Game{{ID: "1", Title: "B"}, {ID: "2", Title: "A"}, {ID: "3", Title: "B"}, {ID: "4", Title: "A"}}
	SortByTitle(games)
	var ids []string
	for _, g := range games {
		ids = append(ids, g.ID)
	}
	if want := "2 4 1 3"; strings.Join(ids, " ") != want {
		t.Fatalf("order = %v, want %s", ids, want)
	}
}

func TestMergeGamesKeepsTheFirstGameOfADuplicatedID(t *testing.T) {
	first := []Game{{ID: "x", Title: "from first"}, {ID: "y", Title: "y"}}
	second := []Game{{ID: "x", Title: "from second"}, {ID: "z", Title: "z"}}
	got := MergeGames(first, second)
	if want := []string{"from first", "y", "z"}; !reflect.DeepEqual(titles(got), want) {
		t.Fatalf("MergeGames = %v, want %v", titles(got), want)
	}
}

func TestMergeGamesWithoutListsReturnsNoGames(t *testing.T) {
	if got := MergeGames(); len(got) != 0 {
		t.Fatalf("MergeGames() = %v, want none", got)
	}
}

func TestEveryPlatformExtensionIsRecognisedAsAGameFile(t *testing.T) {
	for _, p := range Platforms() {
		for _, ext := range p.Extensions {
			if !IsGameFile("some game" + ext) {
				t.Errorf("platform %s accepts %s but IsGameFile rejects it", p.Slug, ext)
			}
		}
	}
}

func TestRomExtensionsHaveNoDuplicatesAndAreLowercaseWithDot(t *testing.T) {
	seen := map[string]bool{}
	for _, ext := range romExtensions {
		if ext != strings.ToLower(ext) || !strings.HasPrefix(ext, ".") {
			t.Errorf("extension %q must be lowercase and start with a dot", ext)
		}
		if seen[ext] {
			t.Errorf("extension %q is listed twice", ext)
		}
		seen[ext] = true
	}
}

func TestEveryArchiveTheLibraryExtractsIsAPublishedArchive(t *testing.T) {
	for _, ext := range []string{".zip", ".7z", ".ZIP"} {
		if !ArchiveExtension(ext) || !IsArchive("game"+ext) {
			t.Errorf("%s must be both extractable and a recognised archive", ext)
		}
	}
	if ArchiveExtension(".rar") {
		t.Error(".rar is published but cannot be extracted")
	}
	if !IsArchive("game.rar") {
		t.Error("a .rar file is a published archive")
	}
}

func TestRegionIsTheSameForARawNameAndItsCleanedTitle(t *testing.T) {
	for _, name := range []string{
		"Super Mario Land (World).zip",
		"Tetris (USA) (Rev 1).7z",
		"No-Intro - Sonic_The_Hedgehog (Europe).zip",
		"Game's Name (Japan).zip",
	} {
		if raw, cleaned := RegionOf(name), RegionOf(CleanTitle(name)); raw != cleaned {
			t.Errorf("%q: region of the raw name = %q, of the title = %q", name, raw, cleaned)
		}
	}
}
