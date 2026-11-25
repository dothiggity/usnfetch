package engine

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// A hostile share's packed article can't unpack to gigabytes in memory: past maxUnframed,
// Unframe gives up. Anything up to it comes through whole.
func TestUnframeLimit(t *testing.T) {
	gz := func(n int) []byte {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		_, _ = w.Write(make([]byte, n))
		_ = w.Close()
		return b.Bytes()
	}
	zs := func(n int) []byte {
		e, _ := zstd.NewWriter(nil)
		defer e.Close()
		return e.EncodeAll(make([]byte, n), nil)
	}
	for name, enc := range map[string]func(int) []byte{"gzip": gz, "zstd": zs} {
		if out, err := Unframe(enc(1 << 20)); err != nil || len(out) != 1<<20 {
			t.Errorf("%s, 1 MB: %d bytes, %v", name, len(out), err)
		}
		if out, err := Unframe(enc(maxUnframed)); err != nil || len(out) != maxUnframed {
			t.Errorf("%s, right at the limit: %d bytes, %v", name, len(out), err)
		}
		bomb := enc(maxUnframed + 1<<20)
		if len(bomb) > 1<<20 {
			t.Fatalf("%s: the bomb isn't small (%d B)", name, len(bomb))
		}
		if _, err := Unframe(bomb); err == nil {
			t.Errorf("%s: %d KB unpacked past the limit", name, len(bomb)>>10)
		}
	}
	if out, _ := Unframe([]byte("plain")); string(out) != "plain" {
		t.Error("plain data changed")
	}
}
