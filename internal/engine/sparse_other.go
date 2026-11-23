//go:build !windows

package engine

import "os"

// setSparse: extending with ftruncate already leaves a hole on Linux/macOS filesystems.
func setSparse(f *os.File) error { return nil }
