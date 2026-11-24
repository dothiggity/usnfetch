package main

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows notifications from usnfetch's own tray icon (a download finished or failed, the
// disk filled up): the icon's info text, which Windows shows as a toast under usnfetch's
// name. The tray library has no call for it, but its icon is an ordinary one: id 100 on a
// hidden window of class "SystrayClass" in this process.

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procShellNotifyIconW         = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
)

// notifyIconData is NOTIFYICONDATAW (976 bytes on 64-bit Windows).
type notifyIconData struct {
	Size             uint32
	Wnd              uintptr
	ID, Flags        uint32
	CallbackMessage  uint32
	Icon             uintptr
	Tip              [128]uint16
	State, StateMask uint32
	Info             [256]uint16
	TimeoutVersion   uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GuidItem         [16]byte
	BalloonIcon      uintptr
}

const (
	nimModify           = 0x1
	nifInfo             = 0x10
	niifInfo            = 0x1
	niifWarning         = 0x2
	niifRespectQuietTim = 0x80 // stay quiet in Focus assist / presentations
	trayIconID          = 100
)

var (
	findMu   sync.Mutex
	found    uintptr
	enumProc = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if int(pid) != os.Getpid() {
			return 1
		}
		var cls [64]uint16
		procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&cls[0])), uintptr(len(cls)))
		if windows.UTF16ToString(cls[:]) == "SystrayClass" {
			found = hwnd
			return 0 // stop
		}
		return 1
	})
)

// trayWindow: the hidden window behind usnfetch's tray icon (0 if there is none yet).
func trayWindow() uintptr {
	findMu.Lock()
	defer findMu.Unlock()
	found = 0
	procEnumWindows.Call(enumProc, 0)
	return found
}

// trayNotify shows a notification from the tray icon; warn: the warning style.
func trayNotify(title, body string, warn bool) {
	hwnd := trayWindow()
	if hwnd == 0 {
		return
	}
	n := notifyIconData{Wnd: hwnd, ID: trayIconID, Flags: nifInfo, InfoFlags: niifInfo | niifRespectQuietTim}
	if warn {
		n.InfoFlags = niifWarning | niifRespectQuietTim
	}
	n.Size = uint32(unsafe.Sizeof(n))
	copyUTF16(n.InfoTitle[:], title)
	copyUTF16(n.Info[:], body)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

// copyUTF16 copies s into dst, cut to fit with its terminating zero.
func copyUTF16(dst []uint16, s string) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(u) > len(dst) {
		u = append(u[:len(dst)-2], '…', 0)
	}
	copy(dst, u)
}
