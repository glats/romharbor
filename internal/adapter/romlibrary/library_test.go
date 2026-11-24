package romlibrary

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/domain/rom"
)

var snesPlatform = rom.Platform{
	Slug:       "snes",
	Dir:        "snes",
	Name:       "Super Nintendo Entertainment System",
	Extensions: []string{".sfc", ".smc", ".gba"},
}

func TestConcurrentInstallsProtectStagingAndCancelWaiters(t *testing.T) {
	var requests atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) > 1 {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("verified ROM"))
	})
	l, root := newLibrary(t)
	digest := sha1.Sum([]byte("verified ROM"))
	req := rom.InstallRequest{Platform: snesPlatform, File: rom.File{
		Name: "Game.sfc", Size: 12, URL: srv.URL, SHA1: hex.EncodeToString(digest[:]),
	}}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := l.Install(context.Background(), req, func(int64, int64) {
			close(started)
			<-release
		})
		done <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := l.Install(ctx, req, nil)
	close(release)
	if firstErr := <-done; firstErr != nil {
		t.Fatal(firstErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
		t.Errorf("waiting install = %v; requests = %d, want canceled without touching staging", err, requests.Load())
	}
	result, err := l.Install(context.Background(), req, nil)
	if err != nil || !result.Skipped || requests.Load() != 1 {
		t.Fatalf("repeat install = %+v, %v", result, err)
	}
	if got := string(readFile(t, filepath.Join(root, "snes", "Game.sfc"))); got != "verified ROM" {
		t.Errorf("installed bytes = %q", got)
	}
	assertAbsent(t, l.abs(result.Path)+".part")
}

type zipEntry struct {
	name string
	body string
}

func newLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	root := t.TempDir()
	return &Library{Root: root}, root
}

func serve(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func servedBody(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}
}

func zipArchive(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		f, err := w.Create(e.name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", e.name, err)
		}
		if _, err := f.Write([]byte(e.body)); err != nil {
			t.Fatalf("write zip entry %s: %v", e.name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s: expected absent, stat error = %v", path, err)
	}
}

func installedFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

func TestInstallWritesPlainROM(t *testing.T) {
	body := []byte("0123456789abcdef")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)

	var lastDownloaded, lastTotal int64
	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Chrono Trigger (USA).sfc", Size: int64(len(body)), URL: srv.URL},
	}, func(downloaded, total int64) {
		lastDownloaded, lastTotal = downloaded, total
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "snes", "Chrono Trigger (USA).sfc")
	if res.Path != "snes/Chrono Trigger (USA).sfc" {
		t.Errorf("Path = %q, want the path relative to the library root", res.Path)
	}
	if res.Size != int64(len(body)) || res.Skipped || res.Extracted {
		t.Errorf("result = %+v, want size %d, not skipped, not extracted", res, len(body))
	}
	if got := readFile(t, want); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
	if lastDownloaded != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Errorf("last progress = (%d, %d), want (%d, %d)", lastDownloaded, lastTotal, len(body), len(body))
	}
	assertAbsent(t, want+".part")
}

func TestInstallResumesPartFile(t *testing.T) {
	body := []byte("0123456789abcdef")
	ranges := make(chan string, 1)
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case ranges <- r.Header.Get("Range"):
		default:
		}
		if r.Header.Get("Range") != "bytes=4-" {
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Range", "bytes 4-15/16")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[4:])
	})
	l, root := newLibrary(t)
	dir := filepath.Join(root, "snes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Game.sfc.part"), body[:4], 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	select {
	case got := <-ranges:
		if got != "bytes=4-" {
			t.Errorf("Range header = %q, want %q", got, "bytes=4-")
		}
	default:
		t.Fatal("server was not contacted")
	}
	if got := readFile(t, l.abs(res.Path)); !bytes.Equal(got, body) {
		t.Errorf("resumed bytes = %q, want %q", got, body)
	}
	assertAbsent(t, filepath.Join(dir, "Game.sfc.part"))
}

