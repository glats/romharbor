package romlibrary

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"app.ddd/internal/domain/rom"
)

type Library struct {
	installOnce  sync.Once
	installGate  chan struct{}
	Root         string
	HTTPClient   *http.Client
	SevenZipPath string
}

func (l *Library) PlatformDir(p rom.Platform) (string, error) {
	name := p.LibraryFolder()
	if name == "" {
		return "", errors.New("romcatalog: platform has no library folder")
	}
	return filepath.Join(l.Root, name), nil
}

func (l *Library) Install(ctx context.Context, req rom.InstallRequest, progress func(downloaded, total int64)) (rom.InstallResult, error) {
	l.installOnce.Do(func() { l.installGate = make(chan struct{}, 1) })
	select {
	case l.installGate <- struct{}{}:
		defer func() { <-l.installGate }()
	case <-ctx.Done():
		return rom.InstallResult{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return rom.InstallResult{}, err
	}
	if l.Root == "" {
		return rom.InstallResult{}, errors.New("romcatalog: library root is not set")
	}
	if req.File.URL == "" {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: %s has no download URL", req.File.Name)
	}
	base, ext, err := rom.SplitFileName(req.File.Name)
	if err != nil {
		return rom.InstallResult{}, err
	}
	archive := rom.ArchiveExtension(ext)
	if req.File.Archive && !archive {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: unsupported archive format for %s", req.File.Name)
	}
	if !archive && ext == "" {
		if len(req.Platform.Extensions) == 0 {
			return rom.InstallResult{}, fmt.Errorf("romcatalog: %s has no extension to install under", req.File.Name)
		}
		ext = req.Platform.Extensions[0]
	}
	dir, err := l.PlatformDir(req.Platform)
	if err != nil {
		return rom.InstallResult{}, err
	}
	target, err := l.targetPath(dir, base, ext)
	if err != nil {
		return rom.InstallResult{}, err
	}
	if archive {
		if err := requireExtractor(ext, l.sevenZipBinary()); err != nil {
			return rom.InstallResult{}, err
		}
	} else if skipPresent(target, req.File.Size) {
		return rom.InstallResult{Path: target, Size: req.File.Size, Skipped: true}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: create %s: %w", dir, err)
	}
	part := target + ".part"
	if progress == nil {
		progress = func(int64, int64) {}
	}
	if err := l.fetch(ctx, req.File, part, progress); err != nil {
		return rom.InstallResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return rom.InstallResult{}, err
	}
	if err := verifyChecksum(part, req.File); err != nil {
		_ = os.Remove(part)
		return rom.InstallResult{}, err
	}
	if archive {
		return l.installArchive(ctx, dir, base, ext, part, req.Platform.Extensions)
	}
	if err := install(part, tempPath(dir, filepath.Base(target)), target); err != nil {
		return rom.InstallResult{}, err
	}
	return installedResult(target, false)
}

func (l *Library) installArchive(ctx context.Context, dir, base, archiveExt, archivePath string, exts []string) (rom.InstallResult, error) {
	workDir, err := os.MkdirTemp(dir, ".extract-")
	if err != nil {
		_ = os.Remove(archivePath)
		return rom.InstallResult{}, fmt.Errorf("romcatalog: create extraction directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	romPath, romExt, err := extractROM(ctx, l.sevenZipBinary(), archiveExt, archivePath, workDir, exts)
	if err != nil {
		_ = os.Remove(archivePath)
		return rom.InstallResult{}, err
	}
	target, err := l.targetPath(dir, base, romExt)
	if err != nil {
		return rom.InstallResult{}, err
	}
	if err := install(romPath, tempPath(dir, base+romExt), target); err != nil {
		return rom.InstallResult{}, err
	}
	_ = os.Remove(archivePath)
	return installedResult(target, true)
}

func (l *Library) targetPath(dir, base, ext string) (string, error) {
	root := filepath.Clean(l.Root)
	target := filepath.Join(dir, base+ext)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", fmt.Errorf("romcatalog: target %q escapes library root %q", target, root)
	}
	return target, nil
}

func (l *Library) client() *http.Client {
	if l.HTTPClient != nil {
		return l.HTTPClient
	}
	return http.DefaultClient
}

func (l *Library) sevenZipBinary() string {
	if l.SevenZipPath != "" {
		return l.SevenZipPath
	}
	return "7z"
}

func tempPath(dir, name string) string {
	return filepath.Join(dir, "."+name+".tmp")
}

func skipPresent(target string, size int64) bool {
	if size <= 0 {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && info.Mode().IsRegular() && info.Size() == size
}

func installedResult(path string, extracted bool) (rom.InstallResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: stat installed ROM: %w", err)
	}
	return rom.InstallResult{Path: path, Size: info.Size(), Extracted: extracted}, nil
}

func (l *Library) fetch(ctx context.Context, file rom.File, part string, progress func(downloaded, total int64)) error {
	var offset int64
	if info, err := os.Stat(part); err == nil {
		offset = info.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URL, nil)
	if err != nil {
		return fmt.Errorf("romcatalog: build request for %s: %w", file.Name, err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return fmt.Errorf("romcatalog: download %s: %w", file.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	resuming := offset > 0 && resp.StatusCode == http.StatusPartialContent
	if !resuming && resp.StatusCode != http.StatusOK {
		if offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			_ = os.Remove(part)
			return fmt.Errorf("romcatalog: download %s: partial file no longer matches the source, discarded; retry", file.Name)
		}
		return fmt.Errorf("romcatalog: download %s: unexpected HTTP status %s", file.Name, resp.Status)
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	var done int64
	if resuming {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		done = offset
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fmt.Errorf("romcatalog: open %s: %w", part, err)
	}
	w := &progressWriter{w: out, downloaded: done, total: max(file.Size, 0), report: progress}
	_, copyErr := io.Copy(w, resp.Body)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("romcatalog: download %s: %w", file.Name, copyErr)
	}
	return nil
}

type progressWriter struct {
	w          io.Writer
	downloaded int64
	total      int64
	report     func(downloaded, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.downloaded += int64(n)
	p.report(p.downloaded, p.total)
	return n, err
}

func verifyChecksum(path string, file rom.File) error {
	if file.MD5 != "" {
		if err := checkDigest(path, md5.New(), "md5", file.MD5); err != nil {
			return err
		}
	}
	if file.SHA1 != "" {
		if err := checkDigest(path, sha1.New(), "sha1", file.SHA1); err != nil {
			return err
		}
	}
	if file.SHA256 != "" {
		return checkDigest(path, sha256.New(), "sha256", file.SHA256)
	}
	return nil
}

func checkDigest(path string, h hash.Hash, algorithm, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("romcatalog: open %s for checksum: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("romcatalog: checksum %s: %w", path, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("romcatalog: %s checksum mismatch: expected %s, got %s", algorithm, expected, actual)
	}
	return nil
}

func install(src, tmp, target string) error {
	if err := os.Rename(src, tmp); err != nil {
		return fmt.Errorf("romcatalog: stage %s: %w", target, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("romcatalog: install %s: %w", target, err)
	}
	return nil
}
