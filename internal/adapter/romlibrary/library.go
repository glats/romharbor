package romlibrary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"app.ddd/internal/adapter/romfetch"
	"app.ddd/internal/domain/rom"
)

// The HTTP statuses a retry cannot fix.
const (
	statusBadRequest   = 400
	statusUnauthorized = 401
	statusForbidden    = 403
	statusNotFound     = 404
	statusGone         = 410
)

// errRestart asks for the download to start again from its first byte.
var errRestart = errors.New("restart the download")

type Library struct {
	installOnce sync.Once
	installGate chan struct{}
	fetcher     *romfetch.Fetcher
	limits      extractLimits
	Root        string
	HTTPClient  romfetch.Doer
}

func (l *Library) PlatformDir(p rom.Platform) (string, error) {
	name := p.LibraryFolder()
	if name == "" {
		return "", errors.New("romcatalog: platform has no library folder")
	}
	return filepath.Join(l.Root, name), nil
}

func (l *Library) Install(ctx context.Context, req rom.InstallRequest, progress func(downloaded, total int64)) (rom.InstallResult, error) {
	l.installOnce.Do(func() {
		l.installGate = make(chan struct{}, 1)
		l.fetcher = romfetch.New(l.HTTPClient)
		if l.limits == (extractLimits{}) {
			l.limits = defaultExtractLimits
		}
	})
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
		return rom.InstallResult{}, notRetryable(fmt.Errorf("romcatalog: unsupported archive format for %s", req.File.Name))
	}
	if archive && req.Platform.Accepts(ext) {
		// The platform lists the archive extension as a ROM extension: the
		// archive is the ROM itself and is installed without extraction.
		archive = false
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
	if !archive && skipPresent(target, req.File.Size) {
		return rom.InstallResult{Path: target, Size: req.File.Size, Skipped: true}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: create %s: %w", dir, err)
	}
	part := target + ".part"
	if progress == nil {
		progress = func(int64, int64) {}
	}
	sums, err := l.fetch(ctx, req.File, part, progress)
	if err != nil {
		if errors.Is(err, rom.ErrNotRetryable) {
			discard(part)
		}
		return rom.InstallResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return rom.InstallResult{}, err
	}
	if err := checkSize(part, req.File); err != nil {
		discard(part)
		return rom.InstallResult{}, err
	}
	if err := sums.verify(); err != nil {
		discard(part)
		return rom.InstallResult{}, err
	}
	if archive {
		return l.installArchive(ctx, dir, base, ext, part, req.Platform.Extensions)
	}
	if err := install(part, tempPath(dir, filepath.Base(target)), target, dir); err != nil {
		return rom.InstallResult{}, err
	}
	discard(part)
	return installedResult(target, false)
}

