package romlibrary

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"app.ddd/internal/domain/rom"
)

func md5Hex(body []byte) string {
	sum := md5.Sum(body)
	return hex.EncodeToString(sum[:])
}

func sha1Hex(body []byte) string {
	sum := sha1.Sum(body)
	return hex.EncodeToString(sum[:])
}

func writePart(t *testing.T, root, name string, body []byte) string {
	t.Helper()
	dir := filepath.Join(root, "snes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, name+".part")
	if err := os.WriteFile(part, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return part
}

func assertNoExtractDir(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.Contains(entry.Name(), ".extract-") {
			t.Errorf("extraction directory %s was left behind", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// L-15: the library marks the failures a retry cannot fix.
func TestInstallMarksTheFailuresARetryCannotFix(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{
			name: "checksum mismatch",
			run: func(t *testing.T) error {
				srv := serve(t, servedBody([]byte("permanent")))
				l, _ := newLibrary(t)
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: 9, URL: srv.URL, MD5: strings.Repeat("0", 32)},
				}, nil)
				return err
			},
		},
		{
			name: "no ROM inside the archive",
			run: func(t *testing.T) error {
				archive := zipArchive(t, []zipEntry{{name: "notes.txt", body: "read me"}})
				srv := serve(t, servedBody(archive))
				l, _ := newLibrary(t)
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Bundle.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
				}, nil)
				return err
			},
		},
		{
			name: "unsupported archive format",
			run: func(t *testing.T) error {
				srv := serve(t, servedBody([]byte("rom")))
				l, _ := newLibrary(t)
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: 3, URL: srv.URL, Archive: true},
				}, nil)
				return err
			},
		},
		{
			name: "extraction entry cap exceeded",
			run: func(t *testing.T) error {
				archive := zipArchive(t, []zipEntry{{name: "Game.sfc", body: "rom"}})
				srv := serve(t, servedBody(archive))
				l, _ := newLibrary(t)
				l.limits = extractLimits{entries: 0, bytes: defaultExtractLimits.bytes}
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Bundle.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
				}, nil)
				return err
			},
		},
		{
			name: "extraction byte cap exceeded",
			run: func(t *testing.T) error {
				archive := zipArchive(t, []zipEntry{{name: "Game.sfc", body: "rom"}})
				srv := serve(t, servedBody(archive))
				l, _ := newLibrary(t)
				l.limits = extractLimits{entries: defaultExtractLimits.entries, bytes: 1}
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Bundle.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
				}, nil)
				return err
			},
		},
		{
			name: "path containment rejection",
			run: func(t *testing.T) error {
				l, _ := newLibrary(t)
				escaping := rom.Platform{Slug: "snes", Dir: "../escape", Extensions: []string{".sfc"}}
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: escaping,
					File:     rom.File{Name: "Game.sfc", URL: "http://unused.invalid/game"},
				}, nil)
				return err
			},
		},
		{
			name: "download URL with an unsupported scheme",
			run: func(t *testing.T) error {
				l, _ := newLibrary(t)
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", URL: "file:///etc/passwd"},
				}, nil)
				return err
			},
		},
		{
			name: "download URL without a host",
			run: func(t *testing.T) error {
				l, _ := newLibrary(t)
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", URL: "http:///game.sfc"},
				}, nil)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			if !errors.Is(err, rom.ErrNotRetryable) {
				t.Fatalf("Install() error = %v, want a failure a retry cannot fix", err)
			}
		})
	}
}

// L-15: the statuses a retry cannot fix, and the ones it can.
func TestInstallClassifiesTheHTTPStatuses(t *testing.T) {
	tests := []struct {
		status       int
		notRetryable bool
	}{
		{status: http.StatusBadRequest, notRetryable: true},
		{status: http.StatusUnauthorized, notRetryable: true},
		{status: http.StatusForbidden, notRetryable: true},
		{status: http.StatusNotFound, notRetryable: true},
		{status: http.StatusGone, notRetryable: true},
		{status: http.StatusRequestTimeout},
		{status: http.StatusTooManyRequests},
		{status: http.StatusInternalServerError},
		{status: http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "no", tt.status)
			})
			l, root := newLibrary(t)

			_, err := l.Install(context.Background(), rom.InstallRequest{
				Platform: snesPlatform,
				File:     rom.File{Name: "Game.sfc", URL: srv.URL},
			}, nil)

			if err == nil {
				t.Fatal("Install() error = nil, want the status to fail the download")
			}
			if got := errors.Is(err, rom.ErrNotRetryable); got != tt.notRetryable {
				t.Errorf("Install() error = %v, not retryable = %v, want %v", err, got, tt.notRetryable)
			}
			if files := installedFiles(t, root); len(files) != 0 {
				t.Errorf("library files = %v, want none", files)
			}
		})
	}
}

