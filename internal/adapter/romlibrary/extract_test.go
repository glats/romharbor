package romlibrary

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app.ddd/internal/domain/rom"
)

var gbaExts = []string{".gba"}

type namedEntry struct {
	name string
	body string
	mode os.FileMode
}

func writeZip(t *testing.T, entries []namedEntry) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", e.name, err)
		}
		if !strings.HasSuffix(e.name, "/") {
			if _, err := f.Write([]byte(e.body)); err != nil {
				t.Fatalf("write zip entry %s: %v", e.name, err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	path := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixture(name string) string { return filepath.Join("testdata", name) }

func extractFrom(t *testing.T, ctx context.Context, limits extractLimits, ext, archive string) (string, string, string, error) {
	t.Helper()
	workDir := t.TempDir()
	romPath, romExt, err := extractROM(ctx, limits, ext, archive, workDir, gbaExts)
	return workDir, romPath, romExt, err
}

func TestExtractPicksTheRegularFileFromBothFormats(t *testing.T) {
	tests := []struct {
		name    string
		ext     string
		archive func(t *testing.T) string
		want    string
	}{
		{
			name: "zip",
			ext:  ".zip",
			archive: func(t *testing.T) string {
				return writeZip(t, []namedEntry{{name: "notes.txt", body: "n"}, {name: "extras/Game.gba", body: "synthetic-rom"}})
			},
			want: "synthetic-rom",
		},
		{name: "7z", ext: ".7z", archive: func(*testing.T) string { return fixture("bundle.7z") }, want: "synthetic-rom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir, romPath, romExt, err := extractFrom(t, context.Background(), defaultExtractLimits, tt.ext, tt.archive(t))
			if err != nil {
				t.Fatalf("extractROM() error = %v", err)
			}
			if romExt != ".gba" || romPath != filepath.Join(workDir, "rom.gba") {
				t.Errorf("extractROM() = %q, %q; want the fixed name rom.gba in the work dir", romPath, romExt)
			}
			if got := readFile(t, romPath); string(got) != tt.want {
				t.Errorf("extracted bytes = %q, want %q", got, tt.want)
			}
			if files := installedFiles(t, workDir); len(files) != 1 {
				t.Errorf("work dir files = %v, want only the ROM", files)
			}
		})
	}
}

func TestExtractIgnoresEverythingButRegularFiles(t *testing.T) {
	tests := []struct {
		name    string
		ext     string
		archive func(t *testing.T) string
		want    string
	}{
		{
			name: "zip symlink and directory entries",
			ext:  ".zip",
			archive: func(t *testing.T) string {
				return writeZip(t, []namedEntry{
					{name: "roms/", mode: os.ModeDir | 0o755},
					{name: "Link.gba", body: "/etc/passwd", mode: os.ModeSymlink | 0o777},
					{name: "Real.gba", body: "synthetic-rom"},
				})
			},
			want: "synthetic-rom",
		},
		{name: "7z symlink entry", ext: ".7z", archive: func(*testing.T) string { return fixture("symlink.7z") }, want: "synthetic-rom"},
		{name: "7z directory entries", ext: ".7z", archive: func(*testing.T) string { return fixture("directory.7z") }, want: "synthetic-rom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir, romPath, _, err := extractFrom(t, context.Background(), defaultExtractLimits, tt.ext, tt.archive(t))
			if err != nil {
				t.Fatalf("extractROM() error = %v", err)
			}
			if got := readFile(t, romPath); string(got) != tt.want {
				t.Errorf("extracted bytes = %q, want %q", got, tt.want)
			}
			if files := installedFiles(t, workDir); len(files) != 1 {
				t.Errorf("work dir files = %v, want only the ROM", files)
			}
		})
	}
}

func TestExtractReportsNoROMWhenOnlyLinksMatch(t *testing.T) {
	archive := writeZip(t, []namedEntry{{name: "Link.gba", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}})

	workDir, _, _, err := extractFrom(t, context.Background(), defaultExtractLimits, ".zip", archive)

	want := rom.NoROMError(gbaExts)
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("extractROM() error = %v, want %v", err, want)
	}
	if files := installedFiles(t, workDir); len(files) != 0 {
		t.Errorf("work dir files = %v, want none", files)
	}
}

func TestExtractRejectsTooManyEntries(t *testing.T) {
	var entries []namedEntry
	for i := range 6 {
		entries = append(entries, namedEntry{name: fmt.Sprintf("f%d.txt", i), body: "x"})
	}
	entries = append(entries, namedEntry{name: "Game.gba", body: "synthetic-rom"})
	limits := extractLimits{entries: 6, bytes: defaultExtractLimits.bytes}
	tests := []struct {
		name    string
		ext     string
		archive string
	}{
		{name: "zip", ext: ".zip", archive: writeZip(t, entries)},
		{name: "7z", ext: ".7z", archive: fixture("many.7z")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir, _, _, err := extractFrom(t, context.Background(), limits, tt.ext, tt.archive)

			if !errors.Is(err, errTooManyEntries) {
				t.Fatalf("extractROM() error = %v, want errTooManyEntries", err)
			}
			if !strings.Contains(err.Error(), "6") {
				t.Errorf("error %q does not state the cap", err)
			}
			if files := installedFiles(t, workDir); len(files) != 0 {
				t.Errorf("work dir files = %v, want none", files)
			}
		})
	}
}

