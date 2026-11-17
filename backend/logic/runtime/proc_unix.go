//go:build !windows

package runtime

import (
	"errors"
	"os"
	"syscall"
)

// processAlive 判断 PID 是否仍然存活。
// Unix 用 Signal(0) 探测：EPERM 说明进程存在但没权限发信号，仍算存活。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return isPermissionError(err)
}

// killPID 先 TERM 后 KILL，与 Agent 侧策略保持一致。
func killPID(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !isProcessGone(err) {
		return err
	}
	return nil
}

func isPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM)
}

func isProcessGone(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}
