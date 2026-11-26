package share

import (
	"context"
	"database/sql"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// ---- search and selection scans over the folder tree --------------------------------
//
// A LIKE scan of every file name takes about a second per 2.7 M files in one SQLite
// connection, so scans split dir_files into rowid ranges and run them on several of the
// read-only connections at once (~0.2-0.3 s for the largest shares, no extra disk or RAM).
// Folder paths are small enough (180 k folders for a 20 TB share) to keep in memory.

type dirCache struct {
	paths []string // by did
	lower []string // paths lower-cased
	names []string // last path element, lower-cased
	rows  int64    // MAX(rowid) of dir_files
}

func (ix *Index) dirs() (*dirCache, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.dc != nil {
		return ix.dc, nil
	}
	// a read-only pool of its own, opened now: the folder tables can be newer than the
	// pooled immutable connections, which never notice a schema change
	if ix.sdb == nil {
		db, err := sql.Open("sqlite", sqliteURI(ix.Path, "mode=ro&immutable=1"))
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(16)
		db.SetMaxIdleConns(16)
		ix.sdb = db
	}
	var n int64
	if err := ix.sdb.QueryRow("SELECT COALESCE(MAX(did), -1) FROM dirs").Scan(&n); err != nil {
		return nil, err
	}
	dc := &dirCache{paths: make([]string, n+1), lower: make([]string, n+1), names: make([]string, n+1)}
	rows, err := ix.sdb.Query("SELECT did, path, name FROM dirs")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var did int64
		var p, name sql.NullString
		if err := rows.Scan(&did, &p, &name); err != nil {
			rows.Close()
			return nil, err
		}
		if did >= 0 && did <= n {
			dc.paths[did], dc.lower[did], dc.names[did] = p.String, strings.ToLower(p.String), strings.ToLower(name.String)
		}
	}
	rows.Close()
	if err := ix.sdb.QueryRow("SELECT COALESCE(MAX(rowid), 0) FROM dir_files").Scan(&dc.rows); err != nil {
		return nil, err
	}
	ix.dc = dc
	return dc, nil
}

// under: the dids at or below each of the folder paths ("" = everything), as a mask.
func (dc *dirCache) under(paths []string) []bool {
	mask := make([]bool, len(dc.paths))
	for _, p := range paths {
		p = normDir(p)
		if p == "" {
			for i := range mask {
				mask[i] = true
			}
			return mask
		}
		pre := p + "/"
		for did, dp := range dc.paths {
			if dp == p || strings.HasPrefix(dp, pre) {
				mask[did] = true
			}
		}
	}
	return mask
}

// scanRow is one dir_files row of a scan.
type scanRow struct {
	FID, DID int64
	Name     string
	Size     int64
}

