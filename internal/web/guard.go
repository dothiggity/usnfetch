package web

import (
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"strings"
	"sync"
)

// What keeps other web pages, and the files inside a share, away from the API:
//
//   - With no web password the API trusts every request from this machine, so it then answers
//     only to names nobody else can point at this machine (DNS rebinding): an IP address,
//     localhost, or this computer's own name. With a password, logging in is what guards it.
//   - Changes have to come from usnfetch's own page (Go's cross-origin protection: the
//     browser's Sec-Fetch-Site header, or Origin against Host) and carry JSON, which another
//     site can't send without asking first (CORS preflight), and usnfetch never says yes.
//   - A share's files are served sandboxed: no script, and an origin of their own. The kinds a
//     browser would run (web pages, XML, SVG opened as a page, scripts) come as plain text.
//   - The 7-Zip and UnRAR paths, the one setting that picks a program to run, can only be
//     changed on the machine itself, and only to a 7-Zip or UnRAR program.

var crossOrigin = http.NewCrossOriginProtection()

// machineNames: this computer's own name, as a browser on it could reach it ("mypc",
// "mypc.local"), lower case.
var machineNames = sync.OnceValue(func() []string {
	n, err := os.Hostname()
	n = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n), "."))
	if err != nil || n == "" {
		return nil
	}
	short, _, _ := strings.Cut(n, ".")
	return []string{n, short, short + ".local"}
})

// hostOK: a Host header that can only mean this machine.
func hostOK(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if host == "" { // HTTP/1.0 without a Host header: not a browser
		return true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	for _, n := range machineNames() {
		if host == n {
			return true
		}
	}
	return false
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// jsonBody: the request has no body, or a JSON one.
func jsonBody(r *http.Request) bool {
	if r.ContentLength == 0 {
		return true
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "application/json"
}

// guard: why a request is turned away, nil if it isn't.
func (s *Server) guard(r *http.Request) *apiError {
	if s.M.Settings.Web.PasswordHash == "" && !hostOK(r.Host) {
		port := "7979"
		if _, p, err := net.SplitHostPort(r.Host); err == nil {
			port = p
		}
		return fail(403, fmt.Sprintf("usnfetch doesn't answer to the name %q while it has no web password. Open it as http://localhost:%s/ or by its IP address, or set a web password (Settings → Web access) to use other names.", r.Host, port), "bad_host")
	}
	if err := crossOrigin.Check(r); err != nil {
		return fail(403, "usnfetch only takes changes from its own page", "cross_origin")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && !safeMethod(r.Method) && !jsonBody(r) {
		return fail(415, "send the request as JSON (Content-Type: application/json)", "not_json")
	}
	return nil
}

// pageHeaders: what every response gets, and the app's own pages a bit more.
func pageHeaders(h http.Header, api bool) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	if !api {
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
	}
}

// fileType: the Content-Type a file from a share is served with. It's what the extension
// says, except that anything a browser would run comes as plain text. An SVG fetched as a
// picture (an <img>) stays one, since pictures can't run script.
func fileType(name string, r *http.Request) string {
	ct := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	if ct == "" {
		return "application/octet-stream"
	}
	mt, _, _ := mime.ParseMediaType(ct)
	if mt == "image/svg+xml" && r.Header.Get("Sec-Fetch-Dest") == "image" {
		return ct
	}
	switch {
	case mt == "text/html", mt == "text/xml", mt == "application/xml", mt == "text/xsl", strings.HasSuffix(mt, "+xml"),
		strings.Contains(mt, "javascript"), strings.Contains(mt, "ecmascript"):
		return "text/plain; charset=utf-8"
	}
	return ct
}

// sandboxFile: a file from a share opens with no script and an origin of its own, so
// nothing in it can reach the API. A PDF is left alone: browsers won't show one sandboxed,
// and their PDF viewers run apart from the page anyway.
func sandboxFile(h http.Header, ct string) {
	if !strings.HasPrefix(ct, "application/pdf") {
		h.Set("Content-Security-Policy", "sandbox")
	}
}

// toolNames: what the 7-Zip and UnRAR paths may name.
var toolNames = map[string][]string{
	"sevenzip_path": {"7z", "7za", "7zz", "7zzs"},
	"unrar_path":    {"unrar", "rar"},
}

// toolOK: p names one of the programs (with or without .exe).
func toolOK(key, p string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(p, `\`, "/")))
	base = strings.TrimSuffix(base, ".exe")
	for _, n := range toolNames[key] {
		if base == n {
			return true
		}
	}
	return false
}
