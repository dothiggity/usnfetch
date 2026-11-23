package share

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SchemaVersion of the index file. Version 2 is shared with the Python implementation,
// so indexes either one built are reused by the other.
const SchemaVersion = 2

const ddl = `
CREATE TABLE meta(k TEXT PRIMARY KEY, v);
CREATE TABLE pool(idx INTEGER PRIMARY KEY, msgid TEXT, nonce BLOB, tag BLOB, size INTEGER, ref INTEGER);
CREATE TABLE files(fid INTEGER PRIMARY KEY, file_id TEXT, path TEXT, size INTEGER, sha256 TEXT,
                   start INTEGER, stride INTEGER, count INTEGER, layout INTEGER, pid INTEGER);
CREATE TABLE file_segs(fid INTEGER, seg INTEGER, idx INTEGER, PRIMARY KEY(fid, seg)) WITHOUT ROWID;
CREATE TABLE file_offs(fid INTEGER, seg INTEGER, idx INTEGER, off INTEGER, size INTEGER, PRIMARY KEY(fid, seg)) WITHOUT ROWID;
CREATE TABLE packs(pid INTEGER PRIMARY KEY, pool_idx INTEGER, nonce BLOB, tag BLOB, size INTEGER);
CREATE TABLE pack_files(file_id TEXT PRIMARY KEY, pid INTEGER, path TEXT, size INTEGER, sha256 TEXT) WITHOUT ROWID;
`

func sqliteURI(path string, params string) string {
	abs, _ := filepath.Abs(path)
	u := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(abs), "/")}
	return u.String() + "?" + params
}

// ---------------------------------------------------------------------------
// builder
// ---------------------------------------------------------------------------

// Builder writes a new index into <path>.<pid>-<n>.tmp and moves it into place only
// when complete, so an index file that exists is a whole one.
type Builder struct {
	final, path              string
	db                       *sql.DB
	tx                       *sql.Tx
	nPool, nFiles, nPacks    int64
	unlinked                 bool // some packed file may still need linking at Finish
	segSize                  int64
	poolBuf, segBuf, fileBuf []any
	packBuf, memberBuf       []any
	stmts                    map[string]*sql.Stmt
}

var tmpSeq int64
var tmpMu sync.Mutex

