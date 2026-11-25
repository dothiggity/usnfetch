package web

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"usnfetch/internal/app"
	"usnfetch/internal/catalog"
	"usnfetch/internal/share"
)

// ---- reviewing downloads of whole shares ----------------------------------------------
//
// Every download goes through a review first. Ticked files are reviewed on the share page
// (sharePlan, with the share's index at hand); whole shares queued from the catalog or
// from pasted links are reviewed here, with what is known without opening them.

// badLine is a line of pasted text that looks like a share link but is not usable.
type badLine struct {
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

var linkSep = regexp.MustCompile(`[\s,;"'<>()\[\]]+`)

// linkLines finds the share links in pasted text: the usable ones, in order and once
// each, and the lines with a link that is not, with the reason.
func linkLines(text string) ([]string, []badLine) {
	var links []string
	bad := []badLine{}
	seen := map[string]bool{}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		for _, tok := range linkSep.Split(line, -1) {
			low := strings.ToLower(tok)
			if !strings.HasPrefix(low, "usn:") && !strings.HasPrefix(low, "usenet:") {
				continue
			}
			if _, err := share.ParseLink(tok); err != nil {
				bad = append(bad, badLine{Line: i + 1, Text: shorten(strings.TrimSpace(line), 90), Reason: err.Error()})
				continue
			}
			if !seen[tok] {
				seen[tok] = true
				links = append(links, tok)
			}
		}
	}
	return links, bad
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// linkFacts: what is known about a share without opening it -- the job folder name a
// download gets, a title to show, and its size (from an earlier opening, else as listed).
type linkFacts struct {
	Link, ID, Name, Title string
	Size                  int64 // 0 = not known
	SizeListed            bool  // the size is the catalog's listed size, not the share's own
	Files                 int64
	InCatalog             bool
}

func (s *Server) factsFor(link string) linkFacts {
	f := linkFacts{Link: link, ID: app.LinkID(link)}
	info := s.M.Store.ShareInfo(link)
	if e, ok := s.Cat.ByLink(link); ok {
		f.Name, f.Title, f.InCatalog = catalog.JobName(e, 90), e.Label(), true
		if e.Size != nil {
			f.Size, f.SizeListed = *e.Size, true
		}
	} else if info != nil && str(info["name"]) != "" {
		f.Name, f.Title = safeFolderName(str(info["name"])), str(info["name"])
	}
	if info != nil {
		if n, ok := info["size"].(float64); ok && n > 0 {
			f.Size, f.SizeListed = int64(n), false
		}
		if n, ok := info["files"].(float64); ok {
			f.Files = int64(n)
		}
	}
	if f.Title == "" {
		f.Title = catalog.FallbackTitle(link)
	}
	return f
}

func safeFolderName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, s)
	return strings.TrimRight(strings.TrimSpace(s), " .")
}

// planShares is the review of downloading whole shares: each one's size and folder,
// the total against the free space, and the download defaults.
func (s *Server) planShares(links []string) map[string]any {
	st := s.M.Settings
	active := map[string][]map[string]any{}
	for _, j := range s.M.Queue() {
		active[j.Link] = append(active[j.Link], map[string]any{"id": j.JobID, "name": j.Name, "state": j.State, "picked": j.Picked})
	}
	items := []map[string]any{}
	var total int64
	unknown, gone := 0, 0
	// is each share still at the provider? (a few seconds at most; skipped without an answer)
	checked := s.linkCheckNow(links, 8*time.Second)
	for _, l := range links {
		f := s.factsFor(l)
		name := f.Name
		if name == "" {
			if pl, err := share.ParseLink(l); err == nil {
				name = "USN_" + pl.FolderID[:min(12, len(pl.FolderID))] // named after the share's folder once it starts
			}
		}
		out := app.JobOutDir(st, name)
		_, err := os.Stat(out)
		it := map[string]any{"link": l, "id": f.ID, "title": f.Title, "name": name, "name_known": f.Name != "", "size": nil,
			"size_listed": f.SizeListed, "files": f.Files, "in_catalog": f.InCatalog, "out_dir": out, "folder_exists": err == nil,
			"active_jobs": active[l], "gone": false}
		if r, ok := checked[l]; ok && r.Err == nil && !r.Found {
			it["gone"] = true // its manifest is at none of the providers: a download would fail
			gone++
		}
		if f.Size > 0 {
			it["size"] = f.Size
			total += f.Size
		} else {
			unknown++
		}
		items = append(items, it)
	}
	var free any
	if n, err := app.DiskFree(st.CompleteDir); err == nil {
		free = n
	}
	return map[string]any{"items": items, "size": total, "unknown": unknown, "gone": gone, "free": free, "complete_dir": st.CompleteDir,
		"sep":             string(os.PathSeparator),
		"extract_default": st.DefaultExtractMode(), "delete_archives": st.DeleteArchives}
}

