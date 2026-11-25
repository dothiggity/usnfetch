package share

import (
	"database/sql"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// One copy per game ("1G1R"): of each game's releases in a system folder (regions, revisions,
// betas), only the best by the regions you prefer, with all of its discs and tracks.

// releaseSplit: a disc, track or side tag, which parts of one release have and the release
// doesn't ("Game (USA) (Disc 2).chd" is part of "Game (USA)").
var releaseSplit = regexp.MustCompile(`(?i)\s*[\(\[](disc|disk|cd|track|side)\s*[^\)\]]*[\)\]]`)
var tagParts = regexp.MustCompile(`[\(\[]([^\)\]]*)[\)\]]`)
var revRE = regexp.MustCompile(`(?i)^(?:rev(?:ision)?\s*([0-9a-z.]+)|v\s*([0-9][0-9.]*))$`)

// releaseRegions: what release tags say, to the region names people prefer.
var releaseRegions = map[string]string{
	"usa": "usa", "us": "usa", "u": "usa", "america": "usa", "north america": "usa", "ntsc-u": "usa", "canada": "usa",
	"world": "world", "w": "world",
	"europe": "europe", "eu": "europe", "e": "europe", "eur": "europe", "pal": "europe", "uk": "europe", "united kingdom": "europe",
	"germany": "europe", "france": "europe", "spain": "europe", "italy": "europe", "netherlands": "europe", "sweden": "europe",
	"scandinavia": "europe", "australia": "australia",
	"japan": "japan", "jp": "japan", "j": "japan", "jpn": "japan", "ntsc-j": "japan",
	"korea": "korea", "k": "korea", "china": "china", "taiwan": "taiwan", "asia": "asia", "brazil": "brazil",
}

// not the real thing: betas, prototypes, demos, hacks…
var lesserWords = []string{"beta", "proto", "prototype", "demo", "sample", "pirate", "hack", "unl", "aftermarket", "kiosk", "debug"}

// goodToolsBad: GoodTools' [b1] bad dump, [h1C] hack, [o] overdump, [t1] trainer, [t+Eng]
// translation, [f1] fix and [p1] pirate flags (not [!] verified or [a] alternate).
var goodToolsBad = regexp.MustCompile(`^(?:[bhotfp][0-9]*[a-z]?|t[+-].*)$`)

type release struct {
	key     string
	files   []GameFile
	region  int // its best region's place in the preference (len = none of them)
	lesser  bool
	rev     float64
	checked bool // [!]: a verified dump (GoodTools)
}

// BestRelease: of one game's files (GameFiles), those of its best release by the regions in
// prefer (best first; names as people write them: "USA", "Europe", "Japan", "World"…).
func BestRelease(files []GameFile, prefer []string) []GameFile {
	rank := map[string]int{}
	for i, p := range prefer {
		w := strings.ToLower(strings.TrimSpace(p))
		if r, ok := releaseRegions[w]; ok {
			w = r
		}
		if _, seen := rank[w]; !seen {
			rank[w] = i
		}
	}
	by := map[string]*release{}
	var order []string
	for _, f := range files {
		name := f.Name
		if !f.Folder {
			name = strings.TrimSuffix(name, path.Ext(name))
		}
		k := strings.ToLower(strings.TrimSpace(releaseSplit.ReplaceAllString(name, "")))
		r := by[k]
		if r == nil {
			r = &release{key: k, region: len(prefer)}
			for _, m := range tagParts.FindAllStringSubmatch(name, -1) {
				tag := strings.TrimSpace(m[1])
				lt := strings.ToLower(tag)
				if lt == "!" {
					r.checked = true
					continue
				}
				if strings.HasPrefix(m[0], "[") && goodToolsBad.MatchString(lt) { // [b1] bad dump, [h1C] hack, [o] overdump, [t] trainer…
					r.lesser = true
					continue
				}
				for _, part := range strings.Split(lt, ",") {
					part = strings.TrimSpace(part)
					if reg, ok := releaseRegions[part]; ok {
						if i, ok := rank[reg]; ok && i < r.region {
							r.region = i
						}
					}
					for _, w := range lesserWords {
						if part == w || strings.HasPrefix(part, w+" ") {
							r.lesser = true
						}
					}
					if rm := revRE.FindStringSubmatch(part); rm != nil {
						v := rm[1] + rm[2]
						if n, err := strconv.ParseFloat(v, 64); err == nil {
							r.rev = n
						} else if len(v) == 1 && v[0] >= 'a' && v[0] <= 'z' { // Rev A, Rev B
							r.rev = float64(v[0]-'a') + 1
						}
					}
				}
			}
			by[k] = r
			order = append(order, k)
		}
		r.files = append(r.files, f)
	}
	if len(order) == 0 {
		return nil
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := by[order[i]], by[order[j]]
		switch {
		case a.lesser != b.lesser:
			return !a.lesser
		case a.region != b.region:
			return a.region < b.region
		case a.rev != b.rev:
			return a.rev > b.rev
		case a.checked != b.checked:
			return a.checked
		}
		return len(a.key) < len(b.key) // the plainest name: fewest extra tags
	})
	return by[order[0]].files
}

// OnePerGame: of a system folder's games, the files of each one's best release (BestRelease);
// and how many files were left out (other regions, revisions, betas). Artwork, game lists and
// the like aren't games, so they're neither kept nor counted.
func (ix *Index) OnePerGame(folder string, prefer []string) ([]GameFile, int, error) {
	d, err := ix.Dir(folder)
	if err != nil || d == nil {
		return nil, 0, err
	}
	groups := map[string][]GameFile{}
	var keys []string
	add := func(g GameFile) {
		k := gameKey(g.Name, g.Folder)
		if k == "" {
			return
		}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], g)
	}
	rows, err := ix.rw.Query("SELECT f.fid, df.name, f.path, f.size FROM dir_files df JOIN files f ON f.fid = df.fid WHERE df.did = ? ORDER BY df.name", d.DID)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var g GameFile
		var size sql.NullInt64
		if err := rows.Scan(&g.FID, &g.Name, &g.Path, &size); err != nil {
			rows.Close()
			return nil, 0, err
		}
		g.Size = size.Int64
		add(g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	drows, err := ix.rw.Query("SELECT name, path, size, count FROM dirs WHERE parent = ? ORDER BY name", d.DID)
	if err != nil {
		return nil, 0, err
	}
	for drows.Next() {
		g := GameFile{Folder: true}
		var size, count sql.NullInt64
		if err := drows.Scan(&g.Name, &g.Path, &size, &count); err != nil {
			drows.Close()
			return nil, 0, err
		}
		g.Size, g.Count = size.Int64, count.Int64
		add(g)
	}
	drows.Close()
	var keep []GameFile
	left := 0
	for _, k := range keys {
		best := BestRelease(groups[k], prefer)
		keep = append(keep, best...)
		for _, g := range groups[k] {
			if g.Folder {
				left += int(g.Count) // a game folder counts its files
			} else {
				left++
			}
		}
		for _, g := range best {
			if g.Folder {
				left -= int(g.Count)
			} else {
				left--
			}
		}
	}
	return keep, left, drows.Err()
}
