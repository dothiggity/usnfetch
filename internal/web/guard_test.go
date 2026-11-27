package web

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"usnfetch/internal/app"
	"usnfetch/internal/catalog"
	"usnfetch/internal/share"
)

// testServer: the web app on a fresh data folder, without its background work.
func testServer(t *testing.T) *Server {
	t.Helper()
	m, err := app.NewManager(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Store.Close)
	m.Settings.CompleteDir = t.TempDir()
	cat, err := app.LoadCatalog(m.Store)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Version: "test", M: m, Cat: cat, Static: fstest.MapFS{}, sessions: map[string]*session{}, mux: http.NewServeMux()}
	m.Settings.Web.Secret = app.RandomID(32)
	s.routes()
	return s
}

type call struct {
	method, path, host, body, ctype string
	hdr                             map[string]string
	remote                          bool // from another computer
	cookie                          *http.Cookie
}

func (s *Server) do(t *testing.T, c call) (int, string, http.Header) {
	t.Helper()
	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	r.RemoteAddr = "127.0.0.1:50000"
	if c.remote {
		r.RemoteAddr = "192.168.1.20:50000"
	}
	r.Host = "127.0.0.1:7979"
	if c.host != "" {
		r.Host = c.host
	}
	if c.body == "" {
		r.ContentLength = 0
	}
	if c.ctype != "" {
		r.Header.Set("Content-Type", c.ctype)
	}
	for k, v := range c.hdr {
		r.Header.Set(k, v)
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	code := ""
	var out struct{ Detail struct{ Error string } }
	if json.Unmarshal(w.Body.Bytes(), &out) == nil {
		code = out.Detail.Error
	}
	return w.Code, code, w.Header()
}

func TestHostOK(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:7979": true, "localhost:7979": true, "LOCALHOST": true, "[::1]:7979": true, "192.168.1.5:7979": true,
		"app.localhost:7979": true, "": true,
		"evil.example:7979": false, "localhost.evil.example": false, "127.0.0.1.nip.io:7979": false, "attacker.com": false,
	} {
		if got := hostOK(host); got != want {
			t.Errorf("hostOK(%q) = %v, want %v", host, got, want)
		}
	}
	if names := machineNames(); len(names) > 0 && !hostOK(names[0]+":7979") {
		t.Errorf("this computer's own name %q is turned away", names[0])
	}
}

