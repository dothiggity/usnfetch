package web

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// saveInside works out a build's systems and games from its open index and keeps them: the
// systems with the share's details (info), the games in the build's games file.
func (s *Server) saveInside(ix *share.Index, info map[string]any) {
	sys, games, err := ix.Games()
	if err != nil || sys == nil {
		sys, games = []share.System{}, nil
	}
	info["inside"], info["inside_v"] = sys, insideVersion
	folder, _ := ix.Info["folder_id"].(string)
	if folder == "" {
		folder, _ = info["folder_id"].(string)
	}
	p := s.gamesFile(folder)
	if p == "" {
		return
	}
	if len(games) == 0 {
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
	err = json.NewEncoder(zw).Encode(games)
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp, p) != nil {
		_ = os.Remove(tmp)
	}
	s.games.forget(folder)
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

// ---- the game lists in memory -----------------------------------------------------------

type gameSet struct {
	mod   time.Time
	games []share.Game
	prep  []prepped
}

type gameCache struct {
	mu    sync.Mutex
	sets  map[string]*gameSet // by folder id
	timer *time.Timer
}

func (c *gameCache) forget(folder string) {
	c.mu.Lock()
	delete(c.sets, folder)
	c.mu.Unlock()
}

// load: a build's games (read again when its file changed), nil if it has none.
func (c *gameCache) load(folder, p string) *gameSet {
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	if c.sets == nil {
		c.sets = map[string]*gameSet{}
	}
	g := c.sets[folder]
	c.mu.Unlock()
	if g != nil && g.mod.Equal(st.ModTime()) {
		return g
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
	g = &gameSet{mod: st.ModTime()}
	if json.NewDecoder(zr).Decode(&g.games) != nil {
		return nil
	}
	g.prep = make([]prepped, len(g.games))
	for i, x := range g.games {
		g.prep[i] = prep(x.Title)
	}
	c.mu.Lock()
	c.sets[folder] = g
	// the lists go a few minutes after the last search (they take some memory)
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
	return g
}

// ---- the search -------------------------------------------------------------------------

type gameBuild struct {
	ID        string `json:"id"`
	CatalogID string `json:"catalog_id,omitempty"`
	Link      string `json:"link,omitempty"`
	Title     string `json:"title"`  // the build
	Folder    string `json:"folder"` // the game's system folder in it
	Name      string `json:"name"`
	Size      int64  `json:"size"`
}

type gameHit struct {
	Title  string      `json:"title"`
	System string      `json:"system"`
	Builds []gameBuild `json:"builds"`
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
		seen := map[string]bool{} // a share opened from the catalog and by its link: once
		for _, c := range s.knownShares() {
			l, err := share.ParseLink(c.link)
			if err != nil || seen[l.FolderID] {
				continue
			}
			seen[l.FolderID] = true
			p := s.gamesFile(l.FolderID)
			if p == "" {
				continue
			}
			set := s.games.load(l.FolderID, p)
			if set == nil {
				continue
			}
			builds++
			id := app.LinkID(c.link)
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
				b := gameBuild{ID: id, CatalogID: c.catalogID, Title: c.title, Folder: g.Folder, Name: g.Name, Size: g.Size}
				if c.catalogID == "" {
					b.Link = c.link
				}
				dup := false
				for _, x := range h.Builds {
					dup = dup || x.ID == id
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
		return map[string]any{"query": q, "games": out, "total": total, "builds": builds, "ms": time.Since(t0).Milliseconds()}, nil
	})
}
