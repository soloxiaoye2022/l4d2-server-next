package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultGameServerPort = 27015
const (
	startupCommentsBegin = "# L4D2 Manager startup options: begin"
	startupCommentsEnd   = "# L4D2 Manager startup options: end"
)

var systemdServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+\.service$`)

// startupFormatOverride lets tests pin the startup file format regardless of
// the host OS. Production code leaves it nil and follows the real platform, but
// the systemd parser tests must run on Windows CI too.
var startupFormatOverride *bool

// usesBatchStartup reports whether the startup definition is a batch launcher
// instead of a systemd unit.
func usesBatchStartup() bool {
	if startupFormatOverride != nil {
		return *startupFormatOverride
	}
	return IsWindows()
}

type ServerStartupConfig struct {
	Available               bool     `json:"available"`
	ServiceName             string   `json:"service_name"`
	WorkingDirectory        string   `json:"working_directory"`
	ExecStart               string   `json:"exec_start"`
	Port                    int      `json:"port"`
	ReloadMode              string   `json:"reload_mode"`
	ReloadStatus            string   `json:"reload_status"`
	ReloadMessage           string   `json:"reload_message,omitempty"`
	ReloadedAt              string   `json:"reloaded_at,omitempty"`
	RestartCommandAvailable bool     `json:"restart_command_available"`
	StartupComments         []string `json:"startup_comments"`
}

type UpdateServerStartupRequest struct {
	WorkingDirectory string   `json:"working_directory"`
	ExecStart        string   `json:"exec_start"`
	StartupComments  []string `json:"startup_comments"`
}

type ServerStartupFileState struct {
	Available bool   `json:"available"`
	Port      int    `json:"port"`
	Service   string `json:"service_name"`
}

type startupApplyResult struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	UnitSHA256   string `json:"unit_sha256"`
	AppliedAt    string `json:"applied_at"`
	TargetPath   string `json:"target_path"`
	DaemonReload bool   `json:"daemon_reload"`
}

func serverStartupUnitPath() string {
	for _, name := range []string{"L4D2_STARTUP_UNIT_PATH", "L4D2_SERVER_UNIT_FILE"} {
		if path := strings.TrimSpace(os.Getenv(name)); path != "" {
			return path
		}
	}
	if IsWindows() {
		// Windows has no systemd unit. The startup source is the batch launcher,
		// so point at it instead of a Unix path that can never exist.
		return filepath.Join(DefaultControlDir(), "l4d2.bat")
	}
	for _, path := range []string{
		"/control/l4d2.service",
		"/etc/systemd/system/l4d2.service",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "/control/l4d2.service"
}

func serverStartupServiceName() string {
	if name := strings.TrimSpace(os.Getenv("L4D2_STARTUP_SERVICE_NAME")); name != "" {
		return name
	}
	return "l4d2.service"
}

func serverStartupReloadTrigger() string {
	if path := strings.TrimSpace(os.Getenv("L4D2_STARTUP_RELOAD_TRIGGER")); path != "" {
		return path
	}
	if IsWindows() {
		// No systemd reload channel on Windows; returning empty makes
		// triggerStartupReload report "restart required" instead of writing
		// into a directory that does not exist.
		return ""
	}
	return "/control/systemd-reload"
}

func serverStartupResultPath() string {
	if path := strings.TrimSpace(os.Getenv("L4D2_STARTUP_RESULT_PATH")); path != "" {
		return path
	}
	if IsWindows() {
		return filepath.Join(DefaultControlDir(), "l4d2-startup-result.json")
	}
	return "/control/l4d2-startup-result.json"
}

func serverRestartCommandAvailable() bool {
	return strings.TrimSpace(os.Getenv("L4D2_RESTART_CMD")) != "" ||
		strings.TrimSpace(os.Getenv("L4D2_CONTAINER_NAME")) != "" ||
		os.Getenv("L4D2_RESTART_BY_RCON") == "true"
}

func GetServerStartupConfig() (ServerStartupConfig, error) {
	cfg := ServerStartupConfig{
		ServiceName:             serverStartupServiceName(),
		Port:                    DefaultGameServerPort,
		ReloadMode:              "filesystem-watch",
		RestartCommandAvailable: serverRestartCommandAvailable(),
	}
	if !systemdServiceNamePattern.MatchString(cfg.ServiceName) {
		return cfg, fmt.Errorf("systemd 服务名无效")
	}

	unitPath := serverStartupUnitPath()
	content, err := os.ReadFile(unitPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg.ReloadStatus = "unavailable"
			return cfg, nil
		}
		return cfg, fmt.Errorf("读取启动项文件失败: %w", err)
	}

	var comments []string
	var workingDirectory, execStart string
	if usesBatchStartup() {
		comments, workingDirectory, execStart, err = parseBatchStartup(string(content))
	} else {
		comments, workingDirectory, execStart, err = parseSystemdStartupUnit(string(content))
	}
	if err != nil {
		return cfg, err
	}

	cfg.Available = true
	cfg.WorkingDirectory = workingDirectory
	cfg.ExecStart = execStart
	cfg.StartupComments = comments
	cfg.Port = ParseGameServerPort(execStart)
	cfg.ReloadStatus, cfg.ReloadMessage, cfg.ReloadedAt = readStartupApplyResult(string(content))
	if cfg.ReloadStatus == "" {
		cfg.ReloadStatus = "ready"
	}
	return cfg, nil
}

func GetServerEndpoint() ServerStartupFileState {
	state := ServerStartupFileState{
		Port:    DefaultGameServerPort,
		Service: serverStartupServiceName(),
	}
	content, err := os.ReadFile(serverStartupUnitPath())
	if err != nil {
		if envPort := parsePort(os.Getenv("L4D2_PORT")); envPort > 0 {
			state.Port = envPort
		}
		return state
	}
	var execStart string
	if usesBatchStartup() {
		_, _, execStart, err = parseBatchStartup(string(content))
	} else {
		_, _, execStart, err = parseSystemdStartupUnit(string(content))
	}
	if err != nil {
		return state
	}
	state.Available = true
	state.Port = ParseGameServerPort(execStart)
	return state
}

func UpdateServerStartup(request UpdateServerStartupRequest) (ServerStartupConfig, error) {
	workingDirectory, err := normalizeSystemdWorkingDirectory(request.WorkingDirectory)
	if err != nil {
		return ServerStartupConfig{}, err
	}
	execStart, err := normalizeSystemdExecStart(request.ExecStart)
	if err != nil {
		return ServerStartupConfig{}, err
	}
	comments, err := normalizeStartupComments(request.StartupComments)
	if err != nil {
		return ServerStartupConfig{}, err
	}

	unitPath := serverStartupUnitPath()
	content, err := os.ReadFile(unitPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return ServerStartupConfig{}, fmt.Errorf("读取启动项文件失败: %w", err)
		}
		// Windows has no packaged unit file, so the first save creates the
		// launcher. Linux keeps the original "cannot edit a missing unit"
		// behaviour because the host installs that file.
		if !usesBatchStartup() {
			return ServerStartupConfig{}, fmt.Errorf("启动项文件不存在，无法编辑")
		}
		content = []byte(batchStartupTemplate(workingDirectory, execStart, comments))
	}

	var updated string
	if usesBatchStartup() {
		updated, err = updateBatchStartup(string(content), workingDirectory, execStart, comments)
	} else {
		updated, err = updateSystemdStartupUnit(string(content), workingDirectory, execStart, comments)
	}
	if err != nil {
		return ServerStartupConfig{}, err
	}
	if err := writeStartupUnitAtomic(unitPath, []byte(updated)); err != nil {
		return ServerStartupConfig{}, err
	}

	mode, applyMessage, err := triggerStartupReload()
	if err != nil {
		return ServerStartupConfig{}, err
	}

	cfg, err := GetServerStartupConfig()
	if err != nil {
		return ServerStartupConfig{}, err
	}
	if mode == "command" {
		cfg.ReloadMode = "command"
	}
	if applyMessage != "" && cfg.ReloadMessage == "" {
		cfg.ReloadMessage = applyMessage
	}
	return cfg, nil
}

func parseSystemdStartupUnit(content string) ([]string, string, string, error) {
	lines := systemdUnitLines(content)
	workingDirectory := ""
	execStarts := make([]string, 0, 1)
	comments := make([]string, 0)
	inCommentBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == startupCommentsBegin:
			inCommentBlock = true
			comments = comments[:0]
		case trimmed == startupCommentsEnd:
			inCommentBlock = false
		case inCommentBlock && strings.HasPrefix(trimmed, "#"):
			comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if comment != "" {
				comments = append(comments, comment)
			}
		case strings.HasPrefix(trimmed, "WorkingDirectory="):
			if workingDirectory != "" {
				return nil, "", "", fmt.Errorf("启动项包含多个 WorkingDirectory，暂不支持自动编辑")
			}
			workingDirectory = strings.TrimSpace(strings.TrimPrefix(trimmed, "WorkingDirectory="))
		case strings.HasPrefix(trimmed, "ExecStart="):
			execStart := strings.TrimSpace(strings.TrimPrefix(trimmed, "ExecStart="))
			if execStart != "" {
				execStarts = append(execStarts, execStart)
			}
		}
	}
	if len(execStarts) == 0 {
		return nil, "", "", fmt.Errorf("启动项中没有 ExecStart")
	}
	if len(execStarts) > 1 {
		return nil, "", "", fmt.Errorf("启动项包含多个 ExecStart，暂不支持自动编辑")
	}
	return comments, workingDirectory, execStarts[0], nil
}

func updateSystemdStartupUnit(content, workingDirectory, execStart string, comments []string) (string, error) {
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	inService := false
	serviceSeen := false
	workingReplaced := false
	execReplaced := false
	execCount := 0

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inService = trimmed == "[Service]"
			if inService {
				serviceSeen = true
			}
			continue
		}
		if !inService {
			continue
		}
		if strings.HasPrefix(trimmed, "WorkingDirectory=") {
			if workingReplaced {
				return "", fmt.Errorf("启动项包含多个 WorkingDirectory，暂不支持自动编辑")
			}
			lines[index] = "WorkingDirectory=" + workingDirectory
			workingReplaced = true
			continue
		}
		if strings.HasPrefix(trimmed, "ExecStart=") {
			execCount++
			if execCount > 1 {
				return "", fmt.Errorf("启动项包含多个 ExecStart，暂不支持自动编辑")
			}
			lines[index] = "ExecStart=" + execStart
			execReplaced = true
		}
	}

	if !serviceSeen {
		return "", fmt.Errorf("启动项缺少 [Service] 段")
	}
	if execCount == 0 {
		return "", fmt.Errorf("启动项中没有 ExecStart")
	}
	if !workingReplaced {
		insertAt := -1
		for index, line := range lines {
			if strings.TrimSpace(line) == "[Service]" {
				insertAt = index + 1
				break
			}
		}
		if insertAt < 0 {
			return "", fmt.Errorf("启动项缺少 [Service] 段")
		}
		lines = append(lines, "")
		copy(lines[insertAt+1:], lines[insertAt:])
		lines[insertAt] = "WorkingDirectory=" + workingDirectory
	}
	if !execReplaced {
		return "", fmt.Errorf("启动项中没有 ExecStart")
	}

	lines = replaceStartupComments(lines, comments)
	result := strings.Join(lines, "\n")
	if newline == "\r\n" {
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}
	return result, nil
}

func replaceStartupComments(lines, comments []string) []string {
	filtered := make([]string, 0, len(lines)+len(comments)+2)
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == startupCommentsBegin {
			inBlock = true
			continue
		}
		if inBlock {
			if trimmed == startupCommentsEnd {
				inBlock = false
			}
			continue
		}
		filtered = append(filtered, line)
	}

	execIndex := -1
	inService := false
	for index, line := range filtered {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inService = trimmed == "[Service]"
			continue
		}
		if inService && strings.HasPrefix(trimmed, "ExecStart=") {
			execIndex = index
			break
		}
	}
	if execIndex < 0 || len(comments) == 0 {
		return filtered
	}

	block := make([]string, 0, len(comments)+2)
	block = append(block, startupCommentsBegin)
	for _, comment := range comments {
		block = append(block, "# "+comment)
	}
	block = append(block, startupCommentsEnd)

	result := make([]string, 0, len(filtered)+len(block))
	result = append(result, filtered[:execIndex]...)
	result = append(result, block...)
	result = append(result, filtered[execIndex:]...)
	return result
}

func normalizeStartupComments(comments []string) ([]string, error) {
	if len(comments) > 200 {
		return nil, fmt.Errorf("启动项注释不能超过 200 行")
	}
	normalized := make([]string, 0, len(comments))
	for _, comment := range comments {
		comment = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#"))
		if comment == "" {
			continue
		}
		if utf8.RuneCountInString(comment) > 512 {
			return nil, fmt.Errorf("单条启动项注释不能超过 512 个字符")
		}
		if strings.ContainsAny(comment, "\r\n\x00") {
			return nil, fmt.Errorf("启动项注释不能包含换行或空字符")
		}
		normalized = append(normalized, comment)
	}
	return normalized, nil
}

func normalizeSystemdWorkingDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("工作目录不能为空")
	}
	if !isAbsolutePathForPlatform(value) || !isCleanPathForStyle(value) {
		if IsWindows() {
			return "", fmt.Errorf("工作目录必须是规范化的绝对路径，例如 D:\\SteamCMD\\l4d2")
		}
		return "", fmt.Errorf("工作目录必须是规范化的绝对路径")
	}
	if utf8.RuneCountInString(value) > 1024 {
		return "", fmt.Errorf("工作目录不能超过 1024 个字符")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("工作目录不能包含换行或空字符")
	}
	return value, nil
}

// isCleanPathForStyle checks that the path has no redundant separators, "." or
// ".." segments. The path style decides the normaliser: POSIX paths keep "/"
// even on Windows hosts, because a container-managed unit file may legitimately
// carry Linux paths while the panel runs on a Windows workstation.
func isCleanPathForStyle(value string) bool {
	if strings.HasPrefix(value, "/") {
		return path.Clean(value) == value
	}
	return filepath.Clean(value) == value
}

// dirOfPathStyle returns the parent directory without changing the path style.
// filepath.Dir rewrites "/home/ubuntu/steamcmd/l4d2" to "\home\ubuntu\steamcmd"
// on Windows, which then fails absolute-path validation.
func dirOfPathStyle(value string) string {
	if strings.HasPrefix(value, "/") {
		return path.Dir(value)
	}
	return filepath.Dir(value)
}

// joinPathStyle joins path segments using the separator style of the base path,
// so a POSIX install directory stays POSIX even when the panel runs on Windows.
func joinPathStyle(base string, elems ...string) string {
	if strings.HasPrefix(base, "/") {
		parts := append([]string{base}, elems...)
		return path.Join(parts...)
	}
	parts := append([]string{base}, elems...)
	return filepath.Join(parts...)
}

// isAbsolutePathForPlatform accepts POSIX paths and Windows drive/UNC paths on
// both platforms. filepath.IsAbs alone cannot be used: on Windows it rejects
// "/home/ubuntu/steamcmd/l4d2", which is still a valid absolute path for a
// container-managed config, and on Linux it rejects "D:\SteamCMD\l4d2".
// Tests and shared fixtures rely on both forms being accepted.
func isAbsolutePathForPlatform(value string) bool {
	if strings.HasPrefix(value, "/") {
		return true
	}
	if len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return strings.HasPrefix(value, `\\`)
}

func normalizeSystemdExecStart(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("启动命令不能为空")
	}
	if utf8.RuneCountInString(value) > 4096 {
		return "", fmt.Errorf("启动命令不能超过 4096 个字符")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("启动命令不能包含换行或空字符")
	}
	firstField := strings.Fields(value)
	if len(firstField) == 0 {
		return "", fmt.Errorf("启动命令不能为空")
	}
	executable := strings.TrimPrefix(firstField[0], "-")
	executable = strings.TrimPrefix(executable, "@")
	if !isAbsolutePathForPlatform(executable) {
		if IsWindows() {
			return "", fmt.Errorf("启动命令必须使用绝对路径，例如 D:\\SteamCMD\\l4d2\\srcds.exe")
		}
		return "", fmt.Errorf("启动命令必须使用绝对路径")
	}
	return value, nil
}

func writeStartupUnitAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("创建启动项目录失败: %w", err)
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(directory, ".l4d2-service-*.tmp")
	if err != nil {
		return fmt.Errorf("创建启动项临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("写入启动项文件失败: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("替换启动项文件失败: %w", err)
	}
	return nil
}

// triggerStartupReload 通知宿主机重载 systemd。
//
// 修复 D2：旧实现无条件写 /control/systemd-reload，但宿主机上没有任何
// path unit 监听这个文件，用户会永远停在"等待重载"。
//
// 现在的规则：
//  1. 配了 L4D2_STARTUP_RELOAD_CMD → 直接执行命令（同步、可验证）；
//  2. 配了 L4D2_STARTUP_RELOAD_TRIGGER → 写该文件（用户自建了监听）；
//  3. 都没配 → 不写死文件，因为写 unit 本身就会触发 l4d2-startup.path。
func triggerStartupReload() (string, string, error) {
	if command := strings.TrimSpace(os.Getenv("L4D2_STARTUP_RELOAD_CMD")); command != "" {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd.exe", "/c", command)
		} else {
			cmd = exec.Command("sh", "-c", command)
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", "", fmt.Errorf("重载 systemd 失败: %s", strings.TrimSpace(string(output)))
		}
		return "command", "systemd 服务定义已重载", nil
	}

	triggerPath := strings.TrimSpace(os.Getenv("L4D2_STARTUP_RELOAD_TRIGGER"))
	if triggerPath == "" {
		if IsWindows() {
			return "none", "启动项已保存，重启后生效", nil
		}
		return "filesystem-watch", "启动项已保存，宿主机 path unit 会自动重载", nil
	}
	if info, err := os.Stat(filepath.Dir(triggerPath)); err != nil || !info.IsDir() {
		return "none", "启动项已保存，未检测到宿主重载通道", nil
	}
	payload, _ := json.Marshal(map[string]string{
		"requested_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err := os.WriteFile(triggerPath, payload, 0600); err != nil {
		return "", "", fmt.Errorf("通知宿主机重载 systemd 失败: %w", err)
	}
	return "filesystem-watch", "已提交 systemd 重载请求", nil
}

func readStartupApplyResult(currentUnit string) (string, string, string) {
	data, err := os.ReadFile(serverStartupResultPath())
	if err != nil {
		return "", "", ""
	}
	var result startupApplyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return "error", "无法读取 systemd 重载结果", ""
	}
	sum := sha256.Sum256([]byte(currentUnit))
	if result.UnitSHA256 != "" && result.UnitSHA256 != hex.EncodeToString(sum[:]) {
		return "pending", "启动项已保存，等待 systemd 重载", result.AppliedAt
	}
	switch result.Status {
	case "success":
		return "success", result.Message, result.AppliedAt
	case "error":
		return "error", result.Message, result.AppliedAt
	default:
		return "pending", result.Message, result.AppliedAt
	}
}
