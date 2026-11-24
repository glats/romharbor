package rom

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Compatibility is what the domain decides about a game variant for a
// platform: whether the platform runs it, and the short tokens that say why.
type Compatibility struct {
	// Compatible reports whether the variant runs on the platform.
	Compatible bool
	// Labels are the short lowercase tokens that describe the variant, in a
	// stable order. They are empty when the variant carries no remark.
	Labels []string
}

// The tokens the compatibility decision carries.
const (
	LabelGood        = "good"
	LabelTranslation = "translation"
	LabelHack        = "hack"
	LabelBadDump     = "baddump"
	LabelBeta        = "beta"
	LabelPrototype   = "prototype"
	LabelDemo        = "demo"
	LabelSample      = "sample"
	LabelUnlicensed  = "unlicensed"
	LabelPAL         = "pal"
	LabelHeavy       = "heavy"
	LabelFormat      = "format"
	LabelZ64         = "z64"
)

var parenTag = regexp.MustCompile(`\(([^()]*)\)`)

var bracketTag = regexp.MustCompile(`\[([^\[\]]*)\]`)

var heavyN64Titles = []string{
	"banjokazooie",
	"banjotooie",
	"goldeneye007",
	"donkeykong64",
	"majorasmask",
}

// KnulliCompatibility decides whether a game variant runs on a platform the
// way Knulli runs it, and labels the reasons.
//
// The decision reads the No-Intro and Redump tags of the title and of the
// published file names. A translation, a hack, a bad dump, a beta, a
// prototype, a demo, a sample and an unlicensed release do not run. A PAL
// release runs and is labeled. A tag the rules do not know never makes a
// game incompatible. The decision does not depend on the case of the title.
func KnulliCompatibility(g Game, p Platform) Compatibility {
	facts := factsOfGame(g)
	return compatibilityOf(facts, g, p)
}

func compatibilityOf(facts variantFacts, g Game, p Platform) Compatibility {
	var labels []string
	compatible := true
	note := func(label string) {
		labels = append(labels, label)
	}
	if facts.good {
		note(LabelGood)
	}
	if facts.translation {
		note(LabelTranslation)
		compatible = false
	}
	if facts.hack {
		note(LabelHack)
		compatible = false
	}
	if facts.badDump {
		note(LabelBadDump)
		compatible = false
	}
	if facts.beta {
		note(LabelBeta)
		compatible = false
	}
	if facts.prototype {
		note(LabelPrototype)
		compatible = false
	}
	if facts.demo {
		note(LabelDemo)
		compatible = false
	}
	if facts.sample {
		note(LabelSample)
		compatible = false
	}
	if facts.unlicensed {
		note(LabelUnlicensed)
		compatible = false
	}
	if isPAL(facts.region) {
		note(LabelPAL)
	}
	if isHeavyGame(g, p) {
		note(LabelHeavy)
	}
	runs, label := formatOf(g, p)
	if label != "" {
		note(label)
	}
	if !runs {
		compatible = false
	}
	return Compatibility{Compatible: compatible, Labels: labels}
}

type variantFacts struct {
	translation bool
	hack        bool
	badDump     bool
	good        bool
	beta        bool
	prototype   bool
	demo        bool
	sample      bool
	unlicensed  bool
	region      string
	disc        int
	hasDisc     bool
}

func factsOfGame(g Game) variantFacts {
	facts := factsOf(g.Title)
	for _, file := range g.Files {
		facts.merge(factsOf(CleanTitle(file.Name)))
	}
	return facts
}

func factsOf(text string) variantFacts {
	var facts variantFacts
	for _, match := range parenTag.FindAllStringSubmatch(text, -1) {
		facts.readParen(strings.TrimSpace(match[1]))
	}
	for _, match := range bracketTag.FindAllStringSubmatch(text, -1) {
		facts.readBracket(strings.TrimSpace(match[1]))
	}
	if facts.region == "" {
		facts.region = RegionOf(text)
	}
	return facts
}

func (f *variantFacts) readParen(value string) {
	switch strings.ToLower(value) {
	case "beta":
		f.beta = true
	case "proto", "prototype":
		f.prototype = true
	case "demo":
		f.demo = true
	case "sample":
		f.sample = true
	case "unl":
		f.unlicensed = true
	case "u":
		f.setRegion("usa")
	case "e":
		f.setRegion("europe")
	case "j":
		f.setRegion("japan")
	case "a":
		f.setRegion("australia")
	default:
		f.readDisc(value)
	}
}

func (f *variantFacts) readBracket(value string) {
	lower := strings.ToLower(value)
	switch {
	case lower == "!":
		f.good = true
	case strings.HasPrefix(lower, "t+"), strings.HasPrefix(lower, "t-"):
		f.translation = true
	case isDumpFlag(lower, "b"):
		f.badDump = true
	case isDumpFlag(lower, "h"):
		f.hack = true
	}
}

func isDumpFlag(tag, flag string) bool {
	rest, ok := strings.CutPrefix(tag, flag)
	if !ok {
		return false
	}
	return rest == "" || (rest[0] >= '0' && rest[0] <= '9')
}

func (f *variantFacts) readDisc(value string) {
	rest, ok := strings.CutPrefix(strings.ToLower(value), "disc ")
	if !ok {
		return
	}
	number, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
	disc, err := strconv.Atoi(number)
	if err != nil {
		return
	}
	f.disc = disc
	f.hasDisc = true
}

func (f *variantFacts) setRegion(region string) {
	if f.region == "" {
		f.region = region
	}
}

func (f *variantFacts) merge(other variantFacts) {
	f.translation = f.translation || other.translation
	f.hack = f.hack || other.hack
	f.badDump = f.badDump || other.badDump
	f.good = f.good || other.good
	f.beta = f.beta || other.beta
	f.prototype = f.prototype || other.prototype
	f.demo = f.demo || other.demo
	f.sample = f.sample || other.sample
	f.unlicensed = f.unlicensed || other.unlicensed
	f.setRegion(other.region)
	if !f.hasDisc {
		f.disc = other.disc
		f.hasDisc = other.hasDisc
	}
}

func formatOf(g Game, p Platform) (bool, string) {
	if len(p.Extensions) == 0 {
		return true, ""
	}
	main, ok := g.Main()
	if !ok || IsArchive(main.Name) {
		return true, ""
	}
	if !p.Accepts(main.Name) {
		return false, LabelFormat
	}
	if p.Slug == "n64" {
		switch strings.ToLower(path.Ext(main.Name)) {
		case ".n64", ".v64":
			return true, LabelZ64
		}
	}
	return true, ""
}

func isPAL(region string) bool {
	return region == "europe" || region == "australia"
}

func isHeavyGame(g Game, p Platform) bool {
	if p.Slug != "" && p.Slug != "n64" {
		return false
	}
	if isHeavyTitle(baseTitle(g.Title)) {
		return true
	}
	for _, file := range g.Files {
		if isHeavyTitle(baseTitle(CleanTitle(file.Name))) {
			return true
		}
	}
	return false
}

func isHeavyTitle(base string) bool {
	normalized := normalizeTitle(base)
	for _, title := range heavyN64Titles {
		if normalized == title {
			return true
		}
	}
	return false
}

func normalizeTitle(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func baseTitle(title string) string {
	stripped := parenTag.ReplaceAllString(title, " ")
	stripped = bracketTag.ReplaceAllString(stripped, " ")
	return strings.Join(strings.Fields(stripped), " ")
}
