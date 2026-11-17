package logic

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"l4d2-manager-next/consts"

	"github.com/google/uuid"
)

const (
	diagnosticsLevelError   = "error"
	diagnosticsLevelWarning = "warning"
	diagnosticsLevelInfo    = "info"
)

var serverDiagnosticsMu sync.Mutex

type ServerDiagnostics struct {
	Available            bool        `json:"available"`
	Message              string      `json:"message,omitempty"`
	RequestID            string      `json:"request_id,omitempty"`
	GeneratedAt          string      `json:"generated_at,omitempty"`
	ServiceName          string      `json:"service_name,omitempty"`
	UnitPath             string      `json:"unit_path,omitempty"`
	UnitModifiedAt       string      `json:"unit_modified_at,omitempty"`
	LoadState            string      `json:"load_state,omitempty"`
	ActiveState          string      `json:"active_state,omitempty"`
	SubState             string      `json:"sub_state,omitempty"`
	UnitFileState        string      `json:"unit_file_state,omitempty"`
	MainPID              string      `json:"main_pid,omitempty"`
	ExecMainStatus       string      `json:"exec_main_status,omitempty"`
	Result               string      `json:"result,omitempty"`
	NRestarts            string      `json:"n_restarts,omitempty"`
	ActiveEnterTimestamp string      `json:"active_enter_timestamp,omitempty"`
	Port                 int         `json:"port,omitempty"`
	PortListening        bool        `json:"port_listening"`
	Listeners            []string    `json:"listeners,omitempty"`
	PortOwners           []PortOwner `json:"port_owners,omitempty"`
	Processes            []string    `json:"processes,omitempty"`
	StatusText           string      `json:"status_text,omitempty"`
	Journal              []string    `json:"journal,omitempty"`
	JournalRecent        []string    `json:"journal_recent,omitempty"`
	DiskFreeBytes        int64       `json:"disk_free_bytes,omitempty"`
	DiskTotalBytes       int64       `json:"disk_total_bytes,omitempty"`
}

// PortOwner is one process holding the game port according to ss.
type PortOwner struct {
	Command string `json:"command"`
	PID     int    `json:"pid"`
}

type ServerDiagnosticHint struct {
	Level      string `json:"level"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

type ServerDiagnosticsResponse struct {
	ServerDiagnostics
	Hints   []ServerDiagnosticHint `json:"hints"`
	Summary string                 `json:"summary,omitempty"`
}

type InvalidAddonVPK struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"`
}

type diagnosticsRequest struct {
	RequestID   string `json:"request_id"`
	ServiceName string `json:"service_name"`
	RequestedAt string `json:"requested_at"`
}

func diagnosticsControlDir() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_DIAGNOSTICS_CONTROL_DIR")); value != "" {
		return value
	}
	return maintenanceControlDir()
}

// DiagnosticsAvailable reports whether this platform can collect a fresh
// startup snapshot. Linux 的 systemd path unit 是一条通道；
// 只要用户接管过运行时，运行时长通道同样可用（D5）。
func DiagnosticsAvailable() bool {
	return HasSystemd() || managedRuntimeAvailable()
}

// serverStartupPort resolves the game port from the startup config, falling
// back to L4D2_PORT and then the default. Windows diagnostics need it before
// any unit file can be parsed.
func serverStartupPort() int {
	if cfg, err := GetServerStartupConfig(); err == nil && cfg.Port > 0 {
		return cfg.Port
	}
	if port := parsePort(os.Getenv("L4D2_PORT")); port > 0 {
		return port
	}
	return DefaultGameServerPort
}

func diagnosticsRequestPath() string {
	return filepath.Join(diagnosticsControlDir(), "diagnostics-request.json")
}

func diagnosticsResultPath() string {
	return filepath.Join(diagnosticsControlDir(), "diagnostics-result.json")
}

