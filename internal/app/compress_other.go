//go:build !windows

package app

import "errors"

// Index cache compression is Windows-only (its file system compresses transparently).
const canCompress = false

func diskSize(path string, size int64) int64 { return size }

func compressFile(path string) error { return errors.ErrUnsupported }
