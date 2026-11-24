package sqlite

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestVerifierQueueRace(t *testing.T) {
	s, c, _ := newJobStore(t)
	ctx := context.Background()
	j := mustEnqueue(t, s, "race")
	claim, _, _ := s.Claim(ctx, time.Second)
	c.Advance(2 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			switch i % 5 {
			case 0:
				_, _ = s.Recover(ctx)
			case 1:
				_, _, _ = s.Claim(ctx, time.Second)
			case 2:
				_, _ = s.Heartbeat(ctx, claim.Lease, 1, 2, time.Second)
			case 3:
				_ = s.Complete(ctx, claim.Lease, rom.InstallResult{Path: "done"})
			case 4:
				_ = s.Fail(ctx, claim.Lease, errors.New("failed"))
			}
		}(i)
	}
	wg.Wait()
	got, ok, e := s.Get(ctx, j.ID)
	if !ok || e != nil {
		t.Fatal(e)
	}
	t.Logf("final status %s", got.Status)
}
