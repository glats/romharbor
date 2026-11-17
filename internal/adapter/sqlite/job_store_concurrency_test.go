package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/domain/rom"
	"app.ddd/internal/domain/user"
)

func TestConcurrentWritersNeverSeeADatabaseLockError(t *testing.T) {
	store, _, path := newJobStore(t)
	store.Now = time.Now
	ctx := context.Background()
	db, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	users := NewUserRepository(db)
	for i := range 20 {
		mustEnqueue(t, store, fmt.Sprintf("seed%d", i))
	}

	var locked, other, operations atomic.Int64
	check := func(err error) {
		operations.Add(1)
		if err == nil {
			return
		}
		if message := strings.ToLower(err.Error()); strings.Contains(message, "locked") || strings.Contains(message, "busy") {
			locked.Add(1)
			t.Logf("lock error: %v", err)
			return
		}
		other.Add(1)
		t.Logf("unexpected error: %v", err)
	}
	deadline := time.Now().Add(1500 * time.Millisecond)
	var wg sync.WaitGroup
	run := func(workers int, work func(i int)) {
		for w := range workers {
			wg.Go(func() {
				for i := 0; time.Now().Before(deadline); i++ {
					work(w*1_000_000 + i)
				}
			})
		}
	}
	run(4, func(i int) { check(store.Heartbeat(ctx, "1", int64(i), 100, time.Minute)) })
	run(2, func(int) {
		claim, ok, err := store.Claim(ctx, time.Minute)
		check(err)
		if ok {
			check(store.Complete(ctx, claim.Job.ID, rom.InstallResult{Path: "/x", Size: 1}))
		}
	})
	run(2, func(i int) { _, err := store.Enqueue(ctx, installRequest(fmt.Sprintf("n%d", i))); check(err) })
	run(1, func(int) { _, err := store.Recover(ctx); check(err) })
	run(1, func(int) { check(store.Retry(ctx, "2", errors.New("e"), time.Now())) })
	run(3, func(int) { _, err := store.List(ctx); check(err) })
	run(3, func(int) { _, _, err := store.Get(ctx, "1"); check(err) })
	run(2, func(i int) {
		check(users.Create(ctx, user.New(user.WithID(fmt.Sprintf("u%d", i)), user.WithName("n"), user.WithEmail(fmt.Sprintf("e%d@x.test", i)))))
	})
	wg.Wait()

	assert.Positive(t, operations.Load())
	assert.Zero(t, locked.Load(), "SQLITE_BUSY surfaced")
	assert.Zero(t, other.Load())
}
