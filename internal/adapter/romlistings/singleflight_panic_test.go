package romlistings

import (
	"context"
	"errors"
	"testing"
)

func TestAPanickingLeaderLeavesNoFlightBehind(t *testing.T) {
	var flights singleflight[int]
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the leader panic must reach the caller")
			}
		}()
		_, _ = flights.do(context.Background(), "key", func(context.Context) (int, error) { panic("upstream") })
	}()
	flights.mu.Lock()
	remaining := len(flights.calls)
	flights.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("the panic left %d flights", remaining)
	}
	value, err := flights.do(context.Background(), "key", func(context.Context) (int, error) { return 7, nil })
	if value != 7 || err != nil {
		t.Fatalf("a new request = %d, %v; want 7, nil", value, err)
	}
}

func TestAWaiterOfAPanickingLeaderGetsAnError(t *testing.T) {
	var flights singleflight[int]
	entered := make(chan struct{})
	release := make(chan struct{})
	go func() {
		defer func() { _ = recover() }()
		_, _ = flights.do(context.Background(), "key", func(context.Context) (int, error) {
			close(entered)
			<-release
			panic("upstream")
		})
	}()
	<-entered
	result := make(chan error, 1)
	go func() {
		_, err := flights.do(context.Background(), "key", func(context.Context) (int, error) { return 1, nil })
		result <- err
	}()
	close(release)
	if err := <-result; err != nil && !errors.Is(err, errFlightAborted) {
		t.Fatalf("waiter error = %v; want %v", err, errFlightAborted)
	}
}
