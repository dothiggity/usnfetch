package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/share"
)

// Into a library (app.Library): where each file of a selection goes on the device. A file in
// one of the build's system folders goes into the library's folder for that system, keeping
// what's below it (a game's own folder, the images/ beside games on an EmulationStation build);
// a BIOS file goes into the library's BIOS folder. The rest has no place there and stays out.

// maxInstall: a selection bigger than this doesn't go into a library in one go.
const maxInstall = 100000

type sysMap struct {
	From   string `json:"from"`   // the build's folder
	System string `json:"system"` // for people
	To     string `json:"to"`     // the library's
	Files  int    `json:"files"`
}

type installPlan struct {
	Dest       map[string]string // share path (with /) -> full path in the library
	Meta       []app.LibMeta     // the games' entries for the device's game lists (app/libmeta.go)
	Media      int               // pictures, videos and manuals going along with the games
	Lists      int               // game lists getting entries
	Files      int
	Size       int64
	Systems    []*sysMap
	Bios       int
	BiosDir    string
	Skipped    int
	SkipEx     []string // a few of them
	There      int      // already in the library at the same size (skipped by the download)
	ThereBytes int64
}

func (p *installPlan) view(lib app.Library) map[string]any {
	var free any
	if f, err := app.DiskFree(lib.Path); err == nil {
		free = f
	}
	return map[string]any{"library": lib.ID, "name": lib.Name, "kind": app.LibraryKindName(lib.Kind), "path": lib.Path, "free": free,
		"files": p.Files, "size": p.Size, "systems": p.Systems, "bios": p.Bios, "bios_dir": p.BiosDir, "media": p.Media, "lists": p.Lists,
		"lists_note": listsNote(lib),
		"skipped":    p.Skipped, "skipped_examples": p.SkipEx, "there": p.There, "there_bytes": p.ThereBytes, "too_many": p.Files >= maxInstall}
}

