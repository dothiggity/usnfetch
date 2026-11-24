// Package web serves the browser UI and its JSON API.
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/http/pprof" // its handlers are mounted under /api/debug/pprof (the default mux it also fills is never served)
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"

	"usnfetch/internal/app"
	"usnfetch/internal/catalog"
	"usnfetch/internal/engine"
	"usnfetch/internal/extract"
	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

const (
	cookieName  = "usnf_session"
	sessionDays = 30
	idleClose   = 30 * time.Minute
)

// Server is the web app.
type Server struct {
	Version string
	M       *app.Manager
	Cat     *app.Catalog
	Static  fs.FS
	DevDir  string // serve the UI from this folder instead (design iteration)
	Restart func() // set when usnfetch can restart itself (tray mode); nil otherwise

	cache     *engine.SegmentCache
	smu       sync.Mutex
	sessions  map[string]*session
	streamMu  sync.Mutex
	streamKey string
	pool      *engine.Pool
	sem       chan struct{}
	mux       *http.ServeMux
}

type session struct {
	mu       sync.Mutex
	ID, Link string
	Title    string
	State    string // loading | ready | error | password
	Error    string
	Code     string // with State "error": "no_provider" when there is no provider to load it from
	Log      []string
	Index    *share.Index
	Check    any
	Checking bool
	LastUsed time.Time
	art      map[string]*artIndex // gallery artwork per game folder kind (gallery.go)
}

func (s *session) log(m string) {
	s.mu.Lock()
	s.Log = append(s.Log, time.Now().Format("[15:04:05] ")+m)
	if len(s.Log) > 200 {
		s.Log = s.Log[len(s.Log)-200:]
	}
	s.mu.Unlock()
}

func New(version string, m *app.Manager, cat *app.Catalog, static fs.FS, devDir string) *Server {
	s := &Server{Version: version, M: m, Cat: cat, Static: static, DevDir: devDir, cache: engine.NewSegmentCache(128 << 20),
		sessions: map[string]*session{}, mux: http.NewServeMux()}
	if m.Settings.Web.Secret == "" {
		m.Settings.Web.Secret = app.RandomID(32)
		_ = m.SaveSettings()
	}
	s.routes()
	go s.janitor()
	return s
}

// ---- helpers ----

type apiError struct {
	status    int
	code, msg string
}

func (e *apiError) Error() string { return e.msg }
func fail(status int, msg, code string) *apiError {
	if code == "" {
		code = "error"
	}
	return &apiError{status, code, msg}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeJSON(w, ae.status, map[string]any{"detail": map[string]string{"error": ae.code, "message": ae.msg}})
		return
	}
	writeJSON(w, 500, map[string]any{"detail": map[string]string{"error": "error", "message": err.Error()}})
}

type handler func(w http.ResponseWriter, r *http.Request) (any, error)

func (s *Server) api(pattern string, h handler) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		v, err := h(w, r)
		if err != nil {
			writeErr(w, err)
			return
		}
		if v != nil {
			writeJSON(w, 200, v)
		}
	})
}

func body(r *http.Request) map[string]any {
	m := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&m)
	return m
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func num(v any, d int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(x); err == nil {
			return n
		}
	}
	return d
}

func strList(v any) []string {
	out := []string{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			out = append(out, str(x))
		}
	}
	return out
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// ---- auth ----

func HashPassword(pw string) string {
	salt := []byte(app.RandomID(8))[:16]
	iters := 200000
	dk := pbkdf2.Key([]byte(pw), salt, iters, 32, sha256.New)
	return fmt.Sprintf("pbkdf2$%d$%s$%s", iters, hex.EncodeToString(salt), hex.EncodeToString(dk))
}

func CheckPassword(pw, stored string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 4 {
		return false
	}
	iters, _ := strconv.Atoi(p[1])
	salt, err1 := hex.DecodeString(p[2])
	want, err2 := hex.DecodeString(p[3])
	if err1 != nil || err2 != nil || iters <= 0 {
		return false
	}
	got := pbkdf2.Key([]byte(pw), salt, iters, len(want), sha256.New)
	return hmac.Equal(got, want)
}

func (s *Server) sign(payload string) string {
	key, _ := hex.DecodeString(s.M.Settings.Web.Secret)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) makeSession() string {
	b, _ := json.Marshal(map[string]int64{"exp": time.Now().Add(sessionDays * 24 * time.Hour).Unix()})
	p := base64.URLEncoding.EncodeToString(b)
	return p + "." + s.sign(p)
}

func (s *Server) sessionOK(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	p, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(p))) {
		return false
	}
	raw, err := base64.URLEncoding.DecodeString(p)
	if err != nil {
		return false
	}
	var v struct{ Exp int64 }
	return json.Unmarshal(raw, &v) == nil && v.Exp > time.Now().Unix()
}

func (s *Server) setCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.makeSession(), Path: "/", MaxAge: sessionDays * 86400, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// ServeHTTP: auth guard, then the routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/") && p != "/api/session" && p != "/api/login" {
		if s.M.Settings.Web.PasswordHash != "" {
			if !s.sessionOK(r) {
				writeJSON(w, 401, map[string]any{"detail": map[string]string{"error": "login", "message": "log in"}})
				return
			}
		} else if !isLoopback(r) {
			writeJSON(w, 403, map[string]any{"detail": map[string]string{"error": "no_password", "message": "Set a web password on the machine running usnfetch (Settings → Web access) before using it from another computer."}})
			return
		}
	}
	if !strings.HasPrefix(p, "/api/") || p == "/api/session" {
		w.Header().Set("Cache-Control", "no-store")
	}
	s.mux.ServeHTTP(w, r)
}

// ---- share sessions ----

