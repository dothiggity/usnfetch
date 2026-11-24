package main

import (
	"testing"
	"unsafe"
)

// Windows reads the structure by its documented layout: a wrong size or offset and the
// notification is silently dropped.
func TestNotifyIconDataLayout(t *testing.T) {
	var n notifyIconData
	want := map[string][2]uintptr{
		"size":       {unsafe.Sizeof(n), 976},
		"Info":       {unsafe.Offsetof(n.Info), 304},
		"InfoTitle":  {unsafe.Offsetof(n.InfoTitle), 820},
		"InfoFlags":  {unsafe.Offsetof(n.InfoFlags), 948},
		"BalloonIco": {unsafe.Offsetof(n.BalloonIcon), 968},
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("offsets below are for 64-bit Windows")
	}
	for k, v := range want {
		if v[0] != v[1] {
			t.Errorf("%s: %d, want %d", k, v[0], v[1])
		}
	}
	var buf [8]uint16
	copyUTF16(buf[:], "a long title that does not fit")
	if buf[len(buf)-1] != 0 {
		t.Error("cut text is not zero-terminated")
	}
}
