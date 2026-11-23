package catalog

// A minimal share-link decoder, just enough to name an untitled entry after its
// share's folder id. It follows decode_share_link / parse_share_link in
// usnfetch.py, including Python's lenient base64 decoding, so that a link either
// yields the same folder id or fails in the same cases.

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// fallbackTitle is _fallback_title: "USN_" + the first 12 characters of the
// share's folder id, or the first 24 characters of the link when it does not parse.
func fallbackTitle(link string) string {
	if id, ok := shareFolderID(link); ok {
		return "USN_" + pyPrefix(id, 12)
	}
	return pyPrefix(link, 24)
}

// FallbackTitle is the title an entry gets when its listing names none.
func FallbackTitle(link string) string { return fallbackTitle(link) }

// shareFolderID extracts the folder id from a share link. In its usenet:// form the path is
// an optional group, the manifest article ids joined by '+', the folder id, then optionally
// the publisher fingerprint, with an optional key after '#'.
func shareFolderID(link string) (string, bool) {
	canonical, ok := decodeShareLink(link)
	if !ok {
		return "", false
	}
	body, _, _ := strings.Cut(canonical[len("usenet://"):], "#")
	var segs []string
	for _, s := range strings.Split(body, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return "", false
	}
	if !strings.Contains(segs[0], "@") { // the first segment is a newsgroup unless it is a msgid
		segs = segs[1:]
	}
	if len(segs) < 2 {
		return "", false
	}
	return segs[1], true
}

// decodeShareLink turns usn:// (unpadded base64url of a wire-format payload)
// into its usenet:// text; usenet:// links are returned as they are.
func decodeShareLink(link string) (string, bool) {
	link = pyStrip(link)
	if strings.HasPrefix(link, "usenet://") {
		return link, true
	}
	if !strings.HasPrefix(link, "usn://") {
		return "", false
	}
	b := link[len("usn://"):]
	data, ok := pyB64Decode(b + strings.Repeat("=", (4-len(b)%4)%4))
	if !ok || len(data) < 2 {
		return "", false
	}
	switch data[0] {
	case 2: // v2: the literal usenet:// text
		if !utf8.Valid(data[1:]) || !strings.HasPrefix(string(data[1:]), "usenet://") {
			return "", false
		}
		return string(data[1:]), true
	case 1: // v1: [has_key][u16 len][newsgroup][u16 len][msgid][16 bytes folder id][32 bytes key]
		hasKey := data[1] != 0
		group, pos, ok := readField(data, 2)
		if !ok {
			return "", false
		}
		msgid, pos, ok := readField(data, pos)
		if !ok {
			return "", false
		}
		// Python slices past the end silently, so short payloads give short ids.
		out := "usenet://" + group + "/" + msgid + "/" + hex.EncodeToString(clampSlice(data, pos, pos+16))
		if hasKey {
			out += "#" + hex.EncodeToString(clampSlice(data, pos+16, pos+48))
		}
		return out, true
	}
	return "", false
}

// readField reads a big-endian u16 length and that many bytes of UTF-8 text at
// pos. Like Python's struct.unpack the length needs two bytes, while the text,
// like a Python slice, is cut short at the end of data.
func readField(data []byte, pos int) (string, int, bool) {
	if pos+2 > len(data) {
		return "", pos, false
	}
	n := int(data[pos])<<8 | int(data[pos+1])
	pos += 2
	text := clampSlice(data, pos, pos+n)
	if !utf8.Valid(text) {
		return "", pos, false
	}
	return string(text), pos + n, true
}

// clampSlice is Python's data[lo:hi] for 0 <= lo <= hi.
func clampSlice(data []byte, lo, hi int) []byte {
	lo, hi = min(lo, len(data)), min(hi, len(data))
	return data[lo:hi]
}

// pyB64Decode is base64.urlsafe_b64decode, i.e. binascii.a2b_base64 in its
// default non-strict mode after mapping "-_" to "+/": characters outside the
// alphabet are skipped, a pad sequence completing a quad ends the input (data
// after it is ignored), stray "=" elsewhere are skipped, and a dangling partial
// quad is an error. Non-ASCII input is an error, as it is for Python.
func pyB64Decode(s string) ([]byte, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return nil, false
		}
	}
	out := make([]byte, 0, len(s)*3/4)
	quad, pads := 0, 0
	var left byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '=' {
			if quad >= 2 {
				if pads++; quad+pads >= 4 {
					return out, true
				}
			}
			continue
		}
		v := b64Value(c)
		if v < 0 {
			continue
		}
		pads = 0
		switch quad {
		case 0:
			left = byte(v)
		case 1:
			out = append(out, left<<2|byte(v)>>4)
			left = byte(v) & 0x0f
		case 2:
			out = append(out, left<<4|byte(v)>>2)
			left = byte(v) & 0x03
		case 3:
			out = append(out, left<<6|byte(v))
		}
		quad = (quad + 1) % 4
	}
	if quad != 0 {
		return nil, false
	}
	return out, true
}

// b64Value maps a base64 character to its value; both the standard ("+/") and
// URL-safe ("-_") forms are accepted, as they are after Python's translation.
func b64Value(c byte) int {
	switch {
	case c >= 'A' && c <= 'Z':
		return int(c - 'A')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 26
	case c >= '0' && c <= '9':
		return int(c-'0') + 52
	case c == '+' || c == '-':
		return 62
	case c == '/' || c == '_':
		return 63
	}
	return -1
}