func (l *Library) installArchive(ctx context.Context, dir, base, archiveExt, archivePath string, exts []string) (rom.InstallResult, error) {
	workDir, err := os.MkdirTemp(dir, ".extract-")
	if err != nil {
		discard(archivePath)
		return rom.InstallResult{}, fmt.Errorf("romcatalog: create extraction directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	if err := ctx.Err(); err != nil {
		return rom.InstallResult{}, err
	}
	entries, closeArchive, err := openArchive(l.limits, archiveExt, archivePath)
	if err != nil {
		if errors.Is(err, rom.ErrNotRetryable) {
			discard(archivePath)
		}
		return rom.InstallResult{}, err
	}
	defer closeArchive()

	if index, ok := findIndex(entries, exts); ok {
		return l.installSet(ctx, dir, base, index, entries, workDir, archivePath)
	}
	romPath, romExt, err := extractEntry(ctx, l.limits, entries, workDir, exts)
	if err != nil {
		if errors.Is(err, rom.ErrNotRetryable) {
			discard(archivePath)
		}
		return rom.InstallResult{}, err
	}
	target, err := l.targetPath(dir, base, romExt)
	if err != nil {
		return rom.InstallResult{}, err
	}
	if err := install(romPath, tempPath(dir, base+romExt), target, dir); err != nil {
		return rom.InstallResult{}, err
	}
	discard(archivePath)
	return installedResult(target, true)
}

// installSet installs the index of a set and every piece it references into one
// folder of the library.
func (l *Library) installSet(ctx context.Context, dir, base string, index archiveEntry, entries []archiveEntry, workDir, archivePath string) (rom.InstallResult, error) {
	folder, err := l.targetPath(dir, base, "")
	if err != nil {
		return rom.InstallResult{}, err
	}
	switch info, statErr := os.Stat(folder); {
	case statErr == nil && info.IsDir():
		if setPresent(folder, path.Base(index.name)) {
			discard(archivePath)
			return installedSetResult(folder, true, true)
		}
		discard(archivePath)
		return rom.InstallResult{}, notRetryable(fmt.Errorf("romcatalog: %s already holds a folder that is not a set", folder))
	case statErr == nil:
		discard(archivePath)
		return rom.InstallResult{}, notRetryable(fmt.Errorf("romcatalog: %s already holds a file", folder))
	case !os.IsNotExist(statErr):
		discard(archivePath)
		return rom.InstallResult{}, fmt.Errorf("romcatalog: stat %s: %w", folder, statErr)
	}
	pieces, err := resolveSet(entries, index)
	if err != nil {
		if errors.Is(err, rom.ErrNotRetryable) {
			discard(archivePath)
		}
		return rom.InstallResult{}, err
	}
	staged := filepath.Join(workDir, base)
	if err := writeSet(ctx, l.limits, pieces, staged); err != nil {
		if errors.Is(err, rom.ErrNotRetryable) {
			discard(archivePath)
		}
		return rom.InstallResult{}, err
	}
	if err := install(staged, tempPath(dir, base), folder, dir); err != nil {
		return rom.InstallResult{}, err
	}
	discard(archivePath)
	return installedSetResult(folder, false, true)
}

func (l *Library) targetPath(dir, base, ext string) (string, error) {
	root := filepath.Clean(l.Root)
	target := filepath.Join(dir, base+ext)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return "", notRetryable(fmt.Errorf("romcatalog: target %q escapes library root %q", target, root))
	}
	return target, nil
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

// installedSetResult reports the folder that holds a set and the bytes it holds.
func installedSetResult(folder string, skipped, extracted bool) (rom.InstallResult, error) {
	size, err := folderSize(folder)
	if err != nil {
		return rom.InstallResult{}, fmt.Errorf("romcatalog: measure %s: %w", folder, err)
	}
	return rom.InstallResult{Path: folder, Size: size, Skipped: skipped, Extracted: extracted}, nil
}

// setPresent reports whether the folder already holds the index of the set.
func setPresent(folder, index string) bool {
	info, err := os.Stat(filepath.Join(folder, index))
	return err == nil && info.Mode().IsRegular()
}

// folderSize returns the bytes the files of a folder hold.
func folderSize(folder string) (int64, error) {
	var total int64
	err := filepath.WalkDir(folder, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// fetch downloads the file into part and returns the digests of the whole
// file. A part that the server cannot resume is dropped and downloaded again.
func (l *Library) fetch(ctx context.Context, file rom.File, part string, progress func(downloaded, total int64)) (*checksums, error) {
	sums, err := l.fetchFrom(ctx, file, part, partSize(part), progress)
	if !errors.Is(err, errRestart) {
		return sums, err
	}
	discard(part)
	return l.fetchFrom(ctx, file, part, 0, progress)
}

// fetchFrom downloads the file into part, resuming at offset when the server
// honors it, and returns the digests of the whole file.
func (l *Library) fetchFrom(ctx context.Context, file rom.File, part string, offset int64, progress func(downloaded, total int64)) (*checksums, error) {
	sums := newChecksums(file)
	var opts []romfetch.OpenOption
	if offset > 0 {
		opts = append(opts, romfetch.WithIfRange(readValidator(part)))
	}
	stream, err := l.fetcher.Open(ctx, file.URL, offset, opts...)
	if err != nil {
		var urlErr *romfetch.URLError
		var statusErr *romfetch.StatusError
		switch {
		case errors.As(err, &urlErr):
			return nil, notRetryable(fmt.Errorf("romcatalog: build request for %s: %w", file.Name, err))
		case errors.Is(err, romfetch.ErrRangeNotSatisfiable):
			return completePart(ctx, file, part)
		case errors.As(err, &statusErr):
			wrapped := fmt.Errorf("romcatalog: download %s: %w", file.Name, err)
			if permanentStatus(statusErr.Code) {
				return nil, notRetryable(wrapped)
			}
			return nil, wrapped
		case err != nil:
			return nil, fmt.Errorf("romcatalog: download %s: %w", file.Name, err)
		}
	}
	defer func() { _ = stream.Body.Close() }()

	var done int64
	if stream.Partial {
		if err := sums.hashFile(ctx, part); err != nil {
			return nil, err
		}
		if err := checkContentRange(stream.ContentRange, offset, file.Size); err != nil {
			return nil, fmt.Errorf("%w: %w", errRestart, err)
		}
		done = offset
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if stream.Partial {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	} else if err := writeValidator(part, stream.Validator); err != nil {
		return nil, err
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("romcatalog: open %s: %w", part, err)
	}
	total := file.Size
	if stream.Length >= 0 {
		total = done + stream.Length
	}
	if total < 0 {
		total = 0
	}
	w := io.MultiWriter(&progressWriter{w: out, downloaded: done, total: total, report: progress}, sums)
	_, copyErr := io.Copy(w, stream.Body)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return nil, fmt.Errorf("romcatalog: download %s: %w", file.Name, copyErr)
	}
	return sums, nil
}

// completePart reports the part as the whole file when its size matches the
// published one and its checksum does not contradict it.
func completePart(ctx context.Context, file rom.File, part string) (*checksums, error) {
	sums := newChecksums(file)
	if file.Size <= 0 || partSize(part) != file.Size {
		discard(part)
		return nil, errRangeNotSatisfiable(file)
	}
	if err := sums.hashFile(ctx, part); err != nil {
		return nil, err
	}
	if _, _, _, mismatch := sums.mismatch(); mismatch {
		discard(part)
		return nil, errRangeNotSatisfiable(file)
	}
	return sums, nil
}

// errRangeNotSatisfiable reports a part the server cannot resume.
func errRangeNotSatisfiable(file rom.File) error {
	return fmt.Errorf("romcatalog: download %s: partial file no longer matches the source, discarded; retry", file.Name)
}

// checkContentRange validates the Content-Range of a partial answer against
// the offset that was asked for and the published size.
func checkContentRange(value string, offset, size int64) error {
	if value == "" {
		return errors.New("the server answered a partial request without a Content-Range")
	}
	start, _, total, ok := parseContentRange(value)
	if !ok {
		return fmt.Errorf("the server answered an unreadable Content-Range %q", value)
	}
	if start != offset {
		return fmt.Errorf("the server answered Content-Range %q, want the range to start at %d", value, offset)
	}
	if total >= 0 && size > 0 && total != size {
		return fmt.Errorf("the server answered Content-Range %q, want a total of %d bytes", value, size)
	}
	return nil
}

// parseContentRange reads the start and the total of a Content-Range header.
// A total of -1 means the header carries none.
func parseContentRange(value string) (start, end, total int64, ok bool) {
	rest, found := strings.CutPrefix(value, "bytes ")
	if !found {
		return 0, 0, 0, false
	}
	rangePart, totalPart, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, 0, false
	}
	startText, endText, found := strings.Cut(rangePart, "-")
	if !found {
		return 0, 0, 0, false
	}
	start, err := strconv.ParseInt(startText, 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	end, err = strconv.ParseInt(endText, 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	if totalPart == "*" {
		return start, end, -1, true
	}
	total, err = strconv.ParseInt(totalPart, 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

// checkSize compares the downloaded byte count with the published size.
func checkSize(part string, file rom.File) error {
	if file.Size <= 0 {
		return nil
	}
	got := partSize(part)
	if got == file.Size {
		return nil
	}
	return fmt.Errorf("romcatalog: download %s: expected %d bytes, got %d", file.Name, file.Size, got)
}

// partSize returns the size of the partial download, zero when it is gone.
func partSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// discard removes a partial download and the validator stored next to it.
func discard(part string) {
	_ = os.Remove(part)
	_ = os.Remove(validatorPath(part))
}

func validatorPath(part string) string { return part + ".meta" }

// readValidator returns the validator the part was downloaded under, empty
// when none was stored.
func readValidator(part string) string {
	data, err := os.ReadFile(validatorPath(part))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// writeValidator stores the validator of the representation the part holds;
// an empty validator removes any stored one.
func writeValidator(part, validator string) error {
	if validator == "" {
		_ = os.Remove(validatorPath(part))
		return nil
	}
	if err := os.WriteFile(validatorPath(part), []byte(validator), 0o644); err != nil {
		return fmt.Errorf("romcatalog: store the validator of %s: %w", part, err)
	}
	return nil
}

// permanentStatus reports whether an HTTP status a retry cannot fix.
func permanentStatus(code int) bool {
	switch code {
	case statusBadRequest, statusUnauthorized, statusForbidden, statusNotFound, statusGone:
		return true
	}
	return false
}

// notRetryable marks a failure a retry cannot fix.
func notRetryable(err error) error {
	return fmt.Errorf("%w: %w", err, rom.ErrNotRetryable)
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

// install moves src to target through tmp and syncs the directory so the
// rename survives a crash. A failed second rename puts the download back.
func install(src, tmp, target, dir string) error {
	if err := os.Rename(src, tmp); err != nil {
		return fmt.Errorf("romcatalog: stage %s: %w", target, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		installErr := fmt.Errorf("romcatalog: install %s: %w", target, err)
		if restoreErr := os.Rename(tmp, src); restoreErr != nil {
			return fmt.Errorf("%w: the staged download stays at %s: %w", installErr, tmp, restoreErr)
		}
		return installErr
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("romcatalog: sync %s: %w", dir, err)
	}
	return nil
}

// syncDir flushes the directory entries so a rename survives a crash.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
