package romlibrary

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"app.ddd/internal/domain/rom"
)

const undeclaredDiscCount = "The source did not declare how many discs this release has."

func installedPlayStationJob(job rom.Job) bool {
	return job.Status == rom.JobDone && strings.HasPrefix(job.Path, "psx/") && containedReference(job.Path) && fs.ValidPath(job.Path)
}

func (l *Library) InspectGame(ctx context.Context, job rom.Job) rom.GameReadiness {
	if !installedPlayStationJob(job) {
		return rom.GameReadiness{Status: rom.ReadinessUnknown, Reason: "Installed PlayStation game not available for inspection."}
	}
	root, err := os.OpenRoot(l.Root)
	if err != nil {
		return rom.GameReadiness{Status: rom.ReadinessUnknown, Reason: "The library cannot be read."}
	}
	defer func() { _ = root.Close() }()
	return inspectInstalledGame(ctx, root, job, nil)
}

func inspectInstalledGame(ctx context.Context, root *os.Root, job rom.Job, indexes map[string][sha256.Size]byte) rom.GameReadiness {
	result := rom.GameReadiness{Status: rom.ReadinessComplete, EntryPoint: job.Path}
	artifact := path.Base(job.Path)
	group, disc, numbered := rom.DiscOf(strings.TrimSuffix(artifact, path.Ext(artifact)))
	if numbered {
		playlist := path.Join(path.Dir(job.Path), group+".m3u")
		if _, err := root.Stat(playlist); !errors.Is(err, fs.ErrNotExist) {
			result.EntryPoint = playlist
		}
	}
	if info, err := root.Stat(result.EntryPoint); err == nil && info.IsDir() {
		entry, err := discDirectoryEntry(root, result.EntryPoint)
		if err != nil || entry == "" {
			return rom.GameReadiness{Status: rom.ReadinessUnknown, Reason: "No disc entry point could be identified."}
		}
		result.EntryPoint = entry
	}

	walk := bundleInspection{
		ctx: ctx, root: root, result: &result, indexes: indexes,
		seen: map[string]bool{}, visiting: map[string]bool{}, discs: map[int]bool{},
	}
	walk.inspect(result.EntryPoint, 0)
	playlist := strings.EqualFold(path.Ext(result.EntryPoint), ".m3u")
	_, sourceDisc, sourceNumbered := rom.DiscOf(job.Title)
	if !playlist && len(result.Files) > 0 {
		if numbered {
			walk.discs[disc] = true
		} else if sourceNumbered {
			walk.discs[sourceDisc] = true
		}
	}
	if playlist || numbered || sourceNumbered {
		total := rom.DeclaredDiscCount(job.Title)
		switch {
		case total == 0 && result.Reason == "":
			result.Reason = undeclaredDiscCount
		case total > 0 && !walk.unnumberedEntry:
			for number := 1; number <= total; number++ {
				if !walk.discs[number] {
					result.MissingDiscs = append(result.MissingDiscs, number)
				}
			}
		case total > 0 && result.Reason == "":
			result.Reason = "The playlist does not identify every disc by number."
		}
	}
	sort.Strings(result.MissingFiles)
	switch {
	case len(result.MissingFiles) > 0 || len(result.MissingDiscs) > 0:
		result.Status = rom.ReadinessIncomplete
	case result.Reason != "":
		result.Status = rom.ReadinessUnknown
	}
	return result
}

func discDirectoryEntry(root *os.Root, name string) (string, error) {
	dir, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	files, err := dir.ReadDir(-1)
	if err != nil {
		return "", err
	}
	best, playlist, rank := "", "", len(discIndexes)
	for _, file := range files {
		if file.Type().IsRegular() && strings.EqualFold(path.Ext(file.Name()), ".m3u") {
			if playlist != "" {
				return "", nil
			}
			playlist = file.Name()
		}
		r := indexRank(file.Name())
		if file.Type().IsRegular() && r >= 0 && (r < rank || r == rank && file.Name() < best) {
			best, rank = file.Name(), r
		}
	}
	if playlist != "" {
		return path.Join(name, playlist), nil
	}
	if best == "" {
		return "", nil
	}
	return path.Join(name, best), nil
}

type bundleInspection struct {
	ctx             context.Context
	root            *os.Root
	result          *rom.GameReadiness
	seen            map[string]bool
	visiting        map[string]bool
	discs           map[int]bool
	indexes         map[string][sha256.Size]byte
	unnumberedEntry bool
}

func (w *bundleInspection) inspect(name string, depth int) {
	if w.visiting[name] {
		w.result.Reason = "A game index references itself through a cycle."
		return
	}
	if w.seen[name] {
		return
	}
	if w.ctx.Err() != nil || depth > 3 || len(w.seen) >= 256 {
		w.result.Reason = "Bundle inspection did not finish."
		return
	}
	w.seen[name] = true
	w.visiting[name] = true
	defer delete(w.visiting, name)
	info, err := w.root.Stat(name)
	if errors.Is(err, fs.ErrNotExist) {
		w.result.MissingFiles = append(w.result.MissingFiles, name)
		return
	}
	if err != nil || !info.Mode().IsRegular() {
		w.result.Reason = "A bundle file cannot be read safely."
		return
	}
	w.result.Files = append(w.result.Files, name)
	if !isIndexName(name) {
		return
	}
	file, err := w.root.Open(name)
	if err != nil {
		w.result.Reason = "A game index cannot be read."
		return
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
	if err != nil || len(body) > maxIndexBytes {
		w.result.Reason = "A game index cannot be read."
		return
	}
	if w.indexes != nil {
		w.indexes[name] = sha256.Sum256(body)
	}
	refs, err := parseIndex(name, body)
	if err != nil || len(refs) == 0 {
		w.result.Reason = "A game index has no usable file references."
		return
	}
	if strings.EqualFold(path.Ext(name), ".cue") {
		for _, line := range splitLines(string(body)) {
			fields := splitFields(line)
			if len(fields) > 0 && strings.EqualFold(fields[0], "FILE") && (len(fields) != 3 || fields[1] == "" || strings.Count(line, "\"")%2 != 0) {
				w.result.Reason = "A cue sheet contains a malformed file directive."
				break
			}
		}
	}
	for _, ref := range refs {
		if w.indexes != nil && (!containedReference(ref) || !portableBundleName(path.Clean(ref))) {
			w.result.Reason = "A game index contains a non-portable file reference."
			continue
		}
		ref = normalizeReference(ref)
		if !containedReference(ref) {
			w.result.Reason = "A game index contains an unsafe file reference."
			continue
		}
		if strings.EqualFold(path.Ext(name), ".m3u") && depth == 0 {
			if _, number, ok := rom.DiscOf(ref); ok {
				w.discs[number] = true
			} else {
				w.unnumberedEntry = true
			}
		}
		w.inspect(path.Join(path.Dir(name), ref), depth+1)
	}
}
