package rom

import (
	"reflect"
	"testing"
)

func TestPageReturnsTheRequestedSliceOfResults(t *testing.T) {
	games := []Game{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	tests := []struct {
		name   string
		offset int
		limit  int
		want   []string
	}{
		{name: "without a limit every game is kept", want: []string{"1", "2", "3"}},
		{name: "the offset skips the first games", offset: 1, want: []string{"2", "3"}},
		{name: "the limit caps the games", limit: 2, want: []string{"1", "2"}},
		{name: "the offset and the limit are applied together", offset: 1, limit: 1, want: []string{"2"}},
		{name: "an offset past the last game returns nothing", offset: 5, want: []string{}},
		{name: "a negative offset is read as none", offset: -1, limit: 1, want: []string{"1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Page(games, tt.offset, tt.limit)
			ids := make([]string, 0, len(got))
			for _, game := range got {
				ids = append(ids, game.ID)
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Errorf("Page(%d, %d) = %v, want %v", tt.offset, tt.limit, ids, tt.want)
			}
		})
	}
}
