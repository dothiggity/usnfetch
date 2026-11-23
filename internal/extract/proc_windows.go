//go:build windows

package extract

import (
	"io/fs"
	"math"
	"os/exec"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// hideWindow keeps console tools from flashing a window when started from the GUI.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// decodeOutput decodes child output with the ANSI code page, which is what Python's
// text-mode subprocess uses on Windows (locale.getencoding()).
func decodeOutput(b []byte) string {
	if len(b) > math.MaxInt32 {
		b = b[len(b)-math.MaxInt32:]
	}
	if len(b) == 0 {
		return ""
	}
	const cpACP = 0
	n, err := windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n == 0 {
		return string(b)
	}
	w := make([]uint16, n)
	if n, err = windows.MultiByteToWideChar(cpACP, 0, &b[0], int32(len(b)), &w[0], n); err != nil {
		return string(b)
	}
	return string(utf16.Decode(w[:n]))
}

// reparseIsDir reports FILE_ATTRIBUTE_DIRECTORY for a reparse point that is not a
// symlink (a junction, a cloud placeholder, ...): Python's DirEntry.is_dir() goes by
// that attribute, so os.walk lists such entries as directories and descends into them.
func reparseIsDir(e fs.DirEntry) bool {
	fi, err := e.Info()
	if err != nil {
		return false
	}
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
}
