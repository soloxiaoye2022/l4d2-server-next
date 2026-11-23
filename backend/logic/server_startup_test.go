package logic

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testSystemdUnit = `[Unit]
Description=Left 4 Dead 2 dedicated server

[Service]
Type=simple
User=root
WorkingDirectory=/home/ubuntu/steamcmd/l4d2
ExecStart=/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 -insecure -tickrate 100 +hostport 27015 +exec server.cfg
Restart=always

[Install]
WantedBy=multi-user.target
`

func TestParseGameServerPortSupportsHostPortForms(t *testing.T) {
	cases := map[string]int{
		`/l4d2/srcds_run -game left4dead2 +hostport 27015 +exec server.cfg`: 27015,
		`/l4d2/srcds_run +hostport "27016"`:                                 27016,
		`/l4d2/srcds_run -port 27017`:                                       27017,
		`/l4d2/srcds_run +hostport 27015 +hostport 27018`:                   27018,
		`/l4d2/srcds_run -game left4dead2`:                                  DefaultGameServerPort,
	}
	for command, want := range cases {
		if got := ParseGameServerPort(command); got != want {
			t.Errorf("ParseGameServerPort(%q) = %d, want %d", command, got, want)
		}
	}
}

func TestUpdateSystemdStartupUnitPreservesOtherFields(t *testing.T) {
	updated, err := updateSystemdStartupUnit(
		testSystemdUnit,
		"/home/ubuntu/steamcmd/l4d2",
		"/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 -insecure -tickrate 60 +hostport 28015 +exec server.cfg",
		[]string{"服务器端口：玩家连接使用的 UDP 端口", "游戏模式：合作模式"},
	)
	if err != nil {
		t.Fatalf("updateSystemdStartupUnit() error = %v", err)
	}
	if !strings.Contains(updated, "WorkingDirectory=/home/ubuntu/steamcmd/l4d2") {
		t.Fatalf("working directory was not preserved:\n%s", updated)
	}
	if !strings.Contains(updated, "+hostport 28015") {
		t.Fatalf("new ExecStart missing:\n%s", updated)
	}
	if !strings.Contains(updated, "Restart=always") || !strings.Contains(updated, "WantedBy=multi-user.target") {
		t.Fatalf("unrelated unit fields were not preserved:\n%s", updated)
	}
	if strings.Index(updated, startupCommentsBegin) > strings.Index(updated, "ExecStart=") ||
		!strings.Contains(updated, "# 服务器端口：玩家连接使用的 UDP 端口") {
		t.Fatalf("startup comments were not placed before ExecStart:\n%s", updated)
	}
}

func TestNormalizeServerStartupRejectsInvalidValues(t *testing.T) {
	invalidWorkingDirectories := []string{"", "relative/path", "/tmp/../etc", "/tmp\n/etc"}
	for _, value := range invalidWorkingDirectories {
		if _, err := normalizeSystemdWorkingDirectory(value); err == nil {
			t.Errorf("normalizeSystemdWorkingDirectory(%q) succeeded, want error", value)
		}
	}

	invalidCommands := []string{"", "srcds_run -game left4dead2", "/l4d2/srcds_run\nstatus", strings.Repeat("/x ", 1366)}
	for _, value := range invalidCommands {
		if _, err := normalizeSystemdExecStart(value); err == nil {
			t.Errorf("normalizeSystemdExecStart(%q) succeeded, want error", value)
		}
	}
}

