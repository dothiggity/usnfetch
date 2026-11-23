//go:build !windows

package main

func doubleClicked() bool { return false }

func cmdTray(args []string) int {
	logf("the tray icon is for Windows; here run: usnfetch serve (the README shows a systemd unit for running it as a service)")
	return 2
}
