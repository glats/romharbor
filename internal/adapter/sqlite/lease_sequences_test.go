package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestStaleHolderCannotWriteAfterAnyRequeueSequence(t *testing.T) {
	for _, mode := range []string{"recover", "retry", "release", "backwards"} {
		t.Run(mode, func(t *testing.T) {
			s, clock, path := newJobStore(t)
			ctx := context.Background()
			mustEnqueue(t, s, "x")
			first, ok, err := s.Claim(ctx, time.Minute)
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			switch mode {
			case "recover":
				clock.Advance(time.Minute + time.Millisecond)
				n, e := s.Recover(ctx)
				if e != nil || n != 1 {
					t.Fatal(n, e)
				}
			case "retry":
				err = s.Retry(ctx, first.Lease, errors.New("retry"), clock.Now())
			case "release", "backwards":
				err = s.Release(ctx, first.Lease)
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "backwards" {
				clock.Advance(-time.Hour)
				if _, err = s.db.ExecContext(ctx, "UPDATE jobs SET run_at=?", clock.Now().UnixMilli()); err != nil {
					t.Fatal(err)
				}
			}
			second, ok, err := s.Claim(ctx, time.Minute)
			if err != nil || !ok {
				t.Fatal("requeued row unclaimable", ok, err)
			}
			if !second.Lease.Until.After(first.Lease.Until) {
				t.Fatal("token did not advance")
			}
			writes := []func() error{
				func() error {
					_, e := s.Heartbeat(ctx, first.Lease, 100, 100, time.Minute)
					return e
				},
				func() error { return s.Complete(ctx, first.Lease, rom.InstallResult{}) },
				func() error { return s.Retry(ctx, first.Lease, errors.New("old"), clock.Now()) },
				func() error { return s.Fail(ctx, first.Lease, errors.New("old")) },
				func() error { return s.Release(ctx, first.Lease) },
			}
			hash := func() [32]byte {
				var data []byte
				for _, suffix := range []string{"", "-wal"} {
					b, e := os.ReadFile(path + suffix)
					if e != nil && !errors.Is(e, os.ErrNotExist) {
						t.Fatal(e)
					}
					data = append(data, b...)
				}
				return sha256.Sum256(data)
			}
			for i, write := range writes {
				before := hash()
				if e := write(); !errors.Is(e, rom.ErrLeaseLost) {
					t.Fatalf("write %d: %v", i, e)
				}
				if before != hash() {
					t.Fatalf("rejected write %d changed the database", i)
				}
			}
			clock.Advance(second.Lease.Until.Sub(clock.Now()) + time.Millisecond)
			if _, err = s.Heartbeat(ctx, second.Lease, 1, 1, time.Minute); !errors.Is(err, rom.ErrLeaseLost) {
				t.Fatal("expired heartbeat renewed", err)
			}
		})
	}
}

func TestThirtyConcurrentStartsEnsureTheSchemaAndAFailingMigrationIsNotMasked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	start := make(chan struct{})
	errs := make(chan error, 30)
	for range 30 {
		go func() {
			<-start
			errs <- EnsureSchema(t.Context(), path)
		}()
	}
	close(start)
	for range 30 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	ready, err := SchemaReady(t.Context(), path)
	if err != nil || !ready {
		t.Fatal(ready, err)
	}

	bad := filepath.Join(t.TempDir(), "bad.db")
	db, err := Open(t.Context(), bad)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE users (unexpected TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSchema(t.Context(), bad); err == nil {
		t.Fatal("a genuinely failing migration was masked")
	}
}

func TestOnlyOneOfTwoHandlesClaimsTheSameJob(t *testing.T) {
	s, _, path := newJobStore(t)
	mustEnqueue(t, s, "race")
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	other := NewJobStore(db)
	other.Now = s.Now
	start := make(chan struct{})
	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for _, store := range []*JobStore{s, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, ok, e := store.Claim(ctx, time.Minute)
			if e != nil {
				t.Error(e)
			}
			if ok {
				results <- true
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(results) != 1 {
		t.Fatalf("claims=%d", len(results))
	}
}
