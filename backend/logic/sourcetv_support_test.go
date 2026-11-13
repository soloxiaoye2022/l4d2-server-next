package logic

import (
	"os"
	"path/filepath"
	"testing"

	"l4d2-manager-next/consts"
)

func TestInstallSourceTVSupport(t *testing.T) {
	root := t.TempDir()
	oldGamePath := consts.GamePath
	oldDataPath := consts.ManagerDataPath
	oldConfigPath := consts.ManagerConfigPath
	consts.GamePath = filepath.Join(root, "left4dead2")
	consts.ManagerDataPath = filepath.Join(root, "data")
	consts.ManagerConfigPath = filepath.Join(consts.ManagerDataPath, "manager_config.json")
	t.Cleanup(func() {
		consts.GamePath = oldGamePath
		consts.ManagerDataPath = oldDataPath
		consts.ManagerConfigPath = oldConfigPath
	})

	if err := os.MkdirAll(filepath.Join(consts.GamePath, "addons", "sourcemod"), 0755); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(root, "l4d2.service")
	unit := `[Service]
WorkingDirectory=/opt/steamcmd/l4d2
ExecStart=/opt/steamcmd/l4d2/srcds_run -game left4dead2 -hltv +tv_enable 1 +tv_port 27021
`
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", filepath.Join(root, "result.json"))
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", filepath.Join(root, "reload"))
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")

	before, err := GetSourceTVSupportStatus()
	if err != nil {
		t.Fatalf("GetSourceTVSupportStatus() error = %v", err)
	}
	if !before.SourceModPresent || before.Installed || len(before.MissingFiles) == 0 {
		t.Fatalf("unexpected status before install: %+v", before)
	}

	after, err := InstallSourceTVSupport()
	if err != nil {
		t.Fatalf("InstallSourceTVSupport() error = %v", err)
	}
	if !after.Installed || len(after.MissingFiles) != 0 || len(after.ModifiedFiles) != 0 {
		t.Fatalf("unexpected status after install: %+v", after)
	}
	if !after.StartupBroadcastEnabled || !after.StartupTVEnabled || after.StartupTVPort != 27021 {
		t.Fatalf("startup SourceTV flags not detected: %+v", after)
	}
	if !after.RestartRequired {
		t.Fatal("restart should be required after first install")
	}
}
