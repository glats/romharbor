package romlibrary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// maxIndexBytes is the size an index file may reach before it is refused.
const maxIndexBytes = 1 << 20

// indexExtensions are the extensions of the files that index the pieces of a
// multi-file set, the outermost index first.
var indexExtensions = []string{".m3u", ".gdi", ".cue"}

// findIndex returns the index of the set the archive holds, when the platform
// accepts the extension of the index.
func findIndex(entries []archiveEntry, exts []string) (archiveEntry, bool) {
	for _, ext := range indexExtensions {
		if !acceptsExtension(exts, ext) {
			continue
		}
		for _, e := range entries {
			if strings.EqualFold(path.Ext(e.name), ext) {
				return e, true
			}
		}
	}
	return archiveEntry{}, false
}

// acceptsExtension reports whether the platform accepts the extension.
func acceptsExtension(exts []string, ext string) bool {
	for _, e := range exts {
		if strings.EqualFold(e, ext) {
			return true
		}
	}
	return false
}

// archiveLookup finds the entries of an archive by the path they hold.
type archiveLookup struct {
	byPath  map[string]archiveEntry
	byLower map[string]archiveEntry
}

func newArchiveLookup(entries []archiveEntry) *archiveLookup {
	lookup := &archiveLookup{
		byPath:  make(map[string]archiveEntry, len(entries)),
		byLower: make(map[string]archiveEntry, len(entries)),
	}
	for _, e := range entries {
		lookup.byPath[e.name] = e
		lower := strings.ToLower(e.name)
		if _, ok := lookup.byLower[lower]; !ok {
			lookup.byLower[lower] = e
		}
	}
	return lookup
}

// resolve finds the entry a reference names inside the folder of the index.
func (l *archiveLookup) resolve(dir, name string) (archiveEntry, bool) {
	full := path.Join(dir, name)
	if e, ok := l.byPath[full]; ok {
		return e, true
	}
	e, ok := l.byLower[strings.ToLower(full)]
	return e, ok
}

// setPiece is one file of the set and the path it keeps inside the folder.
type setPiece struct {
	entry archiveEntry
	path  string
}

// resolveSet returns the index of the set and every piece it references, each
// with the path it keeps inside the installed folder.
func resolveSet(entries []archiveEntry, index archiveEntry) ([]setPiece, error) {
	lookup := newArchiveLookup(entries)
	root := path.Dir(index.name)
	var pieces []setPiece
	seen := make(map[string]bool)
	if err := collectSet(lookup, root, index, seen, &pieces); err != nil {
		return nil, err
	}
	paths := make(map[string]string, len(pieces))
	for _, piece := range pieces {
		if other, ok := paths[piece.path]; ok && other != piece.entry.name {
			return nil, notRetryable(fmt.Errorf("romcatalog: %s and %s both land at %q", other, piece.entry.name, piece.path))
		}
		paths[piece.path] = piece.entry.name
	}
	return pieces, nil
}

// collectSet adds an index and the pieces it references to the set. The
// layout of the archive is kept, so the text of every index stays valid.
func collectSet(lookup *archiveLookup, root string, entry archiveEntry, seen map[string]bool, pieces *[]setPiece) error {
	if seen[entry.name] {
		return nil
	}
	seen[entry.name] = true
	rel, ok := relativeTo(root, entry.name)
	if !ok {
		return notRetryable(fmt.Errorf("romcatalog: %s lands outside the folder of the index", entry.name))
	}
	*pieces = append(*pieces, setPiece{entry: entry, path: rel})
	if !isIndexName(entry.name) {
		return nil
	}
	body, err := readIndex(entry)
	if err != nil {
		return err
	}
	references, err := parseIndex(entry.name, body)
	if err != nil {
		return err
	}
	if len(references) == 0 {
		return notRetryable(fmt.Errorf("romcatalog: %s references no piece of the set", entry.name))
	}
	dir := path.Dir(entry.name)
	for _, reference := range references {
		name := normalizeReference(reference)
		if !containedReference(name) {
			return notRetryable(fmt.Errorf("romcatalog: %s references %q, which is outside the archive", entry.name, reference))
		}
		piece, ok := lookup.resolve(dir, name)
		if !ok {
			return notRetryable(fmt.Errorf("romcatalog: %s references %q, which the archive does not hold", entry.name, reference))
		}
		if err := collectSet(lookup, root, piece, seen, pieces); err != nil {
			return err
		}
	}
	return nil
}

