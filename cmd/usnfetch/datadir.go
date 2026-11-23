package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// defaultData is where usnfetch keeps its settings, job state, catalog and share indexes
// when --data isn't given:
//
//  1. $USNFETCH_DATA, when set;
//  2. the program's own folder for a portable copy: a file named "portable" (or portable.txt)
//     next to the program, or settings already there from a version that kept them beside it;
//  3. otherwise a per-user folder that stays put when the program is upgraded or moved:
//     %LOCALAPPDATA%\usnfetch on Windows, ~/Library/Application Support/usnfetch on macOS,
//     $XDG_DATA_HOME/usnfetch (~/.local/share/usnfetch) elsewhere.
func defaultData() string {
	if d := os.Getenv("USNFETCH_DATA"); d != "" {
		return d
	}
	exe := exeDir()
	for _, marker := range []string{"portable", "portable.txt", "usnfetch_gui.json"} {
		if _, err := os.Stat(filepath.Join(exe, marker)); err == nil {
			return exe
		}
	}
	if d := userDataDir(); d != "" {
		return filepath.Join(d, "usnfetch")
	}
	return exe
}

// userDataDir is the per-user folder for application data, or "" when there isn't one.
func userDataDir() string {
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d
		}
		d, _ := os.UserCacheDir() // %LocalAppData% too, found another way
		return d
	case "darwin":
		d, _ := os.UserConfigDir() // ~/Library/Application Support
		return d
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" && filepath.IsAbs(d) {
			return d
		}
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, ".local", "share")
		}
		return ""
	}
}
