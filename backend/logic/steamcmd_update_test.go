package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"l4d2-manager-next/consts"
)

func TestGenerateSteamCMDUpdateScript(t *testing.T) {
	root := t.TempDir()
	oldGamePath := consts.GamePath
	consts.GamePath = filepath.Join(root, "left4dead2")
	t.Cleanup(func() { consts.GamePath = oldGamePath })
	if err := os.MkdirAll(consts.GamePath, 0755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "l4d2.service")
	unit := `[Service]
WorkingDirectory=/home/ubuntu/steamcmd/l4d2
ExecStart=/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 +hostport 27015
`
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", filepath.Join(root, "result.json"))
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", filepath.Join(root, "reload"))
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")

	config, err := GenerateSteamCMDUpdateScript(GenerateSteamCMDUpdateRequest{})
	if err != nil {
		t.Fatalf("GenerateSteamCMDUpdateScript() error = %v", err)
	}
	if config.SteamDirectory != "/home/ubuntu/steamcmd" {
		t.Fatalf("steam directory = %q", config.SteamDirectory)
	}
	if config.ScriptPath != "/home/ubuntu/steamcmd/l4d2/left4dead2/update_l4d2.txt" {
		t.Fatalf("script path = %q", config.ScriptPath)
	}
	for _, expected := range []string{
		"force_install_dir /home/ubuntu/steamcmd/l4d2",
		"@sSteamCmdForcePlatformType linux",
		"@sSteamCmdForcePlatformType windows",
		"app_update 222860 validate",
	} {
		if !strings.Contains(config.ScriptContent, expected) {
			t.Fatalf("script missing %q:\n%s", expected, config.ScriptContent)
		}
	}
	localPath := filepath.Join(consts.GamePath, "update_l4d2.txt")
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != config.ScriptContent {
		t.Fatal("generated script content does not match response")
	}
}
