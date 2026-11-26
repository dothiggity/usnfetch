package share

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	tNonce = "00112233445566778899aabb"
	tTag   = "00112233445566778899aabbccddeeff"
	tHash  = "3d03524b03aca461275865a9c9cefd9baf7fce74f2f6f93b39657085920e8bcb"
)

// testDocs: a small share in two batches. Pool 0-19 primaries, 20 a copy of 0. Files: evenly
// numbered segments (stride 1 and 2), an irregular list, a seg_range, a packed file linked
// in its own batch, one whose pack comes in the next batch, and one listed only in its pack.
func testDocs() (*Doc, *Doc) {
	d1 := &Doc{SegSize: 100}
	for i := 0; i < 20; i++ {
		d1.Pool = append(d1.Pool, PoolEntry{Msgid: fmt.Sprintf("m%d@x", i), Nonce: tNonce, Tag: tTag, Size: 100})
	}
	ref := int64(0)
	d1.Pool = append(d1.Pool, PoolEntry{Msgid: "c0@x", Ref: &ref})
	d1.Files = []FileRec{
		{FileID: "0a1b2c3d", Path: "A/even.bin", Size: 400, Hash: tHash, Segments: []int64{0, 1, 2, 3}},
		{FileID: "ABCD", Path: "A/strided.bin", Size: 300, Hash: strings.ToUpper(tHash), Segments: []int64{4, 6, 8}},
		{FileID: "file-7", Path: "B/odd.bin", Size: 300, Segments: []int64{9, 11, 10}},
		{FileID: "1234", Path: "B/range.bin", Size: 200, SegRange: &[3]int64{12, 1, 2}},
		{FileID: "aa11", Path: "C/packed.txt", Size: 50},
		{FileID: "cc33", Path: "C/late.txt", Size: 20},
	}
	d1.Packs = []PackRec{{PoolIndex: 14, Nonce: tNonce, Tag: tTag, Size: 100, Members: []PackMember{
		{FileID: "aa11", Path: "C/packed.txt", Size: 50, HasMeta: true},
		{FileID: "bb22", Path: "C/only-in-pack.txt", Size: 10, HasMeta: true, Hash: tHash},
	}}}
	d2 := &Doc{Packs: []PackRec{{PoolIndex: 15, Nonce: tNonce, Tag: tTag, Size: 100, Members: []PackMember{{FileID: "cc33"}}}}}
	return d1, d2
}

