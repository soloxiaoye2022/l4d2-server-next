package logic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMaintenanceTest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	unitPath := filepath.Join(root, "l4d2.service")
	unit := `[Service]
WorkingDirectory=/home/ubuntu/steamcmd/l4d2
ExecStart=/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 +hostport 27015
`
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_STARTUP_RESULT_PATH", filepath.Join(root, "startup-result.json"))
	t.Setenv("L4D2_STARTUP_RELOAD_TRIGGER", filepath.Join(root, "startup-reload"))
	t.Setenv("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")
	t.Setenv("L4D2_MAINTENANCE_CONTROL_DIR", filepath.Join(root, "control"))
	return root
}

func TestServerMaintenanceRequestAndResult(t *testing.T) {
	forceSystemdStartup(t)
	root := setupMaintenanceTest(t)
	status, err := RequestServerMaintenance(MaintenanceActionCheck)
	if err != nil {
		t.Fatalf("RequestServerMaintenance() error = %v", err)
	}
	if status.Status != "pending" || status.Action != MaintenanceActionCheck {
		t.Fatalf("unexpected pending status: %+v", status)
	}
	var request MaintenanceRequest
	data, err := os.ReadFile(filepath.Join(root, "control", "maintenance-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	result := MaintenanceResult{
		RequestID:        request.RequestID,
		Action:           MaintenanceActionCheck,
		Status:           "success",
		Message:          "更新检查完成",
		InstalledBuildID: "23990100",
		LatestBuildID:    "23990100",
		UpdateAvailable:  false,
		FinishedAt:       "2026-11-01T03:00:00Z",
	}
	resultData, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(root, "control", "maintenance-result.json"), resultData, 0644); err != nil {
		t.Fatal(err)
	}
	status, err = GetServerMaintenanceStatus()
	if err != nil {
		t.Fatalf("GetServerMaintenanceStatus() error = %v", err)
	}
	if status.Status != "success" || status.InstalledBuildID != "23990100" {
		t.Fatalf("unexpected result status: %+v", status)
	}
}

func TestConfigureSourceTVStartup(t *testing.T) {
	forceSystemdStartup(t)
	setupMaintenanceTest(t)
	config, err := ConfigureSourceTVStartup()
	if err != nil {
		t.Fatalf("ConfigureSourceTVStartup() error = %v", err)
	}
	for _, token := range []string{"-hltv", "+tv_enable 1", "+tv_port 27021"} {
		if !strings.Contains(config.ExecStart, token) {
			t.Fatalf("configured ExecStart missing %q: %s", token, config.ExecStart)
		}
	}
}
