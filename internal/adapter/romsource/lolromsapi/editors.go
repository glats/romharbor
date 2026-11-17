package lolromsapi

import (
	"context"
	"net/http"
)

// HTMLRequestEditor returns an editor that asks for HTML and identifies the
// client with the given User-Agent.
func HTMLRequestEditor(userAgent string) RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html")
		return nil
	}
}
