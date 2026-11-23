package web

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"usnfetch/internal/engine"
	"usnfetch/internal/share"
)

// ---- box-art gallery --------------------------------------------------------------------
//
// Game folders get a gallery when the share carries artwork for them:
//   launchbox  <base>/Games/<Platform>/… with <base>/Images/<Platform>/Box - Front/<Region>/<Title>-01.jpg,
//              titles from <base>/Data/Platforms/<Platform>.xml (ROM path -> title)
//   es         EmulationStation / RetroBat: art beside the ROMs, e.g. images/<rom>-thumb.png,
//              media/box2dfront/<rom>.png, mixart/<rom>.png
//   images     a folder that is mostly pictures
// Art is matched by a normalised name (letters and digits only) and streamed from Usenet.

type galleryInfo struct {
	Kind, Base, Platform string
}

var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true}

func isImage(p string) bool { return imageExt[strings.ToLower(path.Ext(p))] }

// folders that hold art beside ROMs, and how good a cover what is in them usually is
var esArtDirs = map[string]int{"box2dfront": 0, "boxart": 0, "boxes": 0, "covers": 0, "cover": 0, "thumbnails": 0,
	"images": 1, "media": 1, "mixart": 1, "mix": 1, "snap": 3, "snaps": 3, "screenshots": 3, "titles": 3, "wheel": 5, "marquees": 6}

// esArtFolder: a folder of art beside ROMs holds pictures itself (RetroBat images/,
// mixart/), or is media/ with art folders inside (ES-DE). LaunchBox's own Images/ holds
// only platform folders, so a LaunchBox root is not taken for a game folder.
func esArtFolder(ix *share.Index, f share.Folder) bool {
	name := strings.ToLower(f.Name)
	if _, ok := esArtDirs[name]; !ok {
		return false
	}
	if d, _ := ix.Dir(f.Path); d != nil && d.NFiles > 0 {
		return true
	}
	if name == "media" {
		for _, sub := range []string{"box2dfront", "covers", "boxart", "mixart"} {
			if d, _ := ix.Dir(f.Path + "/" + sub); d != nil {
				return true
			}
		}
	}
	return false
}

// galleryKind decides cheaply (no artwork is read) whether a folder has a gallery.
func (s *Server) galleryKind(ix *share.Index, p string, folders []share.Folder, files []share.FileEntry) galleryInfo {
	if p != "" {
		segs := strings.Split(p, "/")
		for i := len(segs) - 2; i >= 0; i-- {
			if strings.EqualFold(segs[i], "Games") {
				base := strings.Join(segs[:i], "/")
				if d, _ := ix.Dir(path.Join(base, "Images", segs[i+1])); d != nil {
					return galleryInfo{"launchbox", base, segs[i+1]}
				}
			}
		}
	}
	if len(files) > 0 {
		for _, f := range folders {
			if esArtFolder(ix, f) {
				return galleryInfo{"es", p, ""}
			}
		}
	}
	images := 0
	for _, f := range files {
		if isImage(f.Path) {
			images++
		}
	}
	if images >= 3 && images*10 >= len(files)*6 {
		return galleryInfo{"images", p, ""}
	}
	return galleryInfo{}
}

// norm: lower-case letters and digits only, the key art and titles are matched by.
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	reTags    = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	reNumTail = regexp.MustCompile(`-\d{1,3}$`)
	reTheTail = regexp.MustCompile(`(?i)^(.*), the\b(.*)$`)
)

func stem(name string) string { return strings.TrimSuffix(name, path.Ext(name)) }

// cleanTitle: a ROM name without its (region) [flags] tags.
func cleanTitle(name string) string {
	return strings.TrimSpace(reTags.ReplaceAllString(stem(name), ""))
}

// titleKeys: the keys a title may have been saved under ("7th Saga, The" is "The 7th Saga").
func titleKeys(t string) []string {
	keys := []string{norm(t)}
	if m := reTheTail.FindStringSubmatch(t); m != nil {
		keys = append(keys, norm("The "+m[1]+m[2]))
	}
	return keys
}

type artRef struct {
	fid  int64
	name string
	rank int
}

type artIndex struct {
	art    map[string]artRef // norm(name) -> best picture
	titles map[string]string // lower-cased ROM path below base -> LaunchBox title
	byBase map[string]string // lower-cased ROM file name -> LaunchBox title
}

