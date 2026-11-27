package share

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A write waits for a read of the same Index to finish (it doesn't fail or hang), and once
// the Index is closed nothing opens the file for writing again (which would keep it from
// being deleted or replaced).
func TestIndexWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f00-0123456789abcdef.usnindex")
	buildTestIndex(t, path)
	ix, err := OpenIndex(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ix.db.Query("SELECT path FROM files") // a read in progress
	if err != nil {
		t.Fatal(err)
	}
	rows.Next()
	wrote := make(chan error, 1)
	go func() { wrote <- ix.EnsureTree(nil) }()
	select {
	case err := <-wrote:
		t.Fatalf("the write didn't wait for the read: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	rows.Close()
	select {
	case err := <-wrote:
		if err != nil {
			t.Fatalf("the write after the read: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the write still waits after the read ended")
	}
	if _, err := ix.Summary(); err != nil {
		t.Fatalf("reading after the write: %v", err)
	}
	ix.Close()
	if _, _, err := ix.writer(); err == nil {
		t.Error("a writer opened after Close")
	}
	if err := os.Remove(path); err != nil {
		t.Errorf("the closed index can't be deleted: %v", err)
	}
}