func TestGuard(t *testing.T) {
	s := testServer(t)
	type tc struct {
		name       string
		c          call
		status     int
		code       string
		notBlocked bool // only that the guard let it through
	}
	for _, x := range []tc{
		{"page from localhost", call{method: "GET", path: "/api/status", host: "localhost:7979"}, 200, "", false},
		{"DNS rebinding", call{method: "GET", path: "/api/status", host: "evil.example:7979"}, 403, "bad_host", false},
		{"rebinding the page too", call{method: "GET", path: "/", host: "evil.example:7979"}, 403, "", false},
		{"text/plain post (a form on another site)", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "text/plain"}, 415, "not_json", false},
		{"no content type", call{method: "POST", path: "/api/queue/resume", body: "{}"}, 415, "not_json", false},
		{"JSON post", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json"}, 200, "", false},
		{"JSON with charset", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json; charset=utf-8"}, 200, "", false},
		{"no body", call{method: "POST", path: "/api/queue/resume"}, 200, "", false},
		{"cross-site fetch", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json", hdr: map[string]string{"Sec-Fetch-Site": "cross-site"}}, 403, "cross_origin", false},
		{"another local app", call{method: "POST", path: "/api/queue/resume", hdr: map[string]string{"Sec-Fetch-Site": "same-site"}}, 403, "cross_origin", false},
		{"its own page", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json", hdr: map[string]string{"Sec-Fetch-Site": "same-origin"}}, 200, "", false},
		{"old browser, other origin", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json", hdr: map[string]string{"Origin": "http://evil.example"}}, 403, "cross_origin", false},
		{"old browser, own origin", call{method: "POST", path: "/api/queue/resume", body: "{}", ctype: "application/json", hdr: map[string]string{"Origin": "http://127.0.0.1:7979"}}, 200, "", false},
		{"cross-site delete", call{method: "DELETE", path: "/api/jobs/nope", hdr: map[string]string{"Sec-Fetch-Site": "cross-site"}}, 403, "cross_origin", false},
	} {
		st, code, _ := s.do(t, x.c)
		if st != x.status || code != x.code {
			t.Errorf("%s: %d %q, want %d %q", x.name, st, code, x.status, x.code)
		}
	}
	_, _, h := s.do(t, call{method: "GET", path: "/api/status"})
	if h.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("no nosniff on the API")
	}
	_, _, h = s.do(t, call{method: "GET", path: "/static/nope.css"})
	if h.Get("X-Frame-Options") != "DENY" || !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("the app's pages can be framed: %v", h)
	}

	// with a web password the login guards it, so any name will do (a reverse proxy's)
	s.M.Settings.Web.PasswordHash = HashPassword("a long password")
	if st, code, _ := s.do(t, call{method: "GET", path: "/api/status", host: "usnfetch.example.com", remote: true}); st != 401 || code != "login" {
		t.Errorf("with a password, another name: %d %q, want the login", st, code)
	}
	ck := &http.Cookie{Name: cookieName, Value: s.makeSession()}
	if st, _, _ := s.do(t, call{method: "GET", path: "/api/status", host: "usnfetch.example.com", remote: true, cookie: ck}); st != 200 {
		t.Errorf("logged in through another name: %d", st)
	}
}

func TestToolPaths(t *testing.T) {
	s := testServer(t)
	put := func(body string, remote bool, ck *http.Cookie) (int, string) {
		st, code, _ := s.do(t, call{method: "PUT", path: "/api/settings", body: body, ctype: "application/json", remote: remote, cookie: ck})
		return st, code
	}
	if st, code := put(`{"sevenzip_path": "C:\\Users\\me\\Downloads\\usnfetch\\Some build\\payload.exe"}`, false, nil); st != 400 || code != "bad_tool" {
		t.Errorf("a program that isn't 7-Zip: %d %q", st, code)
	}
	if s.M.Settings.SevenZipPath != "" {
		t.Errorf("it was kept anyway: %q", s.M.Settings.SevenZipPath)
	}
	if st, code := put(`{"sevenzip_path": "C:\\Program Files\\7-Zip\\7z.exe", "unrar_path": "/usr/bin/unrar"}`, false, nil); st != 200 {
		t.Errorf("7-Zip itself: %d %q", st, code)
	}
	if s.M.Settings.SevenZipPath != `C:\Program Files\7-Zip\7z.exe` || s.M.Settings.UnrarPath != "/usr/bin/unrar" {
		t.Errorf("paths not saved: %q %q", s.M.Settings.SevenZipPath, s.M.Settings.UnrarPath)
	}
	s.M.Settings.Web.PasswordHash = HashPassword("a long password")
	ck := &http.Cookie{Name: cookieName, Value: s.makeSession()}
	if st, code := put(`{"sevenzip_path": "D:\\7z.exe"}`, true, ck); st != 403 || code != "tool_remote" {
		t.Errorf("changed from another computer: %d %q", st, code)
	}
	// the settings page sends what's there: unchanged paths are fine from anywhere
	if st, code := put(`{"sevenzip_path": "C:\\Program Files\\7-Zip\\7z.exe", "verify": false}`, true, ck); st != 200 || s.M.Settings.Verify {
		t.Errorf("an unchanged path from another computer: %d %q", st, code)
	}
	for p, want := range map[string]bool{"7z": true, "/opt/7zz": true, `C:\x\7ZA.EXE`: true, "7zzs": true, "calc.exe": false, `C:\x\7z.exe.lnk`: false, "7zFM.exe": false} {
		if toolOK("sevenzip_path", p) != want {
			t.Errorf("toolOK(7-Zip, %q) = %v", p, !want)
		}
	}
	if toolOK("unrar_path", "7z") || !toolOK("unrar_path", `C:\Program Files\WinRAR\UnRAR.exe`) {
		t.Error("UnRAR names")
	}
}

func TestFileType(t *testing.T) {
	req := func(dest string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		if dest != "" {
			r.Header.Set("Sec-Fetch-Dest", dest)
		}
		return r
	}
	for _, x := range []struct{ name, dest, want string }{
		{"index.html", "document", "text/plain; charset=utf-8"},
		{"page.HTM", "", "text/plain; charset=utf-8"},
		{"page.xhtml", "document", "text/plain; charset=utf-8"},
		{"feed.xml", "document", "text/plain; charset=utf-8"},
		{"run.js", "document", "text/plain; charset=utf-8"},
		{"run.mjs", "script", "text/plain; charset=utf-8"},
		{"logo.svg", "document", "text/plain; charset=utf-8"},
		{"logo.svg", "image", "image/svg+xml"},
		{"cover.png", "image", "image/png"},
		{"intro.mp4", "video", "video/mp4"},
		{"manual.pdf", "document", "application/pdf"},
		{"readme.txt", "document", "text/plain; charset=utf-8"},
		{"data.zzq9", "document", "application/octet-stream"}, // no type known on any system
	} {
		if got := fileType(x.name, req(x.dest)); got != x.want {
			t.Errorf("fileType(%s, %s) = %q, want %q", x.name, x.dest, got, x.want)
		}
	}
	h := http.Header{}
	sandboxFile(h, "text/plain; charset=utf-8")
	if h.Get("Content-Security-Policy") != "sandbox" {
		t.Error("a share's file isn't sandboxed")
	}
	h = http.Header{}
	sandboxFile(h, "application/pdf")
	if h.Get("Content-Security-Policy") != "" {
		t.Error("a PDF is sandboxed, so browsers won't show it")
	}
}

// v2usn: a usn:// link for a usenet:// one, as the official client writes them.
func v2usn(u string) string {
	return "usn://" + base64.RawURLEncoding.EncodeToString(append([]byte{2}, u...))
}

func TestSameShare(t *testing.T) {
	s := testServer(t)
	const fp = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := "usenet://alt.binaries.test/m0000abcdef@host.x/00112233445566778899aabbccddeeff"
	pinned := v2usn(base + "/" + fp)
	if _, err := s.Cat.Add(catalog.Entry{Link: pinned, Title: "A build", Also: []string{}}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 12; n++ { // cut short by a line wrap, every length: the same share
		cut := pinned[:len(pinned)-n]
		if got := s.sameShare(cut, nil); got != pinned {
			t.Errorf("cut by %d: %q, want the catalog's link", n, got)
		}
	}
	if got := s.sameShare(base+"/"+fp, nil); got != pinned {
		t.Errorf("its usenet:// form: %q", got)
	}
	other := v2usn(base + "/" + strings.Repeat("f", 64)) // pins another publisher: never swapped
	if got := s.sameShare(other, nil); got != other {
		t.Errorf("another publisher's pin was swapped for %q", got)
	}
	diff := v2usn("usenet://alt.binaries.test/m0000abcdef@host.x/ffeeddccbbaa99887766554433221100/" + fp)
	if got := s.sameShare(diff, nil); got != diff {
		t.Errorf("another folder: %q", got)
	}
	// the catalog has a link without the pin: a pinned one is never swapped for it
	bare := v2usn("usenet://alt.binaries.test/m0000beef@host.x/00112233445566778899aabbccddeeff")
	if _, err := s.Cat.Add(catalog.Entry{Link: bare, Title: "B", Also: []string{}}); err != nil {
		t.Fatal(err)
	}
	full := v2usn("usenet://alt.binaries.test/m0000beef@host.x/00112233445566778899aabbccddeeff/" + fp)
	if got := s.sameShare(full, nil); got != full {
		t.Errorf("a pinned link was swapped for one without the pin: %q", got)
	}

	// an old-style link with its folder key after '#': never swapped for one without the key,
	// while a copy that lost its key to a line wrap is the same share
	const key = "aa00112233445566778899aabbccddeeff00112233445566778899aabbccddee"
	keyless := "usenet://alt.binaries.test/m0000cafe@host.x/0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"
	if _, err := s.Cat.Add(catalog.Entry{Link: keyless, Title: "C", Also: []string{}}); err != nil {
		t.Fatal(err)
	}
	if got := s.sameShare(keyless+"#"+key, nil); got != keyless+"#"+key {
		t.Errorf("a link with its folder key was swapped for %q", got)
	}
	keyed := "usenet://alt.binaries.test/m0000d00d@host.x/0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e#" + key
	if _, err := s.Cat.Add(catalog.Entry{Link: keyed, Title: "D", Also: []string{}}); err != nil {
		t.Fatal(err)
	}
	if got := s.sameShare(keyed[:len(keyed)-20], nil); got != keyed {
		t.Errorf("a link whose key was cut off: %q, want the catalog's", got)
	}
	// more copies of the manifest than the catalog's link names: kept
	more := "usenet://alt.binaries.test/m0000d00d@host.x+m0000d00e@host.x/0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e#" + key
	if got := s.sameShare(more, nil); got != more {
		t.Errorf("a link naming more manifest copies was swapped for %q", got)
	}
	// the same new share twice in one paste or list: one share
	same := s.sameAs()
	fresh := v2usn("usenet://alt.binaries.test/m0000feed@host.x/0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d/" + fp)
	if a, b := same(fresh), same(fresh[:len(fresh)-9]); a != fresh || b != fresh {
		t.Errorf("one paste, the share twice: %q, %q", a, b)
	}

	// pasted into Add links, the cut-off link is the catalog's share
	links, bad := linkLines("RetroPie build\n"+pinned[:len(pinned)-10]+"\n", s.sameAs())
	if len(links) != 1 || links[0] != pinned || len(bad) != 0 {
		t.Errorf("Add links: %v %v", links, bad)
	}
	if share.LinkKey("usn://AAAA") != "" || share.LinkKey("not a link") != "" {
		t.Error("LinkKey of something that isn't a share link")
	}
}

// The System page's Open for the data folder: never for another computer, and a missing
// folder is an error, not a file manager opening somewhere else. (Nothing here opens a window.)
func TestRevealDataFolder(t *testing.T) {
	s := testServer(t)
	if st, _, _ := s.do(t, call{method: "POST", path: "/api/system/reveal-data", body: "{}", ctype: "application/json", remote: true}); st == 200 {
		t.Fatal("another computer opened the data folder")
	}
	s.M.DataDir = filepath.Join(t.TempDir(), "gone")
	if st, code, _ := s.do(t, call{method: "POST", path: "/api/system/reveal-data", body: "{}", ctype: "application/json"}); st != 404 || code != "no_folder" {
		t.Fatalf("missing data folder: %d %q", st, code)
	}
}
