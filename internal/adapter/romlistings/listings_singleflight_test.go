package romlistings

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type blockingSource struct {
	games   []rom.Game
	err     error
	calls   atomic.Int32
	release chan struct{}
}

func (s *blockingSource) Name() string        { return "blocking" }
func (s *blockingSource) Platforms() []string { return nil }
func (s *blockingSource) Search(ctx context.Context, _ rom.Query) ([]rom.Game, error) {
	s.calls.Add(1)
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.games, nil
}

func TestConcurrentColdListingAsksTheSourcesOnce(t *testing.T) {
	source := &blockingSource{games: []rom.Game{{ID: "g", Title: "G"}}, release: make(chan struct{})}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)

	const callers = 8
	pages := make([]rom.GamePage, callers)
	errs := make([]error, callers)
	ready := &sync.WaitGroup{}
	ready.Add(callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			pages[i], errs[i] = listings.ObtainPlatformListing(context.Background(), "snes")
		}()
	}
	ready.Wait()
	close(start)
	waitForStableCalls(t, source)
	close(source.release)
	wg.Wait()

	if got := source.calls.Load(); got != 1 {
		t.Fatalf("the sources were asked %d times, want 1", got)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d error = %v", i, errs[i])
		}
		if len(pages[i].Games) != 1 || pages[i].Games[0].ID != "g" {
			t.Fatalf("caller %d = %+v, want the leader's listing", i, pages[i])
		}
	}
}

func TestConcurrentColdListingSharesTheLeaderError(t *testing.T) {
	source := &blockingSource{err: errors.New("upstream unavailable"), release: make(chan struct{})}
	listings := NewListings([]adapter.RomSource{source}, &memoryCache{}, time.Hour)

	const callers = 8
	errs := make([]error, callers)
	ready := &sync.WaitGroup{}
	ready.Add(callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			_, errs[i] = listings.ObtainPlatformListing(context.Background(), "snes")
		}()
	}
	ready.Wait()
	close(start)
	waitForStableCalls(t, source)
	close(source.release)
	wg.Wait()

	if got := source.calls.Load(); got != 1 {
		t.Fatalf("the sources were asked %d times, want 1", got)
	}
	for i := range callers {
		if errs[i] == nil || !errors.Is(errs[i], source.err) {
			t.Fatalf("caller %d error = %v, want the leader's error", i, errs[i])
		}
	}
}

func waitForStableCalls(t *testing.T, source *blockingSource) {
	t.Helper()
	last := int32(-1)
	stable := 0
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := source.calls.Load()
		if got == last {
			stable++
			if stable >= 20 {
				return
			}
		} else {
			stable = 0
			last = got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the sources were never called")
}