func (s *Server) openShare(link, title, password string) (*session, error) {
	link = strings.TrimSpace(link)
	if _, err := share.ParseLink(link); err != nil {
		return nil, fail(400, err.Error(), "bad_link")
	}
	id := app.LinkID(link)
	s.smu.Lock()
	if old := s.sessions[id]; old != nil {
		old.mu.Lock()
		st := old.State
		old.mu.Unlock()
		if (st == "loading" || st == "ready") && password == "" {
			old.LastUsed = time.Now()
			s.smu.Unlock()
			return old, nil
		}
		s.closeSession(old)
	}
	if title == "" {
		if info := s.M.Store.ShareInfo(link); info != nil {
			title = str(info["name"])
		}
	}
	ss := &session{ID: id, Link: link, Title: title, State: "loading", LastUsed: time.Now()}
	s.sessions[id] = ss
	s.smu.Unlock()
	go s.loadShare(ss, password)
	return ss, nil
}

func (s *Server) loadShare(ss *session, password string) {
	fail := func(state, msg string) {
		ss.mu.Lock()
		ss.State, ss.Error = state, msg
		ss.mu.Unlock()
		ss.log(msg)
	}
	accts, err := s.M.Accounts()
	if err != nil {
		ss.mu.Lock()
		ss.Code = "no_provider"
		ss.mu.Unlock()
		fail("error", err.Error())
		return
	}
	ix, err := share.LoadShare(ss.Link, share.LoadOpts{Accounts: accts.All(), Password: password, IndexDir: s.M.IndexDir(), Log: ss.log})
	if err != nil {
		if errors.Is(err, share.ErrPassword) {
			fail("password", err.Error())
		} else {
			fail("error", err.Error())
		}
		return
	}
	s.M.PinIndex(ix.Path)
	if err := ix.EnsureTree(ss.log); err != nil {
		s.M.UnpinIndex(ix.Path)
		ix.Close()
		fail("error", err.Error())
		return
	}
	sum, err := ix.Summary()
	if err != nil {
		s.M.UnpinIndex(ix.Path)
		ix.Close()
		fail("error", err.Error())
		return
	}
	name := sum.Name
	if name == "" {
		name = ss.Title
	}
	if name == "" {
		name = ss.Link[:min(24, len(ss.Link))]
	}
	info := s.M.Store.ShareInfo(ss.Link)
	if info == nil {
		info = map[string]any{}
	}
	info["name"], info["size"], info["files"], info["packed"], info["opened_at"] = name, sum.Size, sum.Files, sum.Packed, float64(time.Now().UnixNano())/1e9
	for _, k := range []string{"format", "version", "access", "created_at", "signature", "owner_fingerprint"} {
		info[k] = ix.Info[k]
	}
	_ = s.M.Store.SetShareInfo(ss.Link, info)
	ss.mu.Lock()
	ss.Index, ss.Check, ss.State = ix, info["check"], "ready"
	if ss.Title == "" {
		ss.Title = name
	}
	ss.mu.Unlock()
	ss.log(fmt.Sprintf("ready: %s files, %.2f GB", commas(sum.Files), float64(sum.Size)/1e9))
	app.ReleaseMemory() // building the index and its folder tree can peak at GBs: give it back now
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// memoryReport: the process's memory (app.MemoryInfo) and the shares open in the browser.
func (s *Server) memoryReport() map[string]any {
	out := app.MemoryInfo()
	open := []map[string]any{}
	s.smu.Lock()
	for _, ss := range s.sessions {
		ss.mu.Lock()
		o := map[string]any{"title": ss.Title, "state": ss.State, "idle_s": int(time.Since(ss.LastUsed).Seconds())}
		if ss.Index != nil {
			o["files"], o["pool_entries"] = ss.Index.FileCount, ss.Index.PoolCount
		}
		ss.mu.Unlock()
		open = append(open, o)
	}
	s.smu.Unlock()
	out["open_shares"] = open
	return out
}

func (s *Server) closeSession(ss *session) { // s.smu held
	delete(s.sessions, ss.ID)
	ss.mu.Lock()
	ix := ss.Index
	ss.Index = nil
	ss.mu.Unlock()
	if ix != nil {
		s.M.UnpinIndex(ix.Path)
		ix.Close()
		app.ReleaseMemory()
	}
}

func (s *Server) janitor() {
	for range time.Tick(time.Minute) {
		s.smu.Lock()
		for _, ss := range s.sessions {
			ss.mu.Lock()
			idle := time.Since(ss.LastUsed) > idleClose && !ss.Checking
			ss.mu.Unlock()
			if idle {
				s.closeSession(ss)
			}
		}
		s.smu.Unlock()
	}
}

func (s *Server) session(id string) (*session, error) {
	s.smu.Lock()
	defer s.smu.Unlock()
	ss := s.sessions[id]
	if ss == nil {
		return nil, fail(404, "share is not open (it may have been closed after being idle); open it again", "not_open")
	}
	ss.LastUsed = time.Now()
	return ss, nil
}

func (s *Server) ready(id string) (*session, *share.Index, error) {
	ss, err := s.session(id)
	if err != nil {
		return nil, nil, err
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.State != "ready" || ss.Index == nil {
		return nil, nil, fail(409, "share is "+ss.State, "not_ready")
	}
	return ss, ss.Index, nil
}

func (s *Server) shareJSON(ss *session) map[string]any {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	logs := ss.Log
	if len(logs) > 40 {
		logs = logs[len(logs)-40:]
	}
	out := map[string]any{"id": ss.ID, "link": ss.Link, "title": ss.Title, "state": ss.State, "error": ss.Error, "error_code": ss.Code,
		"log": append([]string{}, logs...), "checking": ss.Checking, "check": ss.Check, "info": nil, "catalog": nil, "index": nil}
	if ss.State == "ready" {
		out["info"] = s.M.Store.ShareInfo(ss.Link)
	}
	if ss.Index != nil {
		out["index"] = ss.Index.Info
	}
	if e, ok := s.Cat.ByLink(ss.Link); ok {
		out["catalog"] = map[string]any{"id": app.LinkID(e.Link), "title": e.Title, "variant": e.Variant, "listed": e.Listed,
			"source": e.Source, "info": e.Info, "name": catalog.JobName(e, 90)}
	}
	return out
}

// stream resources, rebuilt when the accounts or the connection cap change.
func (s *Server) streamResources() (*engine.Pool, chan struct{}, error) {
	accts, err := s.M.Accounts()
	if err != nil {
		return nil, nil, err
	}
	n := max(1, s.M.Settings.StreamConnections)
	key := fmt.Sprintf("%d|%s", n, accts.Describe())
	for _, a := range accts.All() {
		key += "|" + a.User + a.Password
	}
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if key != s.streamKey {
		if s.pool != nil {
			s.pool.Close()
		}
		s.pool, s.sem, s.streamKey = engine.NewPool(accts), make(chan struct{}, n), key
	}
	return s.pool, s.sem, nil
}

func fileJSON(f share.FileEntry) map[string]any {
	p := strings.ReplaceAll(f.Path, "\\", "/")
	name := path.Base(p)
	mt := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = mt[:i]
	}
	return map[string]any{"fid": f.FID, "name": name, "path": f.Path, "size": f.Size, "packed": f.Packed(), "segments": f.SegCount, "mime": mt}
}

// ---- routes ----

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.static("index.html"))
	s.mux.HandleFunc("GET /static/{name}", func(w http.ResponseWriter, r *http.Request) { s.static(r.PathValue("name"))(w, r) })

	s.api("GET /api/session", func(w http.ResponseWriter, r *http.Request) (any, error) {
		need := s.M.Settings.Web.PasswordHash != ""
		local := isLoopback(r)
		return map[string]any{"version": s.Version, "auth_required": need, "logged_in": (!need && local) || s.sessionOK(r), "remote_blocked": !need && !local}, nil
	})
	s.api("POST /api/login", func(w http.ResponseWriter, r *http.Request) (any, error) {
		stored := s.M.Settings.Web.PasswordHash
		if stored == "" {
			return map[string]any{"ok": true}, nil
		}
		if !CheckPassword(str(body(r)["password"]), stored) {
			time.Sleep(time.Second)
			return nil, fail(401, "wrong password", "bad_password")
		}
		s.setCookie(w)
		return map[string]any{"ok": true}, nil
	})
	s.api("POST /api/logout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
		return map[string]any{"ok": true}, nil
	})
	s.api("GET /api/status", s.status)
	// diagnostics (behind the same login as the rest of /api): memory split into Go's and
	// SQLite's share, what is open, and Go heap profiles for `go tool pprof`
	s.api("GET /api/debug/memory", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return s.memoryReport(), nil
	})
	s.api("POST /api/debug/memory/release", func(w http.ResponseWriter, r *http.Request) (any, error) {
		debug.FreeOSMemory() // what is left after this is in use, not garbage waiting to go
		return s.memoryReport(), nil
	})
	for _, name := range []string{"heap", "allocs", "goroutine"} {
		s.mux.Handle("GET /api/debug/pprof/"+name, pprof.Handler(name))
	}
	s.api("POST /api/system/restart", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if s.Restart == nil {
			return nil, fail(400, "usnfetch can restart itself when it runs in the tray (usnfetch tray)", "no_restart")
		}
		s.M.Log("restart asked for in the web app")
		go func() { time.Sleep(300 * time.Millisecond); s.Restart() }() // answer first
		return map[string]any{"ok": true}, nil
	})
	s.setupRoutes()
	s.api("GET /api/logs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		seq, lines := s.M.Logs(after)
		return map[string]any{"seq": seq, "lines": lines}, nil
	})

	// catalog
	s.api("GET /api/catalog", s.catalogList)
	// import: preview first (what is new, what would change), then apply; a blank detail
	// never replaces a filled one, and differing details are replaced only when asked
	s.api("POST /api/catalog/import", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		name := str(b["name"])
		if name == "" {
			name = "import"
		}
		src := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
		entries := catalog.ParseText(catalog.DecodeListFile([]byte(str(b["text"]))), "")
		if len(entries) == 0 {
			return nil, fail(400, "no usn:// links found in that text", "no_links")
		}
		for i := range entries { // the import's name is the source of links no SOURCE: line names
			if entries[i].Source == "" {
				if _, known := s.Cat.ByLink(entries[i].Link); !known {
					entries[i].Source = src
				}
			}
		}
		preview := b["preview"] == true
		res, err := s.Cat.Merge(entries, app.MergeOptions{Overwrite: b["overwrite"] == true, DryRun: preview})
		if err != nil {
			return nil, err
		}
		if !preview {
			s.M.Log(fmt.Sprintf("catalog import %s: %d link(s), %d new, %d filled in, %d changed, %d left as they were", name, len(entries), res.Added, res.Filled, res.Changed, res.Kept))
		}
		return map[string]any{"found": len(entries), "added": res.Added, "updated": res.Filled + res.Changed, "filled": res.Filled,
			"changed": res.Changed, "kept": res.Kept, "changes": res.Changes, "preview": preview,
			"undo": !preview && res.Added+res.Filled+res.Changed > 0}, nil
	})
	s.api("POST /api/catalog/remove", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ids := map[string]bool{}
		for _, id := range strList(body(r)["ids"]) {
			ids[id] = true
		}
		links := map[string]bool{}
		for _, e := range s.Cat.Snapshot() {
			if ids[app.LinkID(e.Link)] {
				links[e.Link] = true
			}
		}
		return map[string]any{"removed": len(links)}, s.Cat.Remove(links)
	})
	s.planRoutes()

	// shares
	s.api("POST /api/shares/open", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		link, title := str(b["link"]), ""
		if cid := str(b["catalog_id"]); cid != "" {
			e, ok := s.Cat.ByID(cid)
			if !ok {
				return nil, fail(404, "catalog entry not found", "")
			}
			link, title = e.Link, e.Label()
		}
		if link == "" {
			return nil, fail(400, "pass a link or a catalog_id", "")
		}
		ss, err := s.openShare(link, title, str(b["password"]))
		if err != nil {
			return nil, err
		}
		return s.shareJSON(ss), nil
	})
	s.api("GET /api/shares", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.smu.Lock()
		defer s.smu.Unlock()
		out := []map[string]any{}
		for _, ss := range s.sessions {
			ss.mu.Lock()
			out = append(out, map[string]any{"id": ss.ID, "title": ss.Title, "state": ss.State})
			ss.mu.Unlock()
		}
		return out, nil
	})
	s.api("GET /api/shares/{sid}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ss, err := s.session(r.PathValue("sid"))
		if err != nil {
			return nil, err
		}
		return s.shareJSON(ss), nil
	})
	s.api("POST /api/shares/{sid}/close", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.smu.Lock()
		if ss := s.sessions[r.PathValue("sid")]; ss != nil {
			s.closeSession(ss)
		}
		s.smu.Unlock()
		return map[string]any{"ok": true}, nil
	})
	s.api("GET /api/shares/{sid}/tree", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ss, ix, err := s.ready(r.PathValue("sid"))
		if err != nil {
			return nil, err
		}
		q := r.URL.Query()
		offset := max(0, num(q.Get("offset"), 0))
		limit := min(10000, max(1, num(q.Get("limit"), 2000)))
		d, folders, files, err := ix.ListDir(q.Get("path"), offset, limit, q.Get("sort"))
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, fail(404, fmt.Sprintf("no folder %q in this share", q.Get("path")), "no_folder")
		}
		roots, ls := s.localRoots(ss, ix), listings{}
		fj := make([]map[string]any, 0, len(files))
		for _, f := range files {
			m := fileJSON(f)
			if st := fileState(ls, roots, f.Path, f.Size); st != "" {
				m["local"] = st
			}
			fj = append(fj, m)
		}
		dj := make([]map[string]any, 0, len(folders))
		for _, f := range folders {
			dj = append(dj, map[string]any{"name": f.Name, "path": f.Path, "size": f.Size, "count": f.Count, "local": folderThere(ls, roots, f.Path)})
		}
		out := map[string]any{"path": d.Path, "size": d.Size, "count": d.Count, "folders": dj, "files": fj,
			"file_total": d.NFiles, "offset": offset, "truncated": int64(offset+len(files)) < d.NFiles}
		if offset == 0 {
			out["gallery"] = s.galleryKind(ix, d.Path, folders, files).Kind
		}
		return out, nil
	})
	s.api("POST /api/shares/{sid}/check", func(w http.ResponseWriter, r *http.Request) (any, error) {
		ss, ix, err := s.ready(r.PathValue("sid"))
		if err != nil {
			return nil, err
		}
		sample := min(4096, max(8, num(body(r)["sample"], 64)))
		ss.mu.Lock()
		start := !ss.Checking
		ss.Checking = true
		ss.mu.Unlock()
		if start {
			go func() {
				var res any
				accts, err := s.M.Accounts()
				if err == nil {
					var a *engine.Availability
					if a, err = engine.CheckAvailability(ix, accts, sample, 8); err == nil {
						res = a
					}
				}
				if err != nil {
					res = map[string]any{"verdict": "error", "error": err.Error(), "checked_at": float64(time.Now().UnixNano()) / 1e9, "accounts": []any{}}
				}
				if info := s.M.Store.ShareInfo(ss.Link); info != nil {
					b, _ := json.Marshal(res)
					var m any
					_ = json.Unmarshal(b, &m)
					info["check"] = m
					_ = s.M.Store.SetShareInfo(ss.Link, info)
				}
				ss.mu.Lock()
				ss.Check, ss.Checking = res, false
				ss.mu.Unlock()
			}()
		}
		return s.shareJSON(ss), nil
	})
	s.api("POST /api/shares/{sid}/download", s.shareDownload)
	s.api("POST /api/shares/{sid}/plan", s.sharePlan)
	s.api("POST /api/shares/{sid}/ondisk", s.shareOnDisk)
	s.api("GET /api/shares/{sid}/gallery", s.shareGallery)
	s.api("POST /api/queue/pause", func(w http.ResponseWriter, r *http.Request) (any, error) {
		m, _ := body(r)["minutes"].(float64)
		s.M.PauseDownloads(m)
		return s.M.Throttle(), nil
	})
	s.api("POST /api/queue/resume", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.M.ResumeDownloads()
		return s.M.Throttle(), nil
	})
	s.api("POST /api/jobs/reorder", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.M.Reorder(strList(body(r)["ids"]))
		return map[string]any{"ok": true}, nil
	})
	s.api("GET /api/shares/{sid}/search", s.shareSearch)
	s.mux.HandleFunc("GET /api/shares/{sid}/file/{fid}", s.shareFile)

	// jobs
	s.api("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) (any, error) {
		h := s.M.History()
		return map[string]any{"queue": s.M.Queue(), "history": h[:min(300, len(h))], "throttle": s.M.Throttle()}, nil
	})
	s.api("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		j := s.M.Get(r.PathValue("id"))
		if j == nil {
			return nil, fail(404, "no such job", "")
		}
		return j.View(true), nil
	})
	s.api("POST /api/jobs/{id}/stop", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"ok": s.M.Stop(r.PathValue("id"))}, nil
	})
	s.api("POST /api/jobs/{id}/resume", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"ok": s.M.Resume(r.PathValue("id"), body(r)["redownload"] == true)}, nil
	})
	s.api("DELETE /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"ok": s.M.Delete(r.PathValue("id"), r.URL.Query().Get("files") == "1")}, nil
	})

	// settings
	s.api("GET /api/settings", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.settingsView(), nil })
	s.api("PUT /api/settings", s.settingsPut)
	s.api("POST /api/settings/test-account", func(w http.ResponseWriter, r *http.Request) (any, error) {
		accts := s.M.MergeAccounts([]map[string]any{body(r)})
		if len(accts) == 0 {
			return nil, fail(400, "server is required", "")
		}
		a := app.AccountFrom(accts[0])
		t0 := time.Now()
		c, err := nntp.Dial(a, 15*time.Second)
		if err != nil {
			return map[string]any{"ok": false, "message": err.Error()}, nil
		}
		c.Close()
		return map[string]any{"ok": true, "message": fmt.Sprintf("connected and logged in to %s:%d in %d ms", a.Server, a.Port, time.Since(t0).Milliseconds())}, nil
	})
	s.api("POST /api/settings/import-sab", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p := str(body(r)["path"])
		if p == "" {
			p = app.SabIniDefault()
		}
		if st, err := os.Stat(p); p == "" || err != nil || st.IsDir() {
			return nil, fail(400, "sabnzbd.ini not found; give its path (SABnzbd: Config → General shows the config file)", "no_ini")
		}
		found, err := app.ImportSabServers(p)
		if err != nil {
			return nil, err
		}
		st := s.M.Settings
		have := map[string]bool{}
		key := func(a app.AccountSetting) string {
			return strings.ToLower(a.Server) + "|" + strconv.Itoa(a.Port) + "|" + strings.ToLower(a.Username)
		}
		for _, a := range st.Accounts {
			have[key(a)] = true
		}
		added := 0
		for _, a := range found {
			if !have[key(a)] {
				st.Accounts = append(st.Accounts, a)
				added++
			}
		}
		if err := s.M.SaveSettings(); err != nil {
			return nil, err
		}
		return map[string]any{"found": len(found), "added": added, "settings": s.settingsView()}, nil
	})
	s.api("POST /api/settings/web-password", func(w http.ResponseWriter, r *http.Request) (any, error) {
		b := body(r)
		web := &s.M.Settings.Web
		if web.PasswordHash != "" && !CheckPassword(str(b["current"]), web.PasswordHash) {
			return nil, fail(403, "current password is wrong", "bad_password")
		}
		nw := str(b["new"])
		if nw != "" && len(nw) < 8 {
			return nil, fail(400, "use at least 8 characters", "weak")
		}
		if nw == "" && !isLoopback(r) {
			return nil, fail(403, "the password can only be removed from the machine running usnfetch", "remote")
		}
		web.PasswordHash = ""
		if nw != "" {
			web.PasswordHash = HashPassword(nw)
		}
		if err := s.M.SaveSettings(); err != nil {
			return nil, err
		}
		if nw != "" {
			s.setCookie(w)
		}
		return map[string]any{"ok": true, "has_password": nw != ""}, nil
	})
}

