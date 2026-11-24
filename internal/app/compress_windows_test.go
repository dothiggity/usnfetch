package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.usnindex")
	data := bytes.Repeat([]byte("Games/N64/Some Game (USA).n64 3d03524b03aca461275865a9c9cefd9b\n"), 1<<16) // 4 MB, compressible
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	if d := diskSize(p, size); d < size {
		t.Fatalf("on disk %d before compressing, size %d", d, size)
	}
	if err := compressFile(p); err != nil {
		t.Skipf("no file compression here: %v", err) // a temp folder on FAT32 or a network drive
	}
	if d := diskSize(p, size); d >= size/2 {
		t.Errorf("on disk %d after compressing, size %d", d, size)
	}
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back after compressing: %v (same: %v)", err, bytes.Equal(got, data))
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0) // a write decompresses it, and it still works
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("more\n")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, _ = os.ReadFile(p)
	if !bytes.Equal(got, append(data, "more\n"...)) {
		t.Error("contents after a write")
	}
}
