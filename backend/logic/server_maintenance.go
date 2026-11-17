package logic

import (
	"encoding/json"
	"fmt"
	"os"
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
	return DefaultControlDir()
}

func maintenanceRequestPath() string {
	return filepath.Join(maintenanceControlDir(), "maintenance-request.json")
}

func maintenanceResultPath() string {
	return filepath.Join(maintenanceControlDir(), "maintenance-result.json")
}

func GetServerMaintenanceStatus() (ServerMaintenanceStatus, error) {
	steamDir, installDir, serviceName, targetErr := resolveMaintenanceTarget()
	status := ServerMaintenanceStatus{
		Status:           "idle",
		Message:          "尚未检查更新",
		SteamDirectory:   steamDir,
		InstallDirectory: installDir,
		ServiceName:      serviceName,
	}
	if targetErr != nil {
		// 没有任何可用的安装目录：状态页仍要能打开，只是提示未配置。
		status.Message = targetErr.Error()
		return status, nil
	}
	if installDir == "" {
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

// resolveMaintenanceTarget 解析维护任务需要的三个路径。
//
// 顺序很重要：
//  1. 有托管运行时 → 用运行时规格里的工作目录。接管 screen / tmux / docker
//     之后宿主机上根本没有 systemd unit，旧逻辑会直接报"安装目录不可用"（D4）。
//  2. 没有运行时 → 回退旧的启动项解析，保证老部署行为不变。
func resolveMaintenanceTarget() (steamDir string, installDir string, serviceName string, err error) {
	if item, ok := activeRuntime(); ok {
		workingDir := strings.TrimSpace(item.Spec.WorkingDir)
		if workingDir == "" {
			workingDir = strings.TrimSpace(item.GamePath)
		}
		if workingDir != "" {
			serviceName = runtimeServiceLabel(item)
			if item.Target.UnitName != "" {
				serviceName = item.Target.UnitName
			}
			return dirOfPathStyle(workingDir), workingDir, serviceName, nil
		}
	}

	startup, startupErr := GetServerStartupConfig()
	if startupErr != nil {
		return "", "", "", startupErr
	}
	if !startup.Available || startup.WorkingDirectory == "" {
		return "", "", "", fmt.Errorf("启动项中的游戏安装目录不可用")
	}
	return dirOfPathStyle(startup.WorkingDirectory), startup.WorkingDirectory, startup.ServiceName, nil
}

func RequestServerMaintenance(action string) (ServerMaintenanceStatus, error) {
	if action != MaintenanceActionCheck && action != MaintenanceActionUpdate {
		return ServerMaintenanceStatus{}, fmt.Errorf("不支持的维护操作")
	}
	steamDir, installDir, serviceName, err := resolveMaintenanceTarget()
	if err != nil {
		return ServerMaintenanceStatus{}, err
	}
	request := MaintenanceRequest{
		RequestID:   uuid.NewString(),
		Action:      action,
		SteamDir:    steamDir,
		InstallDir:  installDir,
		ServiceName: serviceName,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// 修复 D4：先把"当前运行时"快照写给宿主机脚本。
	// 脚本据此调用 runtime-agent 启停，而不是硬编码 systemctl。
	if _, err := WriteRuntimeControlSnapshot(); err != nil {
		return ServerMaintenanceStatus{}, err
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
