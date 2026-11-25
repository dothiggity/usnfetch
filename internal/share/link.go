// Package share reads usn:// shares: links, manifests and their signatures, folder keys,
// encrypted core indexes, and the per-share SQLite index built from them.
package share

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Link is a parsed share link.
type Link struct {
	Canonical      string   // the usenet://… form
	Newsgroup      string   // "" when the link names none
	FolderID       string   // the share's folder
	ManifestMsgids []string // the manifest article, then copies of it
	OwnerFP        string   // the publisher key the manifest must be signed with (hex), "" if the link pins none
	KeyHex         string   // key after '#' on old links, "" if absent
}

// lowerHex reports whether s is lower-case hex of one of the given lengths.
func lowerHex(s string, lengths ...int) bool {
	ok := false
	for _, n := range lengths {
		ok = ok || len(s) == n
	}
	if !ok {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// DecodeLink turns usn:// (wire v1 or v2) into the canonical usenet:// form.
func DecodeLink(link string) (string, error) {
	link = strings.TrimSpace(link)
	if strings.HasPrefix(link, "usenet://") {
		return link, nil
	}
	if !strings.HasPrefix(link, "usn://") {
		return "", errors.New("not a share link: share links start with usn:// (or usenet://)")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(link[6:], "="))
	if err != nil {
		return "", errors.New("this isn't a complete usn:// link: check that it was copied in full")
	}
	if len(data) < 2 {
		return "", errors.New("this usn:// link is too short: check that it was copied in full")
	}
	switch data[0] {
	case 2:
		out := string(data[1:])
		if !strings.HasPrefix(out, "usenet://") {
			return "", errors.New("share link decodes to something other than usenet://")
		}
		return out, nil
	case 1: // the old field-packed form
		if out, ok := DecodeV1(data, false); ok {
			return out, nil
		}
		return "", errors.New("this usn:// link is cut short: check that it was copied in full")
	}
	return "", fmt.Errorf("this usn:// link uses an unknown encoding (version %d): check that it was copied in full", data[0])
}

// DecodeV1 reads the old field-packed usn:// payload (wire v1): 1, then whether it carries a
// key, the newsgroup's length and the newsgroup, the message ID's length (2 bytes) and the
// message ID, the 16-byte folder ID and, if it has one, the 32-byte key. The publisher's client
// gives the newsgroup's length one byte; usnfetch used to read two. Both are tried, and the one
// that accounts for every byte of the link wins. strict: only such an exact fit; otherwise a
// link with bytes left over is read too (one-byte first, as the publisher's client does).
func DecodeV1(data []byte, strict bool) (string, bool) {
	for _, exact := range []bool{true, false} {
		if !exact && strict {
			break
		}
		for _, wide := range []bool{false, true} {
			if out, ok := v1Layout(data, wide, exact); ok {
				return out, true
			}
		}
	}
	return "", false
}

// v1Layout reads a v1 payload with a one-byte (or, wide, two-byte) newsgroup length.
func v1Layout(data []byte, wide, exact bool) (string, bool) {
	if len(data) < 3 || data[0] != 1 {
		return "", false
	}
	hasKey := data[1] != 0
	p := 2
	var n int
	if wide {
		if p+2 > len(data) {
			return "", false
		}
		n = int(binary.BigEndian.Uint16(data[p:]))
		p += 2
	} else {
		n = int(data[p])
		p++
	}
	if p+n+2 > len(data) {
		return "", false
	}
	group := data[p : p+n]
	p += n
	m := int(binary.BigEndian.Uint16(data[p:]))
	p += 2
	if p+m+16 > len(data) {
		return "", false
	}
	msgid := data[p : p+m]
	p += m
	folder := data[p : p+16]
	p += 16
	var key []byte
	if hasKey {
		if p+32 > len(data) {
			return "", false
		}
		key = data[p : p+32]
		p += 32
	}
	if exact && p != len(data) || !utf8.Valid(group) || !utf8.Valid(msgid) || !strings.Contains(string(msgid), "@") {
		return "", false
	}
	out := fmt.Sprintf("usenet://%s/%s/%s", group, msgid, hex.EncodeToString(folder))
	if hasKey {
		out += "#" + hex.EncodeToString(key)
	}
	return out, true
}

// LinkKey: what tells shares apart, their manifest article and folder ID. Links with the
// same key are one share written differently: usenet:// for usn://, or cut short after the
// folder ID, as forum line-wraps do. "" when the link doesn't get that far.
func LinkKey(link string) string {
	link = strings.TrimSpace(link)
	canonical, err := DecodeLink(link)
	if t := strings.TrimRight(link, "="); err != nil && strings.HasPrefix(t, "usn://") && len(t[6:])%4 == 1 {
		canonical, err = DecodeLink(t[:len(t)-1]) // cut at a length base64 can't have: drop the odd character
	}
	if err != nil {
		return ""
	}
	body, _, _ := strings.Cut(canonical[len("usenet://"):], "#")
	var segs []string
	for _, s := range strings.Split(body, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) > 0 && !strings.Contains(segs[0], "@") {
		segs = segs[1:] // the newsgroup
	}
	if len(segs) < 2 {
		return ""
	}
	first, _, _ := strings.Cut(segs[0], "+")
	return strings.Trim(strings.TrimSpace(first), "<>") + "/" + strings.ToLower(segs[1])
}

// ParseLink parses usn:// and usenet:// share links.
func ParseLink(link string) (*Link, error) {
	canonical, err := DecodeLink(link)
	if err != nil {
		return nil, err
	}
	body, frag, _ := strings.Cut(canonical[len("usenet://"):], "#")
	var segs []string
	for _, s := range strings.Split(body, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return nil, errors.New("share link is empty after usenet://")
	}
	l := &Link{Canonical: canonical}
	if !strings.Contains(segs[0], "@") {
		l.Newsgroup, segs = segs[0], segs[1:]
	}
	if len(segs) < 2 {
		return nil, errors.New("share link lacks the folder ID")
	}
	for _, m := range strings.Split(segs[0], "+") {
		if m = strings.Trim(strings.TrimSpace(m), "<>"); m != "" {
			l.ManifestMsgids = append(l.ManifestMsgids, m)
		}
	}
	l.FolderID = segs[1]
	if len(segs) > 2 {
		if fp := strings.ToLower(segs[2]); lowerHex(fp, 32, 64) {
			l.OwnerFP = fp
		}
	}
	if f := strings.ToLower(frag); lowerHex(f, 64) {
		l.KeyHex = f
	}
	return l, nil
}
