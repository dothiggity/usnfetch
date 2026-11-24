package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/engine"
	"usnfetch/internal/share"
)

// Link checks: the light health check a whole imported list gets. Each link's manifest
// article is looked up at the providers (one small request per link), so the catalog shows
// right away which shares are gone. Opening a share checks a sample of its files on top.

// linkChecks: links waiting for a check, and how far the running one is.
type linkChecks struct {
	mu          sync.Mutex
	pending     []string
	running     bool
	done, total int // of this round (back to 0 when the queue runs dry)
}

// queueLinkCheck adds links to the check queue (each once) and starts it if it is idle.
func (s *Server) queueLinkCheck(links []string) int {
	s.lc.mu.Lock()
	defer s.lc.mu.Unlock()
	seen := map[string]bool{}
	for _, l := range s.lc.pending {
		seen[l] = true
	}
	n := 0
	for _, l := range links {
		if !seen[l] {
			seen[l] = true
			s.lc.pending = append(s.lc.pending, l)
			n++
		}
	}
	s.lc.total += n
	if !s.lc.running && len(s.lc.pending) > 0 {
		s.lc.running = true
		go s.runLinkChecks()
	}
	return n
}

// linkCheckState: nil when no check runs, else {done, total}.
func (s *Server) linkCheckState() any {
	s.lc.mu.Lock()
	defer s.lc.mu.Unlock()
	if !s.lc.running {
		return nil
	}
	return map[string]int{"done": s.lc.done, "total": s.lc.total}
}

func (s *Server) runLinkChecks() {
	for {
		s.lc.mu.Lock()
		batch := s.lc.pending
		s.lc.pending = nil
		if len(batch) == 0 {
			s.lc.running, s.lc.done, s.lc.total = false, 0, 0
			s.lc.mu.Unlock()
			return
		}
		base := s.lc.done
		s.lc.mu.Unlock()

		manifests := map[string][]string{}
		for _, l := range batch {
			if pl, err := share.ParseLink(l); err == nil && len(pl.ManifestMsgids) > 0 {
				manifests[l] = pl.ManifestMsgids
			}
		}
		accts, err := s.M.Accounts()
		if err != nil {
			s.M.Log("link check skipped: " + err.Error())
		} else {
			res := engine.CheckLinks(manifests, accts, 4, func(d int) {
				s.lc.mu.Lock()
				s.lc.done = base + d
				s.lc.mu.Unlock()
			})
			now := float64(time.Now().UnixNano()) / 1e9
			found, gone, unknown := 0, 0, 0
			for l, r := range res {
				if r.Err != nil {
					unknown++
					continue
				}
				if r.Found {
					found++
				} else {
					gone++
				}
				info := s.M.Store.ShareInfo(l)
				if info == nil {
					info = map[string]any{}
				}
				info["link_check"] = map[string]any{"found": r.Found, "checked_at": now}
				_ = s.M.Store.SetShareInfo(l, info)
			}
			msg := fmt.Sprintf("link check: %d of %d share link(s) found at your providers", found, len(res))
			if gone > 0 {
				msg += fmt.Sprintf(", %d gone (their manifest is missing everywhere)", gone)
			}
			if unknown > 0 {
				msg += fmt.Sprintf(", %d not checked (no provider answered)", unknown)
			}
			s.M.Log(msg)
		}
		s.lc.mu.Lock()
		s.lc.done = base + len(batch)
		s.lc.mu.Unlock()
	}
}

// healthOf is an entry's Health for the catalog: the sampled check of its files when there is
// one, else the link check. A link check that finds the manifest gone, newer than the
// sampled check, wins: the share can't be opened any more.
func healthOf(info map[string]any) any {
	ck, _ := info["check"].(map[string]any)
	lk, _ := info["link_check"].(map[string]any)
	at := func(m map[string]any) float64 { f, _ := m["checked_at"].(float64); return f }
	if lk != nil && lk["found"] == false && (ck == nil || at(lk) > at(ck)) {
		return map[string]any{"verdict": "gone", "basis": "link", "checked_at": lk["checked_at"]}
	}
	if ck != nil {
		return map[string]any{"verdict": ck["verdict"], "basis": "sample", "sampled": ck["sampled"], "available": ck["available"], "checked_at": ck["checked_at"]}
	}
	if lk != nil {
		return map[string]any{"verdict": "link_ok", "basis": "link", "checked_at": lk["checked_at"]}
	}
	return nil
}

func (s *Server) linkCheckRoutes() {
	// check the links of these catalog entries (none given: every entry)
	s.api("POST /api/catalog/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := s.M.Accounts(); err != nil {
			return nil, fail(400, err.Error(), "no_provider")
		}
		ids := map[string]bool{}
		for _, id := range strList(body(r)["ids"]) {
			ids[id] = true
		}
		var links []string
		for _, e := range s.Cat.Snapshot() {
			if len(ids) == 0 || ids[app.LinkID(e.Link)] {
				links = append(links, e.Link)
			}
		}
		n := s.queueLinkCheck(links)
		return map[string]any{"queued": n, "check": s.linkCheckState()}, nil
	})
}
