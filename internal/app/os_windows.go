//go:build windows

package app

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dpapi(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	const uiForbidden = 0x1
	if protect {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, uiForbidden, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, uiForbidden, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// lockFile takes an exclusive lock on the first byte of f (the same byte range Python's
// msvcrt.locking uses), failing at once if another process holds it.
func lockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
}

// DiskFree: bytes available to this user on the volume holding path (or its nearest existing parent).
func DiskFree(path string) (int64, error) {
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepathDir(path)
		if parent == path {
			break
		}
		path = parent
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	return int64(free), nil
}

// DiskTotal: size of that volume.
func DiskTotal(path string) (int64, error) {
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepathDir(path)
		if parent == path {
			break
		}
		path = parent
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return 0, err
	}
	return int64(total), nil
}

// LocalDrive: a drive root ("D:\") that is a fixed disk in this computer, not a network, cloud,
// removable or optical one.
func LocalDrive(root string) bool {
	p, err := windows.UTF16PtrFromString(root)
	return err == nil && windows.GetDriveType(p) == windows.DRIVE_FIXED
}