func serverDiagnosticsBridgeHint(message string) ServerDiagnosticHint {
	detail := message
	if detail == "" {
		detail = "面板没有收到宿主机返回的诊断结果。"
	}
	return ServerDiagnosticHint{
		Level:      diagnosticsLevelWarning,
		Title:      "宿主机诊断桥接未就绪",
		Detail:     detail,
		Suggestion: "请确认已安装并启用 l4d2-diagnostics.path / l4d2-diagnostics.service，且 /opt/l4d2-manager/diagnose-server.sh 存在且可执行。",
	}
}

// unsupportedDiagnosticsHint explains the platform limitation instead of
// pretending the bridge is broken. Users on Windows should not go hunting for
// a systemd unit that can never exist there.
func unsupportedDiagnosticsHint() ServerDiagnosticHint {
	return ServerDiagnosticHint{
		Level:      diagnosticsLevelInfo,
		Title:      "当前平台不支持 systemd 诊断",
		Detail:     "启动诊断依赖宿主机的 systemd path unit 采集 journal 与端口快照，Windows 没有对应机制。",
		Suggestion: "Windows 下请直接查看游戏服控制台窗口；面板仍可检查 addons 目录中的损坏 VPK，并执行隔离处置。",
	}
}

// CollectServerDiagnostics asks the host-side path unit for a fresh systemd
// snapshot and waits briefly for the matching result. On platforms without
// systemd it returns immediately with an explanatory hint instead of blocking
// for the full timeout.
func CollectServerDiagnostics(wait time.Duration) ServerDiagnosticsResponse {
	serviceName := serverStartupServiceName()

	// 修复 D5：优先走运行时子系统采集。
	// 旧实现只有 systemd 能出数据，接管 screen/tmux/nohup/docker 后诊断页全空。
	// 只要用户显式接管过运行时，就用 Manager 的 inspect + logs 组装同一套诊断。
	if diagnostics, ok := CollectRuntimeDiagnostics(serverStartupPort()); ok {
		return BuildServerDiagnosticsResponse(diagnostics)
	}

	if !DiagnosticsAvailable() {
		// Windows has no systemd, but the process table still tells us whether
		// the port is bound and who owns it. Report that instead of an empty
		// "unsupported" page.
		diagnostics := CollectLocalDiagnostics(serverStartupPort())
		response := BuildServerDiagnosticsResponse(diagnostics)
		// Keep the platform note, but let real problems win the summary line.
		// The hint list is re-sorted so an unbound port outranks "unsupported".
		response.Hints = append(response.Hints, unsupportedDiagnosticsHint())
		sort.SliceStable(response.Hints, func(i, j int) bool {
			return diagnosticsLevelRank(response.Hints[i].Level) <
				diagnosticsLevelRank(response.Hints[j].Level)
		})
		if len(response.Hints) > 0 {
			response.Summary = response.Hints[0].Title
		}
		return response
	}
	request := diagnosticsRequest{
		RequestID:   uuid.NewString(),
		ServiceName: serviceName,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	serverDiagnosticsMu.Lock()
	defer serverDiagnosticsMu.Unlock()

	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return ServerDiagnosticsResponse{
			ServerDiagnostics: ServerDiagnostics{ServiceName: serviceName},
			Hints:             []ServerDiagnosticHint{serverDiagnosticsBridgeHint(err.Error())},
			Summary:           "无法生成诊断请求",
		}
	}
	if err := writeManagerFileAtomic(diagnosticsRequestPath(), data, 0600); err != nil {
		return ServerDiagnosticsResponse{
			ServerDiagnostics: ServerDiagnostics{ServiceName: serviceName},
			Hints:             []ServerDiagnosticHint{serverDiagnosticsBridgeHint(fmt.Sprintf("写入诊断请求失败: %v", err))},
			Summary:           "宿主机诊断桥接不可用",
		}
	}

	deadline := time.Now().Add(wait)
	for {
		if result, err := readServerDiagnostics(); err == nil && result.RequestID == request.RequestID {
			return BuildServerDiagnosticsResponse(result)
		}
		if time.Now().After(deadline) {
			message := "等待宿主机诊断结果超时，请检查 l4d2-diagnostics.path 是否已启用。"
			return ServerDiagnosticsResponse{
				ServerDiagnostics: ServerDiagnostics{ServiceName: serviceName},
				Hints:             []ServerDiagnosticHint{serverDiagnosticsBridgeHint(message)},
				Summary:           "宿主机诊断桥接未响应",
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
}

func readServerDiagnostics() (ServerDiagnostics, error) {
	var result ServerDiagnostics
	data, err := os.ReadFile(diagnosticsResultPath())
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func BuildServerDiagnosticsResponse(diagnostics ServerDiagnostics) ServerDiagnosticsResponse {
	hints := BuildServerDiagnosticHints(diagnostics)
	summary := "未发现已知的启动失败特征，请查看原始日志。"
	if len(hints) > 0 {
		summary = hints[0].Title
	}
	return ServerDiagnosticsResponse{
		ServerDiagnostics: diagnostics,
		Hints:             hints,
		Summary:           summary,
	}
}

func BuildServerDiagnosticHints(diagnostics ServerDiagnostics) []ServerDiagnosticHint {
	journal := strings.Join(diagnostics.Journal, "\n")
	recent := diagnostics.JournalRecent
	if len(recent) == 0 {
		recent = diagnostics.Journal
	}
	recent = lastServiceStartup(recent)
	recentText := strings.Join(recent, "\n")
	hints := make([]ServerDiagnosticHint, 0, 8)

	invalidVPKs := FindInvalidAddonVPKs()
	if len(invalidVPKs) > 0 {
		names := make([]string, 0, len(invalidVPKs))
		for _, item := range invalidVPKs {
			names = append(names, fmt.Sprintf("%s（%s，%s）", item.Name, formatDiagnosticBytes(item.Size), item.Reason))
		}
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "发现损坏的地图 VPK",
			Detail:     "以下文件无法被正常解析，服务端加载 addons 时可能直接崩溃：\n" + strings.Join(names, "\n"),
			Suggestion: "把对应文件移出 addons 目录后重新上传或重新下载；本次故障就是 12 字节的空 VPK 导致 srcds 启动即段错误。",
		})
	}

	portHijacked := false
	if owners := foreignPortOwners(diagnostics.PortOwners); len(owners) > 0 {
		portHijacked = true
		details := make([]string, 0, len(owners))
		for _, owner := range owners {
			details = append(details, fmt.Sprintf("%s (pid=%d)", owner.Command, owner.PID))
		}
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "游戏端口被其他进程占用",
			Detail:     fmt.Sprintf("端口 %d 当前由非游戏进程占用：%s。srcds 绑定失败后可能自动改用下一个端口，面板仍显示端口已监听，但玩家连不上。", diagnostics.Port, strings.Join(details, "、")),
			Suggestion: "终止占用进程后重启，或修改启动项中的 +hostport；可点击下方「终止占用并重启」自动处理。",
		})
	}

	if strings.Contains(recentText, "Changing to the requested working directory failed") ||
		strings.Contains(recentText, "Failed at step CHDIR") ||
		strings.Contains(recentText, "200/CHDIR") {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "工作目录不存在或不可访问",
			Detail:     "systemd 在切换 WorkingDirectory 时失败（CHDIR），进程根本没被拉起。",
			Suggestion: "检查启动项里的工作目录是否存在、是否有执行权限，例如 /home/ubuntu/steamcmd/l4d2。",
		})
	}

	execMissing := hasExecMissing(recent)
	if execMissing {
		path := extractDiagnosticPath(recent, "Failed to locate executable", "Failed at step EXEC spawning")
		detail := "systemd 找不到启动项里的可执行文件，进程没有被拉起。"
		if path != "" {
			detail = fmt.Sprintf("systemd 找不到可执行文件：%s", path)
		}
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "启动文件缺失",
			Detail:     detail,
			Suggestion: "检查启动项的 ExecStart 路径是否正确，必要时用 SteamCMD 校验安装。",
		})
	}

	execDenied := hasExecPermissionDenied(recent)
	if execDenied {
		path := extractDiagnosticPath(recent, "Failed to locate executable", "Failed at step EXEC spawning")
		detail := "systemd 无法执行启动项里的可执行文件（Permission denied）。"
		if path != "" {
			detail = fmt.Sprintf("systemd 无法执行：%s（Permission denied）", path)
		}
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "启动文件不可执行",
			Detail:     detail,
			Suggestion: "给可执行文件补执行权限，例如 chmod +x /home/ubuntu/steamcmd/l4d2/srcds_run，并确认属主与启动用户一致。",
		})
	}

	if strings.Contains(recentText, "Out of memory") || strings.Contains(recentText, "oom-kill") || strings.Contains(recentText, "Killed process") {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "内存不足，游戏进程被系统终止",
			Detail:     "内核日志显示发生过 OOM 或 Killed process，srcds 可能因内存不足被杀。",
			Suggestion: "增加内存或添加 swap，减少同时加载的大型地图/插件；本机 2G 内存运行 31 人服很容易 OOM。",
		})
	}

	if strings.Contains(recentText, "Server restart in") && !diagnostics.PortListening {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "服务端在崩溃自拉起循环",
			Detail:     "srcds_run 检测到游戏进程退出后会自动重启，所以 systemd 仍可能显示 active，但端口没有监听、RCON 无法连接。",
			Suggestion: "先根据下面的段错误/损坏 VPK 线索修复文件，再重启服务。",
		})
	}

	if strings.Contains(recentText, "Segmentation fault") && len(invalidVPKs) == 0 {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "游戏进程发生段错误",
			Detail:     "日志中出现 Segmentation fault。常见原因是地图或插件 VPK 损坏、插件与当前游戏版本不兼容、或缺少运行库。",
			Suggestion: "查看下方原始日志中崩溃前的最后几行，优先排查最近新增的地图和插件。",
		})
	}

	if strings.Contains(recentText, "Address already in use") && !portHijacked {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "游戏端口被占用",
			Detail:     fmt.Sprintf("日志显示端口 %d 已被其他进程占用。", diagnostics.Port),
			Suggestion: "用 ss -lntup 找到占用端口的进程并停止，或修改启动项中的 +hostport。",
		})
	}

	if strings.Contains(recentText, "Permission denied") && !execDenied {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "文件权限不足",
			Detail:     "日志中出现 Permission denied，游戏目录或 addons 文件可能不属于运行服务的用户。",
			Suggestion: "确认 l4d2.service 的 User 与游戏目录属主一致（例如 chown -R ubuntu:ubuntu）。",
		})
	}

	if missingSrcdsBinary(recent) && !diagnostics.PortListening && !execMissing {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "启动文件缺失",
			Detail:     "日志显示 srcds 或其依赖文件不存在。",
			Suggestion: "检查启动项里的游戏目录和 srcds_run 路径是否正确，必要时用 SteamCMD 校验安装。",
		})
	}

	if state := strings.TrimSpace(diagnostics.LoadState); state != "" && state != "loaded" {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "systemd 单元文件异常",
			Detail:     fmt.Sprintf("LoadState=%s，systemd 没有正确加载该服务单元。", state),
			Suggestion: "执行 systemctl daemon-reload 并检查 unit 语法；面板的「设置 - 启动项」保存后也会触发重载。",
		})
	}

	if strings.Contains(recentText, "Failed to start") || diagnostics.Result == "exit-code" || diagnostics.Result == "signal" {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelError,
			Title:      "systemd 启动单元失败",
			Detail:     fmt.Sprintf("ActiveState=%s SubState=%s Result=%s ExecMainStatus=%s", diagnostics.ActiveState, diagnostics.SubState, diagnostics.Result, diagnostics.ExecMainStatus),
			Suggestion: "查看下方 systemctl status 与 journal 原始输出定位具体失败原因。",
		})
	}

	if diagnostics.DiskTotalBytes > 0 && diagnostics.DiskFreeBytes > 0 && diagnostics.DiskFreeBytes < 1<<30 {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelWarning,
			Title:      "磁盘剩余空间不足",
			Detail:     fmt.Sprintf("游戏目录所在分区只剩 %s，上传地图或写入日志可能失败。", formatDiagnosticBytes(diagnostics.DiskFreeBytes)),
			Suggestion: "清理旧地图、日志或镜像，至少保留 1 GB 空间。",
		})
	}

	if failedPlugins := failedPluginNames(recent); len(failedPlugins) > 0 {
		level := diagnosticsLevelWarning
		title := "插件加载失败"
		if !diagnostics.PortListening {
			level = diagnosticsLevelError
			title = "插件加载失败，可能导致服务无法启动"
		}
		hints = append(hints, ServerDiagnosticHint{
			Level:      level,
			Title:      title,
			Detail:     "以下 SourceMod 插件最近加载失败：\n" + strings.Join(failedPlugins, "\n"),
			Suggestion: "把对应 .smx 移出 addons/sourcemod/plugins 或更新到匹配游戏版本的插件，然后重启服务。",
		})
	}

	if !diagnostics.PortListening && len(hints) == 0 {
		hints = append(hints, ServerDiagnosticHint{
			Level:      diagnosticsLevelWarning,
			Title:      "游戏端口未监听",
			Detail:     fmt.Sprintf("当前没有进程监听端口 %d，面板无法通过 RCON 获取状态。", diagnostics.Port),
			Suggestion: "查看原始日志确认服务端是否仍在启动，或点击重启后再试。",
		})
	}

	if strings.Contains(journal, "steamclient.so") && strings.Contains(journal, "cannot open shared object file") {
		hints = append(hints, ServerDiagnosticHint{
			Level:  diagnosticsLevelInfo,
			Title:  "缺少 Steam 客户端库（通常只是告警）",
			Detail: "日志提示找不到 ~/.steam/sdk32/steamclient.so；以 -insecure 启动时通常不影响运行，但可作为排查参考。",
		})
	}

	sort.SliceStable(hints, func(i, j int) bool {
		return diagnosticsLevelRank(hints[i].Level) < diagnosticsLevelRank(hints[j].Level)
	})
	return hints
}