func TestExtractAcceptsAnArchiveAtTheEntryCap(t *testing.T) {
	limits := extractLimits{entries: 7, bytes: defaultExtractLimits.bytes}

	_, _, _, err := extractFrom(t, context.Background(), limits, ".7z", fixture("many.7z"))
	if err != nil {
		t.Fatalf("extractROM() error = %v, want an archive with exactly the cap accepted", err)
	}
}

func TestExtractRejectsAnEntryOverTheByteCap(t *testing.T) {
	limits := extractLimits{entries: defaultExtractLimits.entries, bytes: 12}
	tests := []struct {
		name    string
		ext     string
		archive string
	}{
		{name: "zip", ext: ".zip", archive: writeZip(t, []namedEntry{{name: "Game.gba", body: "synthetic-rom"}})},
		{name: "7z", ext: ".7z", archive: fixture("bundle.7z")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir, romPath, _, err := extractFrom(t, context.Background(), limits, tt.ext, tt.archive)

			if !errors.Is(err, errExtractTooLarge) {
				t.Fatalf("extractROM() error = %v, want errExtractTooLarge", err)
			}
			if romPath != "" {
				t.Errorf("romPath = %q, want empty on failure", romPath)
			}
			if files := installedFiles(t, workDir); len(files) != 0 {
				t.Errorf("work dir files = %v, want the partial output removed", files)
			}
		})
	}
}

func TestCopyCappedStopsAtTheCapWhateverTheDeclaredSize(t *testing.T) {
	var dst bytes.Buffer

	n, err := copyCapped(context.Background(), &dst, strings.NewReader(strings.Repeat("a", 100)), 40)

	if !errors.Is(err, errExtractTooLarge) {
		t.Fatalf("copyCapped() error = %v, want errExtractTooLarge", err)
	}
	if n > 40 || int64(dst.Len()) > 40 {
		t.Errorf("copyCapped() wrote %d (buffer %d) bytes, want at most the 40-byte cap", n, dst.Len())
	}
}

func TestCopyCappedCopiesExactlyTheCap(t *testing.T) {
	var dst bytes.Buffer

	n, err := copyCapped(context.Background(), &dst, strings.NewReader(strings.Repeat("a", 40)), 40)

	if err != nil || n != 40 || dst.Len() != 40 {
		t.Fatalf("copyCapped() = %d, %v (buffer %d), want 40 bytes and no error", n, err, dst.Len())
	}
}

type endlessReader struct {
	onRead func()
	reads  int
}

func (r *endlessReader) Read(p []byte) (int, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead()
	}
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestCopyCappedStopsBetweenChunksWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &endlessReader{onRead: func() { cancel() }}

	_, err := copyCapped(ctx, io.Discard, src, 1<<40)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyCapped() error = %v, want context.Canceled", err)
	}
	if src.reads > 2 {
		t.Errorf("reads after cancellation = %d, want the copy interrupted at the next chunk", src.reads)
	}
}

func TestExtractIsCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		ext     string
		archive string
	}{
		{name: "zip", ext: ".zip", archive: writeZip(t, []namedEntry{{name: "Game.gba", body: "synthetic-rom"}})},
		{name: "7z", ext: ".7z", archive: fixture("bundle.7z")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir, _, _, err := extractFrom(t, ctx, defaultExtractLimits, tt.ext, tt.archive)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("extractROM() error = %v, want context.Canceled", err)
			}
			if files := installedFiles(t, workDir); len(files) != 0 {
				t.Errorf("work dir files = %v, want none", files)
			}
		})
	}
}

func TestExtractRejectsCorruptAndUnsupportedArchives(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "bad.7z")
	if err := os.WriteFile(garbage, []byte("not a 7z archive at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := extractFrom(t, context.Background(), defaultExtractLimits, ".7z", garbage); err == nil {
		t.Error("extractROM() accepted a corrupt .7z archive")
	}
	_, _, _, err := extractFrom(t, context.Background(), defaultExtractLimits, ".rar", garbage)
	if err == nil || !strings.Contains(err.Error(), "unsupported archive format") {
		t.Errorf("extractROM() error = %v, want unsupported archive format", err)
	}
}

func TestDefaultExtractLimits(t *testing.T) {
	if defaultExtractLimits.entries != 10000 || defaultExtractLimits.bytes != 8<<30 {
		t.Errorf("defaultExtractLimits = %+v, want 10000 entries and 8 GiB", defaultExtractLimits)
	}
}
