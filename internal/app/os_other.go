//go:build !windows

package app

import (
	"errors"
	"os"
	"syscall"
)

func dpapi(data []byte, protect bool) ([]byte, error) {
	return nil, errors.New("DPAPI is Windows-only")
}

func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func statfs(path string) (*syscall.Statfs_t, error) {
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
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// DiskFree: bytes available to this user on the volume holding path.
func DiskFree(path string) (int64, error) {
	st, err := statfs(path)
	if err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// DiskTotal: size of that volume.
func DiskTotal(path string) (int64, error) {
	st, err := statfs(path)
	if err != nil {
		return 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), nil
}

// LocalDrive: drives are a Windows notion; elsewhere there's one tree.
func LocalDrive(root string) bool { return false }
