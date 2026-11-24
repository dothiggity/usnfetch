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

// tokenScore: how well one word of a search matches a title: 3 a whole word (or an
// abbreviation: "smb3", "sm64", "loz"), 2 the start of a word ("3" in "3D"), 0 not at all.
func tokenScore(tok string, p prepped) int {
	best := 0
	for i, w := range p.words {
		if w == tok || p.alt[i] == tok {
			return 3
		}
		if strings.HasPrefix(w, tok) || (p.alt[i] != "" && strings.HasPrefix(p.alt[i], tok)) {
			best = 2
		}
	}
	if best == 0 && abbreviates(tok, p) {
		return 3
	}
	return best
}

// abbreviates: tok is the first letters of words in a row, then maybe the number after them:
// "smb3" Super Mario Bros. 3, "sm64" Super Mario 64, "ff7" Final Fantasy VII, "loz" (The)
// Legend of Zelda.
func abbreviates(tok string, p prepped) bool {
	i := len(tok)
	for i > 0 && tok[i-1] >= '0' && tok[i-1] <= '9' {
		i--
	}
	letters, digits := tok[:i], tok[i:]
	if len(letters) < 2 {
		return false
	}
	n := len(letters)
	for st := 0; st+n <= len(p.words); st++ {
		ok := true
		for k := 0; k < n && ok; k++ {
			ok = p.words[st+k][0] == letters[k]
		}
		if !ok {
			continue
		}
		if digits == "" {
			return true
		}
		if st+n < len(p.words) && (p.words[st+n] == digits || p.alt[st+n] == digits) {
			return true
		}
	}
	return false
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
	score  float64
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
		for _, c := range s.knownShares() {
			l, err := share.ParseLink(c.link)
			if err != nil {
				continue
			}
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
				score := 0.0
				for _, t := range toks {
					ts := tokenScore(t, pp)
					if ts == 0 {
						score = 0
						break
					}
					score += float64(ts)
				}
				if score == 0 {
					continue
				}
				score -= 0.15 * float64(max(0, len(pp.words)-len(toks))) // the closer the title, the higher
				k := g.System + "\x00" + strings.Join(pp.words, " ")
				h := groups[k]
				if h == nil {
					h = &gameHit{Title: g.Title, System: g.System, score: score}
					groups[k] = h
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
			// what more of the builds have ranks higher (the well-known games); hacks lower
			h.score += min(1.5, 0.4*float64(len(h.Builds)-1))
			if strings.Contains(strings.ToLower(h.System+" "+h.Title), "hack") {
				h.score--
			}
			out = append(out, h)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].score != out[j].score {
				return out[i].score > out[j].score
			}
			if len(out[i].Builds) != len(out[j].Builds) {
				return len(out[i].Builds) > len(out[j].Builds)
			}
			if out[i].Title != out[j].Title {
				return out[i].Title < out[j].Title
			}
			return out[i].System < out[j].System
		})
		total := len(out)
		if len(out) > limit {
			out = out[:limit]
		}
		return map[string]any{"query": q, "games": out, "total": total, "builds": builds, "ms": time.Since(t0).Milliseconds()}, nil
	})
}
