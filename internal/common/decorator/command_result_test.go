package decorator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app.ddd/internal/common/trace"
)

type resultCommandProbe func(context.Context, doThing) (string, error)

func (f resultCommandProbe) Handle(ctx context.Context, value doThing) (string, error) {
	return f(ctx, value)
}

func TestApplyCommandResultDecorators(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name       string
		result     string
		err        error
		wantResult string
		wantErr    error
		wantKeys   []string
	}{
		{
			name:       "success",
			result:     "done",
			wantResult: "done",
			wantKeys:   []string{"commands.dothing.duration", "commands.dothing.success"},
		},
		{
			name:       "failure",
			err:        boom,
			wantResult: "",
			wantErr:    boom,
			wantKeys:   []string{"commands.dothing.duration", "commands.dothing.failure"},
		},
		{
			name:       "result with error",
			result:     "partial",
			err:        boom,
			wantResult: "partial",
			wantErr:    boom,
			wantKeys:   []string{"commands.dothing.duration", "commands.dothing.failure"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics := &recordingMetrics{}
			handler := ApplyCommandResultDecorators[doThing, string](resultCommandProbe(func(context.Context, doThing) (string, error) {
				return tt.result, tt.err
			}), metrics)

			result, err := handler.Handle(context.Background(), doThing{ID: "1"})

			assert.Equal(t, tt.wantErr, err)
			assert.Equal(t, tt.wantResult, result)
			assert.Equal(t, tt.wantKeys, metrics.keys)
		})
	}
}

func TestCommandResultDecoratorObservableContract(t *testing.T) {
	previous, level := log.Logger, zerolog.GlobalLevel()
	t.Cleanup(func() { log.Logger = previous; zerolog.SetGlobalLevel(level) })
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	boom := errors.New("handler failure")
	ctx := context.WithValue(context.Background(), trace.TraceIDKey, "trace-contract")

	for _, handlerErr := range []error{nil, boom} {
		t.Run(map[bool]string{true: "success", false: "failure"}[handlerErr == nil], func(t *testing.T) {
			var output bytes.Buffer
			log.Logger = zerolog.New(&output)
			metrics := &recordingMetrics{}
			handler := ApplyCommandResultDecorators[doThing, string](resultCommandProbe(func(received context.Context, value doThing) (string, error) {
				assert.Same(t, ctx, received)
				assert.Equal(t, "sample", value.ID)
				return "done", handlerErr
			}), metrics)

			result, err := handler.Handle(ctx, doThing{ID: "sample"})

			assert.Equal(t, handlerErr, err)
			assert.Equal(t, "done", result)
			outcome := map[bool]string{true: "success", false: "failure"}[handlerErr == nil]
			assert.Equal(t, []string{"commands.dothing.duration", "commands.dothing." + outcome}, metrics.keys)
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			require.Len(t, lines, map[bool]int{true: 2, false: 1}[handlerErr == nil])
			for index, line := range lines {
				var event map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &event))
				message := "Executing command"
				if index == 1 {
					message = "Command executed successfully"
				}
				assert.Equal(t, map[string]any{
					"level":        "debug",
					"traceId":      "trace-contract",
					"command":      "doThing",
					"command_body": `decorator.doThing{ID:"sample"}`,
					"message":      message,
				}, event)
			}
		})
	}
}
