package rom

import (
	"sort"
	"strings"
)

// FilterByTitle returns the games whose title contains text, ignoring case.
// An empty text keeps every game. The input is not modified.
func FilterByTitle(games []Game, text string) []Game {
	if text == "" {
		return games
	}
	needle := strings.ToLower(text)
	out := make([]Game, 0, len(games))
	for _, g := range games {
		if strings.Contains(strings.ToLower(g.Title), needle) {
			out = append(out, g)
		}
	}
	return out
}

// SortByTitle orders games by title, keeping games with equal titles in their
// current order.
func SortByTitle(games []Game) {
	sort.SliceStable(games, func(i, j int) bool { return games[i].Title < games[j].Title })
}

// MergeGames joins lists in the order given. When several games share an ID the
// first one wins, so the order of the lists is the priority of their sources.
func MergeGames(lists ...[]Game) []Game {
	var out []Game
	seen := map[string]bool{}
	for _, list := range lists {
		for _, g := range list {
			if seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			out = append(out, g)
		}
	}
	return out
}