// lastServiceStartup keeps only the journal lines from the most recent start
// attempt. Without this, a fixed time window mixes in failures from earlier
// restarts (or from a previous experiment) and produces false positives.
// journalctl already filters by unit, so the "Starting ..." banner is enough.
func lastServiceStartup(lines []string) []string {
	index := -1
	for i, line := range lines {
		if strings.Contains(line, "Starting ") {
			index = i
		}
	}
	if index < 0 {
		return lines
	}
	return lines[index:]
}

func hasExecMissing(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "Permission denied") {
			continue
		}
		if strings.Contains(line, "Failed to locate executable") {
			return true
		}
		if strings.Contains(line, "Failed at step EXEC spawning") && strings.Contains(line, "No such file or directory") {
			return true
		}
	}
	return false
}

func hasExecPermissionDenied(lines []string) bool {
	for _, line := range lines {
		if !strings.Contains(line, "Permission denied") {
			continue
		}
		if strings.Contains(line, "Failed at step EXEC") || strings.Contains(line, "Failed to locate executable") {
			return true
		}
	}
	return false
}

// foreignPortOwners filters out the game processes, leaving whoever else holds
// the port. srcds silently moves to the next port when logging says
// "ports 12139 SV", so a listening port alone does not prove the game is up.
func foreignPortOwners(owners []PortOwner) []PortOwner {
	foreign := make([]PortOwner, 0, len(owners))
	for _, owner := range owners {
		if strings.HasPrefix(strings.ToLower(owner.Command), "srcds") {
			continue
		}
		foreign = append(foreign, owner)
	}
	return foreign
}