func (s *Server) static(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := path.Base(name)
		var data []byte
		var err error
		if s.DevDir != "" {
			data, err = os.ReadFile(filepath.Join(s.DevDir, name))
		} else {
			data, err = fs.ReadFile(s.Static, name)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) (any, error) {
	q := s.M.Queue()
	accounts := ""
	if a, err := s.M.Accounts(); err == nil {
		accounts = a.Describe()
	}
	running, speed := 0, 0.0
	for _, j := range q {
		if j.State == "planning" || j.State == "downloading" || j.State == "extracting" || j.State == "stopping" {
			running++
		}
		speed += j.Rate
	}
	dir := s.M.Settings.CompleteDir
	disk := map[string]any{"path": dir, "free": nil, "total": nil}
	if f, err := app.DiskFree(dir); err == nil {
		disk["free"] = f
	}
	if t, err := app.DiskTotal(dir); err == nil {
		disk["total"] = t
	}
	s.smu.Lock()
	open := len(s.sessions)
	s.smu.Unlock()
	// the transfer in progress, for the status bar: running jobs first, then the waiting ones
	transfer := []map[string]any{}
	for _, pass := range []bool{true, false} {
		for _, j := range q {
			isRunning := j.State == "planning" || j.State == "downloading" || j.State == "extracting" || j.State == "stopping"
			if isRunning != pass || len(transfer) >= 4 {
				continue
			}
			transfer = append(transfer, map[string]any{"id": j.JobID, "name": j.Name, "picked": j.Picked, "state": j.State, "phase": j.Phase, "bytes": j.Bytes,
				"size": j.Size, "rate": j.Rate, "eta": j.ETA, "file_pos": j.FilePos, "current": j.Current, "missing": j.Missing, "detail": j.Detail})
		}
	}
	// the jobs that finished last, so the browser can say when one is done or failed
	recent := []map[string]any{}
	for _, j := range s.M.History() {
		if len(recent) >= 6 {
			break
		}
		if j.Finished == nil {
			continue
		}
		recent = append(recent, map[string]any{"id": j.JobID, "name": j.Name, "picked": j.Picked, "state": j.State, "detail": j.Detail,
			"finished": *j.Finished, "share": app.LinkID(j.Link)})
	}
	return map[string]any{"version": s.Version, "accounts": accounts, "needs_setup": accounts == "", "disk": disk, "active": len(q), "throttle": s.M.Throttle(), "setup_done": s.M.Settings.SetupDone,
		"transfer": transfer, "can_restart": s.Restart != nil, "recent": recent,
		"running": running, "speed": speed, "open_shares": open, "runtime": runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
		"python": runtime.Version(), "data_dir": s.M.DataDir}, nil
}

// cover is how much of a share its jobs have downloaded between them.
type cover struct {
	ids   []string
	of    int  // the share's files, as its last planned job counted them
	whole bool // a download of the whole share completed
}

func (s *Server) catalogList(w http.ResponseWriter, r *http.Request) (any, error) {
	jobs := map[string]app.JobView{}
	covers := map[string]*cover{}
	h, q := s.M.History(), s.M.Queue()
	for i := len(h) - 1; i >= 0; i-- { // oldest first, active last: newest wins
		jobs[h[i].Link] = h[i]
	}
	for _, j := range q {
		jobs[j.Link] = j
	}
	for _, j := range append(append([]app.JobView{}, h...), q...) {
		c := covers[j.Link]
		if c == nil {
			c = &cover{}
			covers[j.Link] = c
		}
		c.ids = append(c.ids, j.JobID)
		if j.FileCount != nil && *j.FileCount > c.of {
			c.of = *j.FileCount
		}
		if j.State == "completed" && j.Selected == nil {
			c.whole = true
		}
	}
	got := func(link string) any {
		c := covers[link]
		if c == nil {
			return nil
		}
		files := s.M.Store.DoneDistinct(c.ids)
		if files == 0 && !c.whole {
			return nil
		}
		complete := c.whole || (c.of > 0 && files >= int64(c.of))
		return map[string]any{"files": files, "of": c.of, "complete": complete}
	}
	infos := s.M.Store.AllShareInfo()
	entries := s.Cat.Snapshot()
	out := make([]map[string]any, 0, len(entries))
	sources := map[string]bool{}
	for _, e := range entries {
		if e.Source != "" {
			sources[e.Source] = true
		}
		var jv, sh, health any
		if j, ok := jobs[e.Link]; ok {
			jv = map[string]any{"id": j.JobID, "state": j.State, "bytes": j.Bytes, "size": j.Size, "rate": j.Rate, "picked": j.Picked, "detail": j.Detail}
		}
		if info := infos[e.Link]; info != nil {
			if info["size"] != nil {
				sh = map[string]any{"name": info["name"], "size": info["size"], "files": info["files"], "packed": info["packed"], "format": info["format"], "access": info["access"]}
			}
			if ck, ok := info["check"].(map[string]any); ok {
				health = map[string]any{"verdict": ck["verdict"], "sampled": ck["sampled"], "available": ck["available"], "checked_at": ck["checked_at"]}
			}
		}
		also := e.Also
		if also == nil {
			also = []string{}
		}
		out = append(out, map[string]any{"id": app.LinkID(e.Link), "link": e.Link, "title": e.Title, "variant": e.Variant, "listed": e.Listed,
			"listed_size": e.Size, "source": e.Source, "sku": e.SKU, "info": e.Info, "also": also, "added": e.Added,
			"name": catalog.JobName(e, 90), "job": jv, "share": sh, "health": health, "got": got(e.Link)})
	}
	srcs := []string{}
	for k := range sources {
		srcs = append(srcs, k)
	}
	sort.Strings(srcs)
	def := s.M.Settings.CatalogCategory
	if def == "" {
		def = "Default"
	}
	return map[string]any{"entries": out, "sources": srcs, "categories": s.M.Settings.Categories, "default_category": def}, nil
}

// selection is a resolved choice of files and folders of a share.
type selection struct {
	sel     []string // the job's file selection (nil = the whole share)
	folders []string // selected folders, outermost only ("" = the whole share)
	exact   []string // selected files not inside a selected folder
	count   int64
	size    int64
}

func (s *Server) resolveSel(ix *share.Index, b map[string]any) (*selection, error) {
	sum, err := ix.Summary()
	if err != nil {
		return nil, err
	}
	out := &selection{count: sum.Files, size: sum.Size, folders: []string{""}}
	paths := map[string]bool{}
	for _, p := range strList(b["paths"]) {
		paths[p] = true
	}
	folders := map[string]bool{}
	for _, f := range strList(b["folders"]) {
		folders[strings.Join(strings.FieldsFunc(strings.ReplaceAll(f, "\\", "/"), func(r rune) bool { return r == '/' }), "/")] = true
	}
	if folders[""] || (len(paths) == 0 && len(folders) == 0) { // the root folder = everything
		return out, nil
	}
	var dirs []*share.DirInfo
	for f := range folders {
		if d, _ := ix.Dir(f); d != nil {
			dirs = append(dirs, d)
		}
	}
	// a folder is selected as "folder/" (everything under it): one entry however many files
	out.folders, out.count, out.size = nil, 0, 0
	var prefixes []string
	for _, d := range dirs {
		nested := false
		for _, o := range dirs {
			if o != d && strings.HasPrefix(d.Path, o.Path+"/") {
				nested = true
			}
		}
		if !nested {
			prefixes = append(prefixes, d.Path+"/")
			out.folders = append(out.folders, d.Path)
			out.count += d.Count
			out.size += d.Size
		}
	}
	for p := range paths {
		under := false
		for _, pre := range prefixes {
			if strings.HasPrefix(strings.ReplaceAll(p, "\\", "/"), pre) {
				under = true
			}
		}
		if fe, _ := ix.FileByPath(p); fe != nil && !under {
			out.exact = append(out.exact, p)
			out.size += fe.Size
		}
	}
	sort.Strings(out.exact)
	sort.Strings(prefixes)
	sort.Strings(out.folders)
	out.sel = append(append([]string{}, out.exact...), prefixes...)
	out.count += int64(len(out.exact))
	if out.count == 0 {
		return nil, fail(400, "nothing selected", "empty")
	}
	return out, nil
}

// jobNameFor: the name a download of this share gets unless one is given.
func (s *Server) jobNameFor(ss *session, name string) string {
	if name != "" {
		return name
	}
	if e, ok := s.Cat.ByLink(ss.Link); ok {
		return catalog.JobName(e, 90)
	}
	return ""
}

// sharePlan previews a download: size against free space, where the files land, and
// what the extractor would do with the archives in it.
func (s *Server) sharePlan(w http.ResponseWriter, r *http.Request) (any, error) {
	ss, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	b := body(r)
	sel, err := s.resolveSel(ix, b)
	if err != nil {
		return nil, err
	}
	st := s.M.Settings
	cat := str(b["category"])
	if cat == "" {
		cat = st.CatalogCategory
	}
	if cat == "" {
		cat = "Default"
	}
	name, nameAuto, top := s.destination(ss, ix, strings.TrimSpace(str(b["name"])))
	outDir := app.JobOutDir(st, cat, name)
	var example string
	switch {
	case len(sel.folders) > 0 && sel.folders[0] != "":
		example = app.SafeRelPath(app.StripTop(sel.folders[0], top)) + string(os.PathSeparator) + "…"
	case len(sel.exact) > 0:
		example = app.SafeRelPath(app.StripTop(sel.exact[0], top))
	}
	var free any
	if f, err := app.DiskFree(st.CompleteDir); err == nil {
		free = f
	}
	// archives: classify the volume-like names per folder, as the extractor will
	byDir := map[string][]string{}
	sizes := map[string]int64{}
	if len(sel.folders) > 0 {
		an, err := ix.ArchiveNames(r.Context(), sel.folders)
		if err != nil {
			return nil, err
		}
		for d, rows := range an {
			for _, row := range rows {
				byDir[d] = append(byDir[d], row.Name)
				sizes[d+"/"+row.Name] = row.Size
			}
		}
	}
	for _, p := range sel.exact {
		if !extract.LooksLikeVolume(p) {
			continue
		}
		pp := strings.ReplaceAll(p, "\\", "/")
		d, n := path.Dir(pp), path.Base(pp)
		if d == "." {
			d = ""
		}
		byDir[d] = append(byDir[d], n)
		if fe, _ := ix.FileByPath(p); fe != nil {
			sizes[d+"/"+n] = fe.Size
		}
	}
	arc := map[string]int{"rar": 0, "split": 0, "single": 0, "empty": 0}
	var singles []string
	for d, names := range byDir {
		for _, set := range extract.Sets(names) {
			switch {
			case sizes[d+"/"+set.First] == 0:
				arc["empty"]++
			case set.Standalone:
				arc["single"]++
				if len(singles) < 3 {
					singles = append(singles, set.First)
				}
			case set.Kind == "rar":
				arc["rar"]++
			default:
				arc["split"]++
			}
		}
	}
	sort.Strings(singles)
	var active []map[string]any
	for _, j := range s.M.Queue() {
		if j.Link == ss.Link {
			active = append(active, map[string]any{"id": j.JobID, "name": j.Name, "state": j.State})
		}
	}
	return map[string]any{"files": sel.count, "size": sel.size, "free": free, "complete_dir": st.CompleteDir, "sep": string(os.PathSeparator),
		"name": name, "name_auto": nameAuto, "category": cat, "categories": st.Categories, "out_dir": outDir, "example": example, "top": top,
		"extract_default": st.DefaultExtractMode(), "delete_archives": st.DeleteArchives, "archives": arc, "single_examples": singles,
		"active_jobs": active}, nil
}

// shareSearch: folders and files of an open share matching words, under a folder.
func (s *Server) shareSearch(w http.ResponseWriter, r *http.Request) (any, error) {
	ss, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	q := r.URL.Query()
	t0 := time.Now()
	res, err := ix.Search(r.Context(), q.Get("q"), q.Get("path"), min(2000, max(1, num(q.Get("limit"), 300))), q.Get("sort"))
	if err != nil {
		if r.Context().Err() != nil {
			return nil, nil // the browser gave up on it (a newer search replaced it)
		}
		return nil, err
	}
	roots, ls := s.localRoots(ss, ix), listings{}
	files := make([]map[string]any, 0, len(res.Files))
	for _, h := range res.Files {
		fj := fileJSON(h.File)
		fj["folder"] = h.Folder
		if st := fileState(ls, roots, h.File.Path, h.File.Size); st != "" {
			fj["local"] = st
		}
		files = append(files, fj)
	}
	folders := make([]map[string]any, 0, len(res.Folders))
	for _, f := range res.Folders {
		folders = append(folders, map[string]any{"name": f.Name, "path": f.Path, "size": f.Size, "count": f.Count, "local": folderThere(ls, roots, f.Path)})
	}
	return map[string]any{"q": q.Get("q"), "path": q.Get("path"), "folders": folders, "files": files,
		"folder_matches": res.FolderMatches, "file_matches": res.FileMatches, "ms": time.Since(t0).Milliseconds()}, nil
}

func (s *Server) shareDownload(w http.ResponseWriter, r *http.Request) (any, error) {
	ss, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		return nil, err
	}
	b := body(r)
	mode := str(b["extract"])
	if !app.ValidExtractMode(mode) {
		return nil, fail(400, "extract must be off, sets or all", "bad_extract")
	}
	if e := str(b["existing"]); e != "" && e != app.ExistingSkip && e != app.ExistingVerify && e != app.ExistingRedownload {
		return nil, fail(400, "existing must be skip, verify or redownload", "bad_existing")
	}
	sel, err := s.resolveSel(ix, b)
	if err != nil {
		return nil, err
	}
	cat := str(b["category"])
	if cat == "" {
		cat = s.M.Settings.CatalogCategory
	}
	if cat == "" {
		cat = "Default"
	}
	j, err := s.M.AddJob(ss.Link, app.AddOptions{Name: s.jobNameFor(ss, strings.TrimSpace(str(b["name"]))), Category: cat,
		FileSel: sel.sel, ExtractMode: mode, Existing: str(b["existing"]), Picked: pickedLabel(sel), Size: sel.size})
	if err != nil {
		return nil, err
	}
	return map[string]any{"job": j.ID, "files": sel.count, "size": sel.size}, nil
}

