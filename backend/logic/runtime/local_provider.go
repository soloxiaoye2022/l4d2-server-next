package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalClient 在面板进程内直接执行（Windows 原生部署 / 开发机 / 无 Agent 的容器）。
//
// 能力边界必须如实：
//   - docker：只要 docker CLI 可用就全支持
//   - systemd：只有宿主机真在跑 systemd 才支持
//   - bare / nohup：靠 pid 文件与进程存活判断，支持 stop，不支持 start
//   - screen / tmux / mcsm / lgsm：Windows 上不支持
//
// 所有命令都用 argv 数组，不经过 shell。
func (c *LocalClient) Run(request JobRequest, timeout time.Duration) JobResult {
	result := JobResult{
		JobID:        request.JobID,
		Action:       request.Action,
		StartedAt:    time.Now().UTC().Format(time.RFC3339),
		AgentVersion: "local",
	}
	data, err := localDispatch(request, timeout)
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		if unsupported, ok := err.(*unsupportedActionError); ok {
			result.Status = JobUnsupported
			result.Message = unsupported.Error()
			return result
		}
		result.Status = JobError
		result.Message = err.Error()
		return result
	}
	result.Status = JobSuccess
	result.Message = firstNonEmpty(stringFrom(data, "message"), "ok")
	delete(data, "message")
	if len(data) > 0 {
		result.Data = data
	}
	return result
}

type unsupportedActionError struct{ message string }

func (e *unsupportedActionError) Error() string { return e.message }

func unsupportedAction(format string, args ...any) error {
	return &unsupportedActionError{message: fmt.Sprintf(format, args...)}
}

func localDispatch(request JobRequest, timeout time.Duration) (map[string]any, error) {
	switch request.Action {
	case ActionProbe:
		return map[string]any{"message": "local alive", "agent_version": "local"}, nil
	case ActionDetect:
		return localDetect()
	}

	spec := LaunchSpec{}
	if request.Spec != nil {
		spec = *request.Spec
	}
	switch request.Kind {
	case KindDocker:
		return localDocker(request, spec, timeout)
	case KindSystemd, KindSystemdUser:
		return localSystemd(request, spec, timeout)
	case KindNohup, KindBare:
		return localProcess(request, spec)
	default:
		return nil, unsupportedAction("%s 运行时需要安装宿主机 Agent", request.Kind.Label())
	}
}

func localDocker(request JobRequest, spec LaunchSpec, timeout time.Duration) (map[string]any, error) {
	container := strings.TrimSpace(request.Target.ContainerName)
	if container == "" {
		return nil, fmt.Errorf("Docker 运行时缺少容器名")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, unsupportedAction("当前环境没有 docker")
	}
	switch request.Action {
	case ActionInspect:
		output, err := runArgv(timeout, "docker", "inspect", container, "--format", "{{json .State}}")
		if err != nil {
			return nil, err
		}
		var state struct {
			Running bool `json:"Running"`
			Pid     int  `json:"Pid"`
			Status  string
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &state); err != nil {
			return nil, fmt.Errorf("docker inspect 输出无法解析")
		}
		return map[string]any{
			"state":           map[bool]string{true: StateRunning, false: StateStopped}[state.Running],
			"pid":             state.Pid,
			"process_running": state.Running,
			"port_listening":  state.Running && spec.Port > 0,
			"reason":          strings.TrimSpace(state.Status),
		}, nil
	case ActionStart:
		_, err := runArgv(actionTimeout, "docker", "start", container)
		return map[string]any{"message": "容器已启动"}, err
	case ActionStop:
		_, err := runArgv(actionTimeout, "docker", "stop", container)
		return map[string]any{"message": "容器已停止"}, err
	case ActionRestart:
		_, err := runArgv(actionTimeout, "docker", "restart", container)
		return map[string]any{"message": "容器已重启"}, err
	case ActionLogs:
		tail := optionInt(request.Options, "tail", 200)
		output, err := runArgv(timeout, "docker", "logs", "--tail", strconv.Itoa(tail), container)
		if err != nil {
			return nil, err
		}
		return map[string]any{"lines": splitLines(output), "source": "docker logs"}, nil
	default:
		return nil, unsupportedAction("Docker 本地模式不支持动作 %s", request.Action)
	}
}

func localSystemd(request JobRequest, spec LaunchSpec, timeout time.Duration) (map[string]any, error) {
	if !HasSystemdOnHost() {
		return nil, unsupportedAction("当前环境没有 systemd，无法管理 systemd 服务")
	}
	unit := strings.TrimSpace(request.Target.UnitName)
	if unit == "" {
		return nil, fmt.Errorf("systemd 运行时缺少服务名")
	}
	args := []string{"systemctl"}
	if request.Kind == KindSystemdUser || request.Target.Scope == "user" {
		args = append(args, "--user")
	}
	switch request.Action {
	case ActionInspect:
		showArgs := append(append([]string{}, args[1:]...), "show", unit,
			"--property=ActiveState", "--property=MainPID")
		output, err := runArgv(timeout, "systemctl", showArgs...)
		if err != nil {
			return nil, err
		}
		state := ""
		pid := 0
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "ActiveState=") {
				state = strings.TrimPrefix(line, "ActiveState=")
			}
			if strings.HasPrefix(line, "MainPID=") {
				pid, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "MainPID=")))
			}
		}
		running := strings.TrimSpace(state) == "active" && pid > 0
		return map[string]any{
			"state":           map[bool]string{true: StateRunning, false: StateStopped}[running],
			"pid":             pid,
			"process_running": running,
			"port_listening":  running && spec.Port > 0,
			"reason":          "ActiveState=" + strings.TrimSpace(state),
		}, nil
	case ActionStart:
		_, err := runArgv(actionTimeout, "systemctl", append(args[1:], "start", unit)...)
		return map[string]any{"message": "已提交启动请求"}, err
	case ActionStop:
		_, err := runArgv(actionTimeout, "systemctl", append(args[1:], "stop", unit)...)
		return map[string]any{"message": "已提交停止请求"}, err
	case ActionRestart:
		_, err := runArgv(actionTimeout, "systemctl", append(args[1:], "restart", unit)...)
		return map[string]any{"message": "已提交重启请求"}, err
	case ActionLogs:
		tail := optionInt(request.Options, "tail", 200)
		output, err := runArgv(timeout, "journalctl", append(args[1:], "-u", unit, "-n", strconv.Itoa(tail), "--no-pager", "-o", "cat")...)
		if err != nil {
			return nil, err
		}
		return map[string]any{"lines": splitLines(output), "source": "journalctl"}, nil
	default:
		return nil, unsupportedAction("systemd 本地模式不支持动作 %s", request.Action)
	}
}

