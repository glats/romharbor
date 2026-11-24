package romsource

import (
	"context"
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
