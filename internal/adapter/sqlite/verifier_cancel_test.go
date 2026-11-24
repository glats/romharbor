package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app.ddd/internal/adapter"
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
	lease := adapter.RomJobLease{JobID: "999", Until: time.Now()}
	_, beat := s.Heartbeat(context.Background(), lease, 0, 0, 0)
	for _, e := range []error{beat, s.Complete(context.Background(), lease, rom.InstallResult{}), s.Retry(context.Background(), lease, errors.New("x"), time.Now()), s.Release(context.Background(), lease), s.Fail(context.Background(), lease, errors.New("x"))} {
		if !errors.Is(e, rom.ErrLeaseLost) {
			t.Fatal(e)
		}
	}
}
