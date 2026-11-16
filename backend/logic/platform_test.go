package logic

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCurrentPlatformMatchesRuntime(t *testing.T) {
	got := CurrentPlatform()
	switch runtime.GOOS {
	case "windows":
		if got != PlatformWindows {
			t.Fatalf("CurrentPlatform() = %q, want %q", got, PlatformWindows)
		}
	case "linux":
		if got != PlatformLinux {
			t.Fatalf("CurrentPlatform() = %q, want %q", got, PlatformLinux)
		}
	default:
		if got != PlatformOther {
			t.Fatalf("CurrentPlatform() = %q, want %q", got, PlatformOther)
		}
	}
}

func TestIsWindowsAndIsLinuxAreExclusive(t *testing.T) {
	if IsWindows() && IsLinux() {
		t.Fatal("IsWindows and IsLinux must not both be true")
	}
}

func TestHasSystemdOnlyOnLinux(t *testing.T) {
	if got := HasSystemd(); got != IsLinux() {
		t.Fatalf("HasSystemd() = %v, want %v", got, IsLinux())
	}
}

func TestPlatformCapabilitiesAreConsistent(t *testing.T) {
	caps := GetPlatformCapabilities()
	if caps.Platform != string(CurrentPlatform()) {
		t.Fatalf("platform = %q, want %q", caps.Platform, string(CurrentPlatform()))
	}
	if caps.Systemd != IsLinux() {
		t.Fatalf("systemd = %v, want %v", caps.Systemd, IsLinux())
	}
	if caps.StartupDiagnostics != caps.Systemd {
		t.Fatal("startup diagnostics must follow systemd availability")
	}
	if caps.PathSeparator != string(os.PathSeparator) {
		t.Fatalf("path separator = %q, want %q", caps.PathSeparator, string(os.PathSeparator))
	}
	if caps.StartupConfigKind == "" {
		t.Fatal("startup config kind must not be empty")
	}
}

func TestDefaultControlDirHonoursEnvOverride(t *testing.T) {
	const custom = "/tmp/l4d2-custom-control"
	t.Setenv("L4D2_CONTROL_DIR", custom)
	if got := DefaultControlDir(); got != custom {
		t.Fatalf("DefaultControlDir() = %q, want %q", got, custom)
	}
}

func TestDefaultControlDirFallsBackPerPlatform(t *testing.T) {
	t.Setenv("L4D2_CONTROL_DIR", "")
	got := DefaultControlDir()
	if got == "" {
		t.Fatal("DefaultControlDir() must not be empty")
	}
	if IsWindows() {
		if strings.HasPrefix(got, "/control") {
			t.Fatalf("Windows control dir must not be the Unix path, got %q", got)
		}
		if filepath.Base(got) != "control" {
			t.Fatalf("Windows control dir should end with control, got %q", got)
		}
		return
	}
	if got != "/control" {
		t.Fatalf("Linux control dir = %q, want /control", got)
	}
}

func TestIsAbsolutePathForPlatformAcceptsBothStyles(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/home/ubuntu/steamcmd/l4d2", true},
		{"/left4dead2", true},
		{`D:\SteamCMD\l4d2`, true},
		{`C:/SteamCMD/l4d2`, true},
		{`\\server\share\l4d2`, true},
		{"relative/path", false},
		{"", false},
		{"./left4dead2", false},
	}
	for _, tc := range cases {
		if got := isAbsolutePathForPlatform(tc.path); got != tc.want {
			t.Fatalf("isAbsolutePathForPlatform(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestNormalizeWorkingDirectoryAcceptsWindowsPath(t *testing.T) {
	if _, err := normalizeSystemdWorkingDirectory(`D:\SteamCMD\l4d2`); err != nil {
		t.Fatalf("Windows absolute path rejected: %v", err)
	}
	if _, err := normalizeSystemdWorkingDirectory("/home/ubuntu/steamcmd/l4d2"); err != nil {
		t.Fatalf("POSIX absolute path rejected: %v", err)
	}
	if _, err := normalizeSystemdWorkingDirectory("relative/path"); err == nil {
		t.Fatal("relative path must be rejected")
	}
}

func TestNormalizeExecStartAcceptsWindowsPath(t *testing.T) {
	if _, err := normalizeSystemdExecStart(`D:\SteamCMD\l4d2\srcds.exe -game left4dead2`); err != nil {
		t.Fatalf("Windows exec start rejected: %v", err)
	}
	if _, err := normalizeSystemdExecStart("/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2"); err != nil {
		t.Fatalf("POSIX exec start rejected: %v", err)
	}
	if _, err := normalizeSystemdExecStart("srcds_run -game left4dead2"); err == nil {
		t.Fatal("relative exec start must be rejected")
	}
}

func TestDiagnosticsUnavailableWithoutSystemd(t *testing.T) {
	if got := DiagnosticsAvailable(); got != HasSystemd() {
		t.Fatalf("DiagnosticsAvailable() = %v, want %v", got, HasSystemd())
	}
}

func TestUnsupportedDiagnosticsHintIsInformational(t *testing.T) {
	hint := unsupportedDiagnosticsHint()
	if hint.Level != diagnosticsLevelInfo {
		t.Fatalf("level = %q, want info", hint.Level)
	}
	if hint.Title == "" || hint.Suggestion == "" {
		t.Fatal("hint must carry a title and suggestion")
	}
}
