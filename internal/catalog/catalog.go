// Package catalog parses link-list files into share-link catalog entries for
// browsing and queueing. It is a behaviour-for-behaviour port of the parsing
// half of usnfetch_catalog.py (storage lives elsewhere).
//
// The parser understands the block layout of exported link lists:
//
//	SOURCE: sampleshop  (6 builds, 8 links)             section header
//	Some Build Title  [512GB]                           title, listed size
//	SKU: SMP15   /   Info: https://...                  metadata
//	512GB-Some-Image-Name:                              label for the next link
//	usn://...
//
// and degrades to "any line with a link, titled by the text around it" for
// plain lists, CSV rows or saved HTML. One entry per distinct link: a link
// listed under several titles keeps the first and records the others in Also.
package catalog

import (
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Entry is one share link of the catalog.
type Entry struct {
	Link    string   `json:"link"`
	Title   string   `json:"title"`
	Variant string   `json:"variant"` // per-link label inside a listing ("4TB Wii", "Drive R (Main) - 1 of 3")
	Listed  string   `json:"listed"`  // size text as listed ("512GB", "32GB / 64GB")
	Size    *int64   `json:"size"`    // bytes; nil = not pinned by the listing
	Source  string   `json:"source"`
	SKU     string   `json:"sku"`
	Info    string   `json:"info"`
	Also    []string `json:"also"` // other labels the same link was listed under; never nil after parsing
	Added   float64  `json:"added"`
}

// Label is the title, followed by the per-link variant when there is one.
func (e Entry) Label() string {
	if e.Variant != "" {
		return e.Title + " — " + e.Variant
	}
	return e.Title
}

// Haystack is the lowercased text searched by catalog filters (Info excluded).
func (e Entry) Haystack() string {
	parts := append([]string{e.Title, e.Variant, e.Listed, e.Source, e.SKU}, e.Also...)
	return pyLower(strings.Join(parts, " "))
}

// The Python patterns, translated. `\s` becomes pySpaceClass, re.I becomes
// explicit classes holding exactly the characters Python folds together (Go's
// (?i) misses İ/ı for "i"), and the trailing `\b` of the size patterns, which
// in Python is Unicode-aware, is checked in code (see sizeEndsWord).
const (
	sp       = `[` + pySpaceClass + `]`
	sizeBody = `(\p{Nd}+(?:\.\p{Nd}+)?)` + sp + `*([Tt][Bb]|[Gg][Bb]|[Mm][Bb]|[Kk\x{212a}][Bb])`
)

var (
	linkRE      = regexp.MustCompile(`usn://[A-Za-z0-9_=-]+|usenet://[^` + pySpaceClass + `"'<>]+`)
	sizeRE      = regexp.MustCompile(sizeBody)
	leadSizeRE  = regexp.MustCompile(`^` + sp + `*` + sizeBody)
	bracketRE   = regexp.MustCompile(sp + `*\[([^\]]+)\]` + sp + `*$`)
	metaRE      = regexp.MustCompile(`^([Ss\x{17f}][Kk\x{212a}][Uu]|[Ii\x{130}\x{131}][Nn][Ff][Oo]|[Uu][Rr][Ll]|[Pp][Aa][Gg][Ee])` + sp + `*:` + sp + `*(.+)$`)
	sourceRE    = regexp.MustCompile(`^[Ss\x{17f}][Oo][Uu][Rr][Cc][Ee]:` + sp + `*([^` + pySpaceClass + `]+)`)
	ruleRE      = regexp.MustCompile(`^[=\-#*_~` + pySpaceClass + `]+$`)
	blankLineRE = regexp.MustCompile(`\n[ \t]*\n`)
	tagRE       = regexp.MustCompile(`<[^>]+>`)
	charrefRE   = regexp.MustCompile(`&(#[0-9]+;?|#[xX][0-9a-fA-F]+;?|[^\t\n\f <&#;]{1,32};?)`)
)

// Listings use drive (decimal) sizes.
var units = map[string]float64{"KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}

// sizeEndsWord reports whether Python's `\b` holds after a size match ending at
// byte end: the unit ends in a word character, so the next one must not be.
func sizeEndsWord(s string, end int) bool {
	if end == len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[end:])
	return !pyIsWord(r)
}

// findSize returns the submatch indices of Python's _SIZE.search(s[from:]),
// offset into s, or nil. When the `\b` check fails Python retries from the next
// character, so the search does too.
func findSize(s string, from int) []int {
	for from < len(s) {
		m := sizeRE.FindStringSubmatchIndex(s[from:])
		if m == nil {
			return nil
		}
		for i := range m {
			m[i] += from
		}
		if sizeEndsWord(s, m[1]) {
			return m
		}
		_, w := utf8.DecodeRuneInString(s[m[0]:])
		from = m[0] + w
	}
	return nil
}

// countSizes is len(_SIZE.findall(s)).
func countSizes(s string) int {
	n := 0
	for m := findSize(s, 0); m != nil; m = findSize(s, m[1]) {
		n++
	}
	return n
}

// ParseSize returns the bytes of the first "<number> <unit>" in s ("1.5 TB",
// "512gb"), or nil when there is none.
func ParseSize(s string) *int64 {
	m := findSize(s, 0)
	if m == nil {
		return nil
	}
	return sizeBytes(s[m[2]:m[3]], s[m[4]:m[5]])
}

// sizeBytes is int(float(num) * _UNITS[unit.upper()]).
func sizeBytes(num, unit string) *int64 {
	f, _ := strconv.ParseFloat(asciiDigits(num), 64) // well-formed by the regexp; overflow gives +Inf
	unit = strings.ToUpper(unit)
	if strings.HasPrefix(unit, "K") {
		// The Kelvin sign matches "K" under re.I but upper() keeps it, so
		// Python raises KeyError here; treat it as "KB" instead.
		unit = "KB"
	}
	v := f * units[unit]
	n := int64(math.MaxInt64) // Python's int is unbounded; clamp what int64 cannot hold
	if v < math.MaxInt64 {
		n = int64(v)
	}
	return &n
}

// leadSize is _LEAD_SIZE.match(s): a size at the very start of a label
// ("512GB-Image-Name", "4TB Wii"). It returns the number, the unit and the end
// of the match.
func leadSize(s string) (num, unit string, end int, ok bool) {
	m := leadSizeRE.FindStringSubmatchIndex(s)
	if m == nil || !sizeEndsWord(s, m[1]) { // anchored: no other match to fall back on
		return "", "", 0, false
	}
	return s[m[2]:m[3]], s[m[4]:m[5]], m[1], true
}

var decUnits = [...]string{"B", "KB", "MB", "GB", "TB", "PB"}

// FmtDec formats a byte count with decimal units, as fmt_dec ("512.00 GB", "999 B").
func FmtDec(n float64) string {
	i := 0
	for i < len(decUnits)-1 && !(n < 1000) { // NaN runs through to PB, as in Python
		n /= 1000
		i++
	}
	if i > 0 {
		switch {
		case math.IsNaN(n):
			return "nan " + decUnits[i]
		case math.IsInf(n, 1):
			return "inf " + decUnits[i]
		}
		return strconv.FormatFloat(n, 'f', 2, 64) + " " + decUnits[i]
	}
	switch { // f"{int(n)} B": truncate toward zero, exactly
	case math.Abs(n) < 1<<63:
		return strconv.FormatInt(int64(n), 10) + " B"
	case math.IsInf(n, -1):
		return "-inf B" // Python raises OverflowError
	}
	return new(big.Float).SetFloat64(n).Text('f', 0) + " B"
}

// cleanInline is _clean_inline: the text of a line around its links, with
// HTML tags dropped, entities decoded and list punctuation trimmed.
func cleanInline(s string) string {
	s = pyUnescape(tagRE.ReplaceAllLiteralString(s, " "))
	return strings.Trim(collapseSpace(s), " \t-|:,;\"'")
}

type groupLink struct{ link, label string }

// group is one listing: its title, listed size, metadata and labelled links.
type group struct {
	title, listed, sku, info, source string
	links                            []groupLink
}

// newGroup is _group: a bracketed size at the end of the title ("[512GB]")
// becomes the listed size; any other bracket ("[CCI]") stays in the title.
func newGroup(title, source string) *group {
	title, listed := pyStrip(strings.TrimRight(title, ":")), ""
	if m := bracketRE.FindStringSubmatchIndex(title); m != nil && ParseSize(title[m[2]:m[3]]) != nil {
		listed, title = pyStrip(title[m[2]:m[3]]), pyStrip(title[:m[0]])
	}
	return &group{title: title, listed: listed, source: source}
}

// ParseText parses a link list into one entry per distinct link. defaultSource
// names the entries until a "SOURCE:" header does. The text is taken as Python
// would hold it after reading the file (see DecodeListFile); "\r\n" is
// normalised here, as parse_text does.
func ParseText(text, defaultSource string) []Entry {
	var groups []*group
	source := defaultSource
	for _, block := range blankLineRE.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1) {
		var g *group
		label, afterLink := "", false
		for _, raw := range strings.Split(block, "\n") {
			line := pyStrip(raw)
			if line == "" || ruleRE.MatchString(line) {
				continue
			}
			if m := sourceRE.FindStringSubmatch(line); m != nil {
				source, g = m[1], nil // label and afterLink deliberately carry over
				continue
			}
			if links := linkRE.FindAllString(line, -1); links != nil {
				inline := cleanInline(linkRE.ReplaceAllLiteralString(line, " "))
				lab := label
				if g == nil || (inline != "" && afterLink && label == "") {
					// a bare link, or a "title | link" row following another link
					g = newGroup(inline, source)
					groups = append(groups, g)
				} else if lab == "" {
					lab = inline
				}
				for _, lk := range links {
					g.links = append(g.links, groupLink{lk, lab})
				}
				label, afterLink = "", true
				continue
			}
			if m := metaRE.FindStringSubmatch(line); m != nil && g != nil {
				// URL: and Page: also land in Info; afterLink is kept, as in Python
				if strings.ToLower(m[1]) == "sku" {
					g.sku = pyStrip(m[2])
				} else {
					g.info = pyStrip(m[2])
				}
				continue
			}
			switch {
			case g != nil && strings.HasSuffix(line, ":"):
				label = pyStrip(line[:len(line)-1]) // names the next link of this listing
			case g == nil || afterLink:
				g = newGroup(line, source) // a new listing starts
				groups = append(groups, g)
				label = ""
			default:
				label = line // free text between title and link
			}
			afterLink = false
		}
	}
	out := make([]Entry, 0, len(groups))
	for _, g := range groups {
		for _, gl := range g.links {
			e := Entry{
				Link: gl.link, Title: g.title, Variant: gl.label,
				Source: g.source, SKU: g.sku, Info: g.info, Also: []string{},
			}
			if e.Title == "" {
				e.Title = fallbackTitle(gl.link)
			}
			if num, unit, end, ok := leadSize(gl.label); ok { // "512GB-Image-Name", "4TB Wii"
				e.Listed = num + strings.ToUpper(unit)
				e.Size = ParseSize(gl.label[:end])
			} else { // the listing's size pins a lone link only
				e.Listed = g.listed
				if len(g.links) == 1 && countSizes(g.listed) == 1 {
					e.Size = ParseSize(g.listed)
				}
			}
			out = append(out, e)
		}
	}
	return dedupe(out)
}

