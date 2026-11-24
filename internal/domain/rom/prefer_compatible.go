package rom

import (
	"strconv"
	"strings"
)

// PreferCompatible keeps the best variant of every title of a listing.
//
// The variants of a title are the games that share the base title, the title
// without its tags. When one of the variants is compatible, only the best one
// is kept; when none of them is, every variant is kept so the interface can
// label it. The variants of a multi disc title are kept apart, one group per
// disc. The order of the listing is kept.
func PreferCompatible(games []Game, p Platform) []Game {
	keys := make([]string, len(games))
	ranks := make([]variantRank, len(games))
	best := map[string]int{}
	compatible := map[string]bool{}
	for index, game := range games {
		keys[index] = variantKey(game)
		ranks[index] = rankOf(game, p)
		if ranks[index].compatible {
			compatible[keys[index]] = true
		}
		if chosen, ok := best[keys[index]]; !ok || better(ranks[index], ranks[chosen]) {
			best[keys[index]] = index
		}
	}
	out := make([]Game, 0, len(games))
	for index, game := range games {
		if compatible[keys[index]] && best[keys[index]] != index {
			continue
		}
		out = append(out, game)
	}
	return out
}

func variantKey(g Game) string {
	key := strings.ToLower(baseTitle(g.Title))
	facts := factsOf(g.Title)
	if facts.hasDisc {
		key += "\x00" + strconv.Itoa(facts.disc)
	}
	return key
}

type variantRank struct {
	compatible bool
	verified   bool
	region     int
	disc       int
}

func rankOf(g Game, p Platform) variantRank {
	facts := factsOfGame(g)
	compatibility := compatibilityOf(facts, g, p)
	return variantRank{
		compatible: compatibility.Compatible,
		verified:   facts.good,
		region:     regionRank(facts.region),
		disc:       facts.disc,
	}
}

func better(a, b variantRank) bool {
	if a.compatible != b.compatible {
		return a.compatible
	}
	if a.verified != b.verified {
		return a.verified
	}
	if a.region != b.region {
		return a.region < b.region
	}
	return a.disc > b.disc
}

func regionRank(region string) int {
	switch region {
	case "usa":
		return 0
	case "world":
		return 1
	case "europe":
		return 2
	case "japan":
		return 3
	case "":
		return 5
	default:
		return 4
	}
}