// scan runs `SELECT df.fid, df.did, df.name[, f.size] FROM dir_files df [JOIN files f]
// WHERE <where>` over rowid ranges in parallel and calls fn (from several goroutines,
// one per range; chunk says which) for each row. fn returning false stops that range.
func (ix *Index) scan(ctx context.Context, dc *dirCache, where string, args []any, withSize bool,
	fn func(chunk int, r scanRow) bool) (chunks int, err error) {
	chunks = 1
	if dc.rows > 50_000 {
		chunks = min(max(1, runtime.NumCPU()), 12)
	}
	cols, from := "df.fid, df.did, df.name", "dir_files df"
	if withSize {
		cols, from = cols+", f.size", from+" JOIN files f ON f.fid = df.fid"
	}
	q := "SELECT " + cols + " FROM " + from + " WHERE df.rowid BETWEEN ? AND ?"
	if where != "" {
		q += " AND (" + where + ")"
	}
	step := dc.rows/int64(chunks) + 1
	var wg sync.WaitGroup
	var mu sync.Mutex
	for c := 0; c < chunks; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			lo := int64(c)*step + 1
			rows, e := ix.sdb.QueryContext(ctx, q, append([]any{lo, lo + step - 1}, args...)...)
			if e == nil {
				for rows.Next() {
					var r scanRow
					var name sql.NullString
					var size sql.NullInt64
					if withSize {
						e = rows.Scan(&r.FID, &r.DID, &name, &size)
					} else {
						e = rows.Scan(&r.FID, &r.DID, &name)
					}
					if e != nil {
						break
					}
					r.Name, r.Size = name.String, size.Int64
					if !fn(c, r) {
						break
					}
				}
				if e == nil {
					e = rows.Err()
				}
				rows.Close()
			}
			if e != nil {
				mu.Lock()
				if err == nil {
					err = e
				}
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return chunks, err
}

var likeEscape = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likeArg: a LIKE pattern matching s anywhere (with ESCAPE '\').
func likeArg(s string) string { return "%" + likeEscape.Replace(s) + "%" }

// endsArg: a LIKE pattern matching names that end in s (with ESCAPE '\').
func endsArg(s string) string { return "%" + likeEscape.Replace(s) }

// SearchHit is a matching file with its folder path.
type SearchHit struct {
	File   FileEntry
	Folder string
}

// SearchResult of Search.
type SearchResult struct {
	Folders       []Folder
	Files         []SearchHit
	FolderMatches int
	FileMatches   int64
}

// FileKinds: the kinds of file a share's search can be narrowed to, by name ending.
var FileKinds = map[string][]string{
	"video":    {".mp4", ".mkv", ".avi", ".webm", ".mov", ".m4v", ".wmv", ".mpg", ".mpeg", ".ts", ".flv"},
	"picture":  {".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic"},
	"music":    {".mp3", ".flac", ".ogg", ".opus", ".m4a", ".aac", ".wav", ".wma"},
	"document": {".pdf", ".txt", ".nfo", ".md", ".doc", ".docx", ".rtf", ".epub", ".cbz", ".cbr", ".html", ".htm"},
	"archive":  {".zip", ".7z", ".rar", ".tar", ".gz", ".tgz", ".xz", ".zst", ".bz2", ".001"},
}

// Search finds folders and files under scope ("" = the whole share) whose path
// contains every word of query (case-insensitive), at least one of them in the name
// itself: "snes mario" finds files named *mario* in folders named *snes*, not every
// file of an SNES folder. Results are sorted by path; the totals count every match.
func (ix *Index) Search(ctx context.Context, query, scope string, limit int, sortBy string) (*SearchResult, error) {
	return ix.SearchKind(ctx, query, scope, limit, sortBy, nil)
}

// SearchKind is Search for files whose names end in one of exts (FileKinds), when given:
// files only, and with no words, every such file under scope.
func (ix *Index) SearchKind(ctx context.Context, query, scope string, limit int, sortBy string, exts []string) (*SearchResult, error) {
	words := strings.Fields(strings.ToLower(query))
	res := &SearchResult{Folders: []Folder{}, Files: []SearchHit{}}
	if len(words) == 0 && len(exts) == 0 {
		return res, nil
	}
	hasExt := func(ln string) bool {
		for _, e := range exts {
			if strings.HasSuffix(ln, e) {
				return true
			}
		}
		return len(exts) == 0
	}
	dc, err := ix.dirs()
	if err != nil {
		return nil, err
	}
	inScope := dc.under([]string{scope})
	// per word, the folders whose path contains it
	dirHas := make([][]bool, len(words))
	for i, w := range words {
		dirHas[i] = make([]bool, len(dc.lower))
		for did, p := range dc.lower {
			dirHas[i][did] = strings.Contains(p, w)
		}
	}
	nameHit := func(name string) bool {
		for _, w := range words {
			if strings.Contains(name, w) {
				return true
			}
		}
		return false
	}
	// folders: a word in the folder's own name, every word somewhere in its path (none when
	// it's a kind of file that's asked for)
	var fdids []int
	for did := 1; did < len(dc.paths) && len(exts) == 0; did++ {
		if !inScope[did] || dc.paths[did] == normDir(scope) || !nameHit(dc.names[did]) {
			continue
		}
		all := true
		for i := range words {
			all = all && dirHas[i][did]
		}
		if all {
			fdids = append(fdids, did)
		}
	}
	res.FolderMatches = len(fdids)
	sort.Slice(fdids, func(a, b int) bool { return dc.lower[fdids[a]] < dc.lower[fdids[b]] })
	bySize := sortBy == SortSizeDesc || sortBy == SortSizeAsc
	if len(fdids) > limit && !bySize {
		fdids = fdids[:limit]
	}
	if len(fdids) > 4000 { // by size: the biggest / smallest of the first 4000 by path
		fdids = fdids[:4000]
	}
	if len(fdids) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(fdids)), ",")
		args := make([]any, len(fdids))
		for i, d := range fdids {
			args[i] = d
		}
		rows, err := ix.sdb.QueryContext(ctx, "SELECT name, path, size, count FROM dirs WHERE did IN ("+ph+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var f Folder
			_ = rows.Scan(&f.Name, &f.Path, &f.Size, &f.Count)
			res.Folders = append(res.Folders, f)
		}
		rows.Close()
		sortFolders(res.Folders, sortBy, func(f Folder) string { return f.Path })
		if len(res.Folders) > limit {
			res.Folders = res.Folders[:limit]
		}
	}

	// files: SQL keeps names holding any word (ASCII case-insensitive) and ending in one of
	// exts; the rest is checked here
	var conds, econds []string
	var args []any
	for _, e := range exts {
		econds = append(econds, `df.name LIKE ? ESCAPE '\'`)
		args = append(args, endsArg(e))
	}
	for _, w := range words {
		conds = append(conds, `df.name LIKE ? ESCAPE '\'`)
		args = append(args, likeArg(w))
	}
	where := strings.Join(conds, " OR ")
	if len(econds) > 0 {
		where = "(" + strings.Join(econds, " OR ") + ")"
		if len(conds) > 0 {
			where += " AND (" + strings.Join(conds, " OR ") + ")"
		}
	}
	keep := max(limit, 1)
	var mu sync.Mutex
	type hit struct {
		did  int64
		fid  int64
		key  string
		name string
		size int64
	}
	var hits []hit
	less := func(a, b hit) bool { return a.key < b.key }
	switch sortBy {
	case SortSizeDesc:
		less = func(a, b hit) bool { return a.size > b.size || (a.size == b.size && a.key < b.key) }
	case SortSizeAsc:
		less = func(a, b hit) bool { return a.size < b.size || (a.size == b.size && a.key < b.key) }
	}
	_, err = ix.scan(ctx, dc, where, args, bySize, func(_ int, r scanRow) bool {
		if r.DID < 0 || int(r.DID) >= len(inScope) || !inScope[r.DID] {
			return true
		}
		ln := strings.ToLower(r.Name)
		if !hasExt(ln) || (len(words) > 0 && !nameHit(ln)) {
			return true
		}
		for i, w := range words {
			if !strings.Contains(ln, w) && !dirHas[i][r.DID] {
				return true
			}
		}
		key := dc.lower[r.DID] + "/" + ln
		mu.Lock()
		res.FileMatches++
		hits = append(hits, hit{r.DID, r.FID, key, r.Name, r.Size})
		if len(hits) > 4*keep+1024 { // keep the memory bounded: only the first `limit` by path survive
			sort.Slice(hits, func(a, b int) bool { return less(hits[a], hits[b]) })
			hits = hits[:keep]
		}
		mu.Unlock()
		return true
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(a, b int) bool { return less(hits[a], hits[b]) })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if len(hits) == 0 {
		return res, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(hits)), ",")
	fargs := make([]any, len(hits))
	for i, h := range hits {
		fargs[i] = h.fid
	}
	rows, err := ix.sdb.QueryContext(ctx, "SELECT "+fileCols+" FROM files WHERE fid IN ("+ph+")", fargs...)
	if err != nil {
		return nil, err
	}
	byFID := map[int64]FileEntry{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byFID[f.FID] = f
	}
	rows.Close()
	for _, h := range hits {
		if f, ok := byFID[h.fid]; ok {
			res.Files = append(res.Files, SearchHit{File: f, Folder: dc.paths[h.did]})
		}
	}
	return res, nil
}

