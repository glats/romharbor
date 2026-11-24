package romlibrary

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"app.ddd/internal/domain/rom"
)

func TestAnInvalidInstallDoesNotWaitForTheGate(t *testing.T) {
	body := []byte("verified ROM")
	holding := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		close(holding)
		<-release
		_, _ = w.Write(body)
	})
	l, _ := newLibrary(t)
	valid := rom.InstallRequest{Platform: snesPlatform, File: rom.File{
		Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL,
	}}
	done := make(chan error, 1)
	go func() {
		_, err := l.Install(context.Background(), valid, nil)
		done <- err
	}()
	<-holding

	tests := []struct {
		name string
		req  rom.InstallRequest
		want string
	}{
		{
			name: "no download URL",
			req:  rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc"}},
			want: "has no download URL",
		},
		{
			name: "a file the platform refuses",
			req:  rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.md", URL: srv.URL}},
			want: "is not a ROM",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			_, err := l.Install(ctx, tt.req, nil)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Install() error = %v, want one containing %q", err, tt.want)
			}
			select {
			case err := <-done:
				t.Fatalf("the install holding the gate ended early: %v", err)
			default:
			}
		})
	}

	releaseOnce.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatalf("Install() error = %v", err)
	}
}

func TestInstallRefusesAnEmptyLibraryRoot(t *testing.T) {
	l := &Library{}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", URL: "http://unused.invalid/game"},
	}, nil)

	if err == nil || !strings.Contains(err.Error(), "library root is not set") {
		t.Fatalf("Install() error = %v, want the empty root reported", err)
	}
}