// planInstall works out where a selection's files go in a library.
func (s *Server) planInstall(ctx context.Context, ix *share.Index, sel *selection, lib app.Library) (*installPlan, error) {
	systems, err := ix.Inside()
	if err != nil {
		return nil, err
	}
	sort.Slice(systems, func(i, j int) bool { return len(systems[i].Folder) > len(systems[j].Folder) }) // deepest first
	have, err := lib.Systems()
	if err != nil {
		return nil, fail(400, "can't read the library's folders: "+err.Error(), "library")
	}
	bios := map[string]share.Bios{}
	if bs, err := ix.Bioses(); err == nil {
		for _, b := range bs {
			bios[slash(b.Path)] = b
		}
	}
	p := &installPlan{Dest: map[string]string{}, BiosDir: lib.BiosDir()}
	bySys := map[string]*sysMap{}
	roms := lib.RomsDir()
	finder := newMediaFinder(ctx, ix)
	metas := map[string]*app.LibMeta{} // the build's system folder -> its games for the device's list
	units := map[string]bool{}         // games seen (a file, or a game's folder)
	tmp := filepath.Join(s.M.DataDir, "jobtmp", app.RandomID(6))
	// media: a game's pictures, video and manual, where the library keeps them (media.go)
	media := func(sy share.System, m *sysMap, unitRel string) {
		title := share.GameTitle(path.Base(unitRel))
		osRel := app.SafeRelPath(unitRel)
		lm := metas[sy.Folder]
		if lm == nil {
			lm = &app.LibMeta{List: lib.GameListFor(m.To)}
			if lm.List != "" {
				if src, kind := finder.srcList(sy.Folder); src != "" {
					lm.Kind, lm.Src = kind, filepath.Join(tmp, strconv.Itoa(len(metas))+".xml")
					p.Dest[slash(src)] = lm.Src
				}
				p.Lists++
			}
			metas[sy.Folder] = lm
		}
		g := app.LibGame{Key: strings.ToLower(unitRel), Title: title, Rom: filepath.Join(m.To, osRel),
			Path: "./" + slash(osRel), Media: map[string]string{}, Rel: map[string]string{}}
		found := finder.gameMedia(sy.Folder, unitRel, title)
		used := map[string]bool{}
		for _, role := range mediaRoles {
			f, ok := found[role]
			if !ok {
				continue
			}
			dest, tag := lib.MediaDest(m.To, osRel, title, role, path.Ext(f.Name))
			src := f.Folder + "/" + f.Name
			if dest == "" || p.Dest[src] != "" || used[strings.ToLower(dest)] {
				continue // two pictures that'd be one file there: the first role wins
			}
			used[strings.ToLower(dest)] = true
			p.Dest[src] = dest
			p.Media++
			p.Size += f.Size
			if tag != "" {
				if rel, err := filepath.Rel(m.To, dest); err == nil {
					g.Media[tag], g.Rel[tag] = dest, "./"+filepath.ToSlash(rel)
				}
			}
		}
		if lm.List != "" {
			lm.Games = append(lm.Games, g)
		}
	}
	skip := func(pth string) {
		p.Skipped++
		if len(p.SkipEx) < 4 {
			p.SkipEx = append(p.SkipEx, pth)
		}
	}
	add := func(pth string, size int64) bool {
		pth = slash(pth)
		if p.Files >= maxInstall {
			return false
		}
		if strings.EqualFold(path.Base(pth), "gamelist.xml") {
			return true // the device's own game list stays; entries are merged into it (gamelist.go)
		}
		var dest string
		for _, sy := range systems {
			if !strings.HasPrefix(pth, sy.Folder+"/") {
				continue
			}
			rel := pth[len(sy.Folder)+1:]
			first, _, nested := strings.Cut(rel, "/")
			if nested && share.MediaFolder(first) {
				return true // artwork beside the games: it goes along with its game (media)
			}
			m := bySys[sy.Folder]
			if m == nil {
				to := lib.SystemFolder(sy.Name, path.Base(sy.Folder), have)
				m = &sysMap{From: sy.Folder, System: sy.Name, To: filepath.Join(roms, app.SafeRelPath(to))}
				bySys[sy.Folder] = m
				p.Systems = append(p.Systems, m)
			}
			dest = filepath.Join(m.To, app.SafeRelPath(rel))
			m.Files++
			// the game it belongs to: the file itself, or the game's folder it's in
			unit := rel
			if nested {
				unit = first
			}
			if !units[sy.Folder+"/"+unit] && share.IsGame(path.Base(unit), nested) {
				units[sy.Folder+"/"+unit] = true
				media(sy, m, unit)
			}
			break
		}
		if dest == "" {
			if b, ok := bios[pth]; ok && p.BiosDir != "" {
				dest = filepath.Join(p.BiosDir, app.SafeRelPath(path.Join(b.Where, b.Name)))
				p.Bios++
			}
		}
		if dest == "" {
			skip(pth)
			return true
		}
		p.Dest[pth] = dest
		p.Files++
		p.Size += size
		if st, err := os.Stat(dest); err == nil && st.Size() == size {
			p.There++
			p.ThereBytes += size
		}
		return true
	}
	whole := len(sel.folders) == 1 && sel.folders[0] == ""
	if whole {
		err = ix.EachFile(func(f share.FileEntry) error {
			if f.Downloadable() && !add(f.Path, f.Size) {
				return errEnough
			}
			return nil
		})
	} else {
		for _, x := range sel.exact {
			if fe, _ := ix.FileByPath(x); fe != nil {
				add(fe.Path, fe.Size)
			}
		}
		if len(sel.folders) > 0 {
			err = ix.FilesUnder(ctx, sel.folders, func(t share.TreeFile) bool { return add(t.Folder+"/"+t.Name, t.Size) })
		}
	}
	if err != nil && err != errEnough {
		return nil, err
	}
	sort.Slice(p.Systems, func(i, j int) bool { return p.Systems[i].From < p.Systems[j].From })
	keys := make([]string, 0, len(metas))
	for k := range metas {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if lm := metas[k]; lm.List != "" && len(lm.Games) > 0 {
			p.Meta = append(p.Meta, *lm)
		}
	}
	return p, nil
}

// listsNote: what happens to the library's game list, for the review.
func listsNote(lib app.Library) string {
	switch lib.Kind {
	case app.LibESDE:
		if lib.Data == "" {
			return "ES-DE's game lists aren't touched, and without its media folder (Settings → Libraries) the pictures stay out. It shows the games by their file names; scrape them in ES-DE for the rest."
		}
		return "Pictures go into ES-DE's media folder. Its game lists aren't touched (ES-DE mustn't have them changed while it runs): it shows the games by their file names until you scrape them."
	case app.LibLaunchBox:
		return "LaunchBox adds the games itself (Automated ROM Imports, or Tools → Scan → For Added ROMs): its game lists aren't touched while it may be running. The pictures are named for it already."
	}
	return ""
}