// shareFile streams a file straight from Usenet, honouring one HTTP range (players seek
// with these); nothing is written to disk.
func (s *Server) shareFile(w http.ResponseWriter, r *http.Request) {
	_, ix, err := s.ready(r.PathValue("sid"))
	if err != nil {
		writeErr(w, err)
		return
	}
	fid, _ := strconv.ParseInt(r.PathValue("fid"), 10, 64)
	fe, err := ix.File(fid)
	if err != nil || fe == nil || !fe.Downloadable() {
		writeErr(w, fail(404, "no such file in this share", ""))
		return
	}
	pool, sem, err := s.streamResources()
	if err != nil {
		writeErr(w, err)
		return
	}
	rd, err := engine.NewFileReader(ix, *fe, pool, s.cache, sem)
	if err != nil {
		writeErr(w, err)
		return
	}
	size, start, end, status := fe.Size, int64(0), fe.Size, 200
	if rng := r.Header.Get("Range"); strings.HasPrefix(rng, "bytes=") && !strings.Contains(rng, ",") {
		a, b, _ := strings.Cut(rng[6:], "-")
		var e1, e2 error
		if a == "" {
			var n int64
			n, e1 = strconv.ParseInt(b, 10, 64)
			start, end = max(0, size-n), size
		} else {
			start, e1 = strconv.ParseInt(a, 10, 64)
			if b != "" {
				var bb int64
				bb, e2 = strconv.ParseInt(b, 10, 64)
				end = min(bb+1, size)
			}
		}
		if e1 != nil || e2 != nil {
			start, end = 0, size
		} else {
			if start >= size || start >= end {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
				w.WriteHeader(416)
				return
			}
			status = 206
		}
	}
	name := path.Base(strings.ReplaceAll(fe.Path, "\\", "/"))
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	ct := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("Accept-Ranges", "bytes")
	h.Set("Cache-Control", "private, max-age=86400") // a file id names fixed content of this share
	h.Set("Content-Length", strconv.FormatInt(end-start, 10))
	h.Set("Content-Disposition", fmt.Sprintf("%s; filename*=UTF-8''%s", disp, url.PathEscape(name)))
	if status == 206 {
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, size))
	}
	w.WriteHeader(status)
	if end-start > 50_000_000 {
		s.M.Log(fmt.Sprintf("stream %s bytes %d-%d/%d", name, start, end-1, size))
	}
	if r.Method == http.MethodHead {
		return
	}
	if err := rd.WriteRange(w, start, end); err != nil && r.Context().Err() == nil {
		s.M.Log(fmt.Sprintf("stream %s: %v", name, err))
	}
}

