package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"usnfetch/internal/app"
	"usnfetch/internal/web"
	webui "usnfetch/web"
)

// Version of the program, without the "v". Release builds set it from the git tag
// (GoReleaser: -ldflags "-X main.Version=0.10.0"); keep this in step for local builds.
var Version = "0.12.1"

type serveOpts struct {
	host      string
	port      int
	data, dev string
	args      []string // as given (for the autostart entry)
}

func parseServe(name string, args []string) serveOpts {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	host := fs.String("host", "", "address to listen on (default from settings, 127.0.0.1; 0.0.0.0 = every interface)")
	port := fs.Int("port", 0, "port (default from settings, 7979)")
	data := fs.String("data", defaultData(), "data folder: settings, job state, catalog, share indexes")
	dev := fs.String("web", "", "serve the UI from this folder instead of the built-in copy (for design work)")
	_ = fs.Parse(args)
	dir, _ := filepath.Abs(*data)
	return serveOpts{host: *host, port: *port, data: dir, dev: *dev, args: args}
}

// running is a started web app.
type running struct {
	M    *app.Manager
	Web  *web.Server
	URL  string // what a browser on this machine opens
	hs   *http.Server
	Done chan error // the HTTP server ended
}

func startServe(o serveOpts) (*running, error) {
	if err := os.MkdirAll(o.data, 0o755); err != nil {
		return nil, err
	}
	m, err := app.NewManager(o.data, true)
	if err != nil {
		return nil, err
	}
	cat, err := app.LoadCatalog(m.Store)
	if err != nil {
		m.Shutdown(time.Second)
		return nil, err
	}
	srv := web.New(Version, m, cat, webui.FS, o.dev)
	h, p := o.host, o.port
	if h == "" {
		h = m.Settings.Web.Host
	}
	if p == 0 {
		p = m.Settings.Web.Port
	}
	addr := net.JoinHostPort(h, strconv.Itoa(p))
	shown := h
	if ip := net.ParseIP(h); ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		shown = "localhost"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		m.Shutdown(time.Second)
		return nil, err
	}
	r := &running{M: m, Web: srv, URL: fmt.Sprintf("http://%s:%d/", shown, p), hs: &http.Server{Handler: srv, ReadHeaderTimeout: 30 * time.Second}, Done: make(chan error, 1)}
	fmt.Fprintf(os.Stderr, "usnfetch %s: %s  (data: %s)\n", Version, r.URL, o.data)
	if ip := net.ParseIP(h); (ip == nil || !ip.IsLoopback()) && m.Settings.Web.PasswordHash == "" {
		fmt.Fprintln(os.Stderr, "note: other computers are refused until a web password is set (Settings → Web access on this machine, or: usnfetch set-password)")
	}
	go func() { r.Done <- r.hs.Serve(ln) }()
	return r, nil
}

// Stop ends the web server and saves the running jobs' checkpoints.
func (r *running) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = r.hs.Shutdown(ctx)
	cancel()
	r.M.Shutdown(20 * time.Second)
}

func cmdServe(args []string) int {
	r, err := startServe(parseServe("serve", args))
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
		fmt.Fprintln(os.Stderr, "stopping: saving job checkpoints…")
	case err := <-r.Done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("error: %v", err)
		}
	}
	r.Stop()
	return 0
}

func cmdSetPassword(args []string) int {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data folder")
	_ = fs.Parse(args)
	path := filepath.Join(*data, "usnfetch_gui.json")
	s, err := app.LoadSettings(path)
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	read := func(prompt string) string {
		fmt.Fprint(os.Stderr, prompt)
		if term.IsTerminal(int(os.Stdin.Fd())) {
			b, _ := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b)
		}
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimRight(line, "\r\n")
	}
	pw := read("New web password (empty = remove): ")
	if pw != "" {
		if len(pw) < 8 {
			logf("use at least 8 characters")
			return 1
		}
		if term.IsTerminal(int(os.Stdin.Fd())) && read("Again: ") != pw {
			logf("passwords differ")
			return 1
		}
		s.Web.PasswordHash = web.HashPassword(pw)
	} else {
		s.Web.PasswordHash = ""
	}
	if err := app.SaveSettings(s, path); err != nil {
		logf("error: %v", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, map[bool]string{true: "web password set", false: "web password removed"}[pw != ""])
	return 0
}
