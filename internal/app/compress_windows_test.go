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

func TestCompressPass(t *testing.T) {
	m, err := NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Store.Close)
	dir := m.IndexDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("Games/SNES/Another Game (Europe).sfc 9f2c1e0b7a6d5c4b3a291807f6e5d4c3\n"), 1<<16)
	idle, open, building := filepath.Join(dir, "a-1.usnindex"), filepath.Join(dir, "b-2.usnindex"), filepath.Join(dir, "c-3.usnindex.99-1.tmp")
	for _, p := range []string{idle, open, building} {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.PinIndex(open) // a share has it open
	m.compressPass()
	size := int64(len(data))
	if diskSize(idle, size) >= size/2 {
		t.Skip("no file compression in this temp folder")
	}
	if diskSize(open, size) < size || diskSize(building, size) < size {
		t.Error("an open index or a build in progress was compressed")
	}
	st := m.IndexCacheStats()
	if st["files"] != 3 || st["size"].(int64) != 3*size || st["on_disk"].(int64) >= 3*size {
		t.Errorf("stats %v", st)
	}
}