func NewBuilder(path string) (*Builder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmpMu.Lock()
	tmpSeq++
	tmp := fmt.Sprintf("%s.%d-%d.tmp", path, os.Getpid(), tmpSeq)
	tmpMu.Unlock()
	_ = os.Remove(tmp)
	// A small page cache on purpose: rows go in in key order, so a big cache buys no speed,
	// and SQLite's memory grows several times faster than the cache it is given (a 512 MB
	// cache took the build of a 28-million-entry index to ~8 GB, and ~0.5 GB stayed with
	// the process afterwards). 16 MB: the same build speed in a few hundred MB.
	db, err := sql.Open("sqlite", sqliteURI(tmp, "mode=rwc&_pragma=page_size(16384)&_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)&_pragma=cache_size(-16384)&_pragma=temp_store(MEMORY)&_pragma=locking_mode(EXCLUSIVE)"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(strings.Replace(ddl, "CREATE TABLE pack_files(file_id TEXT PRIMARY KEY, pid INTEGER, path TEXT, size INTEGER, sha256 TEXT) WITHOUT ROWID;",
		"CREATE TABLE pack_files(file_id TEXT, pid INTEGER, path TEXT, size INTEGER, sha256 TEXT);", 1)); err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Builder{final: path, path: tmp, db: db, tx: tx}, nil
}

const rowsPerStmt = 400

func (b *Builder) flushRows(table string, cols int, buf *[]any, verb string) error {
	n := len(*buf) / cols
	for n > 0 {
		k := min(n, rowsPerStmt)
		ph := "(" + strings.TrimSuffix(strings.Repeat("?,", cols), ",") + ")"
		q := verb + " INTO " + table + " VALUES " + strings.TrimSuffix(strings.Repeat(ph+",", k), ",")
		var err error
		if k == rowsPerStmt { // the common full batch: parse the statement once
			st := b.stmts[q]
			if st == nil {
				if st, err = b.tx.Prepare(q); err != nil {
					return err
				}
				if b.stmts == nil {
					b.stmts = map[string]*sql.Stmt{}
				}
				b.stmts[q] = st
			}
			_, err = st.Exec((*buf)[:k*cols]...)
		} else {
			_, err = b.tx.Exec(q, (*buf)[:k*cols]...)
		}
		if err != nil {
			return err
		}
		*buf = (*buf)[k*cols:]
		n -= k
	}
	*buf = (*buf)[:0]
	return nil
}

func (b *Builder) flush() error {
	for _, f := range []struct {
		t    string
		c    int
		buf  *[]any
		verb string
	}{{"pool", 6, &b.poolBuf, "INSERT OR REPLACE"}, {"files", 10, &b.fileBuf, "INSERT"}, {"file_segs", 3, &b.segBuf, "INSERT"},
		{"packs", 5, &b.packBuf, "INSERT"}, {"pack_files", 5, &b.memberBuf, "INSERT"}} {
		if err := b.flushRows(f.t, f.c, f.buf, f.verb); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) maybeFlush() error {
	if len(b.poolBuf) > 60000 || len(b.segBuf) > 60000 || len(b.fileBuf) > 20000 || len(b.memberBuf) > 20000 {
		return b.flush()
	}
	return nil
}

func hexOrNil(s string) any {
	if s == "" {
		return nil
	}
	v, err := hex.DecodeString(s)
	if err != nil {
		return nil
	}
	return v
}

// ApplyDoc writes one parsed core-index document (or part of one) with pool indices
// offset by base. It returns the number of pool entries written.
func (b *Builder) ApplyDoc(d *Doc, base int64) error {
	if d.SegSize > 0 && b.segSize == 0 {
		b.segSize = d.SegSize
	}
	for k, e := range d.Pool {
		i := base + d.PoolStart + int64(k)
		if e.Bare {
			b.poolBuf = append(b.poolBuf, i, e.Msgid, nil, nil, 0, nil)
		} else if e.Ref != nil {
			b.poolBuf = append(b.poolBuf, i, e.Msgid, nil, nil, 0, *e.Ref+base)
		} else {
			b.poolBuf = append(b.poolBuf, i, e.Msgid, hexOrNil(e.Nonce), hexOrNil(e.Tag), e.Size, nil)
		}
		b.nPool++
	}
	// a shard lists its packed files in files[] and the packs that hold them in packed_segments[]:
	// give this doc's packs their ids first, so its packed files are linked as they are written
	memberPid := map[string]int64{}
	for k, p := range d.Packs {
		for _, m := range p.Members {
			memberPid[m.FileID] = b.nPacks + int64(k) + 1
		}
	}
	inDoc := map[string]bool{}
	for _, f := range d.Files {
		b.nFiles++
		var hash any
		if f.Hash != "" {
			hash = f.Hash
		}
		if f.SegRange != nil {
			b.fileBuf = append(b.fileBuf, b.nFiles, f.FileID, f.Path, f.Size, hash, f.SegRange[0]+base, max(f.SegRange[1], 1), f.SegRange[2], nil, nil)
		} else {
			var pid any
			if len(f.Segments) == 0 {
				if v, ok := memberPid[f.FileID]; ok {
					pid = v
					inDoc[f.FileID] = true
				} else {
					b.unlinked = true // its pack may come in a later batch
				}
			}
			b.fileBuf = append(b.fileBuf, b.nFiles, f.FileID, f.Path, f.Size, hash, nil, nil, int64(len(f.Segments)), nil, pid)
			for j, idx := range f.Segments {
				b.segBuf = append(b.segBuf, b.nFiles, int64(j), idx+base)
			}
		}
	}
	for _, p := range d.Packs {
		for _, m := range p.Members {
			if !inDoc[m.FileID] {
				b.unlinked = true // listed only in the pack, or in another batch
			}
		}
		b.nPacks++
		b.packBuf = append(b.packBuf, b.nPacks, p.PoolIndex+base, hexOrNil(p.Nonce), hexOrNil(p.Tag), p.Size)
		for _, m := range p.Members {
			if m.FileID == "" {
				continue
			}
			var path, size, hash any
			if m.HasMeta {
				path, size = m.Path, m.Size
				if m.Hash != "" {
					hash = m.Hash
				}
			}
			b.memberBuf = append(b.memberBuf, m.FileID, b.nPacks, path, size, hash)
		}
	}
	return b.maybeFlush()
}

// Finish links packed files to their packs, writes the meta rows and moves the file in place.
func (b *Builder) Finish(meta map[string]any) (string, error) {
	if err := b.flush(); err != nil {
		b.Abort()
		return "", err
	}
	stmts := []string{"CREATE INDEX pool_ref ON pool(ref) WHERE ref IS NOT NULL"}
	if b.nPacks > 0 && b.unlinked {
		stmts = append(stmts,
			"CREATE INDEX pack_files_id ON pack_files(file_id)",
			"CREATE INDEX files_file_id ON files(file_id)",
			"UPDATE files SET pid = (SELECT pid FROM pack_files pf WHERE pf.file_id = files.file_id) WHERE pid IS NULL AND count = 0 AND start IS NULL",
			`INSERT INTO files (file_id, path, size, sha256, start, stride, count, layout, pid)
			 SELECT pf.file_id, pf.path, COALESCE(pf.size, 0), pf.sha256, NULL, NULL, 0, NULL, pf.pid FROM pack_files pf
			 WHERE pf.path IS NOT NULL AND NOT EXISTS (SELECT 1 FROM files f WHERE f.file_id = pf.file_id)`)
	}
	for _, s := range stmts {
		if _, err := b.tx.Exec(s); err != nil {
			b.Abort()
			return "", err
		}
	}
	if err := b.tx.QueryRow("SELECT COUNT(*) FROM files").Scan(&b.nFiles); err != nil {
		b.Abort()
		return "", err
	}
	if b.segSize == 0 {
		b.segSize = 700000
	}
	m := map[string]any{"schema": SchemaVersion, "segment_size": b.segSize, "pool_count": b.nPool, "file_count": b.nFiles,
		"pack_count": b.nPacks, "built": float64(time.Now().UnixNano()) / 1e9, "complete": 1}
	for k, v := range meta {
		m[k] = v
	}
	for k, v := range m {
		if _, err := b.tx.Exec("INSERT OR REPLACE INTO meta VALUES (?, ?)", k, v); err != nil {
			b.Abort()
			return "", err
		}
	}
	for _, st := range b.stmts {
		st.Close()
	}
	if err := b.tx.Commit(); err != nil {
		b.Abort()
		return "", err
	}
	b.db.Close()
	if err := os.Rename(b.path, b.final); err != nil {
		_ = os.Remove(b.final)
		if err2 := os.Rename(b.path, b.final); err2 != nil {
			_ = os.Remove(b.path)
			return "", err2
		}
	}
	return b.final, nil
}

func (b *Builder) Abort() {
	for _, st := range b.stmts {
		st.Close()
	}
	b.stmts = nil
	if b.tx != nil {
		_ = b.tx.Rollback()
	}
	b.db.Close()
	_ = os.Remove(b.path)
}

// ---------------------------------------------------------------------------
// reader
// ---------------------------------------------------------------------------

// FileEntry is one file of the share.
type FileEntry struct {
	FID      int64     `json:"fid"`
	FileID   string    `json:"file_id"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256,omitempty"`
	SegCount int64     `json:"segments"`
	SegRange *[3]int64 `json:"-"` // start, stride, count in the pool
	Pack     *int64    `json:"-"` // packed segment holding this file
}

// Packed reports whether the file lives inside a packed segment.
func (f *FileEntry) Packed() bool { return f.Pack != nil }

// Downloadable: has data to fetch, or is legitimately empty.
func (f *FileEntry) Downloadable() bool { return f.SegCount > 0 || f.Pack != nil || f.Size == 0 }

// Segment is one encrypted article of a file (or a packed segment).
type Segment struct {
	Index  int64
	Offset int64
	Size   int64
	Msgids []string // primary first, then redundancy copies
	Nonce  []byte
	Tag    []byte
}

// Index is an opened share index.
type Index struct {
	Path        string
	Key         []byte
	SegmentSize int64
	PoolCount   int64
	FileCount   int64
	Info        map[string]any // manifest facts (set by LoadShare)
	rw          *sql.DB        // writes and anything that changes after open
	ro          *sql.DB        // immutable read-only connections for the hot path
	mu          sync.Mutex
	files       []FileEntry
	dc          *dirCache // folder paths for search (search.go), loaded on first use
	sdb         *sql.DB   // read-only pool for search scans, opened with dc
}

var errIndex = errors.New("index file is incomplete or from another version")

// OpenIndex opens an index file built by this program or the Python one.
func OpenIndex(path string, key []byte) (*Index, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	rw, err := sql.Open("sqlite", sqliteURI(path, "mode=rw&_pragma=busy_timeout(30000)"))
	if err != nil {
		return nil, err
	}
	rw.SetMaxOpenConns(1)
	meta := map[string]any{}
	rows, err := rw.Query("SELECT k, v FROM meta")
	if err != nil {
		rw.Close()
		return nil, errIndex
	}
	for rows.Next() {
		var k string
		var v any
		_ = rows.Scan(&k, &v)
		meta[k] = v
	}
	rows.Close()
	if num(meta["schema"]) != SchemaVersion || num(meta["complete"]) == 0 {
		rw.Close()
		return nil, errIndex
	}
	ro, err := sql.Open("sqlite", sqliteURI(path, "mode=ro&immutable=1"))
	if err != nil {
		rw.Close()
		return nil, err
	}
	ro.SetMaxOpenConns(64)
	ro.SetMaxIdleConns(64)
	ix := &Index{Path: path, Key: key, rw: rw, ro: ro, Info: map[string]any{}}
	ix.SegmentSize = num(meta["segment_size"])
	if ix.SegmentSize == 0 {
		ix.SegmentSize = 700000
	}
	ix.PoolCount, ix.FileCount = num(meta["pool_count"]), num(meta["file_count"])
	return ix, nil
}

// Usable: the file exists and is a complete index of this schema.
func Usable(path string) bool {
	ix, err := OpenIndex(path, nil)
	if err != nil {
		return false
	}
	ix.Close()
	return true
}

func (ix *Index) Close() {
	if ix.sdb != nil {
		ix.sdb.Close()
	}
	ix.ro.Close()
	ix.rw.Close()
}

const fileCols = "fid, file_id, path, size, sha256, start, stride, count, pid"

func scanFile(sc interface{ Scan(...any) error }) (FileEntry, error) {
	var f FileEntry
	var fileID, path, sha sql.NullString
	var size, start, stride, count, pid sql.NullInt64
	if err := sc.Scan(&f.FID, &fileID, &path, &size, &sha, &start, &stride, &count, &pid); err != nil {
		return f, err
	}
	f.FileID, f.Path, f.Size, f.SHA256, f.SegCount = fileID.String, path.String, size.Int64, sha.String, count.Int64
	if start.Valid {
		st := stride.Int64
		if st == 0 {
			st = 1
		}
		f.SegRange = &[3]int64{start.Int64, st, count.Int64}
	}
	if pid.Valid {
		p := pid.Int64
		f.Pack = &p
	}
	return f, nil
}

// EachFile hands every file to fn, in fid order, without keeping them. A download job
// looks at all of them but keeps only what it picked: loading a share of 2.7 million
// files whole (Files) took 1-2 GB for as long as the job ran.
func (ix *Index) EachFile(fn func(FileEntry) error) error {
	rows, err := ix.rw.Query("SELECT " + fileCols + " FROM files ORDER BY fid")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return err
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Files: every file, loaded on first use (the command line's list and fetch; a download
// job goes through EachFile instead).
func (ix *Index) Files() ([]FileEntry, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.files != nil {
		return ix.files, nil
	}
	rows, err := ix.rw.Query("SELECT " + fileCols + " FROM files ORDER BY fid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FileEntry, 0, ix.FileCount)
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	ix.files = out
	return out, rows.Err()
}

// File by fid / path.
func (ix *Index) File(fid int64) (*FileEntry, error) {
	f, err := scanFile(ix.rw.QueryRow("SELECT "+fileCols+" FROM files WHERE fid = ?", fid))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}

func (ix *Index) FileByPath(path string) (*FileEntry, error) {
	f, err := scanFile(ix.rw.QueryRow("SELECT "+fileCols+" FROM files WHERE path = ?", path))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}

// ---- plans and segment lookups -----------------------------------------------

const (
	LayoutUniform = 1 // offsets by arithmetic: every segment but the last is SegmentSize
	LayoutTable   = 2 // per-segment offset table (file_offs)
)

// Plan is one file's download plan; segments are looked up as they are needed.
type Plan struct {
	Index  *Index
	File   FileEntry
	Layout int
	Count  int64
}

func (ix *Index) Plan(fe FileEntry) (*Plan, error) {
	var layout sql.NullInt64
	if err := ix.rw.QueryRow("SELECT layout FROM files WHERE fid = ?", fe.FID).Scan(&layout); err != nil {
		return nil, err
	}
	l := int(layout.Int64)
	if l == 0 {
		var err error
		if l, err = ix.settleLayout(&fe); err != nil {
			return nil, err
		}
	}
	return &Plan{Index: ix, File: fe, Layout: l, Count: fe.SegCount}, nil
}

func (ix *Index) settleLayout(fe *FileEntry) (int, error) {
	ss, n := ix.SegmentSize, fe.SegCount
	var cnt, odd, tail int64
	var err error
	if fe.SegRange != nil {
		start, stride := fe.SegRange[0], fe.SegRange[1]
		last := start + stride*(n-1)
		err = ix.rw.QueryRow(`SELECT COUNT(*), COALESCE(SUM(idx < ? AND size NOT IN (0, ?)), 0), COALESCE(MAX(CASE WHEN idx = ? THEN size END), 0)
			FROM pool WHERE idx BETWEEN ? AND ? AND (idx - ?) % ? = 0 AND ref IS NULL AND msgid IS NOT NULL`,
			last, ss, last, start, last, start, stride).Scan(&cnt, &odd, &tail)
	} else {
		err = ix.rw.QueryRow(`SELECT COUNT(*), COALESCE(SUM(fs.seg < ? AND p.size NOT IN (0, ?)), 0), COALESCE(MAX(CASE WHEN fs.seg = ? THEN p.size END), 0)
			FROM file_segs fs JOIN pool p ON p.idx = fs.idx WHERE fs.fid = ? AND p.ref IS NULL AND p.msgid IS NOT NULL`,
			n-1, ss, n-1, fe.FID).Scan(&cnt, &odd, &tail)
	}
	if err != nil {
		return 0, err
	}
	if n > 0 && cnt != n {
		return 0, fmt.Errorf("%s: %d of %d pool indices are not primary entries", fe.Path, n-cnt, n)
	}
	tailExpect := fe.Size - ss*(n-1)
	layout := LayoutTable
	if n == 0 || (odd == 0 && fe.Size > 0 && tailExpect > 0 && tailExpect <= ss && (tail == 0 || tail == tailExpect)) {
		layout = LayoutUniform
	} else if err := ix.buildOffsets(fe); err != nil {
		return 0, err
	}
	_, err = ix.rw.Exec("UPDATE files SET layout = ?, size = ? WHERE fid = ?", layout, fe.Size, fe.FID)
	return layout, err
}

func (ix *Index) buildOffsets(fe *FileEntry) error {
	ss := ix.SegmentSize
	var rows *sql.Rows
	var err error
	if fe.SegRange != nil {
		start, stride, n := fe.SegRange[0], fe.SegRange[1], fe.SegRange[2]
		rows, err = ix.rw.Query("SELECT idx, size FROM pool WHERE idx BETWEEN ? AND ? AND (idx - ?) % ? = 0 AND ref IS NULL ORDER BY idx",
			start, start+stride*(n-1), start, stride)
	} else {
		rows, err = ix.rw.Query("SELECT p.idx, p.size FROM file_segs fs JOIN pool p ON p.idx = fs.idx WHERE fs.fid = ? ORDER BY fs.seg", fe.FID)
	}
	if err != nil {
		return err
	}
	type row struct{ idx, size int64 }
	var all []row
	for rows.Next() {
		var r row
		var size sql.NullInt64
		_ = rows.Scan(&r.idx, &size)
		r.size = size.Int64
		if r.size == 0 {
			r.size = ss
		}
		all = append(all, r)
	}
	rows.Close()
	tx, err := ix.rw.Begin()
	if err != nil {
		return err
	}
	_, _ = tx.Exec("DELETE FROM file_offs WHERE fid = ?", fe.FID)
	var off int64
	for i, r := range all {
		if _, err := tx.Exec("INSERT INTO file_offs VALUES (?,?,?,?,?)", fe.FID, i, r.idx, off, r.size); err != nil {
			tx.Rollback()
			return err
		}
		off += r.size
	}
	if fe.Size != 0 && off != fe.Size {
		tx.Rollback()
		return fmt.Errorf("%s: segment sizes sum to %d, file size is %d", fe.Path, off, fe.Size)
	}
	if fe.Size == 0 {
		fe.Size = off
	}
	return tx.Commit()
}

// Segment i of a plan.
func (ix *Index) Segment(p *Plan, i int64) (Segment, error) {
	fe, ss, n := &p.File, ix.SegmentSize, p.Count
	if i < 0 || i >= n {
		return Segment{}, fmt.Errorf("segment %d of %d", i, n)
	}
	var idx, off, size int64
	if p.Layout == LayoutUniform {
		if fe.SegRange != nil {
			idx = fe.SegRange[0] + fe.SegRange[1]*i
		} else if err := ix.ro.QueryRow("SELECT idx FROM file_segs WHERE fid = ? AND seg = ?", fe.FID, i).Scan(&idx); err != nil {
			return Segment{}, err
		}
		off = i * ss
		size = ss
		if i == n-1 {
			size = fe.Size - off
		}
	} else if err := ix.rw.QueryRow("SELECT idx, off, size FROM file_offs WHERE fid = ? AND seg = ?", fe.FID, i).Scan(&idx, &off, &size); err != nil {
		return Segment{}, err
	}
	seg, err := ix.poolSegment(idx)
	if err != nil {
		return Segment{}, fmt.Errorf("segment %d: %w", i, err)
	}
	seg.Index, seg.Offset, seg.Size = i, off, size
	return seg, nil
}

func (ix *Index) poolSegment(idx int64) (Segment, error) {
	var msgid sql.NullString
	var nonce, tag []byte
	err := ix.ro.QueryRow("SELECT msgid, nonce, tag FROM pool WHERE idx = ? AND ref IS NULL", idx).Scan(&msgid, &nonce, &tag)
	if err == sql.ErrNoRows || !msgid.Valid {
		return Segment{}, fmt.Errorf("pool index %d is not a primary entry", idx)
	}
	if err != nil {
		return Segment{}, err
	}
	s := Segment{Msgids: []string{msgid.String}, Nonce: nonce, Tag: tag}
	rows, err := ix.ro.Query("SELECT msgid FROM pool WHERE ref = ? AND msgid IS NOT NULL ORDER BY idx", idx)
	if err != nil {
		return Segment{}, err
	}
	for rows.Next() {
		var m string
		_ = rows.Scan(&m)
		s.Msgids = append(s.Msgids, m)
	}
	rows.Close()
	return s, nil
}

// PackSegment: the article holding packed segment pid (nonce/tag from the pack entry,
// falling back to the pool entry; the publisher writes both from one record).
func (ix *Index) PackSegment(pid int64) (Segment, error) {
	var idx int64
	var nonce, tag []byte
	var size sql.NullInt64
	if err := ix.ro.QueryRow("SELECT pool_idx, nonce, tag, size FROM packs WHERE pid = ?", pid).Scan(&idx, &nonce, &tag, &size); err != nil {
		return Segment{}, fmt.Errorf("packed segment %d is not in the index", pid)
	}
	s, err := ix.poolSegment(idx)
	if err != nil {
		return Segment{}, fmt.Errorf("packed segment %d: %w", pid, err)
	}
	if nonce != nil {
		s.Nonce = nonce
	}
	if tag != nil {
		s.Tag = tag
	}
	if s.Nonce == nil || s.Tag == nil {
		return Segment{}, fmt.Errorf("packed segment %d has no nonce/tag", pid)
	}
	s.Index, s.Size = pid, size.Int64
	return s, nil
}

func (ix *Index) PackIDs() ([]int64, error) {
	rows, err := ix.ro.Query("SELECT pid FROM packs ORDER BY pid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var p int64
		_ = rows.Scan(&p)
		out = append(out, p)
	}
	return out, nil
}

// SegmentAt: the segment holding byte off of the plan's file.
func (ix *Index) SegmentAt(p *Plan, off int64) (int64, error) {
	if p.Layout == LayoutUniform {
		return min(off/ix.SegmentSize, p.Count-1), nil
	}
	lo, hi := int64(0), p.Count-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		var o int64
		if err := ix.rw.QueryRow("SELECT off FROM file_offs WHERE fid = ? AND seg = ?", p.File.FID, mid).Scan(&o); err != nil {
			return 0, err
		}
		if o <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, nil
}

// DoneBytes: bytes already on disk according to a checkpoint.
func (ix *Index) DoneBytes(p *Plan, isDone func(int64) bool, done int64) int64 {
	fe, ss, n := &p.File, ix.SegmentSize, p.Count
	if p.Layout == LayoutUniform {
		b := done * ss
		if n > 0 && isDone(n-1) {
			b -= ss - (fe.Size - ss*(n-1))
		}
		return b
	}
	rows, err := ix.rw.Query("SELECT seg, size FROM file_offs WHERE fid = ?", fe.FID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var b int64
	for rows.Next() {
		var seg, size int64
		_ = rows.Scan(&seg, &size)
		if isDone(seg) {
			b += size
		}
	}
	return b
}

// TotalSegments of all segmented files.
func (ix *Index) TotalSegments() (int64, error) {
	var n int64
	err := ix.rw.QueryRow("SELECT COALESCE(SUM(count), 0) FROM files WHERE count > 0").Scan(&n)
	return n, err
}

// IterSegmentedFiles calls fn for each file with segments, in fid order, until it returns false.
func (ix *Index) IterSegmentedFiles(fn func(FileEntry) bool) error {
	rows, err := ix.rw.Query("SELECT " + fileCols + " FROM files WHERE count > 0 ORDER BY fid")
	if err != nil {
		return err
	}
	var batch []FileEntry
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, f)
	}
	rows.Close()
	for _, f := range batch {
		if !fn(f) {
			break
		}
	}
	return nil
}

// ---- folder tree for browsing ---------------------------------------------------

// EnsureTree builds the folder table once per index file (sizes and file counts per
// folder, each file filed under its folder), the same tables the Python version builds.
func (ix *Index) EnsureTree(log func(string)) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var one int
	if err := ix.rw.QueryRow("SELECT 1 FROM meta WHERE k = 'tree'").Scan(&one); err == nil {
		return nil
	}
	t0 := time.Now()
	type dir struct {
		did, parent       int64
		name, path        string
		size, count, nfil int64
	}
	dirs := map[string]int64{"": 0}
	all := []*dir{{did: 0, parent: -1}}
	rows, err := ix.rw.Query("SELECT fid, path, size FROM files WHERE (count > 0 OR pid IS NOT NULL OR size = 0)")
	if err != nil {
		return err
	}
	type df struct {
		did  int64
		name string
		fid  int64
	}
	var files []df
	for rows.Next() {
		var fid, size int64
		var path string
		_ = rows.Scan(&fid, &path, &size)
		parts := strings.FieldsFunc(strings.ReplaceAll(path, "\\", "/"), func(r rune) bool { return r == '/' })
		if len(parts) == 0 {
			parts = []string{fmt.Sprint(fid)}
		}
		did, prefix := int64(0), ""
		all[0].size += size
		all[0].count++
		for _, p := range parts[:len(parts)-1] {
			if prefix == "" {
				prefix = p
			} else {
				prefix += "/" + p
			}
			d, ok := dirs[prefix]
			if !ok {
				d = int64(len(all))
				dirs[prefix] = d
				all = append(all, &dir{did: d, parent: did, name: p, path: prefix})
			}
			did = d
			all[d].size += size
			all[d].count++
		}
		all[did].nfil++
		files = append(files, df{did, parts[len(parts)-1], fid})
	}
	rows.Close()
	tx, err := ix.rw.Begin()
	if err != nil {
		return err
	}
	for _, s := range []string{"DROP TABLE IF EXISTS dirs", "DROP TABLE IF EXISTS dir_files",
		"CREATE TABLE dirs(did INTEGER PRIMARY KEY, parent INTEGER, name TEXT, path TEXT, size INTEGER, count INTEGER, nfiles INTEGER)",
		"CREATE TABLE dir_files(did INTEGER, name TEXT, fid INTEGER)"} {
		if _, err := tx.Exec(s); err != nil {
			tx.Rollback()
			return err
		}
	}
	var buf []any
	flush := func(table string, cols int) error {
		for len(buf) > 0 {
			k := min(len(buf)/cols, rowsPerStmt)
			ph := "(" + strings.TrimSuffix(strings.Repeat("?,", cols), ",") + ")"
			if _, err := tx.Exec("INSERT INTO "+table+" VALUES "+strings.TrimSuffix(strings.Repeat(ph+",", k), ","), buf[:k*cols]...); err != nil {
				return err
			}
			buf = buf[k*cols:]
		}
		return nil
	}
	for _, d := range all {
		var parent any
		if d.parent >= 0 {
			parent = d.parent
		}
		buf = append(buf, d.did, parent, d.name, d.path, d.size, d.count, d.nfil)
	}
	if err := flush("dirs", 7); err != nil {
		tx.Rollback()
		return err
	}
	for _, f := range files {
		buf = append(buf, f.did, f.name, f.fid)
		if len(buf) >= 3*rowsPerStmt*50 {
			if err := flush("dir_files", 3); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	if err := flush("dir_files", 3); err != nil {
		tx.Rollback()
		return err
	}
	for _, s := range []string{"CREATE UNIQUE INDEX dirs_path ON dirs(path)", "CREATE INDEX dirs_parent ON dirs(parent, name)",
		"CREATE INDEX dir_files_did ON dir_files(did, name)", "INSERT OR REPLACE INTO meta VALUES ('tree', 1)"} {
		if _, err := tx.Exec(s); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if log != nil {
		log(fmt.Sprintf("folder tree: %d files in %d folders (%.1fs)", len(files), len(all)-1, time.Since(t0).Seconds()))
	}
	return nil
}

// TreeSummary: totals and a display name (the single top folder, or the only file).
type TreeSummary struct {
	Size   int64  `json:"size"`
	Files  int64  `json:"files"`
	Packed int64  `json:"packed"`
	Name   string `json:"name"`
}

func (ix *Index) Summary() (TreeSummary, error) {
	var s TreeSummary
	var rootFiles int64
	if err := ix.rw.QueryRow("SELECT size, count, nfiles FROM dirs WHERE did = 0").Scan(&s.Size, &s.Files, &rootFiles); err != nil {
		return s, err
	}
	_ = ix.rw.QueryRow("SELECT COUNT(*) FROM files WHERE pid IS NOT NULL").Scan(&s.Packed)
	var tops []string
	rows, err := ix.rw.Query("SELECT name FROM dirs WHERE parent = 0 LIMIT 2")
	if err == nil {
		for rows.Next() {
			var n string
			_ = rows.Scan(&n)
			tops = append(tops, n)
		}
		rows.Close()
	}
	if len(tops) == 1 && rootFiles == 0 {
		s.Name = tops[0]
	} else if s.Files == 1 {
		_ = ix.rw.QueryRow("SELECT name FROM dir_files WHERE did = 0 LIMIT 1").Scan(&s.Name)
	}
	return s, nil
}

// DirInfo of a folder path ("" = root), nil if there is no such folder.
type DirInfo struct {
	DID    int64  `json:"-"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Count  int64  `json:"count"`
	NFiles int64  `json:"nfiles"`
}

func normDir(path string) string {
	return strings.Join(strings.FieldsFunc(strings.ReplaceAll(path, "\\", "/"), func(r rune) bool { return r == '/' }), "/")
}

func (ix *Index) Dir(path string) (*DirInfo, error) {
	d := DirInfo{Path: normDir(path)}
	err := ix.rw.QueryRow("SELECT did, size, count, nfiles FROM dirs WHERE path = ?", d.Path).Scan(&d.DID, &d.Size, &d.Count, &d.NFiles)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &d, err
}

// Folder is a subfolder in a listing.
type Folder struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Count int64  `json:"count"`
}

// Sort orders for listings and search: by name (the default), or by size.
const (
	SortName     = "name"
	SortSizeDesc = "size-desc"
	SortSizeAsc  = "size-asc"
)

// sortFolders orders folders by name (case-insensitive) or size (then name).
func sortFolders(folders []Folder, by string, key func(Folder) string) {
	sort.SliceStable(folders, func(i, j int) bool {
		a, b := folders[i], folders[j]
		switch {
		case by == SortSizeDesc && a.Size != b.Size:
			return a.Size > b.Size
		case by == SortSizeAsc && a.Size != b.Size:
			return a.Size < b.Size
		}
		return strings.ToLower(key(a)) < strings.ToLower(key(b))
	})
}

// ListDir: a folder's subfolders and a page of its files, by name or size (sortBy).
func (ix *Index) ListDir(path string, offset, limit int, sortBy string) (*DirInfo, []Folder, []FileEntry, error) {
	d, err := ix.Dir(path)
	if err != nil || d == nil {
		return nil, nil, nil, err
	}
	folders := []Folder{}
	rows, err := ix.rw.Query("SELECT name, path, size, count FROM dirs WHERE parent = ? AND did != 0", d.DID)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var f Folder
		_ = rows.Scan(&f.Name, &f.Path, &f.Size, &f.Count)
		folders = append(folders, f)
	}
	rows.Close()
	sortFolders(folders, sortBy, func(f Folder) string { return f.Name })
	order := "df.name"
	switch sortBy {
	case SortSizeDesc:
		order = "f.size DESC, df.name"
	case SortSizeAsc:
		order = "f.size ASC, df.name"
	}
	var cols []string
	for _, c := range strings.Split(fileCols, ",") {
		cols = append(cols, "f."+strings.TrimSpace(c))
	}
	frows, err := ix.rw.Query("SELECT "+strings.Join(cols, ", ")+" FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did = ? ORDER BY "+order+" LIMIT ? OFFSET ?",
		d.DID, limit, offset)
	if err != nil {
		return nil, nil, nil, err
	}
	defer frows.Close()
	files := []FileEntry{}
	for frows.Next() {
		f, err := scanFile(frows)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, f)
	}
	return d, folders, files, nil
}
