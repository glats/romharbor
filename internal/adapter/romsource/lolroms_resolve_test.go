package romsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"app.ddd/internal/domain/rom"
)

func TestLolRomsResolvesAGameFromItsIDWithoutTheNetwork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="/Nintendo/Super%20Nintendo%20Entertainment%20System/Super%20Mario%20World%20(USA).7z">x</a>`))
	}))
	source := NewLolRoms(server.Client())
	source.BaseURL = server.URL
	found, err := source.Search(context.Background(), rom.Query{Text: "mario"})
	server.Close()
	if err != nil || len(found) != 1 {
		t.Fatalf("search = %+v, %v", found, err)
	}

	got, ok := source.ResolveGame(found[0].ID)

	if !ok || !reflect.DeepEqual(got, found[0]) {
		t.Fatalf("ResolveGame(%q) = %+v, %v; want the searched game %+v", found[0].ID, got, ok, found[0])
	}
}

func TestLolRomsRefusesIDsThatAreNotItsOwnGames(t *testing.T) {
	source := NewLolRoms(nil)
	for _, id := range []string{
		"",
		"archive.org:item/file.zip",
		"lolroms:",
		"lolroms:Nintendo/Super%20Nintendo%20Entertainment%20System/Mario.7z",
		"lolroms:/Nintendo/Super%20Nintendo%20Entertainment%20System/Mario.txt",
		"lolroms:/Nintendo/Unknown%20Console/Mario.7z",
		"lolroms:/Nintendo/Super%20Nintendo%20Entertainment%20System/../../etc/Mario.7z",
		"lolroms:/Nintendo/Super%20Nintendo%20Entertainment%20System/Mario.7z?x=1",
		"lolroms:/Mario.7z",
		"lolroms://evil.example/Super%20Nintendo%20Entertainment%20System/Mario.7z",
	} {
		if game, ok := source.ResolveGame(id); ok {
			t.Errorf("ResolveGame(%q) = %+v, want a refusal", id, game)
		}
	}
}
