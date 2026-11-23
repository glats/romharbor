package romlistings

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"time"

	"app.ddd/internal/adapter"
	"app.ddd/internal/domain/rom"
)

func TestVerifierCacheFailureAndStale(t *testing.T) {
	for _, load := range []error{fs.ErrNotExist, errors.New("corrupt")} {
		c := &listingCache{loadErr: load, storeErr: errors.New("disk full")}
		s := &verifierSource{name: "good"}
		l := NewListings([]adapter.RomSource{s}, c, time.Nanosecond)
		var log bytes.Buffer
		l.logger = slog.New(slog.NewTextHandler(&log, nil))
		p, e := l.Games(context.Background(), "snes", "", 0, 50)
		if e != nil || len(p.Games) != 1 {
			t.Fatalf("%+v %v", p, e)
		}
		if strings.Contains(log.String(), "entry unusable") == errors.Is(load, fs.ErrNotExist) {
			t.Fatal(log.String())
		}
		if !strings.Contains(log.String(), "write failed") {
			t.Fatal(log.String())
		}
		s.err = errors.New("offline")
		p, e = l.Games(context.Background(), "snes", "", 0, 50)
		if e != nil || len(p.Games) != 1 || len(p.FailedSources) != 1 {
			t.Fatalf("stale %+v %v", p, e)
		}
	}
	_ = rom.Game{}
}