func TestInstallChecksum(t *testing.T) {
	body := []byte("checksum-me")
	md5Sum := md5.Sum(body)
	sha1Sum := sha1.Sum(body)
	zeroMD5 := strings.Repeat("0", 32)

	tests := []struct {
		name    string
		md5     string
		sha1    string
		wantErr bool
	}{
		{name: "upper-case MD5 matches", md5: strings.ToUpper(hex.EncodeToString(md5Sum[:]))},
		{name: "SHA1 matches", sha1: hex.EncodeToString(sha1Sum[:])},
		{name: "MD5 mismatch installs nothing", md5: zeroMD5, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, servedBody(body))
			l, root := newLibrary(t)
			file := rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL, MD5: tt.md5, SHA1: tt.sha1}

			res, err := l.Install(context.Background(), rom.InstallRequest{Platform: snesPlatform, File: file}, nil)
			target := filepath.Join(root, "snes", "Game.sfc")
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Install() error = %v", err)
				}
				if got := readFile(t, l.abs(res.Path)); !bytes.Equal(got, body) {
					t.Errorf("installed bytes = %q, want %q", got, body)
				}
				return
			}
			if err == nil {
				t.Fatal("Install() error = nil, want checksum mismatch")
			}
			actual := hex.EncodeToString(md5Sum[:])
			if !strings.Contains(err.Error(), zeroMD5) || !strings.Contains(err.Error(), actual) {
				t.Errorf("error %q does not name expected %s and actual %s", err, zeroMD5, actual)
			}
			assertAbsent(t, target)
			assertAbsent(t, target+".part")
		})
	}
}

func TestInstallNeutralizesTraversal(t *testing.T) {
	body := []byte("passwd-rom")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "../../etc/passwd", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "snes", "passwd.sfc")
	if l.abs(res.Path) != want {
		t.Errorf("Path = %q, want %q", res.Path, want)
	}
	if !strings.HasPrefix(l.abs(res.Path), root+string(filepath.Separator)) {
		t.Errorf("Path %q escapes library root %q", l.abs(res.Path), root)
	}
	if got := readFile(t, want); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

func TestInstallSkipsPresentROM(t *testing.T) {
	var hits atomic.Int32
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("0123456789"))
	})
	l, root := newLibrary(t)
	dir := filepath.Join(root, "snes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "Game.sfc")
	local := []byte("abcdefghij")
	if err := os.WriteFile(target, local, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(local)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !res.Skipped || res.Path != "snes/Game.sfc" || l.abs(res.Path) != target {
		t.Errorf("result = %+v, want skipped at %s", res, target)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0", n)
	}
	if got := readFile(t, target); !bytes.Equal(got, local) {
		t.Errorf("existing ROM changed to %q", got)
	}
}

func TestInstallExtractsZipROM(t *testing.T) {
	archive := zipArchive(t, []zipEntry{
		{name: "notes.txt", body: "read me"},
		{name: "disc/alt.sfc", body: "alternate"},
		{name: "Game.sfc", body: "game-data"},
	})
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Bundle.zip", Size: int64(len(archive)), URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "snes", "Bundle.sfc")
	if l.abs(res.Path) != want || !res.Extracted {
		t.Errorf("result = %+v, want extracted ROM at %s", res, want)
	}
	if got := readFile(t, want); string(got) != "game-data" {
		t.Errorf("installed bytes = %q, want the root-level ROM", got)
	}
	files := installedFiles(t, root)
	if len(files) != 1 || files[0] != want {
		t.Errorf("library files = %v, want only %s", files, want)
	}
}

func TestInstallExtractsSevenZipROM(t *testing.T) {
	archive := readFile(t, filepath.Join("testdata", "bundle.7z"))
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Bundle.7z", URL: srv.URL, Archive: true},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "snes", "Bundle.gba")
	if l.abs(res.Path) != want || !res.Extracted {
		t.Errorf("result = %+v, want extracted ROM at %s", res, want)
	}
	if got := readFile(t, want); string(got) != "synthetic-rom" {
		t.Errorf("installed bytes = %q", got)
	}
	files := installedFiles(t, root)
	if len(files) != 1 {
		t.Errorf("library files = %v, want only the ROM", files)
	}
}

func TestInstallFailureInstallsNothing(t *testing.T) {
	tests := []struct {
		name    string
		cancel  bool
		file    func(url string) rom.File
		wantErr string
	}{
		{
			name:    "server error",
			file:    func(url string) rom.File { return rom.File{Name: "Game.sfc", URL: url} },
			wantErr: "500",
		},
		{
			name:    "canceled context",
			cancel:  true,
			file:    func(url string) rom.File { return rom.File{Name: "Game.sfc", URL: url} },
			wantErr: "context canceled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			})
			l, root := newLibrary(t)
			ctx := context.Background()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			_, err := l.Install(ctx, rom.InstallRequest{Platform: snesPlatform, File: tt.file(srv.URL)}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Install() error = %v, want one containing %q", err, tt.wantErr)
			}
			if files := installedFiles(t, root); len(files) != 0 {
				t.Errorf("library files = %v, want none", files)
			}
		})
	}
}