// ArchiveNames returns, per folder path, the files under the given folders whose names
// look like archive volumes (.rar/.rNN, .zip/.zNN, .7z, .NNN), with sizes: what a
// download of those folders would hand to the extractor.
func (ix *Index) ArchiveNames(ctx context.Context, folders []string) (map[string][]scanRow, error) {
	dc, err := ix.dirs()
	if err != nil {
		return nil, err
	}
	mask := dc.under(folders)
	where := `df.name LIKE '%.rar' OR df.name LIKE '%.zip' OR df.name LIKE '%.7z' OR df.name GLOB '*.[0-9][0-9][0-9]' OR df.name GLOB '*.[r-zR-Z][0-9][0-9]'`
	out := map[string][]scanRow{}
	var mu sync.Mutex
	_, err = ix.scan(ctx, dc, where, nil, true, func(_ int, r scanRow) bool {
		if r.DID >= 0 && int(r.DID) < len(mask) && mask[r.DID] {
			mu.Lock()
			p := dc.paths[r.DID]
			out[p] = append(out[p], r)
			mu.Unlock()
		}
		return true
	})
	return out, err
}

// TopFolder: the share's single top-level folder when every file lives inside it, else "".
func (ix *Index) TopFolder() (string, error) {
	var rootFiles int64
	if err := ix.db.QueryRow("SELECT nfiles FROM dirs WHERE did = 0").Scan(&rootFiles); err != nil {
		return "", err
	}
	if rootFiles > 0 {
		return "", nil
	}
	rows, err := ix.db.Query("SELECT name FROM dirs WHERE parent = 0 AND did != 0 LIMIT 2")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var tops []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tops = append(tops, n)
	}
	if len(tops) == 1 {
		return tops[0], nil
	}
	return "", nil
}

