package romlistings

import (
	"context"
	"errors"
	"sync"
)

var errFlightAborted = errors.New("romlistings: the fetch ended without a result")

type flight[T any] struct {
	done  chan struct{}
	value T
	err   error
}

type singleflight[T any] struct {
	mu    sync.Mutex
	calls map[string]*flight[T]
}

func (s *singleflight[T]) do(ctx context.Context, key string, fn func(context.Context) (T, error)) (T, error) {
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[string]*flight[T]{}
	}
	if call, ok := s.calls[key]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.value, call.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}
	call := &flight[T]{done: make(chan struct{})}
	s.calls[key] = call
	s.mu.Unlock()

	call.err = errFlightAborted
	func() {
		defer func() {
			s.mu.Lock()
			delete(s.calls, key)
			s.mu.Unlock()
			close(call.done)
		}()
		call.value, call.err = fn(ctx)
	}()
	return call.value, call.err
}
