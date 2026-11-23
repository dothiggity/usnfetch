package share

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// PoolEntry is one msgid_pool entry: a primary {m,n,t,s}, a redundancy copy {m,ref},
// or a bare message-id string (oldest shares).
type PoolEntry struct {
	Msgid, Nonce, Tag string
	Size              int64
	Ref               *int64
	Bare              bool
}

type rawPool struct {
	M         string      `json:"m"`
	MessageID string      `json:"message_id"`
	N         string      `json:"n"`
	NonceL    string      `json:"nonce"`
	T         string      `json:"t"`
	TagL      string      `json:"tag"`
	S         json.Number `json:"s"`
	SizeL     json.Number `json:"size"`
	Ref       *json.Number `json:"ref"`
}

func (p *PoolEntry) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		p.Bare = true
		return json.Unmarshal(b, &p.Msgid)
	}
	var r rawPool
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	p.Msgid = firstNonEmpty(r.M, r.MessageID)
	if r.Ref != nil {
		v := num(*r.Ref)
		p.Ref = &v
		return nil
	}
	p.Nonce, p.Tag = firstNonEmpty(r.N, r.NonceL), firstNonEmpty(r.T, r.TagL)
	p.Size = num(firstNum(r.S, r.SizeL))
	return nil
}

// FileRec is a files[] record (long or short keys).
type FileRec struct {
	FileID, Path, Hash string
	Size               int64
	Segments           []int64
	SegRange           *[3]int64
}

type rawFile struct {
	FileID   any          `json:"file_id"`
	I        any          `json:"i"`
	Path     string       `json:"path"`
	P        string       `json:"p"`
	Name     string       `json:"name"`
	N        string       `json:"n"`
	Size     json.Number  `json:"size"`
	S        json.Number  `json:"s"`
	Hash     *string      `json:"hash"`
	H        *string      `json:"h"`
	Segments []int64      `json:"segments"`
	Sg       []int64      `json:"sg"`
	SegRange *struct {
		Start  json.Number `json:"start"`
		Stride json.Number `json:"stride"`
		Count  json.Number `json:"count"`
	} `json:"seg_range"`
}

func (f *FileRec) UnmarshalJSON(b []byte) error {
	var r rawFile
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	f.FileID = anyStr(r.FileID)
	if f.FileID == "" {
		f.FileID = anyStr(r.I)
	}
	f.Path = firstNonEmpty(r.Path, r.P, r.Name, r.N)
	f.Size = num(firstNum(r.Size, r.S))
	if r.Hash != nil {
		f.Hash = *r.Hash
	} else if r.H != nil {
		f.Hash = *r.H
	}
	if r.SegRange != nil {
		f.SegRange = &[3]int64{num(r.SegRange.Start), num(r.SegRange.Stride), num(r.SegRange.Count)}
		if f.SegRange[1] == 0 {
			f.SegRange[1] = 1
		}
	} else if r.Segments != nil {
		f.Segments = r.Segments
	} else {
		f.Segments = r.Sg
	}
	return nil
}

// PackMember is one file inside a packed segment.
type PackMember struct {
	FileID, Path, Hash string
	Size               int64
	HasMeta            bool // listed with metadata (files[]), not just by id (file_ids[])
}

// PackRec is a packed_segments[] entry.
type PackRec struct {
	PoolIndex  int64
	Nonce, Tag string
	Size       int64
	Members    []PackMember
}