// dedupe keeps the first entry of each link and records the other labels it
// was listed under in that entry's Also.
func dedupe(entries []Entry) []Entry {
	first := make(map[string]int, len(entries))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		i, seen := first[e.Link]
		if !seen {
			first[e.Link] = len(out)
			out = append(out, e)
			continue
		}
		o := &out[i]
		if l := e.Label(); l != o.Label() && !slices.Contains(o.Also, l) {
			o.Also = append(o.Also, l)
		}
	}
	return out
}

// jobTrim is the set job_name strips from the end of names: " .-–—".
const jobTrim = " .-–—"

// JobName is a folder-friendly job name of at most limit characters (callers
// pass 90): a long per-link label (24+ characters) is already a full image
// name; a short one ("Drive S - 2 of 3") is appended to the title. The title is
// what gets shortened, so sibling links never collapse to the same name.
// Lengths count characters, not bytes; a negative limit cuts from the end, as a
// Python slice does.
func JobName(e Entry, limit int) string {
	title := pyStrip(collapseSpace(e.Title))
	v := pyStrip(collapseSpace(e.Variant))
	var name string
	switch n := utf8.RuneCountInString(v); {
	case n >= 24:
		name = pyPrefix(v, limit)
	case n > 0:
		room := max(16, limit-n-3)
		name = strings.TrimRight(pyPrefix(title, room), jobTrim) + " - " + v
	default:
		name = pyPrefix(title, limit)
	}
	if name = strings.TrimRight(name, jobTrim); name != "" {
		return name
	}
	return fallbackTitle(e.Link)
}
