// usnfetch: downloader and web app for usn:// shares.
//
//	usnfetch serve [--host H] [--port P] [--data DIR]   web app (default http://127.0.0.1:7979)
//	usnfetch list  --link L [account flags]               files in a share
//	usnfetch fetch --link L --file F --out O [--verify]   download one file (resumable)
//	usnfetch set-password [--data DIR]                    web login password
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"usnfetch/internal/engine"
	"usnfetch/internal/nntp"
	"usnfetch/internal/share"
)

func logf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", time.Now().Format("[15:04:05]"), fmt.Sprintf(f, a...))
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

type netFlags struct {
	link, password, indexDir, server, ssl, user, pass string
	port, connections                                 int
}

func addCommon(fs *flag.FlagSet, n *netFlags) {
	fs.StringVar(&n.link, "link", "", "usn:// or usenet:// share link")
	fs.StringVar(&n.password, "password", "", "password for password-protected shares")
	fs.StringVar(&n.indexDir, "index-dir", "", "folder for the on-disk share indexes (default index_cache next to the program)")
	fs.StringVar(&n.server, "server", "", "NNTP server (or env USNFETCH_SERVER)")
	fs.IntVar(&n.port, "port", 0, "NNTP port (or env USNFETCH_PORT, default 563)")
	fs.StringVar(&n.ssl, "ssl", "", "1 or 0 (or env USNFETCH_SSL, default 1)")
	fs.StringVar(&n.user, "user", "", "NNTP username (or env USNFETCH_USER)")
	fs.StringVar(&n.pass, "pass", "", "NNTP password (or env USNFETCH_PASS)")
	fs.IntVar(&n.connections, "connections", 30, "connections")
}

func (n *netFlags) account() (*nntp.Account, error) {
	pick := func(cli, env, def string) string {
		if cli != "" {
			return cli
		}
		if v := os.Getenv(env); v != "" {
			return v
		}
		return def
	}
	portCLI := ""
	if n.port != 0 {
		portCLI = strconv.Itoa(n.port)
	}
	server := pick(n.server, "USNFETCH_SERVER", "")
	if server == "" {
		return nil, errors.New("no NNTP server: pass --server or set USNFETCH_SERVER")
	}
	port, _ := strconv.Atoi(pick(portCLI, "USNFETCH_PORT", "563"))
	ssl := strings.ToLower(pick(n.ssl, "USNFETCH_SSL", "1"))
	return &nntp.Account{Server: server, Port: port, SSL: ssl == "1" || ssl == "true" || ssl == "yes" || ssl == "on",
		User: pick(n.user, "USNFETCH_USER", ""), Password: pick(n.pass, "USNFETCH_PASS", ""),
		Connections: max(1, n.connections)}, nil
}

func (n *netFlags) load() (*share.Index, *engine.AccountSet, error) {
	if n.link == "" {
		return nil, nil, errors.New("pass --link")
	}
	a, err := n.account()
	if err != nil {
		return nil, nil, err
	}
	dir := n.indexDir
	if dir == "" {
		dir = filepath.Join(exeDir(), "index_cache")
	}
	set, _ := engine.NewAccountSet([]*nntp.Account{a})
	ix, err := share.LoadShare(n.link, share.LoadOpts{Accounts: set.All(), Password: n.password, IndexDir: dir, Log: func(s string) { logf("%s", s) }})
	return ix, set, err
}