// L-06: the downloaded byte count is compared with the published size.
func TestInstallRejectsADownloadWhoseSizeDoesNotMatch(t *testing.T) {
	body := []byte("seven77")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: 4321, URL: srv.URL},
	}, nil)

	if err == nil {
		t.Fatal("Install() error = nil, want the size mismatch to fail the install")
	}
	if !strings.Contains(err.Error(), "4321") || !strings.Contains(err.Error(), "got 7") {
		t.Errorf("error %q does not name the expected and the downloaded size", err)
	}
	if errors.Is(err, rom.ErrNotRetryable) {
		t.Errorf("error %v is marked as unfixable, want a retryable failure", err)
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
}

// L-05: a partial answer that does not match the offset is discarded.
func TestInstallDiscardsAPartWhenTheContentRangeDoesNotStartAtTheOffset(t *testing.T) {
	body := []byte("0123456789abcdef")
	var requests atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Range", "bytes 0-11/12")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("wrong-body!"))
			return
		}
		_, _ = w.Write(body)
	})
	l, root := newLibrary(t)
	writePart(t, root, "Game.sfc", body[:4])

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

// L-05: a partial answer whose total does not match the published size is discarded.
func TestInstallDiscardsAPartWhenTheContentRangeTotalDoesNotMatchTheSize(t *testing.T) {
	body := []byte("0123456789abcdef")
	var requests atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Range", "bytes 4-15/99")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("wrong-body!"))
			return
		}
		_, _ = w.Write(body)
	})
	l, root := newLibrary(t)
	writePart(t, root, "Game.sfc", body[:4])

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

// L-05: a range that is not satisfiable over a complete part means the part is the file.
func TestInstallTreatsARangeNotSatisfiableOverACompletePartAsTheWholeFile(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	})
	l, root := newLibrary(t)
	writePart(t, root, "Game.sfc", body)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, SHA1: sha1Hex(body)},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part"))
}

// L-05: a range that is not satisfiable over an incomplete part keeps the pinned message.
func TestInstallDiscardsAnIncompletePartWhenTheRangeIsNotSatisfiable(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	})
	l, root := newLibrary(t)
	part := writePart(t, root, "Game.sfc", body[:4])

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)

	if err == nil || !strings.Contains(err.Error(), "partial file no longer matches the source, discarded; retry") {
		t.Fatalf("Install() error = %v, want the pinned range error", err)
	}
	assertAbsent(t, part)
}

// L-08: an arcade archive is the ROM and is installed without extraction.
func TestInstallKeepsAnArcadeArchiveAsTheROM(t *testing.T) {
	archive := zipArchive(t, []zipEntry{{name: "Game.sfc", body: "inside"}})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	arcade := rom.Platform{Slug: "arcade", Name: "Arcade", Extensions: []string{".zip", ".7z"}}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: arcade,
		File:     rom.File{Name: "pacman.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "arcade", "pacman.zip")
	if res.Path != want || res.Extracted {
		t.Errorf("result = %+v, want the archive installed as the ROM at %s", res, want)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, archive) {
		t.Errorf("installed bytes = %q, want the archive itself", got)
	}
	if files := installedFiles(t, root); len(files) != 1 || files[0] != want {
		t.Errorf("library files = %v, want only %s", files, want)
	}
}

// L-13: the part and the extraction directory survive only what a resume needs.
func TestInstallLeavesNoPartBehindWhenItFails(t *testing.T) {
	body := []byte("0123456789abcdef")
	tests := []struct {
		name     string
		keepPart bool
		run      func(t *testing.T, l *Library, root string) error
	}{
		{
			name: "a network error while the body streams",
			run: func(t *testing.T, l *Library, root string) error {
				srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(body[:4])
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						t.Error("the test server does not support hijacking")
						return
					}
					conn, _, err := hijacker.Hijack()
					if err == nil {
						_ = conn.Close()
					}
				})
				l.HTTPClient = srv.Client()
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
				}, nil)
				return err
			},
			keepPart: true,
		},
		{
			name: "a canceled download",
			run: func(t *testing.T, l *Library, root string) error {
				ctx, cancel := context.WithCancel(context.Background())
				srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Length", "100")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(body[:4])
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					<-r.Context().Done()
				})
				_, err := l.Install(ctx, rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
				}, func(downloaded, _ int64) {
					if downloaded > 0 {
						cancel()
					}
				})
				return err
			},
			keepPart: true,
		},
		{
			name: "a checksum mismatch",
			run: func(t *testing.T, l *Library, root string) error {
				srv := serve(t, servedBody(body))
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, MD5: strings.Repeat("0", 32)},
				}, nil)
				return err
			},
		},
		{
			name: "a size mismatch",
			run: func(t *testing.T, l *Library, root string) error {
				srv := serve(t, servedBody(body))
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", Size: 4321, URL: srv.URL},
				}, nil)
				return err
			},
		},
		{
			name: "an archive without a ROM",
			run: func(t *testing.T, l *Library, root string) error {
				archive := zipArchive(t, []zipEntry{{name: "notes.txt", body: "read me"}})
				srv := serve(t, servedBody(archive))
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Bundle.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
				}, nil)
				return err
			},
		},
		{
			name: "a status a retry cannot fix",
			run: func(t *testing.T, l *Library, root string) error {
				srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "gone", http.StatusNotFound)
				})
				_, err := l.Install(context.Background(), rom.InstallRequest{
					Platform: snesPlatform,
					File:     rom.File{Name: "Game.sfc", URL: srv.URL},
				}, nil)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, root := newLibrary(t)
			err := tt.run(t, l, root)
			if err == nil {
				t.Fatal("Install() error = nil, want the download to fail")
			}
			part := filepath.Join(root, "snes", "Game.sfc.part")
			if tt.keepPart {
				if _, statErr := os.Stat(part); statErr != nil {
					t.Errorf("the part was dropped: %v", statErr)
				}
				return
			}
			assertAbsent(t, part)
			assertNoExtractDir(t, root)
		})
	}
}

