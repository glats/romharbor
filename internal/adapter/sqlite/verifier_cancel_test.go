package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestVerifierCancelled(t *testing.T) {
	ctx, c := context.WithCancel(context.Background())
	c()
	for _, op := range []string{"open", "migrate", "rollback"} {
		p := filepath.Join(t.TempDir(), "absent", "db")
		var e error
		switch op {
		case "open":
			_, e = Open(ctx, p)
		case "migrate":
			e = Migrate(ctx, p)
		case "rollback":
			e = Rollback(ctx, p)
		}
		if !errors.Is(e, context.Canceled) {
			t.Fatalf("%s: %v", op, e)
		}
		if _, e = os.Stat(filepath.Dir(p)); !os.IsNotExist(e) {
			t.Fatalf("%s touched filesystem", op)
		}
	}
}

func TestVerifierMissingLease(t *testing.T) {
	s, _, _ := newJobStore(t)
	for _, e := range []error{s.Heartbeat(context.Background(), "999", 0, 0, 0), s.Complete(context.Background(), "999", rom.InstallResult{}), s.Retry(context.Background(), "999", errors.New("x"), time.Now()), s.Release(context.Background(), "999"), s.Fail(context.Background(), "999", errors.New("x"))} {
		if !errors.Is(e, rom.ErrLeaseLost) {
			t.Fatal(e)
		}
	}
}
