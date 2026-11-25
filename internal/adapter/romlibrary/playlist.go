package romlibrary

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"app.ddd/internal/domain/rom"
)

// discIndexes are the extensions a disc is loaded by, the preferred one first.
var discIndexes = []string{".cue", ".gdi", ".chd", ".pbp", ".mds", ".iso", ".img", ".bin"}

type discEntry struct {
	disc     int
	rank     int
	relative string
}

// writePlaylist refreshes the .m3u playlist of the multi-disc game that
// installed sits next to. The playlist lists every disc of the same title in
// disc order, with paths relative to the playlist, so a core or an emulator
// can swap discs. It exists only while two or more discs are installed, and it
// never moves an installed disc.
func writePlaylist(installed string) error {
	dir := filepath.Dir(installed)
	group, _, ok := rom.DiscOf(strings.TrimSuffix(filepath.Base(installed), filepath.Ext(installed)))
	if !ok {
		return nil
	}
	listing, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("romcatalog: read %s: %w", dir, err)
	}
	best := map[int]discEntry{}
	for _, item := range listing {
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		candidate, ok := discOf(dir, item, group)
		if !ok {
			continue
		}
		if current, seen := best[candidate.disc]; !seen || candidate.rank < current.rank {
			best[candidate.disc] = candidate
		}
	}
	if len(best) < 2 {
		return nil
	}
	discs := make([]discEntry, 0, len(best))
	for _, entry := range best {
		discs = append(discs, entry)
	}
	sort.Slice(discs, func(i, j int) bool { return discs[i].disc < discs[j].disc })
	var body strings.Builder
	for _, entry := range discs {
		body.WriteString(entry.relative)
		body.WriteByte('\n')
	}
	target := filepath.Join(dir, group+".m3u")
	temporary := tempPath(dir, filepath.Base(target))
	if err := os.WriteFile(temporary, []byte(body.String()), 0o644); err != nil {
		return fmt.Errorf("romcatalog: write playlist: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("romcatalog: install playlist: %w", err)
	}
	return nil
}

func discOf(dir string, item os.DirEntry, group string) (discEntry, bool) {
	name := item.Name()
	if item.IsDir() {
		itemGroup, disc, ok := rom.DiscOf(name)
		if !ok || itemGroup != group {
			return discEntry{}, false
		}
		inner, err := os.ReadDir(filepath.Join(dir, name))
		if err != nil {
			return discEntry{}, false
		}
		best, found := discEntry{rank: len(discIndexes)}, false
		for _, file := range inner {
			if rank := indexRank(file.Name()); !file.IsDir() && rank >= 0 && rank < best.rank {
				best = discEntry{disc: disc, rank: rank, relative: name + "/" + file.Name()}
				found = true
			}
		}
		return best, found
	}
	ext := filepath.Ext(name)
	rank := indexRank(name)
	if rank < 0 {
		return discEntry{}, false
	}
	itemGroup, disc, ok := rom.DiscOf(strings.TrimSuffix(name, ext))
	if !ok || itemGroup != group {
		return discEntry{}, false
	}
	return discEntry{disc: disc, rank: rank, relative: name}, true
}

func indexRank(name string) int {
	ext := filepath.Ext(name)
	for rank, candidate := range discIndexes {
		if strings.EqualFold(ext, candidate) {
			return rank
		}
	}
	return -1
}