// L-14: a failed install keeps the download.
func TestInstallKeepsTheDownloadWhenTheInstallFails(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)
	dir := filepath.Join(root, "snes")
	if err := os.MkdirAll(filepath.Join(dir, "Game.sfc"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)

	if err == nil {
		t.Fatal("Install() error = nil, want the install to fail")
	}
	part := filepath.Join(dir, "Game.sfc.part")
	if got := readFile(t, part); !bytes.Equal(got, body) {
		t.Errorf("the download was lost: %s holds %q, want %q", part, got, body)
	}
}

// L-10: the checksum is computed while the bytes stream, not by reading the finished file again.
func TestInstallDoesNotReadTheFinishedDownloadAgain(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not restrict the superuser")
	}
	body := []byte("streamed-once")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)
	part := filepath.Join(root, "snes", "Game.sfc.part")

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, MD5: md5Hex(body)},
	}, func(downloaded, _ int64) {
		if downloaded == int64(len(body)) {
			_ = os.Chmod(part, 0o000)
		}
	})
	if err != nil {
		t.Fatalf("Install() error = %v, want the checksum computed while streaming", err)
	}
	if err := os.Chmod(res.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

// L-10: the checksum covers the bytes of the part and the bytes that follow.
func TestInstallVerifiesTheChecksumAcrossAResumedPart(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=4-" {
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Range", "bytes 4-15/16")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[4:])
	})
	l, root := newLibrary(t)
	writePart(t, root, "Game.sfc", body[:4])

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, SHA1: sha1Hex(body)},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

// L-24: the progress total is the whole file, from the content length and the offset.
func TestInstallReportsTheWholeFileAsTheTotal(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, servedBody(body))
	l, _ := newLibrary(t)

	var lastDownloaded, lastTotal int64
	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", URL: srv.URL},
	}, func(downloaded, total int64) {
		lastDownloaded, lastTotal = downloaded, total
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if lastDownloaded != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Errorf("last progress = (%d, %d), want (%d, %d)", lastDownloaded, lastTotal, len(body), len(body))
	}
}

// L-24: a resumed download reports the whole file as the total.
func TestInstallReportsTheWholeFileAsTheTotalWhenItResumes(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=4-" {
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Range", "bytes 4-15/16")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[4:])
	})
	l, root := newLibrary(t)
	writePart(t, root, "Game.sfc", body[:4])

	var lastDownloaded, lastTotal int64
	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", URL: srv.URL},
	}, func(downloaded, total int64) {
		lastDownloaded, lastTotal = downloaded, total
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if lastDownloaded != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Errorf("last progress = (%d, %d), want (%d, %d)", lastDownloaded, lastTotal, len(body), len(body))
	}
}

