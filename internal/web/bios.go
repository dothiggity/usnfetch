package web

import (
	"encoding/json"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"usnfetch/internal/share"
)

// BIOS search: "ps1 bios usa" finds scph1001.bin, scph5501.bin and scph7001.bin in every
// build opened before, one result per file (same name and size) with the builds that have
// it. Each build's BIOS files (share.Bioses) are kept next to its games (games.go).

type biosSet struct {
	list         []share.Bios
	strong, weak [][]string
}

func readBios(r io.Reader) (*biosSet, error) {
	b := &biosSet{}
	if err := json.NewDecoder(r).Decode(&b.list); err != nil {
		return nil, err
	}
	b.strong, b.weak = make([][]string, len(b.list)), make([][]string, len(b.list))
	for i, x := range b.list {
		b.strong[i], b.weak[i] = share.BiosWords(x)
	}
	return b, nil
}

// biosScore: how well a search matches a BIOS file, in hundredths; ok is false unless every
// word of the search matches one of the file's words (300 a whole one, 200 the start of
// one) or of the folders it is in (100).
func biosScore(toks, strong, weak []string) (score int, ok bool) {
	for _, t := range toks {
		best := 0
		for _, w := range strong {
			if w == t {
				best = 300
				break
			}
			if best < 200 && strings.HasPrefix(w, t) {
				best = 200
			}
		}
		if best == 0 {
			for _, w := range weak {
				if strings.HasPrefix(w, t) {
					best = 100
					break
				}
			}
		}
		if best == 0 {
			return 0, false
		}
		score += best
	}
	return score, true
}

type biosHit struct {
	Name   string      `json:"name"`
	Label  string      `json:"label,omitempty"`
	System string      `json:"system,omitempty"`
	Region string      `json:"region,omitempty"`
	Size   int64       `json:"size"`
	Known  bool        `json:"known,omitempty"`
	Part   bool        `json:"part,omitempty"`
	Builds []gameBuild `json:"builds"`
	score  int
}

// rankBios: the best matches first; a BIOS file known by its name above one only found by
// where it is; a BIOS's side files (NVM, MEC) below it; then by what it is and its name.
func rankBios(out []*biosHit) {
	for _, h := range out {
		h.score += min(75, 25*(len(h.Builds)-1))
		if h.Known {
			h.score += 150
		}
		if h.Part {
			h.score -= 100
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.Label != b.Label {
			return a.Label < b.Label
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func (s *Server) biosRoutes() {
	s.api("GET /api/search/bios", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := share.TitleKey(r.URL.Query().Get("q"))
		toks := strings.Fields(q)
		if len([]rune(q)) < 2 {
			return nil, fail(400, "type at least two letters", "short")
		}
		limit := min(300, max(1, num(r.URL.Query().Get("limit"), 80)))
		t0 := time.Now()
		groups := map[string]*biosHit{}
		builds := 0
		for _, sb := range s.searchBuilds() {
			set := s.bios.load(sb.folder, s.biosFile(sb.folder), readBios)
			if set == nil {
				continue
			}
			builds++
			for i, b := range set.list {
				score, ok := biosScore(toks, set.strong[i], set.weak[i])
				if !ok {
					continue
				}
				k := strings.ToLower(b.Name) + "\x00" + strconv.FormatInt(b.Size, 10)
				h := groups[k]
				if h == nil {
					h = &biosHit{Name: b.Name, Label: b.Label, System: b.System, Region: b.Region, Size: b.Size, Known: b.Known, Part: b.Part, score: score}
					groups[k] = h
				}
				if len(h.Builds) > 0 && h.Builds[len(h.Builds)-1].ID == sb.id {
					continue // the same file twice in one build: the first
				}
				dir := path.Dir(strings.ReplaceAll(b.Path, "\\", "/"))
				gb := sb.at(dir, b.Name, b.Size)
				gb.Path, gb.Count = b.Path, 1
				h.Builds = append(h.Builds, gb)
			}
		}
		out := make([]*biosHit, 0, len(groups))
		for _, h := range groups {
			sort.Slice(h.Builds, func(i, j int) bool { return h.Builds[i].Title < h.Builds[j].Title })
			out = append(out, h)
		}
		rankBios(out)
		total := len(out)
		if len(out) > limit {
			out = out[:limit]
		}
		return map[string]any{"query": q, "bios": out, "total": total, "builds": builds, "ms": time.Since(t0).Milliseconds()}, nil
	})
}
