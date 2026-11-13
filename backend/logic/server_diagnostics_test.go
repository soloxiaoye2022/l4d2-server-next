package logic

import (
	"os"
	"path/filepath"
	"testing"

	"l4d2-manager-next/consts"
)

func TestFindInvalidAddonVPKsDetectsHeaderOnlyArchive(t *testing.T) {
	originalPath := consts.AddonsBasePath
	addons := t.TempDir()
	consts.AddonsBasePath = addons
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	bad := filepath.Join(addons, "1234567890.vpk")
	if err := os.WriteFile(bad, []byte{0x34, 0x12, 0xaa, 0x55, 1, 0, 0, 0, 0, 0, 0, 0}, 0644); err != nil {
		t.Fatalf("write bad vpk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(addons, "README.md"), []byte("ignore"), 0644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	invalid := FindInvalidAddonVPKs()
	if len(invalid) != 1 {
		t.Fatalf("FindInvalidAddonVPKs() = %d entries, want 1 (%+v)", len(invalid), invalid)
	}
	if invalid[0].Name != "1234567890.vpk" {
		t.Fatalf("unexpected invalid vpk: %+v", invalid[0])
	}
}

func TestBuildServerDiagnosticHintsExplainsCrashLoop(t *testing.T) {
	originalPath := consts.AddonsBasePath
	consts.AddonsBasePath = t.TempDir()
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		ActiveState:   "active",
		SubState:      "running",
		Port:          12138,
		PortListening: false,
		Journal: []string{
			"srcds_run: Segmentation fault (core dumped)",
			"srcds_run: Server restart in 10 seconds",
		},
	})

	titles := make(map[string]bool)
	for _, hint := range hints {
		titles[hint.Title] = true
		if hint.Level == "" {
			t.Fatalf("hint %q has empty level", hint.Title)
		}
	}
	for _, want := range []string{"服务端在崩溃自拉起循环", "游戏进程发生段错误"} {
		if !titles[want] {
			t.Fatalf("missing hint %q, got %+v", want, titles)
		}
	}
}

func TestBuildServerDiagnosticHintsMentionsCorruptVPK(t *testing.T) {
	originalPath := consts.AddonsBasePath
	addons := t.TempDir()
	consts.AddonsBasePath = addons
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	if err := os.WriteFile(
		filepath.Join(addons, "bad.vpk"),
		[]byte{0x34, 0x12, 0xaa, 0x55, 1, 0, 0, 0, 0, 0, 0, 0},
		0644,
	); err != nil {
		t.Fatalf("write bad vpk: %v", err)
	}

	hints := BuildServerDiagnosticHints(ServerDiagnostics{Port: 12138})
	if len(hints) == 0 || hints[0].Title != "发现损坏的地图 VPK" {
		t.Fatalf("expected corrupt VPK hint first, got %+v", hints)
	}
	if hints[0].Level != diagnosticsLevelError {
		t.Fatalf("corrupt VPK hint level = %q, want error", hints[0].Level)
	}
}

func TestBuildServerDiagnosticHintsIgnoresSteamClientWarning(t *testing.T) {
	originalPath := consts.AddonsBasePath
	consts.AddonsBasePath = t.TempDir()
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: true,
		Journal: []string{
			"srcds_run[15257]: /home/ubuntu/.steam/sdk32/steamclient.so: cannot open shared object file: No such file or directory",
			"srcds_run[84418]: error: libcurl.so: cannot open shared object file: No such file or directory",
		},
	})
	for _, hint := range hints {
		if hint.Title == "启动文件缺失" {
			t.Fatalf("steamclient.so 告警不应触发启动文件缺失: %+v", hints)
		}
	}
}

func TestBuildServerDiagnosticHintsDetectsMissingSrcdsBinary(t *testing.T) {
	originalPath := consts.AddonsBasePath
	consts.AddonsBasePath = t.TempDir()
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })

	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: false,
		Journal: []string{
			"l4d2.service: Failed to execute /home/ubuntu/steamcmd/l4d2/srcds_run: No such file or directory",
		},
	})
	for _, hint := range hints {
		if hint.Title == "启动文件缺失" {
			return
		}
	}
	t.Fatalf("缺少 srcds 可执行文件时应提示启动文件缺失: %+v", hints)
}
