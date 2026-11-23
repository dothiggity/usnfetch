//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"usnfetch/internal/app"
)

// usnfetch tray: the web app with an icon in the notification area -- open, status,
// pause / resume, start with Windows, quit. It never shows a console window: started from
// Explorer or at sign-in it drops the console Windows made for it, started from a terminal
// it carries on as a background copy of itself and gives the terminal back.

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcess = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole       = kernel32.NewProc("FreeConsole")
	procMessageBox        = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
)

func consoleProcesses() int {
	var ids [16]uint32
	n, _, _ := procGetConsoleProcess.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return int(n)
}

func messageBox(text string, icon uintptr) { // 0x10 error, 0x40 information
	t, _ := windows.UTF16PtrFromString("usnfetch")
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), icon)
}

func openBrowser(url string) {
	verb, _ := windows.UTF16PtrFromString("open")
	u, _ := windows.UTF16PtrFromString(url)
	_ = windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}

// ---- start with Windows (HKCU Run: no administrator rights, runs as you, so the
// passwords sealed for your account still open) ----

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func autostartCommand(o serveOpts) string {
	exe, _ := os.Executable()
	parts := []string{syscall.EscapeArg(exe), "tray", "--no-browser", "--data", syscall.EscapeArg(o.data)}
	if o.dev != "" {
		parts = append(parts, "--web", syscall.EscapeArg(o.dev))
	}
	if o.host != "" {
		parts = append(parts, "--host", o.host)
	}
	if o.port != 0 {
		parts = append(parts, "--port", strconv.Itoa(o.port))
	}
	return strings.Join(parts, " ")
}

func autostartOn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("usnfetch")
	return err == nil
}

func setAutostart(on bool, cmd string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		return k.SetStringValue("usnfetch", cmd)
	}
	if err := k.DeleteValue("usnfetch"); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// openLog points stderr (the app's log, and panics) at <data>/usnfetch.log.
