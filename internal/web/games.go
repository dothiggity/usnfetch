package web

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/share"
)

// Game search: "mario 3" finds Super Mario Bros. 3 and Super Mario 3D World, one result per
// game and system, with the builds that have it. Each opened build's game list (share.Games)
// is kept as <data>/games/<folder id>.json.gz; searches read them into memory, and let them go
// a few minutes after the last search.

func (s *Server) gamesFile(folderID string) string {
	if folderID == "" || strings.ContainsAny(folderID, `/\.`) {
		return ""
	}
	return filepath.Join(s.M.DataDir, "games", folderID+".json.gz")
}

// biosFile: where a build's BIOS files (share.Bioses) are kept, next to its games.
func (s *Server) biosFile(folderID string) string {
	if p := s.gamesFile(folderID); p != "" {
		return strings.TrimSuffix(p, ".json.gz") + ".bios.json.gz"
	}
	return ""
}

// saveInside works out a build's systems, games and BIOS files from its open index and keeps
// them: the systems with the share's details (info), the games and BIOS files in files of
// their own.
func (s *Server) saveInside(ix *share.Index, info map[string]any) {
	folder := shareFolder(ix, info)
	if s.insideKept(ix, info, folder) { // worked out from this very index before: nothing to do over
		return
	}
	info["packed"] = ix.PackedFiles()
	sys, games, gerr := ix.Games()
	if gerr != nil || sys == nil {
		sys, games = []share.System{}, nil
	}
	bios, berr := ix.Bioses()
	info["inside"], info["inside_v"], info["bios_n"] = sys, insideVersion, len(bios)
	if p := s.gamesFile(folder); p != "" {
		writeList(p, games)
		s.games.forget(folder)
	}
	if p := s.biosFile(folder); p != "" {
		writeList(p, bios)
		s.bios.forget(folder)
	}
	delete(info, "inside_of")
	if gerr == nil && berr == nil { // a pass that failed is done again next time
		info["inside_of"], info["lists"] = share.Key(ix.Path), s.listsMark(folder)
	}
}

// shareFolder: the share's folder ID, which names its game and BIOS lists.
func shareFolder(ix *share.Index, info map[string]any) string {
	folder, _ := ix.Info["folder_id"].(string)
	if folder == "" {
		folder, _ = info["folder_id"].(string)
	}
	return folder
}