func TestInstallRejectsEscapingPlatformDir(t *testing.T) {
	l, root := newLibrary(t)
	escaping := rom.Platform{Slug: "snes", Dir: "../escape", Extensions: []string{".sfc"}}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: escaping,
		File:     rom.File{Name: "Game.sfc", URL: "http://unused.invalid/game"},
	}, nil)
	if err == nil {
		t.Fatal("Install() error = nil, want escape rejected")
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
}

func TestInstallGenesisUsesMegadriveFolder(t *testing.T) {
	body := []byte("md-rom")
	srv := serve(t, servedBody(body))
	l, root := newLibrary(t)
	genesis := rom.Platform{Slug: "genesis", Dir: "megadrive", Extensions: []string{".md"}}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: genesis,
		File:     rom.File{Name: "Sonic.md", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	want := filepath.Join(root, "megadrive", "Sonic.md")
	if l.abs(res.Path) != want {
		t.Errorf("Path = %q, want %q", res.Path, want)
	}
	if got := readFile(t, want); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

func TestInstallRejectsNonHTTPURLsBeforeAnyRequest(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "ftp://example.test/Game.sfc", "gopher://example.test/Game.sfc"} {
		t.Run(target, func(t *testing.T) {
			l, root := newLibrary(t)
			var calls atomic.Int32
			l.HTTPClient = doerFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, errors.New("must not be reached")
			})

			_, err := l.Install(context.Background(), rom.InstallRequest{
				Platform: snesPlatform,
				File:     rom.File{Name: "Game.sfc", URL: target},
			}, nil)

			if !errors.Is(err, romfetch.ErrUnsupportedScheme) {
				t.Fatalf("Install() error = %v, want an unsupported scheme error", err)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("client calls = %d, want 0", n)
			}
			if files := installedFiles(t, root); len(files) != 0 {
				t.Errorf("library files = %v, want none", files)
			}
		})
	}
}

func TestInstallUsesTheDefaultFetcherWhenNoClientIsSet(t *testing.T) {
	body := []byte("default-client-rom")
	srv := serve(t, servedBody(body))
	root := t.TempDir()
	l := &Library{Root: root}

	res, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", Size: int64(len(body)), URL: srv.URL},
	}, nil)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, l.abs(res.Path)); !bytes.Equal(got, body) {
		t.Errorf("installed bytes = %q, want %q", got, body)
	}
}

func TestInstallAcceptsAStandardHTTPClient(t *testing.T) {
	srv := serve(t, servedBody([]byte("std-client-rom")))
	l := &Library{Root: t.TempDir(), HTTPClient: srv.Client()}

	if _, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Game.sfc", URL: srv.URL},
	}, nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstallRejectsAnArchiveOverTheExtractionCap(t *testing.T) {
	archive := readFile(t, filepath.Join("testdata", "bundle.7z"))
	srv := serve(t, servedBody(archive))
	l, root := newLibrary(t)
	l.limits = extractLimits{entries: defaultExtractLimits.entries, bytes: 4}

	_, err := l.Install(context.Background(), rom.InstallRequest{
		Platform: snesPlatform,
		File:     rom.File{Name: "Bundle.7z", URL: srv.URL, Archive: true},
	}, nil)

	if !errors.Is(err, errExtractTooLarge) {
		t.Fatalf("Install() error = %v, want errExtractTooLarge", err)
	}
	if files := installedFiles(t, root); len(files) != 0 {
		t.Errorf("library files = %v, want none", files)
	}
}