func openLog(dir string) {
	p := filepath.Join(dir, "usnfetch.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 20<<20 {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	os.Stderr = f
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
	fmt.Fprintf(f, "\n---- %s usnfetch %s tray\n", time.Now().Format("2006-01-02 15:04:05"), Version)
}

func cmdTray(args []string) int {
	browser := true
	var rest []string
	for _, a := range args {
		if a == "--no-browser" {
			browser = false
		} else {
			rest = append(rest, a)
		}
	}
	o := parseServe("tray", rest)
	switch n := consoleProcesses(); {
	case n > 1 && os.Getenv("USNFETCH_TRAY_CHILD") == "": // from a terminal: go on in the background
		exe, _ := os.Executable()
		cmd := exec.Command(exe, append([]string{"tray"}, args...)...)
		cmd.Env = append(os.Environ(), "USNFETCH_TRAY_CHILD=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
		if err := cmd.Start(); err != nil {
			logf("error: %v", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "usnfetch is running in the notification area; log: %s\n", filepath.Join(o.data, "usnfetch.log"))
		return 0
	case n == 1: // a console of its own (started from Explorer or at sign-in): drop it
		procFreeConsole.Call()
	}
	_ = os.MkdirAll(o.data, 0o755)
	openLog(o.data)
	r, err := startServe(o)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") || strings.Contains(strings.ToLower(err.Error()), "lock") {
			if s, e := app.LoadSettings(filepath.Join(o.data, "usnfetch_gui.json")); e == nil && browser {
				port := s.Web.Port
				if o.port != 0 {
					port = o.port
				}
				openBrowser(fmt.Sprintf("http://localhost:%d/", port))
				return 0
			}
		}
		messageBox("usnfetch could not start:\n\n"+err.Error(), 0x10)
		return 1
	}
	if browser {
		openBrowser(r.URL)
	}
	var restart atomic.Bool
	r.Web.Restart = func() { restart.Store(true); systray.Quit() }
	systray.Run(func() { trayMenu(r, o) }, func() {})
	r.Stop() // checkpoints saved, port and data folder free again
	if restart.Load() {
		if err := relaunch(args); err != nil {
			messageBox("usnfetch could not start again:\n\n"+err.Error(), 0x10)
			return 1
		}
	}
	return 0
}

// relaunch starts a new copy of usnfetch with the same options, detached from this one
// and, where allowed, from whatever started this one (so it outlives it).
func relaunch(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	a := append([]string{"tray"}, args...)
	quiet := false
	for _, x := range args {
		quiet = quiet || x == "--no-browser"
	}
	if !quiet {
		a = append(a, "--no-browser") // the browser is already open
	}
	fmt.Fprintf(os.Stderr, "%s restarting\n", time.Now().Format("[15:04:05]"))
	const detached, newGroup, breakaway = 0x00000008, 0x00000200, 0x01000000
	for _, flags := range []uint32{detached | newGroup | breakaway, detached | newGroup} {
		cmd := exec.Command(exe, a...)
		cmd.Env = append(os.Environ(), "USNFETCH_TRAY_CHILD=1")
		cmd.Dir, _ = os.Getwd()
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err = cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
	}
	return err
}

// The tray icons, built from assets/icons by assets/make_icons.py.
var (
	//go:embed icons/idle.ico
	iconIdle []byte // nothing downloading
	//go:embed icons/busy.ico
	iconBusy []byte // a job is loading, downloading or extracting
	//go:embed icons/paused.ico
	iconPaused []byte // downloads paused
)

func mbps(bps float64) string { return strconv.FormatFloat(bps/1e6, 'f', 1, 64) + " MB/s" }

func trayMenu(r *running, o serveOpts) {
	icons := map[string][]byte{"idle": iconIdle, "busy": iconBusy, "paused": iconPaused}
	shown := "idle"
	systray.SetIcon(iconIdle)
	systray.SetTooltip("usnfetch")
	open := systray.AddMenuItem("Open usnfetch", "Open the web app in your browser")
	status := systray.AddMenuItem("Starting…", "")
	status.Disable()
	systray.AddSeparator()
	pause := systray.AddMenuItem("Pause downloads", "Stop downloading until you resume (jobs keep their progress)")
	later := systray.AddMenuItem("Pause for", "")
	p30 := later.AddSubMenuItem("30 minutes", "")
	p60 := later.AddSubMenuItem("1 hour", "")
	p180 := later.AddSubMenuItem("3 hours", "")
	systray.AddSeparator()
	auto := systray.AddMenuItemCheckbox("Start with Windows", "Start usnfetch here when you sign in", autostartOn())
	restartItem := systray.AddMenuItem("Restart usnfetch", "Stop and start again (downloads carry on where they were)")
	quit := systray.AddMenuItem("Quit", "Stop usnfetch (downloads resume next time)")

	wasPaused := false
	refresh := func() {
		th := r.M.Throttle()
		q := r.M.Queue()
		var rate float64
		busy := false
		for _, j := range q {
			rate += j.Rate
			busy = busy || j.State == "planning" || j.State == "downloading" || j.State == "extracting"
		}
		state := "idle"
		switch {
		case th.Paused:
			state = "paused"
		case busy:
			state = "busy"
		}
		if state != shown {
			shown = state
			systray.SetIcon(icons[state])
		}
		var text string
		switch {
		case th.Paused:
			text = "Paused"
			if th.Reason != "paused" {
				text = strings.ToUpper(th.Reason[:1]) + th.Reason[1:]
			}
		case rate > 0:
			text = fmt.Sprintf("Downloading %s · %d in queue", mbps(rate), len(q))
		case len(q) > 0:
			text = fmt.Sprintf("%d in queue", len(q))
		default:
			text = "Idle"
		}
		if !th.Paused && th.Limit > 0 {
			text += " · limit " + mbps(th.Limit)
		}
		status.SetTitle(text)
		systray.SetTooltip("usnfetch: " + text)
		if th.Paused != wasPaused {
			wasPaused = th.Paused
			if th.Paused {
				pause.SetTitle("Resume downloads")
			} else {
				pause.SetTitle("Pause downloads")
			}
		}
	}
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-open.ClickedCh:
				openBrowser(r.URL)
			case <-pause.ClickedCh:
				if r.M.Throttle().Paused {
					r.M.ResumeDownloads()
				} else {
					r.M.PauseDownloads(0)
				}
			case <-p30.ClickedCh:
				r.M.PauseDownloads(30)
			case <-p60.ClickedCh:
				r.M.PauseDownloads(60)
			case <-p180.ClickedCh:
				r.M.PauseDownloads(180)
			case <-auto.ClickedCh:
				on := !auto.Checked()
				if err := setAutostart(on, autostartCommand(o)); err != nil {
					messageBox("Could not change the sign-in start:\n\n"+err.Error(), 0x10)
				} else if on {
					auto.Check()
				} else {
					auto.Uncheck()
				}
				continue
			case <-restartItem.ClickedCh:
				r.Web.Restart()
				return
			case <-quit.ClickedCh:
				systray.Quit()
				return
			case err := <-r.Done:
				messageBox(fmt.Sprintf("The usnfetch web server stopped: %v", err), 0x10)
				systray.Quit()
				return
			case <-t.C:
			}
			refresh()
		}
	}()
	refresh()
}
