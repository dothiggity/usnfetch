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
		return "", errors.New("not a share link (expected usn://… or usenet://…)")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(link[6:], "="))
	if err != nil {
		return "", fmt.Errorf("share link: bad base64: %w", err)
	}
	if len(data) < 2 {
		return "", errors.New("share link is too short to decode")
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
				return errors.New("truncated v1 share link")
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
	return "", fmt.Errorf("share link uses an unknown encoding (version %d)", data[0])
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