func extractDiagnosticPath(lines []string, markers ...string) string {
	pattern := regexp.MustCompile(`(/\S+)`)
	for _, line := range lines {
		for _, marker := range markers {
			if !strings.Contains(line, marker) {
				continue
			}
			if match := pattern.FindString(line); match != "" {
				return strings.TrimRight(match, ":,")
			}
		}
	}
	return ""
}

var failedPluginPattern = regexp.MustCompile(`Failed to load plugin "([^"]+\.smx)"`)

func failedPluginNames(lines []string) []string {
	seen := make(map[string]bool)
	names := make([]string, 0, 4)
	for _, line := range lines {
		for _, match := range failedPluginPattern.FindAllStringSubmatch(line, -1) {
			name := match[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
			if len(names) >= 5 {
				return names
			}
		}
	}
	return names
}

// missingSrcdsPattern captures the path that failed to load. Warnings about
// steamclient.so or libcurl.so reuse the same errno text but never stop the
// server from starting, so only the srcds executables themselves count.
var missingSrcdsPattern = regexp.MustCompile(`(\S+?):\s+(?:cannot open shared object file:\s*)?No such file or directory`)

// missingSrcdsBinary reports whether the journal shows the srcds executable
// itself missing, as opposed to an optional runtime library that failed to load.
func missingSrcdsBinary(journal []string) bool {
	for _, line := range journal {
		for _, match := range missingSrcdsPattern.FindAllStringSubmatch(line, -1) {
			switch filepath.Base(match[1]) {
			case "srcds", "srcds_run", "srcds_linux":
				return true
			}
		}
	}
	return false
}

func diagnosticsLevelRank(level string) int {
	switch level {
	case diagnosticsLevelError:
		return 0
	case diagnosticsLevelWarning:
		return 1
	default:
		return 2
	}
}

func formatDiagnosticBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for value := size / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// FindInvalidAddonVPKs checks top-level addon VPK headers. The Source engine
// mounts these files at startup and can crash on a header-only archive, which
// is exactly what an over-aggressive trim used to produce.
func FindInvalidAddonVPKs() []InvalidAddonVPK {
	entries, err := os.ReadDir(consts.AddonsBasePath)
	if err != nil {
		return nil
	}
	invalid := make([]InvalidAddonVPK, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".vpk") {
			continue
		}
		path := filepath.Join(consts.AddonsBasePath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if reason := invalidAddonVPKReason(path, info.Size()); reason != "" {
			invalid = append(invalid, InvalidAddonVPK{Name: entry.Name(), Size: info.Size(), Reason: reason})
		}
	}
	sort.Slice(invalid, func(i, j int) bool { return invalid[i].Name < invalid[j].Name })
	return invalid
}

func invalidAddonVPKReason(path string, size int64) string {
	file, err := os.Open(path)
	if err != nil {
		return "无法读取"
	}
	defer file.Close()

	header := make([]byte, 28)
	read, err := file.Read(header)
	if err != nil && read < 12 {
		return "文件过短，没有 VPK 头"
	}
	if read < 12 {
		return "文件过短，没有 VPK 头"
	}
	if binary.LittleEndian.Uint32(header[0:4]) != rawVPKMagic {
		return "文件头不是 VPK 格式"
	}
	version := binary.LittleEndian.Uint32(header[4:8])
	treeSize := int64(binary.LittleEndian.Uint32(header[8:12]))
	headerSize := int64(12)
	if version == 2 {
		headerSize = 28
	}
	if treeSize == 0 {
		return "目录树为空（空 VPK）"
	}
	if size < headerSize+treeSize {
		return "文件被截断，目录树不完整"
	}
	return ""
}
