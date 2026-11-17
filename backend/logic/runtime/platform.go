package runtime

import (
	"os"
	"runtime"
)

// getenv 包一层，便于测试替换。
func getenv(key string) string {
	return os.Getenv(key)
}

// isWindowsHost 判断面板进程所在平台。
func isWindowsHost() bool {
	return runtime.GOOS == "windows"
}
