package logic

import (
	"strings"
	"testing"

	"l4d2-manager-next/consts"
)

func withCleanAddons(t *testing.T) {
	t.Helper()
	originalPath := consts.AddonsBasePath
	consts.AddonsBasePath = t.TempDir()
	t.Cleanup(func() { consts.AddonsBasePath = originalPath })
}

func hintTitles(hints []ServerDiagnosticHint) map[string]ServerDiagnosticHint {
	titles := make(map[string]ServerDiagnosticHint, len(hints))
	for _, hint := range hints {
		titles[hint.Title] = hint
	}
	return titles
}

func TestHintsDetectForeignPortOwner(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: true,
		PortOwners:    []PortOwner{{Command: "python3", PID: 4242}},
	})
	hint, ok := hintTitles(hints)["游戏端口被其他进程占用"]
	if !ok {
		t.Fatalf("missing foreign port owner hint, got %+v", hints)
	}
	if !strings.Contains(hint.Detail, "python3 (pid=4242)") {
		t.Fatalf("detail = %q, want the offending process listed", hint.Detail)
	}
}

func TestHintsIgnoreGamePortOwner(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: true,
		PortOwners:    []PortOwner{{Command: "srcds_linux", PID: 4242}},
	})
	if _, ok := hintTitles(hints)["游戏端口被其他进程占用"]; ok {
		t.Fatalf("srcds owner must not be reported as foreign: %+v", hints)
	}
}

func TestHintsDetectBadWorkingDirectory(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port: 12138,
		JournalRecent: []string{
			"l4d2.service: Changing to the requested working directory failed: No such file or directory",
			"l4d2.service: Failed at step CHDIR spawning /bin/mkdir: No such file or directory",
		},
	})
	if _, ok := hintTitles(hints)["工作目录不存在或不可访问"]; !ok {
		t.Fatalf("missing workdir hint, got %+v", hints)
	}
}

func TestHintsDetectBadWorkingDirectoryStatusLine(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port: 12138,
		JournalRecent: []string{
			"systemd[1]: Starting Left 4 Dead 2 dedicated server...",
			"systemd[1]: l4d2.service: Control process exited, code=exited, status=200/CHDIR",
		},
	})
	if _, ok := hintTitles(hints)["工作目录不存在或不可访问"]; !ok {
		t.Fatalf("missing workdir hint for status=200/CHDIR, got %+v", hints)
	}
}

func TestHintsDetectMissingExecutablePath(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port: 12138,
		JournalRecent: []string{
			"l4d2.service: Failed to locate executable /tmp/zz-missing-srcds_run: No such file or directory",
			"l4d2.service: Failed at step EXEC spawning /tmp/zz-missing-srcds_run: No such file or directory",
		},
	})
	hint, ok := hintTitles(hints)["启动文件缺失"]
	if !ok {
		t.Fatalf("missing exec hint, got %+v", hints)
	}
	if !strings.Contains(hint.Detail, "/tmp/zz-missing-srcds_run") {
		t.Fatalf("detail = %q, want the missing path", hint.Detail)
	}
}

func TestHintsDetectExecPermissionDenied(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port: 12138,
		JournalRecent: []string{
			"l4d2.service: Failed to locate executable /home/ubuntu/steamcmd/l4d2/srcds_run: Permission denied",
			"l4d2.service: Failed at step EXEC spawning /home/ubuntu/steamcmd/l4d2/srcds_run: Permission denied",
		},
	})
	titles := hintTitles(hints)
	hint, ok := titles["启动文件不可执行"]
	if !ok {
		t.Fatalf("missing exec permission hint, got %+v", hints)
	}
	if !strings.Contains(hint.Suggestion, "chmod") {
		t.Fatalf("suggestion = %q, want chmod advice", hint.Suggestion)
	}
	if _, duplicated := titles["文件权限不足"]; duplicated {
		t.Fatalf("generic permission hint should not duplicate exec permission: %+v", hints)
	}
}

func TestHintsIgnoreStaleJournalOutsideRecentWindow(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: true,
		Journal:       []string{"srcds_run: Permission denied", "srcds_run: Segmentation fault"},
		JournalRecent: []string{"srcds_run: normal startup"},
	})
	for _, hint := range hints {
		if hint.Title == "文件权限不足" || hint.Title == "游戏进程发生段错误" {
			t.Fatalf("stale journal leaked into hints: %+v", hints)
		}
	}
}

func TestHintsIgnoreFailuresFromPreviousAttempt(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		ServiceName:   "l4d2.service",
		Port:          12138,
		PortListening: true,
		JournalRecent: []string{
			"l4d2.service: Failed at step EXEC spawning /home/ubuntu/steamcmd/l4d2/srcds_run: Permission denied",
			"l4d2.service: Failed to locate executable /tmp/zz-missing-srcds_run: No such file or directory",
			"systemd[1]: Starting Left 4 Dead 2 dedicated server...",
			"srcds_run[100]: Game_srv.so loaded for \"Left 4 Dead 2\"",
		},
	})
	for _, hint := range hints {
		if hint.Title == "启动文件缺失" || hint.Title == "启动文件不可执行" || hint.Title == "文件权限不足" {
			t.Fatalf("failures from a previous start leaked into hints: %+v", hints)
		}
	}
}

func TestHintsDetectOOM(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: false,
		JournalRecent: []string{"kernel: Out of memory: Killed process 4242 (srcds_linux)"},
	})
	if _, ok := hintTitles(hints)["内存不足，游戏进程被系统终止"]; !ok {
		t.Fatalf("missing OOM hint, got %+v", hints)
	}
}

func TestHintsDetectFailedPlugin(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: false,
		JournalRecent: []string{
			`L 11/11/2026 - 04:32:59: [SM] Failed to load plugin "zz_bad.smx": Unable to load plugin (bad header).`,
		},
	})
	hint, ok := hintTitles(hints)["插件加载失败，可能导致服务无法启动"]
	if !ok {
		t.Fatalf("missing plugin hint, got %+v", hints)
	}
	if !strings.Contains(hint.Detail, "zz_bad.smx") {
		t.Fatalf("detail = %q, want plugin name", hint.Detail)
	}
}

func TestHintsDetectLowDisk(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:           12138,
		PortListening:  true,
		DiskFreeBytes:  100 << 20,
		DiskTotalBytes: 40 << 30,
	})
	if _, ok := hintTitles(hints)["磁盘剩余空间不足"]; !ok {
		t.Fatalf("missing disk hint, got %+v", hints)
	}
}

func TestHintsDetectBrokenUnit(t *testing.T) {
	withCleanAddons(t)
	hints := BuildServerDiagnosticHints(ServerDiagnostics{
		Port:          12138,
		PortListening: false,
		LoadState:     "not-found",
	})
	if _, ok := hintTitles(hints)["systemd 单元文件异常"]; !ok {
		t.Fatalf("missing unit hint, got %+v", hints)
	}
}

func TestServiceRepairActionsDoNotRequireBrokenVPK(t *testing.T) {
	withCleanAddons(t)
	originalData := consts.ManagerDataPath
	consts.ManagerDataPath = t.TempDir()
	t.Cleanup(func() { consts.ManagerDataPath = originalData })

	if !isRepairAction(RepairActionRestartService) || !isRepairAction(RepairActionKillPortAndRestart) {
		t.Fatal("service repair actions must be accepted")
	}
	if !isServiceRepairAction(RepairActionRestartService) || isServiceRepairAction(RepairActionQuarantine) {
		t.Fatal("service action classification is wrong")
	}
}
