package share

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"
)

// Manifest is a share manifest as parsed JSON (numbers kept as json.Number).
type Manifest map[string]any

// ParseManifest decodes manifest JSON.
func ParseManifest(raw []byte) (Manifest, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m Manifest
	if err := d.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest is not JSON: %w", err)
	}
	return m, nil
}

func (m Manifest) Str(k string) string { return str(m[k]) }
func (m Manifest) Int(k string) int64  { return num(m[k]) }
func (m Manifest) Map(k string) map[string]any {
	v, _ := m[k].(map[string]any)
	return v
}
func (m Manifest) List(k string) []any {
	v, _ := m[k].([]any)
	return v
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func num(v any) int64 {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return int64(f)
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case string:
		i, _ := strconv.ParseInt(x, 10, 64)
		return i
	}
	return 0
}

// IsSharded: a v5 (or v4-with-shards) manifest.
func (m Manifest) IsSharded() bool {
	return m.Str("core_index_format") == "sharded" || (m.Str("format") == "core_index" && m.Int("version") >= 5)
}

// AccessMode is public / password / private ("" if absent).
func (m Manifest) AccessMode() string {
	acc := m.Map("access")
	if acc == nil {
		return ""
	}
	if mode := str(acc["mode"]); mode != "" {
		return mode
	}
	if str(acc["folder_key_hex"]) != "" {
		return "public"
	}
	return ""
}

// ErrPassword: the share is password protected and no (or a wrong) password was given.
var ErrPassword = errors.New("this share is password protected")

// FolderKey derives the share's AES key.
func FolderKey(m Manifest, link *Link, password string) ([]byte, error) {
	acc := m.Map("access")
	switch m.AccessMode() {
	case "public":
		return hex.DecodeString(str(acc["folder_key_hex"]))
	case "password":
		if password == "" {
			return nil, ErrPassword
		}
		kdf, _ := acc["kdf"].(map[string]any)
		iters := int(num(kdf["iters"]))
		if iters <= 0 {
			iters = 100000
		}
		salt, err := hex.DecodeString(str(acc["salt_hex"]))
		if err != nil {
			return nil, err
		}
		pk := pbkdf2.Key([]byte(password), salt, iters, 32, sha256.New)
		wrapped, err := base64.StdEncoding.DecodeString(str(acc["wrapped_folder_key_b64"]))
		if err != nil {
			return nil, err
		}
		key, err := OpenEnvelope(wrapped, pk)
		if err != nil {
			return nil, fmt.Errorf("%w (wrong password?)", ErrPassword)
		}
		return key, nil
	case "private":
		return nil, errors.New("private shares need the recipient's identity key; not supported")
	}
	if link.KeyHex != "" { // legacy v3 links carry the key in the fragment
		return hex.DecodeString(link.KeyHex)
	}
	return nil, fmt.Errorf("cannot derive the folder key for access mode %q", m.AccessMode())
}

