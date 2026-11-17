package logic

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	runtimelogic "l4d2-manager-next/logic/runtime"
)

// Platform identifies the OS the manager binary runs on. The panel keeps one
// source tree for both targets, so every "is this Linux" decision funnels
// through this file instead of being scattered across controllers.
type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformWindows Platform = "windows"
	PlatformOther   Platform = "other"
)

// PlatformCapabilities tells the frontend what the current host can actually
// do. Features that depend on systemd must be hidden rather than shown as
// broken controls.
type PlatformCapabilities struct {
	Platform           string `json:"platform"`
	Systemd            bool   `json:"systemd"`
	StartupDiagnostics bool   `json:"startup_diagnostics"`
	ProcessControl     bool   `json:"process_control"`
	ServiceUpdate      bool   `json:"service_update"`
	PathSeparator      string `json:"path_separator"`
	DefaultGamePath    string `json:"default_game_path"`
	DefaultControlDir  string `json:"default_control_dir"`
	StartupConfigKind  string `json:"startup_config_kind"`
	// 以下字段是运行时适配框架的对外能力声明（设计文档 §5.6 / D6）。
	AgentMode         string   `json:"agent_mode"`
	RuntimeKinds      []string `json:"runtime_kinds"`
	DefaultRuntimeID  string   `json:"default_runtime_id"`
	RuntimeManagement bool     `json:"runtime_management"`
	RuntimeDetection  bool     `json:"runtime_detection"`
}

func CurrentPlatform() Platform {
	switch runtime.GOOS {
	case "linux":
		return PlatformLinux
	case "windows":
		return PlatformWindows
	default:
		return PlatformOther
	}
}

func IsWindows() bool {
	return runtime.GOOS == "windows"
}

func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// systemdProbe 记录 HasSystemd 的探测结果，避免每次请求都跑一次外部命令。
var systemdProbe struct {
	once  sync.Once
	value bool
}

// HasSystemd 实测宿主机是否真的在用 systemd。
//
// 修复 D1：原实现返回 IsLinux()，在容器、Alpine、WSL 等没有 systemd 的
// Linux 环境上会错误地显示 systemd 专属功能。这里用两个证据：
//  1. /run/systemd/system 目录存在（systemd 运行时的标志）
//  2. systemctl 可执行且能正常响应
//
// 面板自身跑在容器里时，容器内通常没有 systemd，因此额外看宿主挂载点：
// L4D2_HOST_SYSTEMD_DIR（默认 /host/systemd，由 install.sh 挂载）。
func HasSystemd() bool {
	systemdProbe.once.Do(func() {
		systemdProbe.value = probeSystemd()
	})
	return systemdProbe.value
}

// ResetSystemdProbe 让测试可以重置探测缓存。
func ResetSystemdProbe() {
	systemdProbe.once = sync.Once{}
	systemdProbe.value = false
}

func probeSystemd() bool {
	if value := strings.TrimSpace(os.Getenv("L4D2_FORCE_SYSTEMD")); value == "1" || value == "true" {
		return true
	}
	if value := strings.TrimSpace(os.Getenv("L4D2_FORCE_SYSTEMD")); value == "0" || value == "false" {
		return false
	}
	if IsWindows() {
		return false
	}
	for _, directory := range systemdMarkerDirs() {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			return true
		}
	}
	// 目录不存在时再看 systemctl 是否真能回答（WSL1 常见）。
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	command := exec.Command("systemctl", "is-system-running")
	output, err := command.Output()
	if err != nil {
		// is-system-running 在 degraded 状态会返回非零，但输出仍然有效。
		text := strings.TrimSpace(string(output))
		return text == "degraded" || text == "starting" || text == "initializing"
	}
	return true
}

func systemdMarkerDirs() []string {
	dirs := []string{"/run/systemd/system"}
	if value := strings.TrimSpace(os.Getenv("L4D2_HOST_SYSTEMD_DIR")); value != "" {
		dirs = append(dirs, value)
	} else if !IsWindows() {
		dirs = append(dirs, "/host/systemd")
	}
	return dirs
}

func platformDefaultGamePath() string {
	if IsWindows() {
		return "left4dead2"
	}
	return "/left4dead2"
}

// DefaultControlDir is the fallback hand-off directory between the panel and
// host scripts. On Windows there is no host helper, so a relative directory
// next to the executable keeps the panel from writing to a missing /control.
func DefaultControlDir() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_CONTROL_DIR")); value != "" {
		return value
	}
	if IsWindows() {
		if exe, err := os.Executable(); err == nil {
			return filepath.Join(filepath.Dir(exe), "control")
		}
		return "control"
	}
	return "/control"
}

func startupConfigKind() string {
	if IsWindows() {
		return "batch"
	}
	return "systemd"
}

func GetPlatformCapabilities() PlatformCapabilities {
	InitRuntimeManager()
	manager := runtimelogic.Default()
	defaultRuntimeID := ""
	if items, _, err := manager.All(); err == nil {
		for _, item := range items {
			if item.Enabled && item.ArchiveAt == "" {
				defaultRuntimeID = item.ID
				break
			}
		}
	}
	agentMode := manager.AgentMode()
	return PlatformCapabilities{
		Platform:           string(CurrentPlatform()),
		Systemd:            HasSystemd(),
		StartupDiagnostics: HasSystemd(),
		ProcessControl:     agentMode != runtimelogic.AgentModeDisabled,
		ServiceUpdate:      HasSystemd(),
		PathSeparator:      string(os.PathSeparator),
		DefaultGamePath:    platformDefaultGamePath(),
		DefaultControlDir:  DefaultControlDir(),
		StartupConfigKind:  startupConfigKind(),
		AgentMode:          agentMode,
		RuntimeKinds:       runtimelogic.KindStrings(),
		DefaultRuntimeID:   defaultRuntimeID,
		RuntimeManagement:  agentMode != runtimelogic.AgentModeDisabled,
		RuntimeDetection:   agentMode == runtimelogic.AgentModeBridge,
	}
}
