//go:build !windows

package logic

import "syscall"

// hiddenWindowAttr is a no-op outside Windows.
func hiddenWindowAttr() *syscall.SysProcAttr {
	return nil
}

// diskFreeBytes reports free and total bytes for the volume holding path.
func diskFreeBytes(path string) (int64, int64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, false
	}
	blockSize := int64(stat.Bsize)
	return int64(stat.Bavail) * blockSize, int64(stat.Blocks) * blockSize, true
}
