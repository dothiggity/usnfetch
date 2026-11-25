package catalog

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// An old (v1) link the way the publisher's client writes it (the newsgroup's length in one
// byte) is named after its folder ID, as one in usnfetch's old two-byte reading is.
func TestFallbackTitleV1OneByteGroup(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{1, 0, byte(len("alt.binaries.test"))})
	b.WriteString("alt.binaries.test")
	b.Write([]byte{0, byte(len("abc123@news.example"))})
	b.WriteString("abc123@news.example")
	b.Write(bytes.Repeat([]byte{0xab}, 16))
	link := "usn://" + base64.RawURLEncoding.EncodeToString(b.Bytes())
	if got := FallbackTitle(link); got != "USN_abababababab" {
		t.Errorf("title %q", got)
	}
}
