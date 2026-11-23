//go:build !windows

package extract

import (
	"io/fs"
	"os/exec"
	"strings"
	"unicode/utf8"
)

func hideWindow(*exec.Cmd) {}

// decodeOutput decodes child output as UTF-8 (the usual locale encoding), replacing
// each invalid byte with U+FFFD like Python's errors="replace".
func decodeOutput(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		sb.WriteRune(r) // utf8.RuneError for an invalid byte
		b = b[size:]
	}
	return sb.String()
}

func reparseIsDir(fs.DirEntry) bool { return false }
