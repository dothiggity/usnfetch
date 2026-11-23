package share

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// gcmStream decrypts AES-256-GCM ciphertext as it arrives (96-bit nonce, no AAD) and
// checks the tag at the end, so a multi-hundred-MB core index never has to be held in
// memory. Plaintext is handed out before the tag is verified; callers must discard
// everything they built from it if Finish fails (the index builder does).
type gcmStream struct {
	ctr     cipher.Stream
	tagMask [16]byte
	tag     []byte
	table   [16]fieldElement
	y       fieldElement
	partial []byte // < 16 bytes of ciphertext waiting for a full GHASH block
	n       uint64
}

var errTag = errors.New("AES-GCM tag mismatch (corrupt or foreign data)")

func newGCMStream(key, nonce, tag []byte) (*gcmStream, error) {
	if len(nonce) != 12 || len(tag) != 16 {
		return nil, errors.New("gcm: nonce must be 12 bytes and tag 16")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	var h [16]byte
	block.Encrypt(h[:], h[:])
	var j0 [16]byte
	copy(j0[:], nonce)
	j0[15] = 1
	g := &gcmStream{tag: append([]byte(nil), tag...)}
	block.Encrypt(g.tagMask[:], j0[:])
	ctr0 := j0
	ctr0[15] = 2 // inc32(J0)
	g.ctr = cipher.NewCTR(block, ctr0[:])
	x := fieldElement{binary.BigEndian.Uint64(h[:8]), binary.BigEndian.Uint64(h[8:])}
	g.table[reverseBits(1)] = x
	for i := 2; i < 16; i += 2 {
		g.table[reverseBits(i)] = ghashDouble(&g.table[reverseBits(i/2)])
		g.table[reverseBits(i+1)] = ghashAdd(&g.table[reverseBits(i)], &x)
	}
	return g, nil
}

// Decrypt authenticates-in-progress and decrypts ct into dst (len(dst) >= len(ct)).
func (g *gcmStream) Decrypt(dst, ct []byte) {
	g.n += uint64(len(ct))
	data := ct
	if len(g.partial) > 0 {
		need := 16 - len(g.partial)
		if len(data) < need {
			g.partial = append(g.partial, data...)
			g.ctr.XORKeyStream(dst, ct)
			return
		}
		g.partial = append(g.partial, data[:need]...)
		g.update(g.partial)
		g.partial = g.partial[:0]
		data = data[need:]
	}
	full := len(data) &^ 15
	for i := 0; i < full; i += 16 {
		g.update(data[i : i+16])
	}
	if full < len(data) {
		g.partial = append(g.partial, data[full:]...)
	}
	g.ctr.XORKeyStream(dst, ct)
}

// Finish verifies the tag over everything passed to Decrypt.
func (g *gcmStream) Finish() error {
	if len(g.partial) > 0 {
		var b [16]byte
		copy(b[:], g.partial)
		g.update(b[:])
	}
	g.y.high ^= g.n * 8 // low = AAD bits (0), high = ciphertext bits
	ghashMul(&g.table, &g.y)
	var out [16]byte
	binary.BigEndian.PutUint64(out[:8], g.y.low)
	binary.BigEndian.PutUint64(out[8:], g.y.high)
	for i := range out {
		out[i] ^= g.tagMask[i]
	}
	if subtle.ConstantTimeCompare(out[:], g.tag) != 1 {
		return errTag
	}
	return nil
}

func (g *gcmStream) update(block []byte) {
	g.y.low ^= binary.BigEndian.Uint64(block[:8])
	g.y.high ^= binary.BigEndian.Uint64(block[8:])
	ghashMul(&g.table, &g.y)
}

// --- GHASH, 4-bit table method ---
//
// fieldElement, reverseBits, ghashDouble, ghashReduction and ghashMul below are adapted
// from the portable GCM implementation in Go's standard library (crypto/cipher/gcm.go,
// Copyright 2013 The Go Authors), used under Go's BSD-style licence:
//
//   Copyright 2009 The Go Authors.
//
//   Redistribution and use in source and binary forms, with or without
//   modification, are permitted provided that the following conditions are
//   met:
//
//      * Redistributions of source code must retain the above copyright
//   notice, this list of conditions and the following disclaimer.
//      * Redistributions in binary form must reproduce the above
//   copyright notice, this list of conditions and the following disclaimer
//   in the documentation and/or other materials provided with the
//   distribution.
//      * Neither the name of Google LLC nor the names of its
//   contributors may be used to endorse or promote products derived from
//   this software without specific prior written permission.
//
//   THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
//   "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
//   LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
//   A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
//   OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
//   SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
//   LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
//   DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
//   THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
//   (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
//   OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

type fieldElement struct{ low, high uint64 }

func reverseBits(i int) int {
	i = ((i << 2) & 0xc) | ((i >> 2) & 0x3)
	i = ((i << 1) & 0xa) | ((i >> 1) & 0x5)
	return i
}

func ghashAdd(x, y *fieldElement) fieldElement { return fieldElement{x.low ^ y.low, x.high ^ y.high} }

func ghashDouble(x *fieldElement) (d fieldElement) {
	msbSet := x.high&1 == 1
	d.high = x.high >> 1
	d.high |= x.low << 63
	d.low = x.low >> 1
	if msbSet {
		d.low ^= 0xe100000000000000
	}
	return
}

var ghashReduction = []uint16{
	0x0000, 0x1c20, 0x3840, 0x2460, 0x7080, 0x6ca0, 0x48c0, 0x54e0,
	0xe100, 0xfd20, 0xd940, 0xc560, 0x9180, 0x8da0, 0xa9c0, 0xb5e0,
}

func ghashMul(table *[16]fieldElement, y *fieldElement) {
	var z fieldElement
	for i := 0; i < 2; i++ {
		word := y.high
		if i == 1 {
			word = y.low
		}
		for j := 0; j < 64; j += 4 {
			msw := z.high & 0xf
			z.high >>= 4
			z.high |= z.low << 60
			z.low >>= 4
			z.low ^= uint64(ghashReduction[msw]) << 48
			t := table[word&0xf]
			z.low ^= t.low
			z.high ^= t.high
			word >>= 4
		}
	}
	*y = z
}
