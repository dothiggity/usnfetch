//go:build tools

package main

import (
	_ "github.com/klauspost/compress/zstd"
	_ "github.com/ulikunitz/xz"
	_ "golang.org/x/crypto/pbkdf2"
	_ "golang.org/x/sys/windows"
	_ "modernc.org/sqlite"
)
