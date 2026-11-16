package logic

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// HasSystemd reports whether the host uses systemd units for the game server.
// Containers on Linux still rely on the host path unit, so Linux is true even
// when the manager itself runs inside Docker.
func HasSystemd() bool {
	return IsLinux()
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
	return PlatformCapabilities{
		Platform:           string(CurrentPlatform()),
		Systemd:            HasSystemd(),
		StartupDiagnostics: HasSystemd(),
		ProcessControl:     true,
		ServiceUpdate:      HasSystemd(),
		PathSeparator:      string(os.PathSeparator),
		DefaultGamePath:    platformDefaultGamePath(),
		DefaultControlDir:  DefaultControlDir(),
		StartupConfigKind:  startupConfigKind(),
	}
}
