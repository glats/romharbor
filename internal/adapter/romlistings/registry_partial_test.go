package romlistings

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type scriptedSource struct {
	name  string
	games []rom.Game
	err   error
	delay time.Duration
}

func (s scriptedSource) Name() string        { return s.name }
func (s scriptedSource) Platforms() []string { return nil }

func (s scriptedSource) Search(ctx context.Context, _ rom.Query) ([]rom.Game, error) {
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.games, nil
}

func TestSearchKeepsTheGamesOfTheSourcesThatAnsweredAndNamesTheFailedOnes(t *testing.T) {
	captureLogs(t)
	r := newRegistry([]adapter.RomSource{
		scriptedSource{name: "broken", err: errors.New("upstream unavailable")},
		scriptedSource{name: "healthy", games: []rom.Game{{ID: "g", Title: "G"}}},
	}, time.Second, slog.Default())

	page, err := r.search(context.Background(), rom.Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Games) != 1 || page.Games[0].ID != "g" {
		t.Fatalf("games = %+v, want the game of the healthy source", page.Games)
	}
	if len(page.FailedSources) != 1 || page.FailedSources[0] != "broken" {
		t.Fatalf("failed sources = %+v, want [broken]", page.FailedSources)
	}
}

func TestSearchBoundsEverySourceWithItsOwnTimeout(t *testing.T) {
	captureLogs(t)
	r := newRegistry([]adapter.RomSource{
		scriptedSource{name: "slow", delay: time.Minute},
		scriptedSource{name: "healthy", games: []rom.Game{{ID: "g"}}},
	}, 20*time.Millisecond, slog.Default())

	started := time.Now()
	page, err := r.search(context.Background(), rom.Query{})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("search took %s, want it bounded by the source timeout", elapsed)
	}
	if len(page.Games) != 1 || page.Games[0].ID != "g" {
		t.Fatalf("games = %+v, want the game of the healthy source", page.Games)
	}
	if len(page.FailedSources) != 1 || page.FailedSources[0] != "slow" {
		t.Fatalf("failed sources = %+v, want [slow]", page.FailedSources)
	}
}

func TestSearchResolvesADuplicateByConfigurationOrderEvenWhenTheLaterSourceAnswersFirst(t *testing.T) {
	r := newRegistry([]adapter.RomSource{
		scriptedSource{name: "first", games: []rom.Game{{ID: "dup", Title: "From first", Source: "first"}}, delay: 50 * time.Millisecond},
		scriptedSource{name: "second", games: []rom.Game{{ID: "dup", Title: "From second", Source: "second"}, {ID: "other", Title: "Other", Source: "second"}}},
	}, time.Second, slog.Default())

	page, err := r.search(context.Background(), rom.Query{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Games) != 2 {
		t.Fatalf("games = %+v, want the duplicate and the other game", page.Games)
	}
	if page.Games[0].ID != "dup" || page.Games[0].Source != "first" {
		t.Fatalf("duplicate = %+v, want the game of the first configured source", page.Games[0])
	}
	if page.Games[1].ID != "other" {
		t.Fatalf("games = %+v, want the other game", page.Games)
	}
}

func TestSearchFailsOnlyWhenEveryQueriedSourceFailed(t *testing.T) {
	captureLogs(t)
	firstFailure := errors.New("first failure")
	secondFailure := errors.New("second failure")
	r := newRegistry([]adapter.RomSource{
		scriptedSource{name: "first", err: firstFailure},
		scriptedSource{name: "second", err: secondFailure},
	}, time.Second, slog.Default())

	page, err := r.search(context.Background(), rom.Query{})

	if err == nil {
		t.Fatalf("search returned %+v, want the error of the first configured source", page)
	}
	if !errors.Is(err, firstFailure) {
		t.Fatalf("error = %v, want the error of the first configured source", err)
	}
	if len(page.Games) != 0 {
		t.Fatalf("games = %+v, want none", page.Games)
	}
}

func TestSearchLogsEverySourceErrorWithItsSourceName(t *testing.T) {
	logs := captureLogs(t)
	r := newRegistry([]adapter.RomSource{
		scriptedSource{name: "broken", err: errors.New("upstream unavailable")},
		scriptedSource{name: "healthy", games: []rom.Game{{ID: "g"}}},
	}, time.Second, slog.Default())

	if _, err := r.search(context.Background(), rom.Query{}); err != nil {
		t.Fatalf("search: %v", err)
	}

	if got := logLines(logs.String(), "source", "broken"); got != 1 {
		t.Fatalf("logged %d failures for broken, want 1: %s", got, logs.String())
	}
	if got := logLines(logs.String(), "healthy"); got != 0 {
		t.Fatalf("logged %d entries for the healthy source, want 0: %s", got, logs.String())
	}
}
