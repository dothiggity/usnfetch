package catalog

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// DecodeListFile turns the bytes of a link-list file into the text ParseText
// expects, the way Catalog.import_file reads it with
// open(path, encoding="utf-8-sig", errors="replace"): a leading BOM is dropped,
// each maximal invalid UTF-8 subsequence becomes one U+FFFD, and universal
// newlines turn "\r\n" and lone "\r" into "\n".
func DecodeListFile(data []byte) string {
	bom := []byte("\xef\xbb\xbf")
	if len(data) < len(bom) && bytes.HasPrefix(bom, data) {
		return "" // utf-8-sig waits for the rest of a possible BOM and drops it at EOF
	}
	data = bytes.TrimPrefix(data, bom)
	var b strings.Builder
	b.Grow(len(data))
	for len(data) > 0 {
		r, w := utf8.DecodeRune(data)
		if r == utf8.RuneError && w == 1 {
			w = maximalSubpart(data)
			b.WriteRune(utf8.RuneError)
		} else {
			b.Write(data[:w])
		}
		data = data[w:]
	}
	s := strings.ReplaceAll(b.String(), "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// maximalSubpart is the length of the ill-formed sequence at the start of p
// that Python's decoder replaces with a single U+FFFD: a lead byte plus the
// continuation bytes that could still have completed it (Unicode's "maximal
// subpart" practice). Go's decoder would emit one U+FFFD per byte instead.
func maximalSubpart(p []byte) int {
	n, lo, hi := 0, byte(0x80), byte(0xbf) // sequence length, valid range of the second byte
	switch b := p[0]; {
	case b >= 0xc2 && b <= 0xdf:
		n = 2
	case b == 0xe0:
		n, lo = 3, 0xa0
	case b == 0xed:
		n, hi = 3, 0x9f
	case b >= 0xe1 && b <= 0xef:
		n = 3
	case b == 0xf0:
		n, lo = 4, 0x90
	case b == 0xf4:
		n, hi = 4, 0x8f
	case b >= 0xf1 && b <= 0xf3:
		n = 4
	default:
		return 1
	}
	i := 1
	for ; i < n && i < len(p); i++ {
		if c := p[i]; c < lo || c > hi {
			break
		}
		lo, hi = 0x80, 0xbf
	}
	return i
}
