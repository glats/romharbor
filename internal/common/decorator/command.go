package decorator

import "context"

type CommandHandler[C any] interface {
	Handle(context.Context, C) error
}

type CommandResultHandler[C, R any] interface {
	Handle(context.Context, C) (R, error)
}

type commandCall[C any] func(context.Context, C) error

func (call commandCall[C]) Handle(ctx context.Context, value C) error {
	return call(ctx, value)
}

type commandResultCall[C, R any] func(context.Context, C) (R, error)

func (call commandResultCall[C, R]) Handle(ctx context.Context, value C) (R, error) {
	return call(ctx, value)
}

func ApplyCommandDecorators[C any](handler CommandHandler[C], metrics MetricsClient) CommandHandler[C] {
	return commandCall[C](func(ctx context.Context, value C) error {
		return measureCommand(ctx, value, metrics, func() error {
			return handler.Handle(ctx, value)
		})
	})
}

func ApplyCommandResultDecorators[C, R any](handler CommandResultHandler[C, R], metrics MetricsClient) CommandResultHandler[C, R] {
	return commandResultCall[C, R](func(ctx context.Context, value C) (R, error) {
		var result R
		err := measureCommand(ctx, value, metrics, func() error {
			var callErr error
			result, callErr = handler.Handle(ctx, value)
			return callErr
		})
		return result, err
	})
}

func measureCommand[C any](ctx context.Context, value C, metrics MetricsClient, call func() error) (err error) {
	action := generateActionName(value)
	emitAction(ctx, "command", action, value, false)
	defer func() {
		if err == nil {
			emitAction(ctx, "command", action, value, true)
		}
	}()
	return measure(metrics, "commands", action, call)
}