func (p *PackRec) UnmarshalJSON(b []byte) error {
	var r struct {
		SegmentID      any          `json:"segment_id"`
		MsgidPoolIndex *json.Number `json:"msgid_pool_index"`
		Nonce          string       `json:"encryption_nonce"`
		Tag            string       `json:"encryption_tag"`
		Size           json.Number  `json:"size"`
		Files          []struct {
			FileID any         `json:"file_id"`
			Path   string      `json:"path"`
			Name   string      `json:"name"`
			Size   json.Number `json:"size"`
			Hash   *string     `json:"hash"`
		} `json:"files"`
		FileIDs []any `json:"file_ids"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.MsgidPoolIndex == nil {
		return fmt.Errorf("packed segment %v has no msgid_pool_index", r.SegmentID)
	}
	p.PoolIndex, p.Nonce, p.Tag, p.Size = num(*r.MsgidPoolIndex), r.Nonce, r.Tag, num(r.Size)
	for _, m := range r.Files {
		pm := PackMember{FileID: anyStr(m.FileID), Path: firstNonEmpty(m.Path, m.Name), Size: num(m.Size), HasMeta: true}
		if m.Hash != nil {
			pm.Hash = *m.Hash
		}
		p.Members = append(p.Members, pm)
	}
	for _, id := range r.FileIDs {
		p.Members = append(p.Members, PackMember{FileID: anyStr(id)})
	}
	return nil
}

// Doc is a parsed core-index document, or a batch of one (PoolStart = position of its
// first pool entry in the whole document).
type Doc struct {
	SegSize   int64
	PoolStart int64
	Pool      []PoolEntry
	Files     []FileRec
	Packs     []PackRec
}

func (d *Doc) empty() bool { return len(d.Pool) == 0 && len(d.Files) == 0 && len(d.Packs) == 0 }

// ParseDoc reads a core-index document in one streaming pass and hands it out in
// batches of about `batch` items (batch <= 0: one Doc for the whole document).
func ParseDoc(r io.Reader, batch int, emit func(*Doc) error) error {
	d := json.NewDecoder(r)
	d.UseNumber()
	if err := expectDelim(d, '{'); err != nil {
		return err
	}
	cur, poolN, gotPool := &Doc{}, int64(0), false
	flush := func() error {
		if cur.empty() && cur.SegSize == 0 {
			return nil
		}
		if err := emit(cur); err != nil {
			return err
		}
		cur = &Doc{PoolStart: poolN}
		return nil
	}
	full := func() bool { return batch > 0 && len(cur.Pool)+len(cur.Files)+len(cur.Packs) >= batch }
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, _ := t.(string)
		switch {
		case key == "segment_size":
			var n json.Number
			if err := d.Decode(&n); err != nil {
				return err
			}
			cur.SegSize = num(n)
		case key == "msgid_pool" || (key == "s" && !gotPool):
			n, err := eachInArray(d, func() error {
				var e PoolEntry
				if err := d.Decode(&e); err != nil {
					return err
				}
				cur.Pool = append(cur.Pool, e)
				poolN++
				if full() {
					return flush()
				}
				return nil
			})
			if err != nil {
				return err
			}
			gotPool = gotPool || n > 0
		case key == "files":
			if _, err := eachInArray(d, func() error {
				var f FileRec
				if err := d.Decode(&f); err != nil {
					return err
				}
				cur.Files = append(cur.Files, f)
				if full() {
					return flush()
				}
				return nil
			}); err != nil {
				return err
			}
		case key == "packed_segments" || key == "packed_segs":
			if _, err := eachInArray(d, func() error {
				var p PackRec
				if err := d.Decode(&p); err != nil {
					return err
				}
				cur.Packs = append(cur.Packs, p)
				if full() {
					return flush()
				}
				return nil
			}); err != nil {
				return err
			}
		default:
			var skip json.RawMessage
			if err := d.Decode(&skip); err != nil {
				return err
			}
		}
	}
	if err := expectDelim(d, '}'); err != nil {
		return err
	}
	return flush()
}

func expectDelim(d *json.Decoder, want json.Delim) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	if t != want {
		return fmt.Errorf("core index: expected %q, got %v", want, t)
	}
	return nil
}

// eachInArray calls fn for every element of the array value that comes next; a value
// that is not an array is skipped (returns 0).
func eachInArray(d *json.Decoder, fn func() error) (int, error) {
	t, err := d.Token()
	if err != nil {
		return 0, err
	}
	if t != json.Delim('[') {
		if t == json.Delim('{') {
			for depth := 1; depth > 0; {
				t, err := d.Token()
				if err != nil {
					return 0, err
				}
				switch t {
				case json.Delim('{'), json.Delim('['):
					depth++
				case json.Delim('}'), json.Delim(']'):
					depth--
				}
			}
		}
		return 0, nil
	}
	n := 0
	for d.More() {
		if err := fn(); err != nil {
			return n, err
		}
		n++
	}
	_, err = d.Token() // ']'
	return n, err
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func firstNum(a ...json.Number) json.Number {
	for _, x := range a {
		if x != "" && x != "0" {
			return x
		}
	}
	return "0"
}

func anyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}
