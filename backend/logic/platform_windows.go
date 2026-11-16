//go:build windows

package logic

import (
	"syscall"
	"unsafe"
)

// hiddenWindowAttr keeps netstat/tasklist from flashing a console window when
// the manager runs as a background process.
func hiddenWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// diskFreeBytes reports free and total bytes for the volume holding path.
// Linux uses syscall.Statfs in the sibling file.
func diskFreeBytes(path string) (int64, int64, bool) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeAvailable, total, totalFree uint64
	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return 0, 0, false
	}
	return int64(freeAvailable), int64(total), true
}