func localProcess(request JobRequest, spec LaunchSpec) (map[string]any, error) {
	switch request.Action {
	case ActionInspect:
		pid := 0
		if request.Kind == KindBare {
			if raw := strings.TrimSpace(request.Target.PidFile); raw != "" {
				pid, _ = strconv.Atoi(raw)
			}
		} else if spec.LogPath != "" {
			pid = readPIDFile(filepath.Join(filepath.Dir(spec.LogPath), "srcds.pid"))
		}
		running := pid > 0 && processAlive(pid)
		return map[string]any{
			"state":           map[bool]string{true: StateRunning, false: StateStopped}[running],
			"pid":             pid,
			"process_running": running,
			"port_listening":  running && spec.Port > 0,
		}, nil
	case ActionStop:
		pid := 0
		if raw := strings.TrimSpace(request.Target.PidFile); raw != "" {
			pid, _ = strconv.Atoi(raw)
		}
		if pid <= 0 {
			return nil, fmt.Errorf("未记录进程 PID，无法停止")
		}
		if err := killPID(pid); err != nil {
			return nil, err
		}
		return map[string]any{"message": fmt.Sprintf("已停止进程 %d", pid)}, nil
	case ActionLogs:
		if spec.LogPath == "" {
			return map[string]any{"lines": []string{}, "source": "无日志路径"}, nil
		}
		output, err := readTail(spec.LogPath, optionInt(request.Options, "tail", 200))
		if err != nil {
			return nil, err
		}
		return map[string]any{"lines": splitLines(output), "source": spec.LogPath}, nil
	default:
		return nil, unsupportedAction("裸进程/nohup 本地模式不支持动作 %s，请安装宿主机 Agent", request.Action)
	}
}

// localDetect 在本地模式下做最小扫描：只找 Docker 容器。
// 进程级扫描需要读宿主 /proc，容器内做不到，必须交给 Agent。
func localDetect() (map[string]any, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, unsupportedAction("当前环境没有 docker，本地模式无法扫描；请安装宿主机 Agent")
	}
	output, err := runArgv(detectTimeout, "docker", "ps", "-a", "--format", "{{.Names}}|{{.Image}}|{{.State}}")
	if err != nil {
		return nil, err
	}
	candidates := make([]map[string]any, 0, 8)
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) != 3 {
			continue
		}
		name, image, state := parts[0], strings.ToLower(parts[1]), parts[2]
		haystack := strings.ToLower(name + " " + image)
		if !strings.Contains(haystack, "l4d2") && !strings.Contains(haystack, "srcds") &&
			!strings.Contains(haystack, "left4dead") {
			continue
		}
		candidates = append(candidates, map[string]any{
			"kind":       string(KindDocker),
			"name":       "容器 " + name,
			"running":    strings.EqualFold(state, "running"),
			"target":     map[string]any{"container_name": name},
			"confidence": 0.9,
			"evidence":   []string{"docker ps 显示容器 " + name, "镜像 " + image},
		})
	}
	return map[string]any{
		"candidates": candidates,
		"warnings":   []string{"本地模式只能扫描 Docker 容器；systemd/screen/tmux/nohup 需要宿主机 Agent"},
	}, nil
}

// HasSystemdOnHost 供本地模式判断是否真的能执行 systemctl。
func HasSystemdOnHost() bool {
	if isWindowsHost() {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	if info, err := os.Stat("/run/systemd/system"); err == nil && info.IsDir() {
		return true
	}
	return false
}

func runArgv(timeout time.Duration, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Env = os.Environ()
	done := make(chan struct{})
	var output []byte
	var runErr error
	go func() {
		output, runErr = command.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		return "", fmt.Errorf("命令超时: %s", name)
	}
	if runErr != nil {
		text := strings.TrimSpace(string(output))
		if len(text) > 400 {
			text = text[:400]
		}
		return "", fmt.Errorf("%s 执行失败: %s", name, text)
	}
	return string(output), nil
}

func optionInt(options map[string]any, key string, fallback int) int {
	if options == nil {
		return fallback
	}
	switch value := options[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case string:
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(value, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func readPIDFile(path string) int {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(content)))
	if pid > 0 && !processAlive(pid) {
		return 0
	}
	return pid
}

func readTail(path string, count int) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取日志失败: %w", err)
	}
	lines := splitLines(string(content))
	if count > 0 && len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, "\n"), nil
}