func commas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func selectFile(files []share.FileEntry, sel string) (share.FileEntry, error) {
	var real []share.FileEntry
	for _, f := range files {
		if f.Downloadable() {
			real = append(real, f)
		}
	}
	if sel != "" {
		var hits []share.FileEntry
		for _, f := range real {
			p := strings.ReplaceAll(f.Path, "\\", "/")
			if f.Path == sel || f.FileID == sel || strings.HasSuffix(p, "/"+sel) || filepath.Base(p) == sel {
				hits = append(hits, f)
			}
		}
		switch len(hits) {
		case 1:
			return hits[0], nil
		case 0:
			return share.FileEntry{}, fmt.Errorf("no file matches %q; use `list`", sel)
		}
		var names []string
		for _, h := range hits[:min(5, len(hits))] {
			names = append(names, h.Path)
		}
		return share.FileEntry{}, fmt.Errorf("%d files match %q: %s", len(hits), sel, strings.Join(names, ", "))
	}
	if len(real) == 1 {
		return real[0], nil
	}
	return share.FileEntry{}, fmt.Errorf("share has %d files; pick one with --file (see `list`)", len(real))
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	var n netFlags
	addCommon(fs, &n)
	_ = fs.Parse(args)
	ix, _, err := n.load()
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	defer ix.Close()
	files, err := ix.Files()
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	sorted := append([]share.FileEntry(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for _, f := range sorted {
		what := fmt.Sprintf("%9s seg", commas(f.SegCount))
		if f.Packed() {
			what = "    packed"
		}
		fmt.Printf("%16s  %s  %s\n", commas(f.Size), what, f.Path)
	}
	return 0
}

func cmdFetch(args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	var n netFlags
	addCommon(fs, &n)
	file := fs.String("file", "", "path, basename or file_id of the file to fetch")
	out := fs.String("out", "", "output path (sparse, resumable)")
	verify := fs.Bool("verify", false, "SHA-256 the output against the index hash when complete")
	retries := fs.Int("missing-retries", 3, "retry passes for articles missing on every copy")
	asJSON := fs.Bool("json", false, "print a JSON summary on stdout")
	_ = fs.Parse(args)
	if *out == "" {
		logf("error: --out is required")
		return 1
	}
	ix, set, err := n.load()
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	defer ix.Close()
	files, err := ix.Files()
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	fe, err := selectFile(files, *file)
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	pool := engine.NewPool(set)
	defer pool.Close()
	emit := func(v any) {
		if *asJSON {
			b, _ := json.MarshalIndent(v, "", "  ")
			fmt.Println(string(b))
		}
	}
	if fe.SegCount == 0 { // packed or empty: fetched whole
		if fe.Pack == nil {
			if err := engine.WriteWhole(*out, nil); err != nil {
				logf("error: %v", err)
				return 1
			}
			emit(map[string]any{"file": fe.Path, "out": *out, "size": 0, "packed": false, "complete": true, "sha256_ok": true})
			logf("done: %s (0 B, empty) OK", fe.Path)
			return 0
		}
		pf, err := engine.NewPackFetcher(ix, []engine.Wanted{{File: fe, Out: *out}}, pool, func(s string) { logf("%s", s) })
		if err != nil {
			logf("error: %v", err)
			return 1
		}
		pf.MissingRetries = *retries
		r := pf.Run()
		missing := []string{}
		for p := range r.Missing {
			missing = append(missing, p)
		}
		emit(map[string]any{"file": fe.Path, "out": *out, "size": fe.Size, "packed": true, "complete": r.Complete,
			"sha256_ok": r.Complete, "missing": missing, "failed": r.Failed, "elapsed_s": r.ElapsedS})
		if r.Complete {
			logf("done: %s (%s B, packed) OK", fe.Path, commas(fe.Size))
			return 0
		}
		logf("done: %s FAILED %v %v", fe.Path, r.Failed, r.Missing)
		return 2
	}
	plan, err := ix.Plan(fe)
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	eng, err := engine.NewEngine(plan, *out, pool, func(s string) { logf("%s", s) })
	if err != nil {
		logf("error: %v", err)
		return 1
	}
	eng.MissingRetries, eng.Verify = *retries, *verify
	res := eng.Run()
	msg := fmt.Sprintf("done: %s/%s segments, avg %.2f MiB/s, %.1fs", commas(res.Done), commas(res.Segments), res.AvgMiBps, res.ElapsedS)
	if len(res.Missing) > 0 {
		msg += fmt.Sprintf(", missing %d", len(res.Missing))
	}
	if len(res.Failed) > 0 {
		msg += fmt.Sprintf(", failed %d", len(res.Failed))
	}
	if res.SHA256OK != nil {
		msg += map[bool]string{true: ", sha256 OK", false: ", sha256 MISMATCH"}[*res.SHA256OK]
	}
	logf("%s", msg)
	emit(res)
	if res.Complete && (res.SHA256OK == nil || *res.SHA256OK) {
		return 0
	}
	return 2
}

func usage() {
	fmt.Fprint(os.Stderr, `usnfetch -- downloader and web app for usn:// shares

  usnfetch serve [--host 127.0.0.1] [--port 7979] [--data DIR]   web app
  usnfetch tray  [same options as serve]                           web app with a tray icon (Windows)
  usnfetch list  --link LINK [account flags]                       files in a share
  usnfetch fetch --link LINK --file F --out PATH [--verify]        download one file
  usnfetch set-password [--data DIR]                               web login password

account flags: --server --port --ssl 1|0 --user --pass --connections N,
or env USNFETCH_SERVER / USNFETCH_PORT / USNFETCH_SSL / USNFETCH_USER / USNFETCH_PASS
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if p := os.Getenv("USNF_CPUPROFILE"); p != "" { // diagnostics: go tool pprof -top usnfetch.exe <file>
		if f, err := os.Create(p); err == nil {
			_ = pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}
	}
	var code int
	switch os.Args[1] {
	case "list":
		code = cmdList(os.Args[2:])
	case "fetch":
		code = cmdFetch(os.Args[2:])
	case "tray":
		code = cmdTray(os.Args[2:])
	case "serve":
		code = cmdServe(os.Args[2:])
	case "set-password":
		code = cmdSetPassword(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		code = 2
	}
	pprof.StopCPUProfile()
	os.Exit(code)
}