func TestGetServerStartupConfigReadsUnitAndStatus(t *testing.T) {
	forceSystemdStartup(t)
	root := t.TempDir()
	unitPath := filepath.Join(root, "l4d2.service")
	resultPath := filepath.Join(root, "result.json")
	triggerPath := filepath.Join(root, "reload")
	if err := os.WriteFile(unitPath, []byte(testSystemdUnit), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", resultPath)
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", triggerPath)
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")

	cfg, err := GetServerStartupConfig()
	if err != nil {
		t.Fatalf("GetServerStartupConfig() error = %v", err)
	}
	if !cfg.Available || cfg.Port != 27015 || !strings.Contains(cfg.ExecStart, "+hostport 27015") {
		t.Fatalf("unexpected startup config: %+v", cfg)
	}
	if cfg.ReloadStatus != "ready" {
		t.Fatalf("reload status = %q, want ready", cfg.ReloadStatus)
	}
}

func TestUpdateServerStartupWritesUnitAndReloadTrigger(t *testing.T) {
	forceSystemdStartup(t)
	root := t.TempDir()
	unitPath := filepath.Join(root, "l4d2.service")
	resultPath := filepath.Join(root, "result.json")
	triggerPath := filepath.Join(root, "reload")
	if err := os.WriteFile(unitPath, []byte(testSystemdUnit), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", resultPath)
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", triggerPath)
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")

	cfg, err := UpdateServerStartup(UpdateServerStartupRequest{
		WorkingDirectory: "/home/ubuntu/steamcmd/l4d2",
		ExecStart:        "/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 -tickrate 60 +hostport 27016 +exec server.cfg",
		StartupComments:  []string{"服务器端口：27016"},
	})
	if err != nil {
		t.Fatalf("UpdateServerStartup() error = %v", err)
	}
	if cfg.Port != 27016 || cfg.ReloadMode != "filesystem-watch" {
		t.Fatalf("unexpected updated config: %+v", cfg)
	}
	if _, err := os.Stat(triggerPath); err != nil {
		t.Fatalf("reload trigger was not written: %v", err)
	}
	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "+hostport 27016") {
		t.Fatalf("updated unit missing new port:\n%s", content)
	}
	if !strings.Contains(string(content), "# 服务器端口：27016") {
		t.Fatalf("updated unit missing startup comment:\n%s", content)
	}
}

// 容器部署时真实 unit 是只读挂载，保存必须自动退回可写快照，
// 而不是把 "read-only file system" 抛给用户。
func TestUpdateServerStartupFallsBackToWritableSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无法用 chmod 模拟只读目录")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限限制，无法模拟只读挂载")
	}
	forceSystemdStartup(t)
	root := t.TempDir()
	readOnlyDir := filepath.Join(root, "systemd-units")
	controlDir := filepath.Join(root, "control")
	if err := os.MkdirAll(readOnlyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(controlDir, 0755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(readOnlyDir, "l4d2.service")
	if err := os.WriteFile(unitPath, []byte(testSystemdUnit), 0644); err != nil {
		t.Fatal(err)
	}
	// 目录置为不可写，模拟容器里的 :ro 挂载
	if err := os.Chmod(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0755) })

	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_CONTROL_DIR", controlDir)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", filepath.Join(controlDir, "result.json"))
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", filepath.Join(controlDir, "reload"))
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")

	cfg, err := UpdateServerStartup(UpdateServerStartupRequest{
		WorkingDirectory: "/data/l4d2-server",
		ExecStart:        "/data/l4d2-server/srcds_run -game left4dead2 -insecure -tickrate 100 +hostport 27015 +exec server.cfg",
	})
	if err != nil {
		t.Fatalf("UpdateServerStartup() error = %v", err)
	}
	if cfg.Port != 27015 || !strings.Contains(cfg.ExecStart, "+hostport 27015") {
		t.Fatalf("返回值应来自快照回读，实际: %+v", cfg)
	}
	snapshot := filepath.Join(controlDir, "l4d2.service")
	written, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("快照未生成: %v", err)
	}
	if !strings.Contains(string(written), "/data/l4d2-server/srcds_run") {
		t.Fatalf("快照内容不正确:\n%s", written)
	}
	original, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != testSystemdUnit {
		t.Fatalf("只读 unit 不应被修改:\n%s", original)
	}
}

// 快照比真实 unit 新时必须读快照，否则页面永远显示旧参数。
func TestServerStartupReadPathPrefersNewerSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("该用例依赖 Unix 路径语义")
	}
	forceSystemdStartup(t)
	root := t.TempDir()
	controlDir := filepath.Join(root, "control")
	if err := os.MkdirAll(controlDir, 0755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "l4d2.service")
	if err := os.WriteFile(unitPath, []byte(testSystemdUnit), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(unitPath, old, old); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(controlDir, "l4d2.service")
	if err := os.WriteFile(snapshot, []byte(testSystemdUnit), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_CONTROL_DIR", controlDir)

	if got := serverStartupReadPath(); got != snapshot {
		t.Fatalf("serverStartupReadPath() = %q, want %q", got, snapshot)
	}

	// 宿主机手工改 unit 后（更新），应重新以真实 unit 为准
	newer := time.Now().Add(time.Hour)
	if err := os.Chtimes(unitPath, newer, newer); err != nil {
		t.Fatal(err)
	}
	if got := serverStartupReadPath(); got != unitPath {
		t.Fatalf("serverStartupReadPath() = %q, want %q", got, unitPath)
	}
}
