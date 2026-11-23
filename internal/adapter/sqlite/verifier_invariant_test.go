package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"
)

func TestVerifierCancelledUnchanged(t *testing.T) {
	_, _, p := newJobStore(t)
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	ctx, c := context.WithCancel(context.Background())
	c()
	for _, op := range []func() error{func() error { _, e := Open(ctx, p); return e }, func() error { return Migrate(ctx, p) }, func() error { return Rollback(ctx, p) }} {
		if e := op(); !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		after, e := os.ReadFile(p)
		if e != nil || sha256.Sum256(before) != sha256.Sum256(after) {
			t.Fatal("cancel changed database", e)
		}
	}
}
