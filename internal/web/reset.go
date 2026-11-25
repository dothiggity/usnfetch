package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/share"
)

// Starting over, to test usnfetch from scratch: System → Reset (app.ResetOptions, done as
// usnfetch starts again) and Forget this build (one share, right away). Downloaded files are
// never touched.

// dirBytes: how many files under dir (those match says, all when nil), and their size.
func dirBytes(dir string, match func(string) bool) (n int, size int64) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || match != nil && !match(d.Name()) {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n++
			size += info.Size()
		}
		return nil
	})
	return n, size
}

func (s *Server) resetRoutes() {
	// what a reset would wipe, for its dialog
	s.api("GET /api/system/reset", func(w http.ResponseWriter, r *http.Request) (any, error) {
		cacheN, cacheB := dirBytes(s.M.IndexDir(), func(n string) bool { return strings.Contains(n, ".usnindex") || strings.Contains(n, ".usnlist") })
		artN, artB := dirBytes(filepath.Join(s.M.DataDir, "art_cache"), nil)
		listN, listB := dirBytes(filepath.Join(s.M.DataDir, "games"), func(n string) bool { return strings.HasSuffix(n, ".json.gz") })
		opened, health := 0, 0
		for _, m := range s.M.Store.AllShareInfo() {
			o, h := false, false
			for k := range m {
				if k == "link_check" || k == "check" {
					h = true
				} else {
					o = true
				}
			}
			if o {
				opened++
			}
			if h {
				health++
			}
		}
		return map[string]any{
			"caches": map[string]any{"indexes": cacheN, "art": artN, "lists": listN, "bytes": cacheB + artB + listB},
			"opened": opened, "health": health, "catalog": len(s.Cat.Snapshot()), "jobs": s.M.JobCount(),
			"providers": len(s.M.Settings.Accounts), "downloading": s.M.Downloading(), "can_restart": s.Restart != nil,
			"complete_dir": s.M.Settings.CompleteDir,
		}, nil
	})

	// a reset: kept for the next start, which is now when usnfetch can restart itself
	s.api("POST /api/system/reset", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		o := app.ResetOptions{Caches: b["caches"] == true, Opened: b["opened"] == true, Health: b["health"] == true,
			Catalog: b["catalog"] == true, Jobs: b["jobs"] == true, Settings: b["settings"] == true, KeepProviders: b["keep_providers"] == true}
		if !o.Any() {
			return nil, fail(400, "pick something to reset", "empty")
		}
		if s.M.Downloading() {
			return nil, fail(409, "a download is running: pause it or let it finish first", "downloading")
		}
		if err := s.M.RequestReset(o); err != nil {
			return nil, err
		}
		s.M.Log("reset asked for in the web app: done as usnfetch starts again")
		if s.Restart == nil {
			return map[string]any{"ok": true, "restart": false}, nil
		}
		go func() { time.Sleep(300 * time.Millisecond); s.Restart() }() // answer first
		return map[string]any{"ok": true, "restart": true}, nil
	})

	// forget one build: its saved index, box art, game and BIOS lists, and what opening it
	// learned (its health stays), so it opens again from scratch
	s.api("POST /api/shares/forget", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		link, title := str(b["link"]), ""
		if cid := str(b["catalog_id"]); cid != "" {
			for _, e := range s.Cat.Snapshot() {
				if app.LinkID(e.Link) == cid {
					link, title = e.Link, e.Label()
				}
			}
		}
		l, err := share.ParseLink(link)
		if err != nil {
			return nil, fail(400, "no such share", "bad_link")
		}
		if title == "" {
			title = "share " + l.FolderID[:min(12, len(l.FolderID))]
		}
		for _, j := range s.M.Queue() {
			if j.Link == link && app.Running(j.State) {
				return nil, fail(409, app.ErrShareInUse.Error(), "in_use")
			}
		}
		s.smu.Lock()
		for _, ss := range s.sessions {
			if lk, err := share.ParseLink(ss.Link); err == nil && lk.FolderID == l.FolderID {
				s.closeSession(ss)
			}
		}
		s.smu.Unlock()
		n, size, err := s.M.ForgetShareFiles(l.FolderID)
		if errors.Is(err, app.ErrShareInUse) {
			return nil, fail(409, err.Error(), "in_use")
		}
		if err != nil {
			return nil, err
		}
		for _, p := range []string{s.gamesFile(l.FolderID), s.biosFile(l.FolderID)} {
			if info, err := os.Stat(p); err == nil && os.Remove(p) == nil {
				n++
				size += info.Size()
			}
		}
		s.games.forget(l.FolderID)
		s.bios.forget(l.FolderID)
		for lk := range s.M.Store.AllShareInfo() { // every link it's known by
			if x, err := share.ParseLink(lk); err == nil && x.FolderID == l.FolderID {
				_ = s.M.Store.ForgetShareInfo(lk, true, false)
			}
		}
		s.M.Log(fmt.Sprintf("forgot %s: %d cached files (%s); opening it again starts from scratch", title, n, fmtSize(size)))
		return map[string]any{"ok": true, "files": n, "bytes": size}, nil
	})
}

func fmtSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}
