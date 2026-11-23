package romlibrary

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bodgit/sevenzip"

	"app.ddd/internal/domain/rom"
)

const (
	maxArchiveEntries = 10000
	maxExtractedBytes = 8 << 30
	copyChunkSize     = 32 * 1024
)

var (
	errTooManyEntries  = errors.New("archive entry cap exceeded")
	errExtractTooLarge = errors.New("extraction size cap exceeded")
)

type extractLimits struct {
	entries int
	bytes   int64
}

var defaultExtractLimits = extractLimits{entries: maxArchiveEntries, bytes: maxExtractedBytes}

type archiveEntry struct {
	name string
	size uint64
	open func() (io.ReadCloser, error)
}

// extractROM unpacks the ROM an archive contains into workDir under a fixed
// name and returns its path together with the ROM's extension. Only regular
// files are considered, and the archive is held to limits.
func extractROM(ctx context.Context, limits extractLimits, archiveExt, archivePath, workDir string, exts []string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	entries, closeArchive, err := openArchive(limits, archiveExt, archivePath)
	if err != nil {
		return "", "", err
	}
	defer closeArchive()
	return extractEntry(ctx, limits, entries, workDir, exts)
}

// openArchive opens an archive and returns the regular files it holds. The
// returned function closes the archive.
func openArchive(limits extractLimits, archiveExt, archivePath string) ([]archiveEntry, func(), error) {
	switch strings.ToLower(archiveExt) {
	case ".zip":
		return openZip(limits, archivePath)
	case ".7z":
		return open7z(limits, archivePath)
	}
	return nil, func() {}, notRetryable(fmt.Errorf("romcatalog: unsupported archive format %q", archiveExt))
}

func openZip(limits extractLimits, archivePath string) ([]archiveEntry, func(), error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, nil, fmt.Errorf("romcatalog: open zip archive: %w", err)
	}
	if err := checkEntryCount(len(r.File), limits); err != nil {
		_ = r.Close()
		return nil, nil, err
	}
	var entries []archiveEntry
	for _, f := range r.File {
		if f.Mode().IsRegular() {
			entries = append(entries, archiveEntry{name: f.Name, size: f.UncompressedSize64, open: f.Open})
		}
	}
	return entries, func() { _ = r.Close() }, nil
}

func open7z(limits extractLimits, archivePath string) ([]archiveEntry, func(), error) {
	r, err := sevenzip.OpenReader(archivePath)
	if err != nil {
		return nil, nil, fmt.Errorf("romcatalog: open 7z archive: %w", err)
	}
	if err := checkEntryCount(len(r.File), limits); err != nil {
		_ = r.Close()
		return nil, nil, err
	}
	var entries []archiveEntry
	for _, f := range r.File {
		if f.Mode().IsRegular() {
			entries = append(entries, archiveEntry{name: f.Name, size: f.UncompressedSize, open: f.Open})
		}
	}
	return entries, func() { _ = r.Close() }, nil
}

func checkEntryCount(n int, limits extractLimits) error {
	if n > limits.entries {
		return notRetryable(fmt.Errorf("romcatalog: archive holds more than %d entries: %w", limits.entries, errTooManyEntries))
	}
	return nil
}

func extractEntry(ctx context.Context, limits extractLimits, entries []archiveEntry, workDir string, exts []string) (string, string, error) {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	i, ok := rom.PickROM(names, exts)
	if !ok {
		return "", "", notRetryable(rom.NoROMError(exts))
	}
	entry := entries[i]
	if entry.size > uint64(limits.bytes) {
		return "", "", notRetryable(fmt.Errorf("romcatalog: extract %s: larger than the %d byte cap: %w", entry.name, limits.bytes, errExtractTooLarge))
	}
	ext := path.Ext(entry.name)
	romPath := filepath.Join(workDir, "rom"+ext)
	if _, err := writeEntry(ctx, entry, romPath, limits.bytes); err != nil {
		_ = os.Remove(romPath)
		return "", "", err
	}
	return romPath, ext, nil
}

func writeEntry(ctx context.Context, entry archiveEntry, dst string, limit int64) (int64, error) {
	rc, err := entry.open()
	if err != nil {
		return 0, fmt.Errorf("romcatalog: open %s in archive: %w", entry.name, err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("romcatalog: create %s: %w", dst, err)
	}
	written, copyErr := copyCapped(ctx, out, rc, limit)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return written, fmt.Errorf("romcatalog: extract %s: %w", entry.name, copyErr)
	}
	return written, nil
}

// copyCapped copies src to dst in chunks, checking ctx between chunks and
// failing with errExtractTooLarge before more than limit bytes are written.
func copyCapped(ctx context.Context, dst io.Writer, src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, copyChunkSize)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			if written+int64(n) > limit {
				return written, errExtractTooLarge
			}
			w, err := dst.Write(buf[:n])
			written += int64(w)
			if err != nil {
				return written, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}
