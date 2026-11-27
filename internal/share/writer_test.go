package share

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A write waits for a read of the same Index to finish (SQLite alone would give up after its
// busy timeout), then goes through; once the Index is closed nothing opens the file for writing
// again (that handle would keep it from being deleted or replaced).
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
	got := make(chan struct{})
	wrote := make(chan error, 1)
	go func() {
		w, done, err := ix.writer()
		close(got)
		if err != nil {
			wrote <- err
			return
		}
		defer done()
		_, err = w.Exec("UPDATE files SET layout = 1")
		wrote <- err
	}()
	select {
	case <-got:
		t.Fatal("the writer didn't wait for the read")
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
	if err := ix.EnsureTree(nil); err != nil { // reads, then writes, on its own
		t.Fatal(err)
	}
	if _, err := ix.Summary(); err != nil {
		t.Fatalf("reading after the writes: %v", err)
	}
	ix.Close()
	if _, _, err := ix.writer(); err == nil {
		t.Error("a writer opened after Close")
	}
	if err := os.Remove(path); err != nil {
		t.Errorf("the closed index can't be deleted: %v", err)
	}
}