// TreeFile is a file found by FilesUnder.
type TreeFile struct {
	FID    int64
	Folder string // its folder's path
	Name   string
	Size   int64
}

// FilesUnder calls fn for every file at or below the given folders ("" = everything),
// stopping early when fn returns false. Few folders are read through the folder index,
// many by a parallel scan (fn is then called from several goroutines, one at a time).
func (ix *Index) FilesUnder(ctx context.Context, folders []string, fn func(TreeFile) bool) error {
	dc, err := ix.dirs()
	if err != nil {
		return err
	}
	mask := dc.under(folders)
	var dids []int64
	for did, on := range mask {
		if on {
			dids = append(dids, int64(did))
			if len(dids) > 4000 {
				break
			}
		}
	}
	if len(dids) <= 4000 {
		for i := 0; i < len(dids); i += 500 {
			part := dids[i:min(i+500, len(dids))]
			args := make([]any, len(part))
			for k, d := range part {
				args[k] = d
			}
			ph := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
			rows, err := ix.sdb.QueryContext(ctx, "SELECT df.did, df.name, df.fid, f.size FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did IN ("+ph+")", args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var t TreeFile
				var did int64
				var name sql.NullString
				var size sql.NullInt64
				if err := rows.Scan(&did, &name, &t.FID, &size); err != nil {
					rows.Close()
					return err
				}
				t.Folder, t.Name, t.Size = dc.paths[did], name.String, size.Int64
				if !fn(t) {
					rows.Close()
					return nil
				}
			}
			rows.Close()
		}
		return ctx.Err()
	}
	var mu sync.Mutex
	stop := false
	_, err = ix.scan(ctx, dc, "", nil, true, func(_ int, r scanRow) bool {
		if r.DID < 0 || int(r.DID) >= len(mask) || !mask[r.DID] {
			return true
		}
		mu.Lock()
		defer mu.Unlock()
		if stop {
			return false
		}
		if !fn(TreeFile{FID: r.FID, Folder: dc.paths[r.DID], Name: r.Name, Size: r.Size}) {
			stop = true
			return false
		}
		return true
	})
	return err
}