func (a *artIndex) add(key string, ref artRef) {
	if key == "" {
		return
	}
	if old, ok := a.art[key]; !ok || ref.rank < old.rank {
		a.art[key] = ref
	}
}

var lbTypes = []string{"Box - Front", "Box - Front - Reconstructed", "Fanart - Box - Front", "Box - 3D", "Screenshot - Game Title", "Clear Logo"}
var lbRegions = map[string]int{"north america": 0, "united states": 1, "world": 2, "": 3, "europe": 4}

var esSuffix = []struct {
	suf  string
	rank int
}{{"-thumb", 0}, {"-boxart", 0}, {"-box", 0}, {"-cover", 0}, {"-image", 1}, {"-mixart", 1}, {"-mix", 1},
	{"-titleshot", 3}, {"-screenshot", 3}, {"-snap", 3}, {"-wheel", 5}, {"-logo", 5}, {"-marquee", 6},
	{"-boxback", 9}, {"-fanart", 9}, {"-map", 9}, {"-manual", 9}, {"-bezel", 9}}

func (s *Server) buildArt(r *http.Request, ix *share.Index, g galleryInfo, folders []share.Folder) (*artIndex, error) {
	a := &artIndex{art: map[string]artRef{}, titles: map[string]string{}, byBase: map[string]string{}}
	ctx := r.Context()
	switch g.Kind {
	case "launchbox":
		img := path.Join(g.Base, "Images", g.Platform)
		var dirs []string
		for _, t := range lbTypes {
			if d, _ := ix.Dir(path.Join(img, t)); d != nil {
				dirs = append(dirs, d.Path)
			}
		}
		if len(dirs) > 0 {
			err := ix.FilesUnder(ctx, dirs, func(t share.TreeFile) bool {
				if !isImage(t.Name) {
					return true
				}
				rel := strings.TrimPrefix(t.Folder, img+"/")
				typ, region, _ := strings.Cut(rel, "/")
				rank := 9000
				for i, x := range lbTypes {
					if x == typ {
						rank = i * 1000
					}
				}
				if rr, ok := lbRegions[strings.ToLower(region)]; ok {
					rank += rr * 100
				} else {
					rank += 500
				}
				st := stem(t.Name)
				if m := reNumTail.FindString(st); m != "" {
					n, _ := strconv.Atoi(m[1:])
					rank += min(n, 99)
					st = strings.TrimSuffix(st, m)
				}
				a.add(norm(st), artRef{t.FID, t.Name, rank})
				return true
			})
			if err != nil {
				return nil, err
			}
		}
		if err := s.loadLaunchBoxTitles(a, ix, path.Join(g.Base, "Data", "Platforms", g.Platform+".xml")); err != nil {
			s.M.Log("gallery: " + err.Error())
		}
	case "es":
		var dirs []string
		for _, f := range folders {
			if _, ok := esArtDirs[strings.ToLower(f.Name)]; ok {
				dirs = append(dirs, f.Path)
			}
		}
		err := ix.FilesUnder(ctx, dirs, func(t share.TreeFile) bool {
			if !isImage(t.Name) {
				return true
			}
			rank := 2
			for _, part := range strings.Split(strings.TrimPrefix(t.Folder, g.Base+"/"), "/") {
				if r, ok := esArtDirs[strings.ToLower(part)]; ok && r != 1 {
					rank = r
				}
			}
			st := stem(t.Name)
			for _, x := range esSuffix {
				if len(st) > len(x.suf) && strings.EqualFold(st[len(st)-len(x.suf):], x.suf) {
					st, rank = st[:len(st)-len(x.suf)], x.rank
					break
				}
			}
			if rank < 9 {
				a.add(norm(st), artRef{t.FID, t.Name, rank})
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	return a, nil
}

// loadLaunchBoxTitles reads the platform's game list (streamed from Usenet).
func (s *Server) loadLaunchBoxTitles(a *artIndex, ix *share.Index, xmlPath string) error {
	fe, err := ix.FileByPath(xmlPath)
	if err != nil || fe == nil || !fe.Downloadable() {
		return nil // no game list: titles come from the ROM names
	}
	if fe.Size > 200<<20 {
		return fmt.Errorf("%s is too large to read (%d MB)", xmlPath, fe.Size>>20)
	}
	pool, sem, err := s.streamResources()
	if err != nil {
		return err
	}
	rd, err := engine.NewFileReader(ix, *fe, pool, s.cache, sem)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := rd.WriteRange(&buf, 0, fe.Size); err != nil {
		return fmt.Errorf("reading %s: %w", xmlPath, err)
	}
	dec := xml.NewDecoder(&buf)
	dec.Strict = false
	type game struct {
		Title           string `xml:"Title"`
		ApplicationPath string `xml:"ApplicationPath"`
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Game" {
			continue
		}
		var g game
		if dec.DecodeElement(&g, &se) != nil || g.Title == "" || g.ApplicationPath == "" {
			continue
		}
		p := strings.ToLower(strings.ReplaceAll(g.ApplicationPath, "\\", "/"))
		if i := strings.Index(p, "games/"); i >= 0 {
			p = p[i:]
		}
		a.titles[p] = g.Title
		a.byBase[path.Base(p)] = g.Title
	}
	return nil
}

var gallerySkip = map[string]bool{".srm": true, ".sav": true, ".state": true, ".cfg": true, ".txt": true, ".xml": true, ".nfo": true,
	".ini": true, ".db": true, ".json": true, ".log": true, ".md": true, ".url": true, ".lnk": true}

func (s *Server) shareGallery(w http.ResponseWriter, r *http.Request) (any, error) {
	ss, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	offset := max(0, num(q.Get("offset"), 0))
	limit := min(2000, max(1, num(q.Get("limit"), 300)))
	d, folders, files, err := ix.ListDir(q.Get("path"), offset, limit, q.Get("sort"))
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fail(404, fmt.Sprintf("no folder %q in this share", q.Get("path")), "no_folder")
	}
	g := s.galleryKind(ix, d.Path, folders, files)
	var a *artIndex
	if g.Kind == "launchbox" || g.Kind == "es" {
		key := g.Kind + "|" + g.Base + "|" + g.Platform
		ss.mu.Lock()
		a = ss.art[key]
		ss.mu.Unlock()
		if a == nil {
			if a, err = s.buildArt(r, ix, g, folders); err != nil {
				return nil, err
			}
			ss.mu.Lock()
			if ss.art == nil {
				ss.art = map[string]*artIndex{}
			}
			ss.art[key] = a
			ss.mu.Unlock()
		}
	}
	roots, ls := s.localRoots(ss, ix), listings{}
	items := []map[string]any{}
	matched := 0
	for _, f := range files {
		ext := strings.ToLower(path.Ext(f.Path))
		fj := fileJSON(f)
		if st := fileState(ls, roots, f.Path, f.Size); st != "" {
			fj["local"] = st
		}
		item := map[string]any{"file": fj, "title": cleanTitle(fj["name"].(string)), "art": nil}
		switch g.Kind {
		case "images":
			if !isImage(f.Path) {
				continue
			}
			item["title"], item["art"] = stem(fj["name"].(string)), f.FID
			matched++
		case "launchbox", "es":
			if gallerySkip[ext] || isImage(f.Path) {
				continue
			}
			name := fj["name"].(string)
			var keys []string
			if g.Kind == "launchbox" {
				rel := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(f.Path, "\\", "/"), g.Base+"/"))
				t := a.titles[rel]
				if t == "" {
					t = a.byBase[strings.ToLower(name)]
				}
				if t != "" {
					item["title"] = t
					keys = append(keys, titleKeys(t)...)
				}
			} else {
				keys = append(keys, norm(stem(name)))
			}
			keys = append(keys, titleKeys(cleanTitle(name))...)
			for _, k := range keys {
				if ref, ok := a.art[k]; ok {
					item["art"] = ref.fid
					matched++
					break
				}
			}
		default:
			if gallerySkip[ext] {
				continue
			}
		}
		items = append(items, item)
	}
	fl := make([]map[string]any, 0, len(folders))
	if offset == 0 {
		for _, f := range folders {
			if _, art := esArtDirs[strings.ToLower(f.Name)]; art && g.Kind == "es" {
				continue
			}
			fl = append(fl, map[string]any{"name": f.Name, "path": f.Path, "size": f.Size, "count": f.Count, "local": folderThere(ls, roots, f.Path)})
		}
	}
	return map[string]any{"kind": g.Kind, "platform": g.Platform, "items": items, "folders": fl, "matched": matched,
		"total": d.NFiles, "offset": offset, "next": offset + len(files), "truncated": int64(offset+len(files)) < d.NFiles}, nil
}
