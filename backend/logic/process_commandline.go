package logic

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"l4d2-manager-next/consts"
)

// 包装脚本场景：unit 里的 ExecStart 指向 start_server.sh 这类脚本，
// 真正的 srcds 参数在脚本内部拼装，从 unit 文件里永远读不到。
// 面板通过宿主进程表取实际命令行，才能显示真实生效的启动参数。

const (
	hostProcDirEnv  = "L4D2_HOST_PROC_DIR"
	defaultHostProc = "/host/proc"
	localProcDir    = "/proc"
)

// HostProcRoot 返回可以读取宿主进程信息的目录。
// 容器部署挂载 /proc 到 /host/proc；原生部署直接用 /proc。
func HostProcRoot() string {
	if value := strings.TrimSpace(os.Getenv(hostProcDirEnv)); value != "" {
		return value
	}
	if info, err := os.Stat(defaultHostProc); err == nil && info.IsDir() {
		return defaultHostProc
	}
	return localProcDir
}

// ProcessCommandLine 描述一个正在运行的游戏服务端进程。
type ProcessCommandLine struct {
	PID  int      `json:"pid"`
	Exec string   `json:"exec"`
	Args []string `json:"args"`
	// Command 是完整命令行，前端直接拿它解析启动选项。
	Command string `json:"command"`
	// LaunchExec 是可用于写入 ExecStart 的启动器路径，已解析为绝对路径。
	LaunchExec string `json:"launch_exec"`
	// ViaCgroup 表示该进程是通过 cgroup 归属确认属于目标 unit 的。
	ViaCgroup bool `json:"via_cgroup"`
}

// readProcessCommandLine 解析 /proc/<pid>/cmdline（NUL 分隔）。
func readProcessCommandLine(procRoot string, pid int) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, os.ErrNotExist
	}
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			args = append(args, part)
		}
	}
	if len(args) == 0 {
		return nil, os.ErrNotExist
	}
	return args, nil
}

// processBelongsToUnit 通过 cgroup 归属判断进程是否属于指定 systemd unit。
// cgroup v2 形如 `0::/system.slice/l4d2.service`，v1 分多行 controller 名。
func processBelongsToUnit(procRoot string, pid int, unitName string) bool {
	raw, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "/"+unitName)
}

// isGameServerProcess 判断命令行是否是游戏服务端。
// 认 srcds 开头的可执行文件（srcds_run 包装与 srcds_linux 本体都会命中），
// 避免把面板自身、shell 会话等算进来。
func isGameServerProcess(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(filepath.Base(arg)), "srcds") {
			return true
		}
	}
	return false
}

// resolveProcessExec 把 ./srcds_linux 这类相对路径解析成绝对路径。
// 容器里读的是宿主 /host/proc/<pid>/cmdline，相对路径要按进程的 cwd 还原。
func resolveProcessExec(procRoot string, pid int, exec string) string {
	if filepath.IsAbs(exec) {
		return exec
	}
	if link, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "cwd")); err == nil {
		return filepath.Join(link, exec)
	}
	// 容器里读宿主 cwd 可能失败，退回从游戏目录推断。
	for _, dir := range []string{consts.GamePath, filepath.Dir(consts.GamePath)} {
		candidate := filepath.Join(dir, exec)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return exec
}

// FindServiceProcess 找出目标 unit 正在运行的游戏服务端命令行。
//
// 优先按 cgroup 精确归属；取不到时退回"命令行像游戏服"的兜底匹配，
// 但只有唯一候选才采纳，避免多实例部署下取错进程。
func FindServiceProcess(unitName string) (ProcessCommandLine, bool) {
	procRoot := HostProcRoot()
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return ProcessCommandLine{}, false
	}

	type candidate struct {
		line  ProcessCommandLine
		exact bool
	}
	best := candidate{}
	fallbackCount := 0
	fallback := ProcessCommandLine{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		args, err := readProcessCommandLine(procRoot, pid)
		if err != nil || !isGameServerProcess(args) {
			continue
		}
		line := ProcessCommandLine{
			PID:        pid,
			Exec:       args[0],
			Args:       args[1:],
			Command:    strings.Join(args, " "),
			LaunchExec: resolveProcessExec(procRoot, pid, args[0]),
		}
		if unitName != "" && processBelongsToUnit(procRoot, pid, unitName) {
			line.ViaCgroup = true
			// 同 unit 下 srcds_run 的 shell 包装和真正的 srcds_linux 都会命中，
			// 取参数最长的那个，即最内层的真实服务端进程。
			if !best.exact || len(line.Args) > len(best.line.Args) {
				best = candidate{line: line, exact: true}
			}
			continue
		}
		fallbackCount++
		if len(line.Args) > len(fallback.Args) {
			fallback = line
		}
	}

	if best.exact {
		return best.line, true
	}
	if fallbackCount == 1 {
		return fallback, true
	}
	return ProcessCommandLine{}, false
}

// isWrapperExecStart 判断 ExecStart 是否指向包装脚本而不是游戏服务端本体。
// unit 里写 /data/l4d2-server/start_server.sh 时，参数都在脚本内部。
func isWrapperExecStart(execStart string) bool {
	tokens, err := splitStartupCommand(strings.TrimSpace(execStart))
	if err != nil || len(tokens) == 0 {
		return false
	}
	base := strings.ToLower(filepath.Base(tokens[0]))
	return !strings.HasPrefix(base, "srcds")
}
