package romlistings

import (
	"context"
	"errors"
	"io/fs"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

type verifierSource struct {
	name  string
	delay time.Duration
	err   error
	calls atomic.Int32
}

func (s *verifierSource) Name() string        { return s.name }
func (s *verifierSource) Platforms() []string { return nil }
func (s *verifierSource) Search(ctx context.Context, _ rom.Query) ([]rom.Game, error) {
	s.calls.Add(1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.delay):
	}
	return []rom.Game{{ID: "same", Title: s.name}}, s.err
}

type verifierCache struct{ stores int }

func (c *verifierCache) Load(string) (rom.Listing, error) { return rom.Listing{}, fs.ErrNotExist }
func (c *verifierCache) Store(string, rom.Listing) error  { c.stores++; return nil }
func TestVerifierPartialPriority(t *testing.T) {
	a := &verifierSource{name: "first", delay: 10 * time.Millisecond}
	b := &verifierSource{name: "second"}
	bad := &verifierSource{name: "timeout", delay: time.Second}
	c := &verifierCache{}
	l := NewListings([]adapter.RomSource{a, b, bad}, c, time.Hour)
	l.sources.registry.timeout = 30 * time.Millisecond
	for range 2 {
		p, e := l.ObtainPlatformListing(context.Background(), "snes")
		if e != nil || len(p.Games) != 1 || p.Games[0].Title != "first" || len(p.FailedSources) != 1 || p.FailedSources[0] != "timeout" {
			t.Fatalf("%+v %v", p, e)
		}
	}
	if c.stores != 0 || len(l.listings) != 0 || a.calls.Load() != 2 {
		t.Fatal("partial response cached")
	}
	a.err = errors.New("bad")
	b.err = errors.New("bad")
	p, e := l.ObtainPlatformListing(context.Background(), "snes")
	if e == nil {
		t.Fatalf("all-failed %+v %v", p, e)
	}
}
