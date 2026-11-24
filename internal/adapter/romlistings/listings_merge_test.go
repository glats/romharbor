package romlistings

import (
	"context"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type searchOnlySource struct{ gameSource }

func (searchOnlySource) SearchOnly() bool { return true }

func TestSearchListsAGameOnceWhenTheListingAndTheSearchBothReturnIt(t *testing.T) {
	listed := gameSource{games: []rom.Game{{ID: "same", Title: "Mario Listed"}}}
	found := searchOnlySource{gameSource{games: []rom.Game{
		{ID: "same", Title: "Mario Found"},
		{ID: "other", Title: "Mario Other"},
	}}}
	listings := NewListings([]adapter.RomSource{listed, found}, &memoryCache{}, time.Hour)

	page, err := listings.Games(context.Background(), "snes", "mario", 0, 10)
	if err != nil {
		t.Fatalf("Games() error = %v", err)
	}
	var ids []string
	for _, g := range page.Games {
		ids = append(ids, g.ID+":"+g.Title)
	}
	if want := []string{"same:Mario Listed", "other:Mario Other"}; len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("games = %v, want %v", ids, want)
	}
}
