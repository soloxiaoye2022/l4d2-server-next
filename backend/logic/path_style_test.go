package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"l4d2-manager-next/consts"
)

func TestDirOfPathStyleKeepsPosixStyle(t *testing.T) {
	got := dirOfPathStyle("/home/ubuntu/steamcmd/l4d2")
	if got != "/home/ubuntu/steamcmd" {
		t.Fatalf("dirOfPathStyle() = %q, want /home/ubuntu/steamcmd", got)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("POSIX path must not gain backslashes: %q", got)
	}
}

func TestDirOfPathStyleHandlesWindowsDrive(t *testing.T) {
	got := dirOfPathStyle(`D:\SteamCMD\l4d2`)
	want := `D:\SteamCMD`
	if got != want {
		t.Fatalf("dirOfPathStyle() = %q, want %q", got, want)
	}
}

func TestJoinPathStyleKeepsPosixStyle(t *testing.T) {
	got := joinPathStyle("/home/ubuntu/steamcmd/l4d2", "left4dead2", "update_l4d2.txt")
	want := "/home/ubuntu/steamcmd/l4d2/left4dead2/update_l4d2.txt"
	if got != want {
		t.Fatalf("joinPathStyle() = %q, want %q", got, want)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("POSIX join must not produce backslashes: %q", got)
	}
}

func TestJoinPathStyleHandlesWindowsDrive(t *testing.T) {
	got := joinPathStyle(`D:\SteamCMD\l4d2`, "left4dead2", "update_l4d2.txt")
	want := `D:\SteamCMD\l4d2\left4dead2\update_l4d2.txt`
	if got != want {
		t.Fatalf("joinPathStyle() = %q, want %q", got, want)
	}
}

func TestIsCleanPathForStyleAcceptsBothStyles(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/home/ubuntu/steamcmd/l4d2", true},
		{"/left4dead2", true},
		{`D:\SteamCMD\l4d2`, true},
		{`\\server\share\l4d2`, true},
		{"/home//ubuntu", false},
		{"/home/./ubuntu", false},
		{`D:\SteamCMD\\l4d2`, false},
	}
	for _, tc := range cases {
		if got := isCleanPathForStyle(tc.path); got != tc.want {
			t.Fatalf("isCleanPathForStyle(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestRepairRejectedWithoutHostHelper(t *testing.T) {
	if HasSystemd() {
		t.Skip("requires a host without systemd")
	}
	// Service-only actions never touch addons, so they reach the platform guard
	// even when the addons directory is clean.
	result, err := RequestServerRepair(RepairActionRestartService, "test", 0)
	if err != nil {
		t.Fatalf("RequestServerRepair() error = %v", err)
	}
	if result.Status != RepairStatusError {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "宿主机处置脚本") {
		t.Fatalf("message = %q", result.Message)
	}
}

func TestRepairQuarantineRejectedWithoutHostHelper(t *testing.T) {
	if HasSystemd() {
		t.Skip("requires a host without systemd")
	}
	// Quarantine also delegates to the host script; with no broken VPK present
	// the noop short circuit wins, so create one to reach the guard.
	originalPath := consts.AddonsBasePath
	consts.AddonsBasePath = t.TempDir()
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	broken := filepath.Join(consts.AddonsBasePath, "broken.vpk")
	if err := os.WriteFile(broken, make([]byte, 12), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := RequestServerRepair(RepairActionQuarantine, "test", 0)
	if err != nil {
		t.Fatalf("RequestServerRepair() error = %v", err)
	}
	if result.Status != RepairStatusError {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if !strings.Contains(result.Message, "宿主机处置脚本") {
		t.Fatalf("message = %q", result.Message)
	}
}