func buildTestIndex(t *testing.T, path string) {
	t.Helper()
	b, err := NewBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	d1, d2 := testDocs()
	for _, d := range []*Doc{d1, d2} {
		if err := b.ApplyDoc(d, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Finish(map[string]any{"folder_id": "f00"}); err != nil {
		t.Fatal(err)
	}
}

// segIdx: the pool indices of a file's segments, however they are stored.
func segIdx(t *testing.T, ix *Index, f FileEntry) []int64 {
	t.Helper()
	pl, err := ix.Plan(f)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for i := int64(0); i < pl.Count; i++ {
		s, err := ix.Segment(pl, i)
		if err != nil {
			t.Fatal(err)
		}
		var idx int64
		fmt.Sscanf(s.Msgids[0], "m%d@x", &idx)
		out = append(out, idx)
	}
	return out
}

func checkFullIndex(t *testing.T, ix *Index) {
	t.Helper()
	if !ix.HasPool || ix.FileCount != 7 {
		t.Fatalf("pool %v, %d files", ix.HasPool, ix.FileCount)
	}
	want := map[string]struct {
		id, sha string
		segs    []int64
		packed  bool
	}{
		"A/even.bin":         {"0a1b2c3d", tHash, []int64{0, 1, 2, 3}, false},
		"A/strided.bin":      {"ABCD", strings.ToUpper(tHash), []int64{4, 6, 8}, false},
		"B/odd.bin":          {"file-7", "", []int64{9, 11, 10}, false},
		"B/range.bin":        {"1234", "", []int64{12, 13}, false},
		"C/packed.txt":       {"aa11", "", nil, true},
		"C/late.txt":         {"cc33", "", nil, true},
		"C/only-in-pack.txt": {"bb22", tHash, nil, true},
	}
	for p, w := range want {
		f, err := ix.FileByPath(p)
		if err != nil || f == nil {
			t.Fatalf("%s: %v", p, err)
		}
		if f.FileID != w.id || f.SHA256 != w.sha || f.Packed() != w.packed {
			t.Errorf("%s: id %q sha %q packed %v", p, f.FileID, f.SHA256, f.Packed())
		}
		if !w.packed {
			if got := segIdx(t, ix, *f); !slices.Equal(got, w.segs) {
				t.Errorf("%s: segments %v, want %v", p, got, w.segs)
			}
		}
	}
	even, _ := ix.FileByPath("A/even.bin")
	if even.SegRange == nil || *even.SegRange != [3]int64{0, 1, 4} {
		t.Errorf("an evenly numbered list is kept as a range: %v", even.SegRange)
	}
	if odd, _ := ix.FileByPath("B/odd.bin"); odd.SegRange != nil {
		t.Error("an irregular list became a range")
	}
	pl, _ := ix.Plan(*even)
	if s, _ := ix.Segment(pl, 0); !slices.Equal(s.Msgids, []string{"m0@x", "c0@x"}) {
		t.Errorf("copies of a segment: %v", s.Msgids)
	}
	var n int
	ix.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('pack_files', 'files_file_id')").Scan(&n)
	if n != 0 {
		t.Error("the build's linking tables are in the index")
	}
}

func TestIndexV3(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "f00-0123456789abcdef.usnindex")
	buildTestIndex(t, full)
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(left) > 0 {
		t.Errorf("build leftovers: %v", left)
	}
	ix, err := OpenIndex(full, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkFullIndex(t, ix)
	if err := ix.EnsureTree(nil); err != nil {
		t.Fatal(err)
	}
	ix.Close()
	if InUse(full) {
		t.Error("closed index still counted as open")
	}

	// the file list: browsing works, downloading data does not
	list := ListPath(full)
	if err := Slim(full, list); err != nil {
		t.Fatal(err)
	}
	if sch, pool := FileInfo(list); sch != SchemaVersion || pool {
		t.Fatalf("file list: schema %d pool %v", sch, pool)
	}
	if err := GzipFile(list); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(list); err == nil {
		t.Error("the plain file list is still there after zipping")
	}
	if err := GunzipFile(list); err != nil {
		t.Fatal(err)
	}
	lx, err := OpenIndex(list, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lx.Close()
	if lx.HasPool {
		t.Error("file list says it has the pool")
	}
	d, folders, files, err := lx.ListDir("A", 0, 10, SortName)
	if err != nil || d == nil || len(folders) != 0 || len(files) != 2 || files[0].Path != "A/even.bin" || files[0].Size != 400 {
		t.Fatalf("listing from the file list: %v %v %v", err, folders, files)
	}
	if f, _ := lx.FileByPath("C/late.txt"); f == nil || !f.Packed() || !f.Downloadable() {
		t.Errorf("packed file in the list: %+v", f)
	}
	if _, err := lx.Plan(files[0]); !errors.Is(err, ErrNoDownloadData) {
		t.Errorf("plan on a file list: %v", err)
	}
}

// v2DDL: the schema 2 layout, as the Python version and earlier usnfetch wrote it.
const v2DDL = `
CREATE TABLE meta(k TEXT PRIMARY KEY, v);
CREATE TABLE pool(idx INTEGER PRIMARY KEY, msgid TEXT, nonce BLOB, tag BLOB, size INTEGER, ref INTEGER);
CREATE TABLE files(fid INTEGER PRIMARY KEY, file_id TEXT, path TEXT, size INTEGER, sha256 TEXT,
                   start INTEGER, stride INTEGER, count INTEGER, layout INTEGER, pid INTEGER);
CREATE TABLE file_segs(fid INTEGER, seg INTEGER, idx INTEGER, PRIMARY KEY(fid, seg)) WITHOUT ROWID;
CREATE TABLE file_offs(fid INTEGER, seg INTEGER, idx INTEGER, off INTEGER, size INTEGER, PRIMARY KEY(fid, seg)) WITHOUT ROWID;
CREATE TABLE packs(pid INTEGER PRIMARY KEY, pool_idx INTEGER, nonce BLOB, tag BLOB, size INTEGER);
CREATE TABLE pack_files(file_id TEXT PRIMARY KEY, pid INTEGER, path TEXT, size INTEGER, sha256 TEXT) WITHOUT ROWID;
`

func TestConvertV2(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f00-v2.usnindex")
	db, err := sql.Open("sqlite", sqliteURI(p, "mode=rwc"))
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{v2DDL,
		"INSERT INTO meta VALUES ('schema', 2), ('complete', 1), ('segment_size', 100), ('pool_count', 21), ('file_count', 7)"}
	for i := 0; i < 20; i++ {
		stmts = append(stmts, fmt.Sprintf("INSERT INTO pool VALUES (%d, 'm%d@x', x'%s', x'%s', 100, NULL)", i, i, tNonce, tTag))
	}
	stmts = append(stmts, "INSERT INTO pool VALUES (20, 'c0@x', NULL, NULL, 0, 0)",
		"INSERT INTO files VALUES (1, '0a1b2c3d', 'A/even.bin', 400, '"+tHash+"', NULL, NULL, 4, NULL, NULL)",
		"INSERT INTO files VALUES (2, 'ABCD', 'A/strided.bin', 300, '"+strings.ToUpper(tHash)+"', NULL, NULL, 3, NULL, NULL)",
		"INSERT INTO files VALUES (3, 'file-7', 'B/odd.bin', 300, NULL, NULL, NULL, 3, NULL, NULL)",
		"INSERT INTO files VALUES (4, '1234', 'B/range.bin', 200, NULL, 12, 1, 2, NULL, NULL)",
		"INSERT INTO files VALUES (5, 'aa11', 'C/packed.txt', 50, NULL, NULL, NULL, 0, NULL, 1)",
		"INSERT INTO files VALUES (6, 'cc33', 'C/late.txt', 20, NULL, NULL, NULL, 0, NULL, 2)",
		"INSERT INTO files VALUES (7, 'bb22', 'C/only-in-pack.txt', 10, '"+tHash+"', NULL, NULL, 0, NULL, 1)",
		"INSERT INTO packs VALUES (1, 14, x'"+tNonce+"', x'"+tTag+"', 100), (2, 15, x'"+tNonce+"', x'"+tTag+"', 100)",
		"INSERT INTO pack_files VALUES ('aa11', 1, 'C/packed.txt', 50, NULL), ('bb22', 1, 'C/only-in-pack.txt', 10, NULL), ('cc33', 2, NULL, NULL, NULL)")
	for fid, segs := range map[int][]int{1: {0, 1, 2, 3}, 2: {4, 6, 8}, 3: {9, 11, 10}} {
		for k, idx := range segs {
			stmts = append(stmts, fmt.Sprintf("INSERT INTO file_segs VALUES (%d, %d, %d)", fid, k, idx))
		}
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	db.Close()
	if _, err := OpenIndex(p, nil); err == nil {
		t.Fatal("a version 2 index opened without conversion")
	}
	if err := Convert(p); err != nil {
		t.Fatal(err)
	}
	ix, err := OpenIndex(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	checkFullIndex(t, ix)
	if err := ix.EnsureTree(nil); err != nil {
		t.Fatal(err)
	}
}
