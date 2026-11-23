//go:build windows

package engine

import (
	"os"

	"golang.org/x/sys/windows"
)

// setSparse marks the file sparse so extending it (and writing out of order) never
// zero-fills the gap.
func setSparse(f *os.File) error {
	var ret uint32
	return windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &ret, nil)
}
