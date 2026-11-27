package rom

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxBaseNameBytes = 120

// archiveExtensions are the container formats the sources publish.
var archiveExtensions = []string{".zip", ".7z", ".rar"}

// imageExtensions are the disc and cartridge image formats whose extension is
// dropped from a title, because their files are named after the game alone.
var imageExtensions = []string{".iso", ".cia", ".rvz", ".wbfs", ".gcm", ".cso", ".cdi", ".gdi"}

// romExtensions are the file extensions of every platform, so a platform that
// accepts an extension can never be rejected as a non-game file.
var romExtensions = platformExtensions()

func platformExtensions() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range platforms {
		for _, ext := range p.Extensions {
			if !seen[ext] {
				seen[ext] = true
				out = append(out, ext)
			}
		}
	}
	return out
}

// ArchiveExtension reports whether ext is an archive format that Install can extract.
func ArchiveExtension(ext string) bool {
	lower := strings.ToLower(ext)
	return lower == ".zip" || lower == ".7z"
}

// IsArchive reports whether a published file name is a container archive.
func IsArchive(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// IsGameFile reports whether a file of an item holds a game. Firmware and
// BIOS collections are not games.
func IsGameFile(name string) bool {
	lower := strings.ToLower(name)
	for _, part := range strings.Split(lower, "/") {
		if part == "bios" || part == "firmware" || part == "system" {
			return false
		}
	}
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	for _, ext := range romExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

var titleNoise = regexp.MustCompile(`(?i)^(no-intro\s*-\s*|redump\s*-\s*|mame\s*-\s*)`)

// CleanTitle turns a published file name into a readable game title.
func CleanTitle(name string) string {
	base := path.Base(name)
	for _, ext := range archiveExtensions {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	lower := strings.ToLower(base)
	for _, ext := range imageExtensions {
		if strings.HasSuffix(lower, ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	base = titleNoise.ReplaceAllString(base, "")
	base = strings.ReplaceAll(base, "'", "")
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.TrimSpace(base)
	return base
}

// RegionOf reads the region tag of a No-Intro or Redump title.
func RegionOf(title string) string {
	lower := strings.ToLower(title)
	for _, region := range []string{
		"world", "usa", "europe", "japan", "asia", "australia", "brazil", "korea", "china",
	} {
		if strings.Contains(lower, "("+region+")") || strings.Contains(lower, "("+region[:3]) {
			return region
		}
	}
	return ""
}

// SplitFileName returns the sanitized base name and the extension of a file
// name, dropping any directory part.
func SplitFileName(fileName string) (string, string, error) {
	name := sanitizeName(lastPathElement(fileName))
	ext := path.Ext(name)
	base := strings.Trim(truncateBytes(strings.TrimSuffix(name, ext), maxBaseNameBytes), ". ")
	if base == "" {
		return "", "", fmt.Errorf("romcatalog: %q has no usable ROM name", fileName)
	}
	return base, ext, nil
}

func lastPathElement(s string) string {
	return s[strings.LastIndexAny(s, `/\`)+1:]
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.Trim(strings.Join(strings.Fields(b.String()), " "), ". ")
}

func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// PickROM returns the index of the ROM to install: the first match at the
// archive root, or otherwise the first match in any folder.
func PickROM(names []string, exts []string) (int, bool) {
	first := -1
	for i, name := range names {
		if !HasROMExt(name, exts) {
			continue
		}
		if !strings.Contains(name, "/") {
			return i, true
		}
		if first < 0 {
			first = i
		}
	}
	return first, first >= 0
}

// HasROMExt reports whether the extension of name is one of the extensions,
// ignoring case.
func HasROMExt(name string, exts []string) bool {
	return AcceptsExtension(exts, path.Ext(name))
}

// NoROMError reports an archive that holds no file with an accepted extension.
func NoROMError(exts []string) error {
	return fmt.Errorf("romcatalog: archive contains no file with extension %s", strings.Join(exts, ", "))
}

var discTag = regexp.MustCompile(`(?i)\s*\((?:disc|disk|cd)\s*(\d+)(?:\s*of\s*(\d+))?\)`)

func DeclaredDiscCount(name string) int {
	match := discTag.FindStringSubmatch(name)
	if len(match) != 3 {
		return 0
	}
	disc, _ := strconv.Atoi(match[1])
	count, err := strconv.Atoi(match[2])
	if err != nil || disc < 1 || count < disc || count > 99 {
		return 0
	}
	return count
}

// DiscOf reads the disc tag of a multi-disc game name. It returns the name
// without the tag, which is shared by every disc of the same release and
// revision, and the number of the disc.
func DiscOf(name string) (group string, disc int, ok bool) {
	match := discTag.FindStringSubmatchIndex(name)
	if match == nil {
		return "", 0, false
	}
	number, err := strconv.Atoi(name[match[2]:match[3]])
	if err != nil || number < 1 {
		return "", 0, false
	}
	rest := name[:match[0]] + name[match[1]:]
	return strings.Join(strings.Fields(rest), " "), number, true
}
