package app

import (
	"syscall"
	"unsafe"
)

const canCompress = true

var procGetCompressedFileSizeW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// diskSize: the space a file of the given size takes on disk (less once compressed).
func diskSize(path string, size int64) int64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return size
	}
	var high uint32
	low, _, callErr := procGetCompressedFileSizeW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&high)))
	if uint32(low) == 0xFFFFFFFF && callErr != syscall.Errno(0) {
		return size
	}
	return int64(high)<<32 | int64(uint32(low))
}

// FSCTL_SET_EXTERNAL_BACKING: CTL_CODE(FILE_DEVICE_FILE_SYSTEM, 195, METHOD_BUFFERED, FILE_SPECIAL_ACCESS)
const fsctlSetExternalBacking = 0x9030C

// wofFileCompression: WOF_EXTERNAL_INFO (version 1, provider 2 = file) followed by
// FILE_PROVIDER_EXTERNAL_INFO_V1 (version 1, algorithm 3 = XPRESS16K, no flags).
type wofFileCompression struct {
	wofVersion, wofProvider       uint32
	fileVersion, algorithm, flags uint32
}

// compressFile compresses a file with Windows' own file compression (XPRESS16K). Readers
// see the same bytes; a write decompresses the file again.
func compressFile(path string) error {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	in := wofFileCompression{wofVersion: 1, wofProvider: 2, fileVersion: 1, algorithm: 3}
	var n uint32
	// fails on file systems without it (FAT32, exFAT, network drives) and with
	// ERROR_COMPRESSION_NOT_BENEFICIAL when the data would not shrink
	return syscall.DeviceIoControl(h, fsctlSetExternalBacking, (*byte)(unsafe.Pointer(&in)), uint32(unsafe.Sizeof(in)), nil, 0, &n, nil)
}
