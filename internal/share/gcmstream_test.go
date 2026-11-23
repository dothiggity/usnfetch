package share

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"testing"
)

// The streaming decrypter must agree with crypto/cipher's GCM for every length and any
// way of splitting the ciphertext, and must reject a flipped bit.
func TestGCMStreamMatchesStdlib(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	for _, n := range []int{0, 1, 15, 16, 17, 31, 32, 100, 4095, 70000} {
		pt := make([]byte, n)
		rand.Read(pt)
		nonce := make([]byte, 12)
		rand.Read(nonce)
		sealed := aead.Seal(nil, nonce, pt, nil)
		ct, tag := sealed[:n], sealed[n:]
		for _, chunk := range []int{1, 7, 16, 33, 4096} {
			g, err := newGCMStream(key, nonce, tag)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]byte, 0, n)
			for i := 0; i < n; i += chunk {
				j := min(n, i+chunk)
				buf := make([]byte, j-i)
				g.Decrypt(buf, ct[i:j])
				got = append(got, buf...)
			}
			if err := g.Finish(); err != nil {
				t.Fatalf("n=%d chunk=%d: %v", n, chunk, err)
			}
			if !bytes.Equal(got, pt) {
				t.Fatalf("n=%d chunk=%d: plaintext differs", n, chunk)
			}
		}
		if n > 0 {
			bad := append([]byte(nil), ct...)
			bad[n/2] ^= 1
			g, _ := newGCMStream(key, nonce, tag)
			g.Decrypt(make([]byte, n), bad)
			if g.Finish() == nil {
				t.Fatalf("n=%d: corrupted ciphertext accepted", n)
			}
		}
	}
}