// relativeTo returns the path a file keeps inside the folder of the index,
// when the file lives under the folder of the index.
func relativeTo(root, name string) (string, bool) {
	if root == "." || root == "" {
		if !containedReference(name) {
			return "", false
		}
		return name, true
	}
	prefix := root + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	rel := name[len(prefix):]
	if !containedReference(rel) {
		return "", false
	}
	return rel, true
}

// isIndexName reports whether the name is the index of a set.
func isIndexName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, candidate := range indexExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

// normalizeReference turns the path of a piece into the form the archive uses.
func normalizeReference(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

// containedReference reports whether the reference names a file of the archive.
func containedReference(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") {
		return false
	}
	if len(name) >= 2 && name[1] == ':' {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// readIndex reads the body of an index file, bounded by the index cap.
func readIndex(entry archiveEntry) ([]byte, error) {
	rc, err := entry.open()
	if err != nil {
		return nil, fmt.Errorf("romcatalog: open %s in archive: %w", entry.name, err)
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, maxIndexBytes+1))
	if err != nil {
		return nil, fmt.Errorf("romcatalog: read %s in archive: %w", entry.name, err)
	}
	if len(body) > maxIndexBytes {
		return nil, notRetryable(fmt.Errorf("romcatalog: extract %s: larger than the %d byte index cap: %w", entry.name, maxIndexBytes, errExtractTooLarge))
	}
	return body, nil
}

// parseIndex reads the pieces an index file references.
func parseIndex(name string, body []byte) ([]string, error) {
	switch strings.ToLower(path.Ext(name)) {
	case ".cue":
		return parseCue(string(body)), nil
	case ".gdi":
		return parseGDI(string(body))
	case ".m3u":
		return parseM3U(string(body)), nil
	}
	return nil, notRetryable(fmt.Errorf("romcatalog: unsupported index file %q", name))
}

// parseCue reads the files a cue sheet lists.
func parseCue(body string) []string {
	var names []string
	for _, line := range splitLines(body) {
		fields := splitFields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FILE") {
			continue
		}
		names = append(names, fields[1])
	}
	return names
}

// parseGDI reads the tracks a GDI index lists. The first line holds the number
// of tracks and every track line names its file in the fifth field.
func parseGDI(body string) ([]string, error) {
	lines := splitLines(body)
	if len(lines) == 0 {
		return nil, nil
	}
	if _, err := strconv.Atoi(lines[0]); err != nil {
		return nil, fmt.Errorf("romcatalog: the GDI index does not start with a track count: %w", err)
	}
	var names []string
	for _, line := range lines[1:] {
		fields := splitFields(line)
		if len(fields) < 5 {
			continue
		}
		names = append(names, fields[4])
	}
	return names, nil
}

// parseM3U reads the files a playlist lists.
func parseM3U(body string) []string {
	var names []string
	for _, line := range splitLines(body) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	return names
}

// splitLines splits a body into the lines that hold something.
func splitLines(body string) []string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// splitFields splits a line into fields, keeping a quoted field together.
func splitFields(line string) []string {
	var fields []string
	var current strings.Builder
	quoted := false
	started := false
	for _, r := range line {
		switch {
		case r == '"':
			if quoted {
				fields = append(fields, current.String())
				current.Reset()
				started = false
				quoted = false
				continue
			}
			quoted = true
			started = true
		case (r == ' ' || r == '\t') && !quoted:
			if started {
				fields = append(fields, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if started {
		fields = append(fields, current.String())
	}
	return fields
}

// writeSet writes the index and every piece of the set into the folder,
// keeping the paths the archive holds them under.
func writeSet(ctx context.Context, limits extractLimits, pieces []setPiece, folder string) error {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return fmt.Errorf("romcatalog: create %s: %w", folder, err)
	}
	remaining := limits.bytes
	for _, piece := range pieces {
		dst := filepath.Join(folder, filepath.FromSlash(piece.path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("romcatalog: create %s: %w", filepath.Dir(dst), err)
		}
		if remaining < 0 || piece.entry.size > uint64(remaining) {
			return notRetryable(fmt.Errorf("romcatalog: extract %s: the set does not fit in the %d byte cap: %w", piece.entry.name, limits.bytes, errExtractTooLarge))
		}
		written, err := writeEntry(ctx, piece.entry, dst, remaining)
		remaining -= written
		if err != nil {
			if errors.Is(err, errExtractTooLarge) {
				return notRetryable(fmt.Errorf("romcatalog: extract %s: the set does not fit in the %d byte cap: %w", piece.entry.name, limits.bytes, errExtractTooLarge))
			}
			return err
		}
	}
	return nil
}
