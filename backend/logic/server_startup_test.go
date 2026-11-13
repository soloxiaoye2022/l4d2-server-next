package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
