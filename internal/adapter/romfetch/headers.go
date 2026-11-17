package romfetch

import (
	"context"
	"net/http"
)

// Headers returns a request editor, shaped for the generated clients'
// RequestEditorFn, that sets the given headers on every outgoing request.
func Headers(values map[string]string) func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		for name, value := range values {
			req.Header.Set(name, value)
		}
		return nil
	}
}