// OpenEnvelope decrypts nonce(12) || tag(16) || ciphertext.
func OpenEnvelope(blob, key []byte) ([]byte, error) {
	if len(blob) < 28 {
		return nil, errors.New("encrypted blob too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, _ := cipher.NewGCM(block)
	ct := make([]byte, 0, len(blob)-12)
	ct = append(append(ct, blob[28:]...), blob[12:28]...)
	return aead.Open(nil, blob[:12], ct, nil)
}

// ---------------------------------------------------------------------------
// signatures (v4 / v5 manifests, Ed25519 over a binary to-be-signed string)
// ---------------------------------------------------------------------------

var sigDomain = []byte("usenetsync:manifest-sig:v1")
var modeByte = map[string]byte{"public": 0, "password": 1, "private": 2}

// SignatureError: the manifest's signature or owner pin does not check out.
type SignatureError struct{ Msg string }

func (e *SignatureError) Error() string { return e.Msg }
func sigErr(f string, a ...any) error   { return &SignatureError{fmt.Sprintf(f, a...)} }

func be32(n int64) []byte     { var b [4]byte; binary.BigEndian.PutUint32(b[:], uint32(n)); return b[:] }
func be64(n int64) []byte     { var b [8]byte; binary.BigEndian.PutUint64(b[:], uint64(n)); return b[:] }
func withLen(b []byte) []byte { return append(be32(int64(len(b))), b...) }

func parseOwnerKey(p string) (ed25519.PublicKey, error) {
	blk, _ := pem.Decode([]byte(p))
	if blk == nil {
		return nil, errors.New("the owner key is not a PEM block")
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("the owner key is not an Ed25519 key")
	}
	return pub, nil
}

// hashAccess = SHA-256 of Python's json.dumps(access, separators=(",",":"), sort_keys=True).
func hashAccess(access any) []byte {
	var b bytes.Buffer
	pyJSON(&b, access)
	h := sha256.Sum256(b.Bytes())
	return h[:]
}

// corePartsBytes is the v4 to-be-signed encoding of a core-index part list.
func corePartsBytes(parts []any) []byte {
	ps := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		if m, ok := p.(map[string]any); ok {
			ps = append(ps, m)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return num(ps[i]["part_number"]) < num(ps[j]["part_number"]) })
	out := be32(int64(len(ps)))
	for _, p := range ps {
		out = append(out, be32(num(p["part_number"]))...)
		out = append(out, withLen([]byte(str(p["primary_msgid"])))...)
		red, _ := p["redundancy_msgids"].([]any)
		out = append(out, be32(int64(len(red)))...)
		for _, r := range red {
			out = append(out, withLen([]byte(str(r)))...)
		}
		out = append(out, be64(num(p["size"]))...)
		if p["nonce"] != nil && p["tag"] != nil {
			n, _ := hex.DecodeString(str(p["nonce"]))
			t, _ := hex.DecodeString(str(p["tag"]))
			out = append(out, 1)
			out = append(out, withLen(n)...)
			out = append(out, withLen(t)...)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

// shardSetHash = SHA256("USSSR1\0" || u64(n) || XOR of SHA256(u32(shard_index) || digest)).
func shardSetHash(shards []any) (string, error) {
	var acc [32]byte
	seen := map[int64]bool{}
	for _, s := range shards {
		m, _ := s.(map[string]any)
		i := num(m["shard_index"])
		if seen[i] {
			return "", fmt.Errorf("shard %d is listed twice", i)
		}
		seen[i] = true
		dig, err := hex.DecodeString(str(m["digest"]))
		if err != nil {
			return "", err
		}
		tok := sha256.Sum256(append(be32(i), dig...))
		for k := range acc {
			acc[k] ^= tok[k]
		}
	}
	h := sha256.Sum256(append(append([]byte("USSSR1\x00"), be64(int64(len(seen)))...), acc[:]...))
	return hex.EncodeToString(h[:]), nil
}

// VerifyManifestSignature checks a v4/v5 manifest's Ed25519 signature and the owner pin
// carried by the link. Returns the owner fingerprint.
func VerifyManifestSignature(m Manifest, linkFP string) (string, error) {
	sig := m.Map("sig")
	if sig == nil || str(sig["alg"]) != "ed25519" || str(sig["signature"]) == "" {
		return "", sigErr("manifest has no Ed25519 signature")
	}
	pub, err := parseOwnerKey(m.Str("owner_public_key"))
	if err != nil {
		return "", sigErr("owner public key unreadable: %v", err)
	}
	sum := sha256.Sum256(pub)
	fp := hex.EncodeToString(sum[:])
	if linkFP != "" && !strings.HasPrefix(fp, strings.ToLower(strings.TrimSpace(linkFP))) {
		return "", sigErr("publisher key mismatch: this link is pinned to a different key than the one that signed the manifest")
	}
	if strings.ToLower(strings.TrimSpace(m.Str("owner_key_fingerprint"))) != fp {
		return "", sigErr("the manifest's stated key fingerprint does not belong to its own public key")
	}
	access := m.Map("access")
	code, ok := modeByte[str(access["mode"])]
	if !ok {
		return "", sigErr("unknown access mode %q", str(access["mode"]))
	}
	adig := hashAccess(access)
	head := withLen(pub)
	head = append(head, withLen([]byte(m.Str("share_id")))...)
	head = append(head, withLen([]byte(m.Str("folder_id")))...)
	head = append(head, be32(m.Int("folder_version"))...)
	head = append(head, withLen([]byte(m.Str("prev_version_message_id")))...)
	head = append(head, withLen([]byte(m.Str("created_at")))...)
	head = append(head, code)
	head = append(head, adig...)
	tv := num(sig["tbs_version"])
	var tbs []byte
	switch m.Str("format") {
	case "core_index": // v5
		if tv != 1 {
			return "", sigErr("unsupported v5 tbs_version %d", tv)
		}
		root := strings.ToLower(m.Str("shards_root"))
		if shards := m.List("shards"); shards != nil {
			r, err := shardSetHash(shards)
			if err != nil {
				return "", sigErr("%v", err)
			}
			if r != root {
				return "", sigErr("the listed shards do not add up to the manifest's shard hash")
			}
		}
		if strings.ToLower(str(sig["shards_root"])) != root {
			return "", sigErr("the signed shard hash differs from the manifest's")
		}
		rb, err := hex.DecodeString(root)
		if err != nil || len(rb) != 32 {
			return "", sigErr("shards_root is not 32-byte hex")
		}
		tbs = append(append(append([]byte{}, sigDomain...), 5, 1), head...)
		tbs = append(tbs, rb...)
		tbs = append(tbs, be32(m.Int("num_shards"))...)
		tbs = append(tbs, withLen([]byte(m.Str("shard_key")))...)
		tbs = append(tbs, withLen([]byte(m.Str("folder_digest")))...)
	case "share_v4_manifest":
		var tail []byte
		switch tv {
		case 1:
			parts, _ := m.Map("core_index")["parts"].([]any)
			if len(parts) == 0 {
				return "", sigErr("this v4 manifest signs its index parts but lists none")
			}
			tail = corePartsBytes(parts)
		case 2:
			tail, err = hex.DecodeString(str(sig["core_index_root"]))
			if err != nil || len(tail) != 32 {
				return "", sigErr("the signed index hash is not 32 bytes")
			}
		default:
			return "", sigErr("unsupported v4 tbs_version %d", tv)
		}
		tbs = append(append(append([]byte{}, sigDomain...), 4, byte(tv)), head...)
		tbs = append(tbs, tail...)
	default:
		return "", sigErr("no signature scheme for manifest format %q", m.Str("format"))
	}
	signature, err := base64.StdEncoding.DecodeString(str(sig["signature"]))
	if err != nil || !ed25519.Verify(pub, tbs, signature) {
		return "", sigErr("signature does not verify: the manifest was changed, or signed with a different key")
	}
	if d, ok := sig["access_digest"]; ok && strings.ToLower(str(d)) != hex.EncodeToString(adig) {
		return "", sigErr("the signed access hash differs from the manifest's access settings")
	}
	return fp, nil
}

// CheckSignature: "verified", or "unsigned" for formats that predate signing. Anything
// wrong with a signed format is an error (no silent downgrade).
func CheckSignature(m Manifest, link *Link) (string, error) {
	switch m.Str("format") {
	case "share_v4_manifest", "core_index":
	default:
		return "unsigned", nil
	}
	if link.OwnerFP == "" {
		return "", errors.New("this share's manifest is signed, but the link carries no owner fingerprint to check it against; ask for the full share link")
	}
	if _, err := VerifyManifestSignature(m, link.OwnerFP); err != nil {
		return "", fmt.Errorf("manifest signature check failed: %w", err)
	}
	return "verified", nil
}

// pyJSON writes v exactly as Python's json.dumps(v, separators=(",", ":"), sort_keys=True)
// would (ensure_ascii: non-ASCII as \uXXXX, '/' and <>& unescaped).
func pyJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		b.WriteString(x.String())
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		pyString(b, x)
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			pyJSON(b, e)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // UTF-8 byte order == code point order == Python's sort
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			pyString(b, k)
			b.WriteByte(':')
			pyJSON(b, x[k])
		}
		b.WriteByte('}')
	default:
		pyString(b, fmt.Sprint(x))
	}
}

func pyString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r < 0x20 || (r >= 0x7f && r < 0x10000):
				if r == 0x7f { // DEL is printable ASCII to Python's encoder
					b.WriteRune(r)
				} else {
					fmt.Fprintf(b, `\u%04x`, r)
				}
			case r >= 0x10000:
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			case r == utf8.RuneError:
				fmt.Fprintf(b, `\u%04x`, r)
			default:
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
