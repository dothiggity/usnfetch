// Package share reads usn:// shares: links, manifests and their signatures, folder
// keys, encrypted core indexes, and the per-share SQLite index built from them.
package share

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
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
	case 1: // legacy field-packed v1
		p := 1
		need := func(n int) error {
			if p+n > len(data) {
				return errors.New("this usn:// link is cut short: check that it was copied in full")
			}
			return nil
		}
		if err := need(3); err != nil {
			return "", err
		}
		hasKey := data[p] != 0
		p++
		n := int(binary.BigEndian.Uint16(data[p:]))
		p += 2
		if err := need(n + 2); err != nil {
			return "", err
		}
		group := string(data[p : p+n])
		p += n
		n = int(binary.BigEndian.Uint16(data[p:]))
		p += 2
		if err := need(n + 16); err != nil {
			return "", err
		}
		msgid := string(data[p : p+n])
		p += n
		folder := hex.EncodeToString(data[p : p+16])
		p += 16
		out := fmt.Sprintf("usenet://%s/%s/%s", group, msgid, folder)
		if hasKey {
			if err := need(32); err != nil {
				return "", err
			}
			out += "#" + hex.EncodeToString(data[p:p+32])
		}
		return out, nil
	}
	return "", fmt.Errorf("this usn:// link uses an unknown encoding (version %d): check that it was copied in full", data[0])
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
