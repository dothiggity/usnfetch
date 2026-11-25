package share

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// v1Link builds an old field-packed usn:// link; wide: the newsgroup's length in two bytes
// (how usnfetch used to read it) rather than one (how the publisher's client writes it).
func v1Link(group, msgid string, withKey, wide bool) string {
	var b bytes.Buffer
	b.WriteByte(1)
	if withKey {
		b.WriteByte(1)
	} else {
		b.WriteByte(0)
	}
	if wide {
		b.WriteByte(byte(len(group) >> 8))
	}
	b.WriteByte(byte(len(group)))
	b.WriteString(group)
	b.WriteByte(byte(len(msgid) >> 8))
	b.WriteByte(byte(len(msgid)))
	b.WriteString(msgid)
	b.Write(bytes.Repeat([]byte{0xab}, 16)) // folder ID
	if withKey {
		b.Write(bytes.Repeat([]byte{0xcd}, 32))
	}
	return "usn://" + base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// Old (v1) links in either layout, and the current (v2) form, all decode to the same link.
func TestDecodeLinkV1BothLayouts(t *testing.T) {
	folder, key := strings.Repeat("ab", 16), strings.Repeat("cd", 32)
	want := "usenet://alt.binaries.test/abc123@news.example/" + folder
	for _, wide := range []bool{false, true} {
		for _, withKey := range []bool{false, true} {
			w := want
			if withKey {
				w += "#" + key
			}
			got, err := DecodeLink(v1Link("alt.binaries.test", "abc123@news.example", withKey, wide))
			if err != nil || got != w {
				t.Errorf("wide=%v key=%v: %q, %v; want %q", wide, withKey, got, err, w)
			}
		}
	}
	v2 := "usn://" + base64.RawURLEncoding.EncodeToString(append([]byte{2}, want...))
	if got, err := DecodeLink(v2); err != nil || got != want {
		t.Errorf("v2: %q, %v", got, err)
	}
	l, err := ParseLink(v1Link("alt.binaries.test", "abc123@news.example", true, false))
	if err != nil || l.Newsgroup != "alt.binaries.test" || l.FolderID != folder || l.KeyHex != key ||
		len(l.ManifestMsgids) != 1 || l.ManifestMsgids[0] != "abc123@news.example" {
		t.Errorf("parsed: %+v, %v", l, err)
	}
	// a link cut short in either layout is refused, not misread
	for _, wide := range []bool{false, true} {
		full := v1Link("alt.binaries.test", "abc123@news.example", true, wide)
		if got, err := DecodeLink(full[:len(full)-50]); err == nil {
			t.Errorf("wide=%v: a cut-short link decoded to %q", wide, got)
		}
	}
}