// listsMark: what the folder's game and BIOS list files are now (their times and sizes; 0 for
// one that isn't there), to tell later whether they're still the ones written.
func (s *Server) listsMark(folder string) string {
	mark := func(p string) string {
		if st, err := os.Stat(p); p != "" && err == nil {
			return strconv.FormatInt(st.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(st.Size(), 10)
		}
		return "0"
	}
	return mark(s.gamesFile(folder)) + "|" + mark(s.biosFile(folder))
}

// insideKept: the share's systems, packed-file count and game and BIOS lists were worked out
// from this same index (a re-posted share has a new one) by this version of the code, and the
// list files are still the ones written then: not removed by a reset, nor rewritten for
// another post of the same folder. Working them out takes seconds on a big build, so opening
// it again uses them as they are.
func (s *Server) insideKept(ix *share.Index, info map[string]any, folder string) bool {
	if str(info["inside_of"]) != share.Key(ix.Path) || num(info["inside_v"], -1) != insideVersion || info["packed"] == nil {
		return false
	}
	return folder != "" && str(info["lists"]) == s.listsMark(folder)
}

// shareKindSearch: one share's games (kind "games") or BIOS files ("bios") matching q, or all
// of them when q is empty, under scope; from the build's own lists and with the matching of
// the Search page, best first. no_list: the build has no such list (not a game build, or not
// worked out yet).
func (s *Server) shareKindSearch(ss *session, ix *share.Index, kind, q, scope string, limit int) map[string]any {
	t0 := time.Now()
	folder, _ := ix.Info["folder_id"].(string)
	norm := func(p string) string { // as the folder tree has paths: "/", no empty parts
		return strings.Join(strings.FieldsFunc(strings.ReplaceAll(p, "\\", "/"), func(r rune) bool { return r == '/' }), "/")
	}
	scope = norm(scope)
	under := func(p string) bool {
		p = norm(p)
		return scope == "" || p == scope || strings.HasPrefix(p, scope+"/")
	}
	toks := strings.Fields(share.TitleKey(q))
	if strings.TrimSpace(q) != "" && len(toks) == 0 { // only punctuation: nothing to match on
		return map[string]any{"q": q, "path": scope, "kind": kind, "items": []any{}, "folders": []any{}, "files": []any{},
			"folder_matches": 0, "file_matches": 0, "ms": time.Since(t0).Milliseconds()}
	}
	type hit struct {
		path, system, label, name string
		folder, file              string // a game's system folder and file, as its list has them
		score                     int
		known                     bool
	}
	var hits []hit
	noList := true
	if kind == "games" {
		if set := s.games.load(folder, s.gamesFile(folder), readGames); set != nil {
			noList = false
			for i, g := range set.games {
				p := g.Folder + "/" + g.Name
				if !under(p) {
					continue
				}
				sc, ok := 0, true
				if len(toks) > 0 {
					sc, ok = titleScore(toks, set.prep[i])
				}
				if ok {
					hits = append(hits, hit{path: p, system: g.System, name: g.Title, folder: g.Folder, file: g.Name, score: sc})
				}
			}
		}
	} else if set := s.bios.load(folder, s.biosFile(folder), readBios); set != nil {
		noList = false
		for i, b := range set.list {
			if !under(b.Path) {
				continue
			}
			sc, ok := 0, true
			if len(toks) > 0 {
				sc, ok = biosScore(toks, set.strong[i], set.weak[i])
			}
			if ok {
				hits = append(hits, hit{path: b.Path, system: b.System, label: b.Label, name: b.Name, score: sc, known: b.Known})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.known != b.known {
			return a.known
		}
		if x, y := strings.ToLower(a.name), strings.ToLower(b.name); x != y {
			return x < y
		}
		return a.path < b.path
	})
	total := len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	// a game is all its files (discs, tracks, regions, revisions), read once per system folder
	parts := map[string]map[string][]share.GameFile{}
	if kind == "games" {
		byFolder := map[string][]string{}
		for _, h := range hits {
			byFolder[h.folder] = append(byFolder[h.folder], h.file)
		}
		for f, names := range byFolder {
			parts[f], _ = ix.GameFilesIn(f, names)
		}
	}
	roots, ls := s.localRoots(ss, ix), listings{}
	fileItem := func(fe share.FileEntry, h hit) map[string]any {
		fj := fileJSON(fe)
		fj["type"] = "file"
		if d := path.Dir(strings.ReplaceAll(fe.Path, "\\", "/")); d != "." {
			fj["folder"] = d
		}
		fj["system"], fj["label"] = h.system, h.label
		if st := fileState(ls, roots, fe.Path, fe.Size); st != "" {
			fj["local"] = st
		}
		return fj
	}
	folderItem := func(p string, size, count int64, h hit) map[string]any {
		return map[string]any{"type": "folder", "name": path.Base(p), "path": p, "size": size, "count": count,
			"local": folderThere(ls, roots, p), "system": h.system}
	}
	// in rank order; a game of several files is one item with them all (its parts)
	items := []map[string]any{}
	for _, h := range hits {
		if gf := parts[h.folder][h.file]; len(gf) > 1 {
			var size int64
			var ps []map[string]any
			for _, g := range gf {
				size += g.Size
				if g.Folder {
					ps = append(ps, folderItem(g.Path, g.Size, g.Count, h))
				} else if fe, _ := ix.File(g.FID); fe != nil {
					ps = append(ps, fileItem(*fe, h))
				}
			}
			items = append(items, map[string]any{"type": "game", "name": h.name, "system": h.system, "folder": h.folder, "size": size, "parts": ps})
		} else if fe, _ := ix.FileByPath(h.path); fe != nil {
			items = append(items, fileItem(*fe, h))
		} else if d, _ := ix.Dir(h.path); d != nil { // a game kept in a folder of its own
			items = append(items, folderItem(d.Path, d.Size, d.Count, h))
		}
	}
	return map[string]any{"q": q, "path": scope, "kind": kind, "no_list": noList, "items": items, "folders": []any{}, "files": []any{},
		"folder_matches": 0, "file_matches": total, "ms": time.Since(t0).Milliseconds()}
}

// writeList keeps a list as zipped JSON at p (an empty one: no file).
func writeList[T any](p string, list []T) {
	if len(list) == 0 {
		_ = os.Remove(p)
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	zw := gzip.NewWriter(f)
	err = json.NewEncoder(zw).Encode(list)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
	}
}

// ---- matching ---------------------------------------------------------------------------

// roman numerals as numbers, both ways: "ff7" finds Final Fantasy VII, "zelda ii" Zelda 2
var romanNum = map[string]string{"ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9",
	"x": "10", "xi": "11", "xii": "12", "xiii": "13", "xiv": "14", "xv": "15", "xvi": "16"}
var numRoman = func() map[string]string {
	m := map[string]string{}
	for r, n := range romanNum {
		m[n] = r
	}
	return m
}()

// prepped: a title's words, and each word's other spelling as a number ("vii" / "7").
type prepped struct {
	words, alt []string
}

func prep(title string) prepped {
	w := strings.Fields(share.TitleKey(title))
	alt := make([]string, len(w))
	for i, x := range w {
		if n, ok := romanNum[x]; ok {
			alt[i] = n
		} else if r, ok := numRoman[x]; ok {
			alt[i] = r
		}
	}
	return prepped{w, alt}
}

// tokenMatch: how well one word of a search matches a title (in hundredths), and which of the
// title's words it stands for (from, to): 300 a whole word, 200 the start of a word ("3" in
// "3D"), a little over 300 an abbreviation, standing for all the words it abbreviates ("smb3":
// Super Mario Bros. 3, above an arcade set named smb3); 0 not at all.
func tokenMatch(tok string, p prepped) (score, from, to int) {
	pre := -1
	for i, w := range p.words {
		if w == tok || p.alt[i] == tok {
			return 300, i, i + 1
		}
		if pre < 0 && (strings.HasPrefix(w, tok) || (p.alt[i] != "" && strings.HasPrefix(p.alt[i], tok))) {
			pre = i
		}
	}
	if pre >= 0 {
		return 200, pre, pre + 1
	}
	if from, to, ok := abbreviates(tok, p); ok {
		return 300 + 5*(to-from-1), from, to
	}
	return 0, 0, 0
}

// stopWords: small words a search may leave out without it counting against the title.
var stopWords = map[string]bool{"the": true, "of": true, "a": true, "an": true, "and": true, "in": true, "on": true, "to": true, "for": true}

// titleScore: how well a search (its words, from TitleKey) matches a title, in hundredths;
// ok is false unless every word of the search matches. The title's words the search leaves
// out count against it, so the closer the title the higher: for "mario 3", Mario Party 3
// above Mario Hoops 3 on 3, and for "smb3", Super Mario Bros. 3 above Super Mario Advance 4.
func titleScore(toks []string, p prepped) (score int, ok bool) {
	var covered uint64
	for _, t := range toks {
		s, from, to := tokenMatch(t, p)
		if s == 0 {
			return 0, false
		}
		score += s
		for i := from; i < to && i < 64; i++ {
			covered |= 1 << i
		}
	}
	for i, w := range p.words {
		if (i >= 64 || covered&(1<<i) == 0) && !stopWords[w] {
			score -= 30
		}
	}
	return score, true
}

// abbreviates: tok is the first letters of words in a row, then maybe the number after them:
// "smb3" Super Mario Bros. 3, "sm64" Super Mario 64, "ff7" Final Fantasy VII, "loz" (The)
// Legend of Zelda. from and to are the words it stands for.
func abbreviates(tok string, p prepped) (from, to int, ok bool) {
	i := len(tok)
	for i > 0 && tok[i-1] >= '0' && tok[i-1] <= '9' {
		i--
	}
	letters, digits := tok[:i], tok[i:]
	if len(letters) < 2 {
		return 0, 0, false
	}
	n := len(letters)
	for st := 0; st+n <= len(p.words); st++ {
		match := true
		for k := 0; k < n && match; k++ {
			match = p.words[st+k][0] == letters[k]
		}
		if !match {
			continue
		}
		if digits == "" {
			return st, st + n, true
		}
		if st+n < len(p.words) && (p.words[st+n] == digits || p.alt[st+n] == digits) {
			return st, st + n + 1, true
		}
	}
	return 0, 0, false
}

// ---- the lists in memory ----------------------------------------------------------------

// listCache: the builds' lists (games, BIOS files) in memory by folder id, each read again
// when its file changes. They go a few minutes after the last search (they take some memory).
type listCache[T any] struct {
	mu    sync.Mutex
	sets  map[string]cachedList[T]
	timer *time.Timer
}

type cachedList[T any] struct {
	mod time.Time
	v   *T
}

func (c *listCache[T]) forget(folder string) {
	c.mu.Lock()
	delete(c.sets, folder)
	c.mu.Unlock()
}

// load: a build's list, from its file at p by read; nil if it has none.
func (c *listCache[T]) load(folder, p string, read func(io.Reader) (*T, error)) *T {
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	g, ok := c.sets[folder]
	c.mu.Unlock()
	if ok && g.mod.Equal(st.ModTime()) {
		return g.v
	}
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil
	}
	v, err := read(zr)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	if c.sets == nil {
		c.sets = map[string]cachedList[T]{}
	}
	c.sets[folder] = cachedList[T]{st.ModTime(), v}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(5*time.Minute, func() {
		c.mu.Lock()
		c.sets = nil
		c.mu.Unlock()
		app.ReleaseMemory()
	})
	c.mu.Unlock()
	return v
}

type gameSet struct {
	games []share.Game
	prep  []prepped
}

func readGames(r io.Reader) (*gameSet, error) {
	g := &gameSet{}
	if err := json.NewDecoder(r).Decode(&g.games); err != nil {
		return nil, err
	}
	g.prep = make([]prepped, len(g.games))
	for i, x := range g.games {
		g.prep[i] = prep(x.Title)
	}
	return g, nil
}

// ---- the search -------------------------------------------------------------------------

// gameBuild: a build that has a game (or BIOS file), and where.
type gameBuild struct {
	ID        string `json:"id"`
	CatalogID string `json:"catalog_id,omitempty"`
	Link      string `json:"link,omitempty"`
	Title     string `json:"title"`  // the build
	Folder    string `json:"folder"` // the game's system folder in it (a BIOS file's folder)
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"` // a BIOS file's
	Size      int64  `json:"size"`
	Count     int64  `json:"count,omitempty"` // the game's files
	Full      bool   `json:"full,omitempty"`  // its full index is cached: downloads start right away
}

type gameHit struct {
	Title  string      `json:"title"`
	System string      `json:"system"`
	Builds []gameBuild `json:"builds"`
	Have   []string    `json:"have,omitempty"` // the libraries that have it already (library.go: markHave)
	score  int         // titleScore, then rankGames' adjustments
	key    string      // the title's words (TitleKey)
	words  int
}

// rankGames: the best matches first. A game more of the builds have ranks a little higher
// (the well-known ones), though never above a closer title; a hack well below the games it
// hacks, and below the near misses ("mario 3": Super Mario 3D World). Equal ones go
// shorter title first ("Super Mario Bros. 3" before "Mario Hoops 3 on 3"), then by name and
// system (the NES game before the Wii one of the same name).
func rankGames(out []*gameHit) {
	for _, h := range out {
		h.score += min(75, 25*(len(h.Builds)-1))
		if strings.Contains(strings.ToLower(h.System+" "+h.Title), "hack") {
			h.score -= 200
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if len(a.Builds) != len(b.Builds) {
			return len(a.Builds) > len(b.Builds)
		}
		if a.words != b.words {
			return a.words < b.words
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.System < b.System
	})
}

// searchBuild: a build a search looks in (one with a game list or a BIOS list).
type searchBuild struct {
	folder, id, catalogID, link, title string
	full                               bool
}

func (b searchBuild) at(folder, name string, size int64) gameBuild {
	return gameBuild{ID: b.id, CatalogID: b.catalogID, Link: b.link, Title: b.title, Folder: folder, Name: name, Size: size, Full: b.full}
}

// searchBuilds: the builds with a game or BIOS list, once each however many links a share
// is known by (the catalog's first).
func (s *Server) searchBuilds() []searchBuild {
	seen := map[string]bool{}
	var out []searchBuild
	for _, c := range s.knownShares() {
		l, err := share.ParseLink(c.link)
		if err != nil || seen[l.FolderID] || s.gamesFile(l.FolderID) == "" {
			continue
		}
		seen[l.FolderID] = true
		if _, err := os.Stat(s.gamesFile(l.FolderID)); err != nil {
			if _, err := os.Stat(s.biosFile(l.FolderID)); err != nil {
				continue
			}
		}
		b := searchBuild{folder: l.FolderID, id: app.LinkID(c.link), catalogID: c.catalogID, title: c.title,
			full: strings.HasSuffix(s.cachedIndex(l.FolderID), ".usnindex")}
		if c.catalogID == "" {
			b.link = c.link
		}
		out = append(out, b)
	}
	return out
}

// knownShares: every share usnfetch knows the link of (catalog, or opened by its link).
func (s *Server) knownShares() []cachedShare {
	seen := map[string]bool{}
	var out []cachedShare
	add := func(link, title, catalogID string) {
		if seen[link] {
			return
		}
		seen[link] = true
		out = append(out, cachedShare{link: link, title: title, catalogID: catalogID})
	}
	for _, e := range s.Cat.Snapshot() {
		add(e.Link, e.Label(), app.LinkID(e.Link))
	}
	for link, info := range s.M.Store.AllShareInfo() {
		if name, _ := info["name"].(string); name != "" {
			add(link, name, "")
		}
	}
	return out
}

func (s *Server) gameRoutes() {
	// one game of an open build, to download it from a search: its files and folders as a
	// selection for /plan and /download (?folder= its system folder, ?name= one of its files)
	s.api("GET /api/shares/{sid}/game", func(w http.ResponseWriter, r *http.Request) (any, error) {
		_, ix, err := s.ready(r.PathValue("sid"))
		if err != nil {
			return nil, err
		}
		items, err := ix.GameFiles(r.URL.Query().Get("folder"), r.URL.Query().Get("name"))
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, fail(404, "this build doesn't have that game any more", "no_game")
		}
		paths, folders := []string{}, []string{}
		var files, size int64
		for _, it := range items {
			if it.Folder {
				folders = append(folders, it.Path)
				files += it.Count
			} else {
				paths = append(paths, it.Path)
				files++
			}
			size += it.Size
		}
		return map[string]any{"items": items, "paths": paths, "folders": folders, "files": files, "size": size}, nil
	})

	// one file of an open build by its path (a BIOS file from a search), to stream it
	s.api("GET /api/shares/{sid}/fileid", func(w http.ResponseWriter, r *http.Request) (any, error) {
		_, ix, err := s.ready(r.PathValue("sid"))
		if err != nil {
			return nil, err
		}
		fe, err := ix.FileByPath(r.URL.Query().Get("path"))
		if err != nil {
			return nil, err
		}
		if fe == nil {
			return nil, fail(404, "this build doesn't have that file any more", "no_file")
		}
		return map[string]any{"fid": fe.FID, "size": fe.Size, "name": path.Base(strings.ReplaceAll(fe.Path, "\\", "/"))}, nil
	})

	s.api("GET /api/search/games", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := share.TitleKey(r.URL.Query().Get("q"))
		toks := strings.Fields(q)
		if len([]rune(q)) < 2 {
			return nil, fail(400, "type at least two letters", "short")
		}
		limit := min(300, max(1, num(r.URL.Query().Get("limit"), 80)))
		t0 := time.Now()
		groups := map[string]*gameHit{}
		builds := 0
		for _, sb := range s.searchBuilds() {
			set := s.games.load(sb.folder, s.gamesFile(sb.folder), readGames)
			if set == nil {
				continue
			}
			builds++
			for i, g := range set.games {
				pp := set.prep[i]
				score, ok := titleScore(toks, pp)
				if !ok {
					continue
				}
				key := strings.Join(pp.words, " ")
				h := groups[g.System+"\x00"+key]
				if h == nil {
					h = &gameHit{Title: g.Title, System: g.System, score: score, key: key, words: len(pp.words)}
					groups[g.System+"\x00"+key] = h
				}
				b := sb.at(g.Folder, g.Name, g.Size)
				b.Count = max(1, g.Count)
				dup := false
				for _, x := range h.Builds {
					dup = dup || x.ID == b.ID
				}
				if !dup {
					h.Builds = append(h.Builds, b)
				}
			}
		}
		out := make([]*gameHit, 0, len(groups))
		for _, h := range groups {
			sort.Slice(h.Builds, func(i, j int) bool { return h.Builds[i].Title < h.Builds[j].Title })
			out = append(out, h)
		}
		rankGames(out)
		total := len(out)
		if len(out) > limit {
			out = out[:limit]
		}
		s.markHave(out)
		return map[string]any{"query": q, "games": out, "total": total, "builds": builds, "ms": time.Since(t0).Milliseconds()}, nil
	})
}
