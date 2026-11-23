package catalog

// Python str semantics that the Go standard library does not reproduce exactly.
// The parser is a port of usnfetch_catalog.py, whose regexes and string methods
// are Unicode-aware in ways Go's RE2 and strings package are not:
//
//   - Python's `\s`, str.strip() and str.split() whitespace is str.isspace(): it
//     includes \x1c-\x1f, which neither Go's `\s` (ASCII only, and without \v)
//     nor unicode.IsSpace include.
//   - Python's `\d` is any Unicode decimal digit and float() accepts them.
//   - Python's `\b` uses Unicode \w (letters, numbers, underscore).
//   - Python's str.lower() applies the full case mapping (İ -> "i̇") and the
//     final-sigma rule.
//   - Python's html.unescape differs from html.UnescapeString on numeric
//     references.

import (
	"html"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pySpaceClass is the body of a regexp character class matching exactly the
// characters Python considers whitespace (str.isspace(), and `\s` in a str pattern).
const pySpaceClass = `\x{9}-\x{d}\x{1c}-\x{20}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

// pyIsSpace reports whether r is whitespace for Python's str.isspace().
func pyIsSpace(r rune) bool {
	if r <= 0x20 {
		return (r >= 0x9 && r <= 0xd) || r >= 0x1c
	}
	switch r {
	case 0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// pyStrip is Python's str.strip() with no arguments.
func pyStrip(s string) string { return strings.TrimFunc(s, pyIsSpace) }

// collapseSpace is Python's re.sub(r"\s+", " ", s).
func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		if pyIsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
		} else {
			b.WriteString(s[i : i+w]) // copy bytes as-is (keeps invalid UTF-8 untouched)
			inSpace = false
		}
		i += w
	}
	return b.String()
}

// pyIsWord reports whether r is a `\w` character in a Python str pattern
// (str.isalnum() or "_"); used to emulate Python's `\b`.
func pyIsWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// asciiDigits rewrites a `\d+(\.\d+)?` match with ASCII digits so that
// strconv.ParseFloat accepts it the way Python's float() accepts any Unicode
// decimal digit ("١٢" == 12).
func asciiDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < utf8.RuneSelf {
			b.WriteRune(r)
			continue
		}
		// Unicode encodes decimal digits in contiguous runs of ten starting at
		// zero, so a digit's value is its distance from the start of the run.
		lo := r
		for unicode.Is(unicode.Nd, lo-1) {
			lo--
		}
		b.WriteByte(byte('0' + (r-lo)%10))
	}
	return b.String()
}

// pyPrefix is Python's s[:n] on code points, including negative n.
func pyPrefix(s string, n int) string {
	if n < 0 {
		if n += utf8.RuneCountInString(s); n < 0 {
			n = 0
		}
	}
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}

// pyLower is Python's str.lower(): the simple lowercase mapping plus the two
// unconditional full mappings Python applies (U+0130 -> "i̇" and the
// final-sigma rule for U+03A3).
func pyLower(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range rs {
		switch r {
		case 0x130:
			b.WriteString("i̇")
		case 0x3a3:
			if finalSigma(rs, i) {
				b.WriteRune(0x3c2)
			} else {
				b.WriteRune(0x3c3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// finalSigma mirrors CPython's handle_capital_sigma: a capital sigma lowercases
// to final ς when preceded by a cased letter and not followed by one, skipping
// case-ignorable characters in both directions.
func finalSigma(rs []rune, i int) bool {
	j := i - 1
	for j >= 0 && isCaseIgnorable(rs[j]) {
		j--
	}
	if j < 0 || !isCased(rs[j]) {
		return false
	}
	j = i + 1
	for j < len(rs) && isCaseIgnorable(rs[j]) {
		j++
	}
	return j == len(rs) || !isCased(rs[j])
}

// isCased is the Unicode Cased property (Lu, Ll, Lt, Other_Lowercase, Other_Uppercase).
func isCased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// isCaseIgnorable is the Unicode Case_Ignorable property: Mn, Me, Cf, Lm, Sk and
// the Word_Break MidLetter / MidNumLet / Single_Quote characters.
func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', 0xb7, 0x387, 0x55f, 0x5f4, 0x2018, 0x2019, 0x2024, 0x2027,
		0xfe13, 0xfe52, 0xfe55, 0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// pyUnescape is Python's html.unescape. Named references are delegated to
// html.UnescapeString (same HTML5 table and longest-legacy-prefix rule); numeric
// references follow Python, which unlike Go drops invalid code points
// (&#1; -> ""), maps out-of-range values to U+FFFD and never overflows.
func pyUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return charrefRE.ReplaceAllStringFunc(s, replaceCharref)
}

func replaceCharref(m string) string {
	ref := m[1:]
	if ref[0] != '#' {
		// Python matches up to 32 name characters; Go stops at the first
		// non-alphanumeric one, but since every entity name is alphanumeric
		// both end up at the same full name or longest legacy prefix.
		return html.UnescapeString(m)
	}
	digits, base := ref[1:], 10
	if digits[0] == 'x' || digits[0] == 'X' {
		digits, base = digits[1:], 16
	}
	digits = strings.TrimRight(digits, ";")
	num := 0
	for i := 0; i < len(digits) && num <= 0x10ffff; i++ { // saturate: only "> 0x10FFFF" matters
		c := digits[i]
		var d int
		switch {
		case c <= '9':
			d = int(c - '0')
		case c >= 'a':
			d = int(c-'a') + 10
		default:
			d = int(c-'A') + 10
		}
		num = num*base + d
	}
	if r, ok := invalidCharrefs[num]; ok {
		return r
	}
	if (num >= 0xd800 && num <= 0xdfff) || num > 0x10ffff {
		return "�"
	}
	if isInvalidCodepoint(num) {
		return ""
	}
	return string(rune(num))
}

// invalidCharrefs is html._invalid_charrefs: numeric references that the HTML5
// spec remaps (NUL, CR and the Windows-1252 range 0x80-0x9F).
var invalidCharrefs = map[int]string{
	0x00: "�", 0x0d: "\r",
	0x80: "€", 0x81: "\u0081", 0x82: "‚", 0x83: "ƒ", 0x84: "„", 0x85: "…",
	0x86: "†", 0x87: "‡", 0x88: "ˆ", 0x89: "‰", 0x8a: "Š", 0x8b: "‹",
	0x8c: "Œ", 0x8d: "\u008d", 0x8e: "Ž", 0x8f: "\u008f", 0x90: "\u0090", 0x91: "‘",
	0x92: "’", 0x93: "“", 0x94: "”", 0x95: "•", 0x96: "–", 0x97: "—",
	0x98: "˜", 0x99: "™", 0x9a: "š", 0x9b: "›", 0x9c: "œ", 0x9d: "\u009d",
	0x9e: "ž", 0x9f: "Ÿ",
}

// isInvalidCodepoint is membership of html._invalid_codepoints (references that
// Python's unescape removes): C0/C1 controls except tab, LF, FF, CR, the
// noncharacters U+FDD0-U+FDEF and U+xxFFFE/U+xxFFFF.
func isInvalidCodepoint(n int) bool {
	switch {
	case n >= 0x1 && n <= 0x8, n == 0xb, n >= 0xe && n <= 0x1f, n >= 0x7f && n <= 0x9f,
		n >= 0xfdd0 && n <= 0xfdef:
		return true
	}
	return n&0xfffe == 0xfffe && n <= 0x10ffff
}
