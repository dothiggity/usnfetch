package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"usnfetch/internal/app"
	"usnfetch/internal/share"
)

// ---- what of a share is already on this machine --------------------------------------
//
// A share's files can be in the folders its jobs wrote (older jobs laid them out a little
// differently) and in the folder a new download of it would use. A file counts as there
// when a file of its name and size is in place with no download checkpoint beside it.

// destination: the job folder name, whether it was derived, and the share's top folder
// that a new download of this share would use.
func (s *Server) destination(ss *session, ix *share.Index, name string) (string, bool, string) {
	name = s.jobNameFor(ss, name)
	auto := name == ""
	top, _ := ix.TopFolder()
	if auto {
		if top != "" {
			name = top
		} else if l, err := share.ParseLink(ss.Link); err == nil {
			name = "USN_" + l.FolderID[:min(12, len(l.FolderID))]
		}
	}
	return name, auto, top
}

func (s *Server) localRoots(ss *session, ix *share.Index) []app.OutputRoot {
	name, _, top := s.destination(ss, ix, "")
	roots := append(s.M.OutputRoots(ss.Link, top), app.OutputRoot{Dir: app.JobOutDir(s.M.Settings, name), Strip: top})
	seen := map[string]bool{}
	var out []app.OutputRoot
	for _, r := range roots {
		k := strings.ToLower(r.Dir) + "|" + r.Strip
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}

// listings caches directory listings for one request: lower-cased name -> size (-1 = folder).
type listings map[string]map[string]int64

func (l listings) get(dir string) map[string]int64 {
	k := strings.ToLower(dir)
	if m, ok := l[k]; ok {
		return m
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		l[k] = nil
		return nil
	}
	m := make(map[string]int64, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			m[strings.ToLower(e.Name())] = -1
		} else if info, err := e.Info(); err == nil {
			m[strings.ToLower(e.Name())] = info.Size()
		}
	}
	l[k] = m
	return m
}

func localPath(r app.OutputRoot, sharePath string) string {
	return filepath.Join(r.Dir, app.SafeRelPath(app.StripTop(sharePath, r.Strip)))
}

var localRank = map[string]int{"": 0, "differs": 1, "partial": 2, "ok": 3}

// fileState: "ok" (in place), "partial" (a download of it is under way or stopped),
// "differs" (a file of that name but another size), "" (not there).
func fileState(ls listings, roots []app.OutputRoot, sharePath string, size int64) string {
	best := ""
	for _, r := range roots {
		p := localPath(r, sharePath)
		m := ls.get(filepath.Dir(p))
		if m == nil {
			continue
		}
		name := strings.ToLower(filepath.Base(p))
		sz, ok := m[name]
		st := ""
		switch {
		case !ok || sz < 0:
			continue
		case hasKey(m, name+".usnfetch"):
			st = "partial"
		case sz == size:
			st = "ok"
		default:
			st = "differs"
		}
		if localRank[st] > localRank[best] {
			best = st
		}
	}
	return best
}

func hasKey(m map[string]int64, k string) bool { _, ok := m[k]; return ok }

func folderThere(ls listings, roots []app.OutputRoot, sharePath string) bool {
	for _, r := range roots {
		p := localPath(r, sharePath)
		if m := ls.get(filepath.Dir(p)); m != nil && hasKey(m, strings.ToLower(filepath.Base(p))) && m[strings.ToLower(filepath.Base(p))] < 0 {
			return true
		}
	}
	return false
}

// shareOnDisk counts how much of a selection is already in the folder a download with
// this name would use (what "skip files already on disk" would skip).
func (s *Server) shareOnDisk(w http.ResponseWriter, r *http.Request) (any, error) {
	ss, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	b := body(r)
	sel, err := s.resolveSel(ix, b)
	if err != nil {
		return nil, err
	}
	if sel.count > 400_000 {
		return map[string]any{"counted": false}, nil
	}
	name, _, top := s.destination(ss, ix, strings.TrimSpace(str(b["name"])))
	roots := []app.OutputRoot{{Dir: app.JobOutDir(s.M.Settings, name), Strip: top}}
	ls := listings{}
	var files, partial int64
	var bytes int64
	count := func(p string, size int64) {
		switch fileState(ls, roots, p, size) {
		case "ok":
			files++
			bytes += size
		case "partial":
			partial++
		}
	}
	if len(sel.folders) > 0 {
		if _, err := os.Stat(roots[0].Dir); err == nil { // nothing to look at when the folder does not exist
			err := ix.FilesUnder(r.Context(), sel.folders, func(t share.TreeFile) bool {
				p := t.Name
				if t.Folder != "" {
					p = t.Folder + "/" + t.Name
				}
				count(p, t.Size)
				return true
			})
			if err != nil {
				return nil, err
			}
		}
	}
	for _, p := range sel.exact {
		if fe, _ := ix.FileByPath(p); fe != nil {
			count(fe.Path, fe.Size)
		}
	}
	return map[string]any{"counted": true, "files": files, "bytes": bytes, "partial": partial, "of": sel.count, "dir": roots[0].Dir}, nil
}
