package yenc

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 129, 70000, 2_000_000} {
		data := make([]byte, n)
		rand.Read(data)
		got, info, err := Decode(Encode(data, "a b.bin"))
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if !bytes.Equal(got, data) || info.Name != "a b.bin" {
			t.Fatalf("n=%d: mismatch (name %q)", n, info.Name)
		}
	}
}

func TestMultipartAndErrors(t *testing.T) {
	data := []byte("hello yenc world")
	enc := Encode(data, "x")
	// rewrite as a multi-part article with pcrc32
	s := strings.Replace(string(enc), "=ybegin line=128", "=ybegin part=1 total=1 line=128", 1)
	s = strings.Replace(s, "name=x\r\n", "name=x\r\n=ypart begin=1 end=16\r\n", 1)
	s = strings.Replace(s, "crc32=", "part=1 pcrc32=", 1)
	got, info, err := Decode([]byte(s))
	if err != nil || !bytes.Equal(got, data) || info.Begin != 1 || info.End != 16 {
		t.Fatalf("multipart: %v %q %+v", err, got, info)
	}
	bad := bytes.Replace(enc, []byte{data[3] + 42}, []byte{data[3] + 43}, 1)
	if _, _, err := Decode(bad); err == nil {
		t.Fatal("corrupt article accepted")
	}
	if _, _, err := Decode(enc[:len(enc)-30]); err == nil {
		t.Fatal("truncated article accepted")
	}
}

func BenchmarkDecode2MB(b *testing.B) {
	data := make([]byte, 2_000_000)
	rand.Read(data)
	enc := Encode(data, "x")
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if _, _, err := Decode(enc); err != nil {
			b.Fatal(err)
		}
	}
}
