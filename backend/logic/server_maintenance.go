package logic

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MaintenanceActionCheck  = "check"
	MaintenanceActionUpdate = "update"
)

type MaintenanceRequest struct {
	RequestID   string `json:"request_id"`
	Action      string `json:"action"`
	SteamDir    string `json:"steam_dir"`
	InstallDir  string `json:"install_dir"`
	ServiceName string `json:"service_name"`
	RequestedAt string `json:"requested_at"`
}

type MaintenanceResult struct {
	RequestID        string `json:"request_id"`
	Action           string `json:"action"`
	Status           string `json:"status"`
	Message          string `json:"message"`
	InstalledBuildID string `json:"installed_build_id"`
	LatestBuildID    string `json:"latest_build_id"`
	UpdateAvailable  bool   `json:"update_available"`
	LogTail          string `json:"log_tail"`
	FinishedAt       string `json:"finished_at"`
}

type ServerMaintenanceStatus struct {
	Action           string `json:"action"`
	Status           string `json:"status"`
	Message          string `json:"message"`
	InstalledBuildID string `json:"installed_build_id"`
	LatestBuildID    string `json:"latest_build_id"`
	UpdateAvailable  bool   `json:"update_available"`
	LogTail          string `json:"log_tail"`
	FinishedAt       string `json:"finished_at"`
	RequestedAt      string `json:"requested_at,omitempty"`
	SteamDirectory   string `json:"steam_directory"`
	InstallDirectory string `json:"install_directory"`
	ServiceName      string `json:"service_name"`
}

func maintenanceControlDir() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_MAINTENANCE_CONTROL_DIR")); value != "" {
		return value
	}
	return "/control"
}

func maintenanceRequestPath() string {
	return filepath.Join(maintenanceControlDir(), "maintenance-request.json")
}

func maintenanceResultPath() string {
	return filepath.Join(maintenanceControlDir(), "maintenance-result.json")
}

func GetServerMaintenanceStatus() (ServerMaintenanceStatus, error) {
	startup, err := GetServerStartupConfig()
	if err != nil {
		return ServerMaintenanceStatus{}, err
	}
	status := ServerMaintenanceStatus{
		Status:           "idle",
		Message:          "尚未检查更新",
		SteamDirectory:   path.Dir(startup.WorkingDirectory),
		InstallDirectory: startup.WorkingDirectory,
		ServiceName:      startup.ServiceName,
	}
	if startup.WorkingDirectory == "" {
		return status, nil
	}

	request, requestErr := readMaintenanceRequest()
	if requestErr != nil {
		if !os.IsNotExist(requestErr) {
			return status, requestErr
		}
		return status, nil
	}
	status.RequestedAt = request.RequestedAt
	status.Action = request.Action
	status.Status = "pending"
	status.Message = "等待宿主机执行维护任务"
	status.SteamDirectory = request.SteamDir
	status.InstallDirectory = request.InstallDir
	status.ServiceName = request.ServiceName

	result, resultErr := readMaintenanceResult()
	if resultErr == nil && result.RequestID == request.RequestID {
		status.Action = result.Action
		status.Status = result.Status
		status.Message = result.Message
		status.InstalledBuildID = result.InstalledBuildID
		status.LatestBuildID = result.LatestBuildID
		status.UpdateAvailable = result.UpdateAvailable
		status.LogTail = result.LogTail
		status.FinishedAt = result.FinishedAt
		return status, nil
	}
	if resultErr != nil && !os.IsNotExist(resultErr) {
		return status, resultErr
	}
	requestedAt, parseErr := time.Parse(time.RFC3339, request.RequestedAt)
	if parseErr == nil && time.Since(requestedAt) > 20*time.Minute {
		status.Status = "error"
		status.Message = "维护任务超时，未收到宿主机执行结果"
	}
	return status, nil
}

func RequestServerMaintenance(action string) (ServerMaintenanceStatus, error) {
	if action != MaintenanceActionCheck && action != MaintenanceActionUpdate {
		return ServerMaintenanceStatus{}, fmt.Errorf("不支持的维护操作")
	}
	startup, err := GetServerStartupConfig()
	if err != nil {
		return ServerMaintenanceStatus{}, err
	}
	if !startup.Available || startup.WorkingDirectory == "" {
		return ServerMaintenanceStatus{}, fmt.Errorf("启动项中的游戏安装目录不可用")
	}
	request := MaintenanceRequest{
		RequestID:   uuid.NewString(),
		Action:      action,
		SteamDir:    path.Dir(startup.WorkingDirectory),
		InstallDir:  startup.WorkingDirectory,
		ServiceName: startup.ServiceName,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return ServerMaintenanceStatus{}, err
	}
	if err := writeManagerFileAtomic(maintenanceRequestPath(), data, 0600); err != nil {
		return ServerMaintenanceStatus{}, fmt.Errorf("提交维护任务失败: %w", err)
	}
	return GetServerMaintenanceStatus()
}

func readMaintenanceRequest() (MaintenanceRequest, error) {
	var request MaintenanceRequest
	data, err := os.ReadFile(maintenanceRequestPath())
	if err != nil {
		return request, err
	}
	err = json.Unmarshal(data, &request)
	return request, err
}

func readMaintenanceResult() (MaintenanceResult, error) {
	var result MaintenanceResult
	data, err := os.ReadFile(maintenanceResultPath())
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}
