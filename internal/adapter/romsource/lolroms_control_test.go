package romsource

import "testing"

func TestResolveGameRejectsControlCharacters(t *testing.T) {
	source := NewLolRoms(nil)
	for _, name := range []string{"x\r\n.7z", "x\x00.7z", "x%0D%0A.7z", "x%00.7z", "x\x7f.7z"} {
		id := "lolroms:/Nintendo/Super%20Nintendo%20Entertainment%20System/" + name
		if _, ok := source.ResolveGame(id); ok {
			t.Errorf("accepted malformed ID %q", id)
		}
	}
}
