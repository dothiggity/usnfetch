package web

import (
	"context"
	"fmt"
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

// What's inside each build (share.Index.Inside) and search across every share opened before,
// both from the share indexes kept in the cache (share/cache.go).

// insideVersion: bump to work the summaries out again (after changing share.Inside).
const insideVersion = 5 // 2: games grouped by title, each build's game list kept; 3: one name per console; 4: scene-style names, more console names; 5: BIOS files, game file counts

// cachedIndex: the cache file this share's index is kept in (the full index if there is
// one, else its file list, plain or zipped), "" if none.
func (s *Server) cachedIndex(folderID string) string {
	if folderID == "" || strings.ContainsAny(folderID, `/\.`) {
		return ""
	}
	dir := s.M.IndexDir()
	for _, pat := range []string{"*.usnindex", "*.usnlist", "*.usnlist" + share.GzExt} {
		if m, _ := filepath.Glob(filepath.Join(dir, folderID+"-"+pat)); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

// openCached opens a cache file for reading (a zipped file list is unzipped first; the
// cache's upkeep zips it again once it is idle).
func openCached(p string) (*share.Index, error) {
	share.CacheMu.Lock()
	defer share.CacheMu.Unlock()
	if plain, ok := strings.CutSuffix(p, share.GzExt); ok {
		if _, err := os.Stat(plain); err != nil {
			if err := share.GunzipFile(plain); err != nil {
				return nil, err
			}
		}
		p = plain
	}
	return share.OpenIndex(p, nil)
}

// cachedShare: a share with an index in the cache, and how people know it.
type cachedShare struct {
	link, title, catalogID, file string
}

// cachedShares: every share whose index is in the cache that usnfetch knows the link of
// (from the catalog, or from a share opened by its link).
func (s *Server) cachedShares() []cachedShare {
	seen := map[string]bool{}
	var out []cachedShare
	add := func(link, title, catalogID string) {
		l, err := share.ParseLink(link)
		if err != nil || seen[l.FolderID] {
			return
		}
		seen[l.FolderID] = true
		if f := s.cachedIndex(l.FolderID); f != "" {
			out = append(out, cachedShare{link, title, catalogID, f})
		}
	}
	for _, e := range s.Cat.Snapshot() {
		add(e.Link, e.Label(), app.LinkID(e.Link))
	}
	for link, info := range s.M.Store.AllShareInfo() {
		if name, _ := info["name"].(string); name != "" {
			add(link, name, "")
		}
	}
	for _, j := range append(s.M.Queue(), s.M.History()...) { // shares downloaded without being browsed
		add(j.Link, j.Name, "")
	}
	return out
}

// fillInside works out what's inside each cached share that doesn't have it yet (shares
// opened or downloaded before this existed), one at a time, in the background.
func (s *Server) fillInside() {
	n := 0
	for _, c := range s.cachedShares() {
		if info := s.M.Store.ShareInfo(c.link); info != nil && num(info["inside_v"], 0) >= insideVersion {
			continue
		}
		if s.insideOf(c.link) {
			n++
		}
		time.Sleep(200 * time.Millisecond)
	}
	if n > 0 {
		s.M.Log(fmt.Sprintf("worked out what's inside %d share(s) opened or downloaded before", n))
		s.M.TidyIndexCache() // file lists unzipped for this go back to being zipped
	}
}

// indexed is told by the app (Manager.OnIndexed) when a download is done with the share's
// index: what's inside the share is worked out now, unless it's known already. It runs on
// the job's goroutine, so no other job of the share opens the index meanwhile.
func (s *Server) indexed(link string) {
	if info := s.M.Store.ShareInfo(link); info != nil && num(info["inside_v"], 0) >= insideVersion {
		return
	}
	s.insideOf(link)
}

// insideOf works out what's inside a share from its index in the cache (its systems, game
// list and BIOS files: saveInside) and keeps it with the share's details, starting those if
// the share was never opened. false if it has no index in the cache or it can't be read.
func (s *Server) insideOf(link string) bool {
	s.insideMu.Lock()
	defer s.insideMu.Unlock()
	l, err := share.ParseLink(link)
	if err != nil {
		return false
	}
	info := s.M.Store.ShareInfo(link)
	if info != nil && num(info["inside_v"], 0) >= insideVersion {
		return false // done meanwhile
	}
	if info == nil { // details kept under another spelling of the share's link: those
		for _, other := range s.M.Store.ShareLinks() {
			if ol, err := share.ParseLink(other); err == nil && ol.FolderID == l.FolderID && other != link {
				if link, info = other, s.M.Store.ShareInfo(other); info != nil && num(info["inside_v"], 0) >= insideVersion {
					return false
				}
				break
			}
		}
	}
	f := s.cachedIndex(l.FolderID)
	if f == "" {
		return false
	}
	// building its folder tree writes to the index file, so not while anything has it open (a
	// download reads it as a file that never changes; a share being browsed works it out itself)
	share.CacheMu.Lock()
	busy := share.InUse(f) || share.InUse(strings.TrimSuffix(f, share.GzExt))
	share.CacheMu.Unlock()
	if busy {
		return false
	}
	ix, err := openCached(f)
	if err != nil {
		return false
	}
	defer ix.Close()
	if ix.EnsureTree(nil) != nil {
		return false
	}
	if info = s.M.Store.ShareInfo(link); info == nil { // downloaded, never browsed
		info = map[string]any{}
		if sum, err := ix.Summary(); err == nil && sum.Name != "" {
			info["name"], info["size"], info["files"], info["packed"] = sum.Name, sum.Size, sum.Files, sum.Packed
		}
	}
	info["folder_id"] = l.FolderID
	s.saveInside(ix, info)
	err = s.M.Store.SetShareInfo(link, info)
	app.ReleaseMemory() // its folder tree can be big: give it back now
	return err == nil
}

// ---- search across every opened share -------------------------------------------------

type shareHits struct {
	ID        string           `json:"id"` // the share's id (its page: #/share/<id>/<folder>)
	Title     string           `json:"title"`
	CatalogID string           `json:"catalog_id,omitempty"`
	Link      string           `json:"link,omitempty"`
	Files     int64            `json:"files"`   // matching files in all
	Folders   int              `json:"folders"` // matching folders in all
	Hits      []map[string]any `json:"hits"`    // the first few: folders, then files
}

func (s *Server) searchRoutes() {
	s.api("GET /api/search", func(w http.ResponseWriter, r *http.Request) (any, error) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len([]rune(q)) < 2 {
			return nil, fail(400, "type at least two letters", "short")
		}
		per := min(50, max(1, num(r.URL.Query().Get("per"), 8)))
		shares := s.cachedShares()
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		var mu sync.Mutex
		var out []shareHits
		failed := 0
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		t0 := time.Now()
		for _, c := range shares {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if ctx.Err() != nil {
					return
				}
				ix, err := openCached(c.file)
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				defer ix.Close()
				res, err := ix.Search(ctx, q, "", per, share.SortName)
				if err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					return
				}
				if res.FileMatches == 0 && res.FolderMatches == 0 {
					return
				}
				h := shareHits{ID: app.LinkID(c.link), Title: c.title, CatalogID: c.catalogID, Files: res.FileMatches, Folders: res.FolderMatches, Hits: []map[string]any{}}
				if c.catalogID == "" {
					h.Link = c.link
				}
				for _, f := range res.Folders {
					if len(h.Hits) < per {
						h.Hits = append(h.Hits, map[string]any{"folder": true, "path": f.Path, "name": f.Name, "size": f.Size, "count": f.Count})
					}
				}
				for _, f := range res.Files {
					if len(h.Hits) < per {
						h.Hits = append(h.Hits, map[string]any{"folder": false, "path": f.File.Path, "name": filepath.Base(f.File.Path), "size": f.File.Size, "in": f.Folder})
					}
				}
				mu.Lock()
				out = append(out, h)
				mu.Unlock()
			}()
		}
		wg.Wait()
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i].Files+int64(out[i].Folders), out[j].Files+int64(out[j].Folders)
			if a != b {
				return a > b
			}
			return out[i].Title < out[j].Title
		})
		if out == nil {
			out = []shareHits{}
		}
		s.M.TidyIndexCache() // file lists unzipped for the search go back to being zipped
		return map[string]any{"query": q, "shares": out, "searched": len(shares), "failed": failed,
			"timed_out": ctx.Err() != nil, "ms": time.Since(t0).Milliseconds()}, nil
	})
}