var errEnough = &apiError{400, "too_many", "enough"}

func slash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// libraries: what Settings sends, checked and filled in (ids, names, refresh addresses).
func libraries(in any) ([]app.Library, error) {
	l, ok := in.([]any)
	if !ok {
		return nil, fail(400, "libraries: a list", "bad_library")
	}
	out := []app.Library{}
	for i, x := range l {
		m, _ := x.(map[string]any)
		lib := app.Library{ID: str(m["id"]), Name: strings.TrimSpace(str(m["name"])), Kind: str(m["kind"]),
			Path: strings.TrimSpace(str(m["path"])), Data: strings.TrimSpace(str(m["data"])), Refresh: strings.TrimSpace(str(m["refresh"]))}
		if !app.ValidLibraryKind(lib.Kind) {
			return nil, fail(400, "library "+strconv.Itoa(i+1)+": pick RetroBat, Batocera, ES-DE or LaunchBox", "bad_library")
		}
		if lib.Path == "" || !filepath.IsAbs(lib.Path) {
			return nil, fail(400, "library "+strconv.Itoa(i+1)+`: give the full path of its folder, such as D:\RetroBat`, "bad_library")
		}
		lib.Path = filepath.Clean(lib.Path)
		if lib.Data != "" {
			lib.Data = filepath.Clean(lib.Data)
		}
		if lib.ID == "" {
			lib.ID = app.RandomID(4)
		}
		if lib.Name == "" {
			lib.Name = app.LibraryKindName(lib.Kind)
		}
		if _, given := m["refresh"]; !given && str(m["id"]) == "" { // a new one: the usual address
			lib.Refresh = app.DefaultRefresh(lib.Kind, lib.Path)
		}
		if u, err := url.Parse(lib.Refresh); lib.Refresh != "" && (err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
			return nil, fail(400, "library "+strconv.Itoa(i+1)+": the refresh address has to be a web address (http://…)", "bad_library")
		}
		out = append(out, lib)
	}
	return out, nil
}

func (s *Server) libraryRoutes() {
	// whether a library's folders are there and look right (before or after saving it)
	s.api("POST /api/libraries/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		ls, err := libraries([]any{b})
		if err != nil {
			return nil, err
		}
		out := app.CheckLibrary(ls[0])
		out["refresh"] = ls[0].Refresh
		out["default_refresh"] = app.DefaultRefresh(ls[0].Kind, ls[0].Path)
		return out, nil
	})
}

// libraryList: the libraries, for the download review's Save to.
func libraryList(st *app.Settings) []map[string]any {
	out := []map[string]any{}
	for _, l := range st.Libraries {
		out = append(out, map[string]any{"id": l.ID, "name": l.Name, "kind": app.LibraryKindName(l.Kind), "path": l.Path})
	}
	return out
}

// markHave: which of your libraries already have each game: any release of it (its title, as
// share.TitleKey reads file names) in the library's folder for its system. Downloading it
// again, from whichever build, is a duplicate. A library on a share that doesn't answer (the device is off) can take Windows
// many seconds to give up on, so this gets a second and a half, else no marks.
func (s *Server) markHave(hits []*gameHit) {
	libs := s.M.Settings.Libraries
	if len(libs) == 0 || len(hits) == 0 {
		return
	}
	done := make(chan [][]string, 1)
	go func() {
		haves := make([]map[string][]string, len(libs))
		for i, l := range libs {
			haves[i], _ = l.Systems()
		}
		listing := map[string]map[string]bool{} // folder -> the titles of its games (TitleKey)
		marks := make([][]string, len(hits))
		for k, h := range hits {
			for i, l := range libs {
				key := share.TitleKey(h.Title)
				for _, b := range h.Builds {
					dir := filepath.Join(l.RomsDir(), l.SystemFolder(h.System, path.Base(b.Folder), haves[i]))
					names := listing[dir]
					if names == nil {
						names = map[string]bool{}
						if ents, err := os.ReadDir(dir); err == nil {
							for _, e := range ents {
								names[share.TitleKey(share.GameTitle(e.Name()))] = true
							}
						}
						listing[dir] = names
					}
					if names[key] {
						marks[k] = append(marks[k], l.Name)
						break
					}
				}
			}
		}
		done <- marks
	}()
	select {
	case marks := <-done:
		for k, m := range marks {
			hits[k].Have = m
		}
	case <-time.After(1500 * time.Millisecond):
	}
}
