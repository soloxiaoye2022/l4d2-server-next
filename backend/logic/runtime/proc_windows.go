//go:build windows

package runtime

import (
	"errors"
	"os"
)

// processAlive 在 Windows 上用 FindProcess + 打开句柄判断。
// os.FindProcess 在 Windows 会真的打开进程句柄，句柄失败说明进程不存在。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process.Release()
	return true
}

// killPID 在 Windows 上直接 TerminateProcess。
func killPID(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	return nil
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission)
}

func isProcessGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}
