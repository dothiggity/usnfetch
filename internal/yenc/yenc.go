// Package yenc decodes yEnc article bodies (single- or multi-part) and checks their CRC.
package yenc

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
)

// ErrNoYenc is returned for a body without a =ybegin line.
var ErrNoYenc = errors.New("yenc: no =ybegin line")

// Header fields of interest.
type Info struct {
	Name  string
	Size  int64 // =ybegin size (whole file)
	Part  int
	Begin int64 // =ypart begin (1-based), 0 if absent
	End   int64
}

// Decode decodes an NNTP article body that has already been dot-unstuffed (lines end
// in CRLF or LF). The CRC from =yend is checked: pcrc32 for a part, crc32 for a
// single-part article. A declared size that does not match is an error too.
func Decode(body []byte) ([]byte, Info, error) {
	var info Info
	start := bytes.Index(body, []byte("=ybegin "))
	if start < 0 {
		return nil, info, ErrNoYenc
	}
	pos := start
	line, pos := nextLine(body, pos)
	hdr := fields(line[len("=ybegin "):])
	info.Name = hdr["name"]
	info.Size, _ = strconv.ParseInt(hdr["size"], 10, 64)
	info.Part, _ = strconv.Atoi(hdr["part"])
	out := make([]byte, 0, estimate(hdr, len(body)))
	var footer map[string]string
	for pos < len(body) {
		line, next := nextLine(body, pos)
		pos = next
		if len(line) >= 6 && line[0] == '=' && line[1] == 'y' {
			if bytes.HasPrefix(line, []byte("=ypart ")) {
				p := fields(line[len("=ypart "):])
				info.Begin, _ = strconv.ParseInt(p["begin"], 10, 64)
				info.End, _ = strconv.ParseInt(p["end"], 10, 64)
				continue
			}
			if bytes.HasPrefix(line, []byte("=yend")) {
				footer = fields(line[len("=yend"):])
				break
			}
		}
		out = decodeLine(out, line)
	}
	if footer == nil {
		return nil, info, errors.New("yenc: no =yend line (truncated article)")
	}
	if s, ok := footer["size"]; ok {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n != int64(len(out)) {
			return nil, info, fmt.Errorf("yenc: decoded %d bytes, =yend says %d", len(out), n)
		}
	}
	want := footer["pcrc32"]
	if want == "" && info.Part == 0 {
		want = footer["crc32"]
	}
	if want != "" {
		w, err := strconv.ParseUint(want, 16, 32)
		if err == nil && uint32(w) != crc32.ChecksumIEEE(out) {
			return nil, info, fmt.Errorf("yenc: CRC mismatch (%08x != %s)", crc32.ChecksumIEEE(out), want)
		}
	}
	return out, info, nil
}

func estimate(hdr map[string]string, bodyLen int) int {
	return bodyLen // decoded is always <= encoded
}

// nextLine returns the line at pos without its line ending, and the start of the next line.
func nextLine(b []byte, pos int) ([]byte, int) {
	i := bytes.IndexByte(b[pos:], '\n')
	if i < 0 {
		return trimCR(b[pos:]), len(b)
	}
	return trimCR(b[pos : pos+i]), pos + i + 1
}

func trimCR(l []byte) []byte {
	if n := len(l); n > 0 && l[n-1] == '\r' {
		return l[:n-1]
	}
	return l
}

func decodeLine(out, line []byte) []byte {
	esc := false
	for _, c := range line {
		if esc {
			out = append(out, c-64-42)
			esc = false
			continue
		}
		if c == '=' {
			esc = true
			continue
		}
		out = append(out, c-42)
	}
	return out
}

// fields parses `key=value` pairs; name= runs to the end of the line (it may contain spaces).
func fields(s []byte) map[string]string {
	m := map[string]string{}
	str := string(s)
	for len(str) > 0 {
		for len(str) > 0 && str[0] == ' ' {
			str = str[1:]
		}
		eq := indexByte(str, '=')
		if eq < 0 {
			break
		}
		k := str[:eq]
		str = str[eq+1:]
		if k == "name" {
			m[k] = str
			break
		}
		sp := indexByte(str, ' ')
		if sp < 0 {
			m[k] = str
			break
		}
		m[k] = str[:sp]
		str = str[sp+1:]
	}
	return m
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Encode yEncodes data as a single-part article body (for tests and the fake server).
func Encode(data []byte, name string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "=ybegin line=128 size=%d name=%s\r\n", len(data), name)
	col := 0
	for _, c := range data {
		e := c + 42
		switch e {
		case 0, '\n', '\r', '=':
			b.WriteByte('=')
			e += 64
			col++
		case '\t', ' ', '.':
			if col == 0 || col == 127 {
				b.WriteByte('=')
				e += 64
				col++
			}
		}
		b.WriteByte(e)
		col++
		if col >= 128 {
			b.WriteString("\r\n")
			col = 0
		}
	}
	if col > 0 {
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "=yend size=%d crc32=%08x\r\n", len(data), crc32.ChecksumIEEE(data))
	return b.Bytes()
}