// queueShares queues whole shares with the choices made in their review.
func (s *Server) queueShares(links []string, b map[string]any) ([]string, []string, error) {
	mode := str(b["extract"])
	if !app.ValidExtractMode(mode) {
		return nil, nil, fail(400, "extract must be off, sets or all", "bad_extract")
	}
	existing := str(b["existing"])
	if existing != "" && existing != app.ExistingSkip && existing != app.ExistingVerify && existing != app.ExistingRedownload {
		return nil, nil, fail(400, "existing must be skip, verify or redownload", "bad_existing")
	}
	jobs, bad := []string{}, []string{}
	for _, l := range links {
		f := s.factsFor(l)
		name := f.Name
		if n := strings.TrimSpace(str(b["name"])); n != "" && len(links) == 1 {
			name = n
		}
		var size int64
		if !f.SizeListed {
			size = f.Size
		}
		j, err := s.M.AddJob(l, app.AddOptions{Name: name, Password: str(b["password"]), ArchivePassword: str(b["archive_password"]),
			PlanOnly: b["plan_only"] == true, ExtractMode: mode, Existing: existing, Size: size})
		if err != nil {
			bad = append(bad, f.Title+": "+err.Error())
			continue
		}
		jobs = append(jobs, j.ID)
	}
	return jobs, bad, nil
}

func (s *Server) catalogLinks(ids []string) ([]string, []string) {
	var links, missing []string
	for _, id := range ids {
		if e, ok := s.Cat.ByID(id); ok {
			links = append(links, e.Link)
		} else {
			missing = append(missing, id)
		}
	}
	return links, missing
}

func (s *Server) planRoutes() {
	s.api("POST /api/catalog/plan", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		links, _ := s.catalogLinks(strList(b["ids"]))
		if len(links) == 0 {
			return nil, fail(400, "nothing selected", "empty")
		}
		return s.planShares(links), nil
	})
	s.api("POST /api/catalog/download", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		links, missing := s.catalogLinks(strList(b["ids"]))
		jobs, bad, err := s.queueShares(links, b)
		if err != nil {
			return nil, err
		}
		for _, id := range missing {
			bad = append(bad, id+": not in the catalog")
		}
		return map[string]any{"jobs": jobs, "errors": bad}, nil
	})
	// pasted links: which are usable (and why the others are not), then the review
	s.api("POST /api/jobs/plan", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		links, bad := linkLines(str(b["text"]))
		if len(links) == 0 && len(bad) == 0 {
			return nil, fail(400, "no usn:// links in that text", "no_links")
		}
		out := s.planShares(links)
		out["bad"] = bad
		return out, nil
	})
	s.api("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		links, badLines := linkLines(str(b["text"]))
		if len(links) == 0 && len(badLines) == 0 {
			return nil, fail(400, "no usn:// links in that text", "no_links")
		}
		jobs, bad, err := s.queueShares(links, b)
		if err != nil {
			return nil, err
		}
		for _, l := range badLines {
			bad = append(bad, fmt.Sprintf("line %d: %s", l.Line, l.Reason))
		}
		return map[string]any{"jobs": jobs, "errors": bad, "bad": badLines}, nil
	})
	s.api("POST /api/jobs/{id}/pause", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"ok": s.M.PauseJob(r.PathValue("id"))}, nil
	})
	s.api("POST /api/catalog/undo", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ok, err := s.Cat.Undo()
		if err == nil && ok {
			s.M.Log("catalog: last import undone")
		}
		return map[string]any{"ok": ok}, err
	})
	// a share opened by its link, kept in the catalog under its own name
	s.api("POST /api/catalog/add", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ss, err := s.session(str(body(r)["sid"]))
		if err != nil {
			return nil, err
		}
		ss.mu.Lock()
		e := catalog.Entry{Link: ss.Link, Title: ss.Title, Source: "opened", Also: []string{}}
		ss.mu.Unlock()
		added, err := s.Cat.Add(e)
		if err != nil {
			return nil, err
		}
		return map[string]any{"added": added, "id": app.LinkID(e.Link)}, nil
	})
}

// pickedLabel says what of the share a selection is, to tell the jobs of a share apart:
// "" (all of it), a folder's or a file's name, or how many items.
func pickedLabel(sel *selection) string {
	if sel.sel == nil {
		return ""
	}
	nf, nx := len(sel.folders), len(sel.exact)
	switch {
	case nf == 1 && nx == 0:
		return path.Base(strings.ReplaceAll(sel.folders[0], "\\", "/"))
	case nf == 0 && nx == 1:
		return path.Base(strings.ReplaceAll(sel.exact[0], "\\", "/"))
	case nf == 0:
		return fmt.Sprintf("%d files", nx)
	case nx == 0:
		return fmt.Sprintf("%d folders", nf)
	}
	return fmt.Sprintf("%d items", nf+nx)
}