// breakingDownload serves body under validator but cuts the connection after
// cut bytes while the declared length stays complete.
func breakingDownload(body []byte, cut int, validator string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if validator != "" {
			w.Header().Set("ETag", validator)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body[:cut])
	}
}

// conditionalDownload serves body with validator and honors Range only when
// If-Range matches the validator, as net/http does.
func conditionalDownload(requests *[]http.Header, body []byte, validator string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Header.Clone())
		if validator != "" {
			w.Header().Set("ETag", validator)
		}
		ifRange := r.Header.Get("If-Range")
		if r.Header.Get("Range") == "" || (ifRange != "" && ifRange != validator) {
			_, _ = w.Write(body)
			return
		}
		var start int
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &start)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[start:])
	}
}

func installTwice(t *testing.T, first, second http.HandlerFunc, file rom.File) (rom.InstallResult, string, error) {
	t.Helper()
	var current atomic.Value
	current.Store(first)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { current.Load().(http.HandlerFunc)(w, r) })
	l, root := newLibrary(t)
	file.URL = srv.URL
	req := rom.InstallRequest{Platform: snesPlatform, File: file}
	if _, err := l.Install(context.Background(), req, nil); err == nil {
		t.Fatal("first Install() error = nil, want the cut download to fail")
	}
	current.Store(second)
	res, err := l.Install(context.Background(), req, nil)
	return res, root, err
}

// L-05: a resume is conditional on the validator the part was downloaded under.
func TestInstallResumesWithIfRangeWhenTheFileIsUnchanged(t *testing.T) {
	body := []byte("0123456789abcdef")
	var requests []http.Header

	res, root, err := installTwice(t,
		breakingDownload(body, 4, `"v1"`),
		conditionalDownload(&requests, body, `"v1"`),
		rom.File{Name: "Game.sfc", Size: int64(len(body))})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
	if len(requests) != 1 || requests[0].Get("Range") != "bytes=4-" || requests[0].Get("If-Range") != `"v1"` {
		t.Errorf("resume request headers = %v, want Range bytes=4- and If-Range \"v1\"", requests)
	}
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part"))
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part.meta"))
}

func TestInstallRestartsWhenTheFileChangedSinceThePartWasDownloaded(t *testing.T) {
	oldBody := []byte("0123456789abcdef")
	newBody := []byte("ABCDEFGHIJKLMNOP")
	var requests []http.Header

	res, root, err := installTwice(t,
		breakingDownload(oldBody, 4, `"v1"`),
		conditionalDownload(&requests, newBody, `"v2"`),
		rom.File{Name: "Game.sfc", Size: int64(len(newBody)), SHA1: sha1Hex(newBody)})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, newBody) {
		t.Errorf("installed bytes = %q, want the new file %q", got, newBody)
	}
	if len(requests) != 1 || requests[0].Get("If-Range") != `"v1"` {
		t.Errorf("resume request headers = %v, want If-Range \"v1\"", requests)
	}
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part.meta"))
}

func TestInstallSendsNoIfRangeWhenTheServerGaveNoValidator(t *testing.T) {
	body := []byte("0123456789abcdef")
	var requests []http.Header

	res, _, err := installTwice(t,
		breakingDownload(body, 4, ""),
		conditionalDownload(&requests, body, ""),
		rom.File{Name: "Game.sfc", Size: int64(len(body))})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, res.Path); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
	if len(requests) != 1 || requests[0].Get("Range") != "bytes=4-" || requests[0].Get("If-Range") != "" {
		t.Errorf("resume request headers = %v, want Range bytes=4- and no If-Range", requests)
	}
}

func TestInstallDiscardsTheValidatorWithThePart(t *testing.T) {
	body := []byte("0123456789abcdef")
	var requests []http.Header
	var current atomic.Value
	current.Store(breakingDownload(body, 4, `"v1"`))
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) { current.Load().(http.HandlerFunc)(w, r) })
	l, root := newLibrary(t)
	req := rom.InstallRequest{Platform: snesPlatform, File: rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, SHA1: sha1Hex([]byte("other"))}}
	if _, err := l.Install(context.Background(), req, nil); err == nil {
		t.Fatal("first Install() error = nil")
	}
	current.Store(conditionalDownload(&requests, body, `"v1"`))

	_, err := l.Install(context.Background(), req, nil)

	if !errors.Is(err, rom.ErrNotRetryable) {
		t.Fatalf("Install() error = %v, want a checksum failure marked not retryable", err)
	}
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part"))
	assertAbsent(t, filepath.Join(root, "snes", "Game.sfc.part.meta"))
}
