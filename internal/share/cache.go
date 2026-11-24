package share

import (
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// The share index cache keeps each share's index in one of two forms:
//
//   <folder>-<manifest hash>.usnindex     the full index: the file list, its folder tree, and
//                                         the per-article message-ids and decryption values
//                                         that downloading, streaming and checks need
//   <folder>-<manifest hash>.usnlist      the file list and folder tree only: enough to browse,
//                                         search and see sizes; zipped (.usnlist.gz) while idle
//
// A full index that has gone unused while the cache is over its limit is cut down to its file
// list (Slim). When that share is downloaded or streamed again the full index is fetched again
// (LoadShare with NeedPool). Shares never change (a re-published one has a new manifest, so a
// new file name), so nothing in the cache goes stale; it is kept by last use, never by age.

// CacheMu is held while a cached index file is chosen, converted, unzipped or opened
// (LoadShare), and by the cache's own upkeep for each file it changes, so the two never act
// on the same file at once.
var CacheMu sync.Mutex

var (
	openMu    sync.Mutex
	openFiles = map[string]int{} // index files open in this process (lowercased path)
)

func markOpen(path string, d int) {
	k := strings.ToLower(path)
	openMu.Lock()
	defer openMu.Unlock()
	if openFiles[k] += d; openFiles[k] <= 0 {
		delete(openFiles, k)
	}
}

// InUse: an index file this process has open.
func InUse(path string) bool {
	openMu.Lock()
	defer openMu.Unlock()
	return openFiles[strings.ToLower(path)] > 0
}

// ErrNoDownloadData: only the share's file list is saved; the rest of its index is needed.
var ErrNoDownloadData = errors.New("only this share's file list is saved here: the rest of its index has to be fetched again first")

// ListPath: where the file-list form of an index lives.
func ListPath(indexPath string) string {
	return strings.TrimSuffix(indexPath, ".usnindex") + ".usnlist"
}

// GzExt: an idle file list, zipped.
const GzExt = ".gz"

// FileInfo: what an index file in the cache holds (schema 0 = not a complete index).
func FileInfo(path string) (schema int64, pool bool) {
	if _, err := os.Stat(path); err != nil {
		return 0, false
	}
	db, err := sql.Open("sqlite", sqliteURI(path, "mode=ro&immutable=1"))
	if err != nil {
		return 0, false
	}
	defer db.Close()
	meta := map[string]any{}
	rows, err := db.Query("SELECT k, v FROM meta WHERE k IN ('schema', 'complete', 'pool')")
	if err != nil {
		return 0, false
	}
	for rows.Next() {
		var k string
		var v any
		_ = rows.Scan(&k, &v)
		meta[k] = v
	}
	rows.Close()
	if num(meta["complete"]) == 0 {
		return 0, false
	}
	p, ok := meta["pool"]
	return num(meta["schema"]), !ok || num(p) != 0
}

// workDB opens a new database for a conversion, with the build's settings (no journal: the
// file is renamed into place only when complete).
func workDB(tmp string) (*sql.DB, error) {
	_ = os.Remove(tmp)
	db, err := sql.Open("sqlite", sqliteURI(tmp, "mode=rwc&_pragma=page_size(16384)&_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)&_pragma=cache_size(-16384)&_pragma=temp_store(FILE)&_pragma=locking_mode(EXCLUSIVE)"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// replaceFile moves a finished temp file over dst, keeping dst's last-used time when told.
func replaceFile(tmp, dst string, mtime time.Time) error {
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(dst)
		if err2 := os.Rename(tmp, dst); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	if !mtime.IsZero() {
		_ = os.Chtimes(dst, mtime, mtime)
	}
	return nil
}

const treeDDL = `CREATE TABLE dirs(did INTEGER PRIMARY KEY, parent INTEGER, name TEXT, path TEXT, size INTEGER, count INTEGER, nfiles INTEGER);
CREATE TABLE dir_files(did INTEGER, name TEXT, fid INTEGER);`

const treeIndexes = `CREATE UNIQUE INDEX dirs_path ON dirs(path);
CREATE INDEX dirs_parent ON dirs(parent, name);
CREATE INDEX dir_files_did ON dir_files(did, name);`

// copyTree copies the folder tree from the attached database src (if it has one).
func copyTree(tx *sql.Tx, src string) (bool, error) {
	var one int
	if tx.QueryRow("SELECT 1 FROM "+src+".sqlite_master WHERE type = 'table' AND name = 'dirs'").Scan(&one) != nil {
		return false, nil
	}
	if tx.QueryRow("SELECT 1 FROM "+src+".meta WHERE k = 'tree'").Scan(&one) != nil {
		return false, nil
	}
	for _, q := range []string{treeDDL, "INSERT INTO main.dirs SELECT did, parent, name, path, size, count, nfiles FROM " + src + ".dirs",
		// row order kept: search splits its scans by rowid
		"INSERT INTO main.dir_files SELECT did, name, fid FROM " + src + ".dir_files ORDER BY rowid", treeIndexes} {
		if _, err := tx.Exec(q); err != nil {
			return false, err
		}
	}
	return true, nil
}

// hexSQL: SQL turning a lowercase-hex text column into its bytes, as compactHex does.
func hexSQL(col string) string {
	return fmt.Sprintf("CASE WHEN typeof(%[1]s) = 'text' AND length(%[1]s) > 0 AND length(%[1]s) %% 2 = 0 AND lower(hex(unhex(%[1]s))) = %[1]s THEN unhex(%[1]s) ELSE %[1]s END", col)
}

// Convert rewrites a version 2 index (built by the Python version or by usnfetch before
// version 3) as version 3, in place: nothing is downloaded again. A big share's index comes
// out at roughly half the size. The file must not be open.
func Convert(path string) error {
	if sch, _ := FileInfo(path); sch == SchemaVersion {
		return nil
	} else if sch != 2 {
		return errIndex
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d-v3.tmp", path, os.Getpid())
	db, err := workDB(tmp)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		db.Close()
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	for _, q := range []string{ddl, "ATTACH DATABASE " + quoteSQL(path) + " AS old"} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		"INSERT INTO main.pool SELECT idx, msgid, nonce, tag, size, ref FROM old.pool",
		"INSERT INTO main.files SELECT fid, " + hexSQL("file_id") + ", path, size, " + hexSQL("sha256") + ", start, stride, count, layout, pid FROM old.files",
		"INSERT INTO main.file_offs SELECT fid, seg, idx, off, size FROM old.file_offs",
		"INSERT INTO main.packs SELECT pid, pool_idx, nonce, tag, size FROM old.packs",
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	// segment lists written one row per segment: evenly numbered ones become start/stride
	type rng struct{ fid, start, stride int64 }
	var ranges []rng
	var irregular []int64
	rows, err := tx.Query("SELECT fid, idx FROM old.file_segs ORDER BY fid, seg")
	if err != nil {
		return err
	}
	cur, n, first, stride, prev, even := int64(-1), int64(0), int64(0), int64(0), int64(0), true
	end := func() {
		if cur < 0 {
			return
		}
		if even && (n == 1 || stride >= 1) {
			ranges = append(ranges, rng{cur, first, max(stride, 1)})
		} else {
			irregular = append(irregular, cur)
		}
	}
	for rows.Next() {
		var fid, idx int64
		if err := rows.Scan(&fid, &idx); err != nil {
			rows.Close()
			return err
		}
		if fid != cur {
			end()
			cur, n, first, stride, even = fid, 0, idx, 0, true
		} else if n == 1 {
			stride = idx - prev
		} else if idx-prev != stride {
			even = false
		}
		prev = idx
		n++
	}
	end()
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	upd, err := tx.Prepare("UPDATE main.files SET start = ?, stride = ? WHERE fid = ?")
	if err != nil {
		return err
	}
	for _, r := range ranges {
		if _, err := upd.Exec(r.start, r.stride, r.fid); err != nil {
			upd.Close()
			return err
		}
	}
	upd.Close()
	for _, fid := range irregular {
		if _, err := tx.Exec("INSERT INTO main.file_segs SELECT fid, seg, idx FROM old.file_segs WHERE fid = ?", fid); err != nil {
			return err
		}
	}
	if _, err := copyTree(tx, "old"); err != nil {
		return err
	}
	for _, q := range []string{"CREATE INDEX pool_ref ON pool(ref) WHERE ref IS NOT NULL",
		"INSERT INTO main.meta SELECT k, v FROM old.meta",
		fmt.Sprintf("INSERT OR REPLACE INTO main.meta VALUES ('schema', %d), ('pool', 1), ('converted', %f)", SchemaVersion, float64(time.Now().UnixNano())/1e9)} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = db.Exec("DETACH DATABASE old")
	db.Close()
	done = true
	return replaceFile(tmp, path, st.ModTime())
}

// Slim writes the file list and folder tree of a full index to dst (the share's .usnlist):
// what browsing and search need, without the per-article data downloading needs. The full
// index must have its folder tree (EnsureTree) and must not be written to meanwhile.
func Slim(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d-list.tmp", dst, os.Getpid())
	db, err := workDB(tmp)
	if err != nil {
		return err
	}
	done := false
	defer func() {
		db.Close()
		if !done {
			_ = os.Remove(tmp)
		}
	}()
	for _, q := range []string{ddl, "ATTACH DATABASE " + quoteSQL(src) + " AS src"} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// what a listing shows and needs: name, size, whether it has data (count) or is packed (pid)
	if _, err := tx.Exec("INSERT INTO main.files (fid, path, size, count, pid) SELECT fid, path, size, count, pid FROM src.files"); err != nil {
		return err
	}
	if ok, err := copyTree(tx, "src"); err != nil {
		return err
	} else if !ok {
		return errors.New("the index has no folder tree yet")
	}
	for _, q := range []string{"INSERT INTO main.meta SELECT k, v FROM src.meta",
		"INSERT OR REPLACE INTO main.meta VALUES ('pool', 0)"} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = db.Exec("DETACH DATABASE src")
	db.Close()
	done = true
	return replaceFile(tmp, dst, st.ModTime())
}

// GzipFile zips path to path.gz (keeping its last-used time) and removes path.
func GzipFile(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s%s.%d.tmp", path, GzExt, os.Getpid())
	out, err := os.Create(tmp)
	if err != nil {
		in.Close()
		return err
	}
	zw := gzip.NewWriter(out)
	_, err = io.Copy(zw, in)
	in.Close()
	if err == nil {
		err = zw.Close()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := replaceFile(tmp, path+GzExt, st.ModTime()); err != nil {
		return err
	}
	return os.Remove(path)
}

// GunzipFile unzips path.gz back to path (keeping its last-used time) and removes path.gz.
func GunzipFile(path string) error {
	gz := path + GzExt
	st, err := os.Stat(gz)
	if err != nil {
		return err
	}
	in, err := os.Open(gz)
	if err != nil {
		return err
	}
	defer in.Close()
	zr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d-unzip.tmp", path, os.Getpid())
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, zr)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := replaceFile(tmp, path, st.ModTime()); err != nil {
		return err
	}
	in.Close()
	return os.Remove(gz)
}