func (s *Server) settingsView() map[string]any {
	st := s.M.Settings
	b, _ := json.Marshal(st)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	accts := []any{}
	for _, a := range st.Accounts {
		ab, _ := json.Marshal(a)
		var am map[string]any
		_ = json.Unmarshal(ab, &am)
		am["has_password"] = a.Password != ""
		am["password"] = ""
		accts = append(accts, am)
	}
	m["accounts"] = accts
	m["web"] = map[string]any{"host": st.Web.Host, "port": st.Web.Port, "has_password": st.Web.PasswordHash != ""}
	m["sab_ini"] = app.SabIniDefault()
	sz, ur := extract.FindTools(st.SevenZipPath, st.UnrarPath)
	m["tools"] = map[string]any{"sevenzip": sz, "unrar": ur} // what extraction would use ("" = not found)
	return m
}

func (s *Server) settingsPut(w http.ResponseWriter, r *http.Request) (any, error) {
	b := body(r)
	st := s.M.Settings
	getStr := func(k string, dst *string) {
		if v, ok := b[k]; ok {
			*dst = strings.TrimSpace(str(v))
		}
	}
	getInt := func(k string, dst *int) {
		if v, ok := b[k]; ok {
			*dst = num(v, *dst)
		}
	}
	getBool := func(k string, dst *bool) {
		if v, ok := b[k].(bool); ok {
			*dst = v
		}
	}
	getStr("complete_dir", &st.CompleteDir)
	getStr("sevenzip_path", &st.SevenZipPath)
	getStr("unrar_path", &st.UnrarPath)
	getStr("index_dir", &st.IndexDir)
	getStr("catalog_category", &st.CatalogCategory)
	getInt("missing_retries", &st.MissingRetries)
	getInt("max_parallel_jobs", &st.MaxParallelJobs)
	getInt("stream_connections", &st.StreamConnections)
	getBool("verify", &st.Verify)
	getBool("extract", &st.Extract)
	getBool("extract_standalone", &st.ExtractStandalone)
	if v, ok := b["speed_limit_mbps"]; ok {
		f, _ := v.(float64)
		if sv, ok := v.(string); ok {
			f, _ = strconv.ParseFloat(strings.TrimSpace(sv), 64)
		}
		st.SpeedLimit = max(0, f)
	}
	if v, ok := b["schedule"]; ok {
		raw, _ := json.Marshal(v)
		var rules []app.ScheduleRule
		if err := json.Unmarshal(raw, &rules); err != nil {
			return nil, fail(400, "schedule: "+err.Error(), "bad_schedule")
		}
		for i, r := range rules {
			if !app.ValidRule(r) {
				return nil, fail(400, fmt.Sprintf("schedule rule %d: times are HH:MM, action is pause or limit (with MB/s)", i+1), "bad_schedule")
			}
		}
		if rules == nil {
			rules = []app.ScheduleRule{}
		}
		st.Schedule = rules
	}
	getBool("delete_archives", &st.DeleteArchives)
	if v, ok := b["categories"]; ok {
		var cats []string
		var raw []string
		if l, ok := v.([]any); ok {
			for _, x := range l {
				raw = append(raw, str(x))
			}
		} else {
			raw = strings.Split(str(v), ",")
		}
		for _, c := range raw {
			if c = strings.TrimSpace(c); c != "" {
				cats = append(cats, c)
			}
		}
		if len(cats) == 0 {
			cats = []string{"Default"}
		}
		st.Categories = cats
	}
	if v, ok := b["accounts"]; ok {
		var in []map[string]any
		if l, ok := v.([]any); ok {
			for _, x := range l {
				if m, ok := x.(map[string]any); ok {
					in = append(in, m)
				}
			}
		}
		st.Accounts = s.M.MergeAccounts(in)
	}
	if err := s.M.SaveSettings(); err != nil {
		return nil, err
	}
	s.M.Kick()
	return s.settingsView(), nil
}
