package logic

import (
	"encoding/json"
	"fmt"
	"l4d2-manager-next/consts"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const DefaultMapHotReloadCommand = "update_addon_paths; mission_reload"
const SteamCDNHost = "cdn.steamusercontent.com"

type ManagerConfig struct {
	EnableSelfService    bool            `json:"enable_self_service"`
	LastSelfServiceTime  time.Time       `json:"last_self_service_time"`
	EnablePlayerStats    bool            `json:"enable_player_stats"`
	EnableMonitorHistory bool            `json:"enable_monitor_history"`
	EnableVPKTrim        bool            `json:"enable_vpk_trim"`
	MapImportPolicy      MapImportPolicy `json:"map_import_policy"`
	MapHotReloadCommand  string          `json:"map_hot_reload_command"`
	SteamCDNIP           string          `json:"steam_cdn_ip,omitempty"`
	SteamAPIKey          string          `json:"steam_api_key,omitempty"`
	// DiskUsageLimitPercent 是地图上传与下载任务允许的磁盘使用率上限。
	DiskUsageLimitPercent int `json:"disk_usage_limit_percent"`
	// MapCleanup 是地图自动清理设置，由 map_cleanup.go 读取与写入。
	MapCleanup MapCleanupSettings `json:"map_cleanup"`
}

var (
	managerConfig          *ManagerConfig
	managerConfigMutex     sync.RWMutex
	managerConfigLoadError error
)

func init() {
	LoadManagerConfig()
}

func LoadManagerConfig() {
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()

	managerConfig = &ManagerConfig{
		EnableSelfService:     false,
		EnablePlayerStats:     true,
		EnableMonitorHistory:  true,
		EnableVPKTrim:         true,
		MapHotReloadCommand:   DefaultMapHotReloadCommand,
		SteamCDNIP:            "",
		DiskUsageLimitPercent: DefaultDiskUsageLimitPercent,
		MapCleanup:            MapCleanupSettings{RetentionDays: DefaultMapCleanupRetentionDays},
	}
	managerConfigLoadError = nil

	if _, err := os.Stat(consts.ManagerConfigPath); os.IsNotExist(err) {
		return
	}

	data, err := os.ReadFile(consts.ManagerConfigPath)
	if err != nil {
		managerConfigLoadError = fmt.Errorf("读取面板配置失败: %w", err)
		return
	}

	// 先反序列化到副本，校验通过才发布。半解析的配置不能让破坏性任务生效。
	loaded := *managerConfig
	if err := json.Unmarshal(data, &loaded); err != nil {
		managerConfigLoadError = fmt.Errorf("解析面板配置失败: %w", err)
		return
	}
	if err := validateStoredMapCleanupConfig(data, loaded.MapCleanup); err != nil {
		managerConfigLoadError = err
		return
	}
	managerConfig = &loaded

	if !IsDiskUsageLimitPercentValid(managerConfig.DiskUsageLimitPercent) {
		managerConfig.DiskUsageLimitPercent = DefaultDiskUsageLimitPercent
	}

	normalizedIP, err := NormalizeSteamCDNIP(managerConfig.SteamCDNIP)
	if err != nil {
		managerConfig.SteamCDNIP = ""
		return
	}
	managerConfig.SteamCDNIP = normalizedIP
}

func saveManagerConfig() error {
	return persistManagerConfig(managerConfig)
}

// persistManagerConfig 写入完整的候选配置，成功之前不发布给读者。
// 调用方持有 managerConfigMutex，写盘成功后再替换 managerConfig。
func persistManagerConfig(candidate *ManagerConfig) error {
	data, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		return err
	}
	if err := consts.EnsureManagerDataPath(); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(consts.ManagerConfigPath), ".manager-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	// manager_config.json 里可能存有 Steam API Key，权限固定 0600。
	mode := os.FileMode(0600)
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary.Name(), consts.ManagerConfigPath)
}

func GetSelfServiceConfig() ManagerConfig {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	config := *managerConfig
	// Steam API keys are exposed only through the dedicated masked status API.
	config.SteamAPIKey = ""
	return config
}

func SetSelfServiceEnable(enable bool) error {
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	previous := managerConfig.EnableSelfService
	managerConfig.EnableSelfService = enable
	if err := saveManagerConfig(); err != nil {
		managerConfig.EnableSelfService = previous
		return err
	}
	return nil
}

func IsPlayerStatsEnabled() bool {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	return managerConfig.EnablePlayerStats
}

func SetPlayerStatsEnable(enable bool) error {
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	managerConfig.EnablePlayerStats = enable
	return saveManagerConfig()
}

func IsMonitorHistoryEnabled() bool {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	return managerConfig.EnableMonitorHistory
}

func SetMonitorHistoryEnable(enable bool) error {
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	managerConfig.EnableMonitorHistory = enable
	return saveManagerConfig()
}

func IsVPKTrimEnabled() bool {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	return managerConfig.EnableVPKTrim
}

func SetVPKTrimEnable(enable bool) error {
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	managerConfig.EnableVPKTrim = enable
	return saveManagerConfig()
}

func GetMapHotReloadCommand() string {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	if strings.TrimSpace(managerConfig.MapHotReloadCommand) == "" {
		return DefaultMapHotReloadCommand
	}
	return managerConfig.MapHotReloadCommand
}

func IsMapHotReloadCommandDefault() bool {
	return GetMapHotReloadCommand() == DefaultMapHotReloadCommand
}

func NormalizeMapHotReloadCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return DefaultMapHotReloadCommand, nil
	}
	if utf8.RuneCountInString(command) > 512 {
		return "", fmt.Errorf("热重载命令不能超过 512 个字符")
	}
	if strings.ContainsAny(command, "\r\n\x00") {
		return "", fmt.Errorf("热重载命令不能包含换行或空字符")
	}
	return command, nil
}

func SetMapHotReloadCommand(command string) (string, error) {
	normalized, err := NormalizeMapHotReloadCommand(command)
	if err != nil {
		return "", err
	}

	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	managerConfig.MapHotReloadCommand = normalized
	return normalized, saveManagerConfig()
}

func GetSteamCDNIP() string {
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	return managerConfig.SteamCDNIP
}

func NormalizeSteamCDNIP(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return "", fmt.Errorf("Steam CDN 指定 IP 格式无效")
	}

	return addr.Unmap().String(), nil
}

func SetSteamCDNIP(value string) (string, error) {
	normalized, err := NormalizeSteamCDNIP(value)
	if err != nil {
		return "", err
	}

	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	managerConfig.SteamCDNIP = normalized
	return normalized, saveManagerConfig()
}

type SteamAPIKeyStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	MaskedKey  string `json:"masked_key"`
}

func NormalizeSteamAPIKey(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", nil
	}
	if len(normalized) != 32 {
		return "", fmt.Errorf("Steam API Key 必须为 32 位十六进制字符")
	}
	for _, char := range normalized {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return "", fmt.Errorf("Steam API Key 只能包含 0-9 和 A-F")
		}
	}
	return normalized, nil
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func GetSteamAPIKey() string {
	if value := strings.TrimSpace(os.Getenv("STEAM_API_KEY")); value != "" {
		return value
	}
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	if managerConfig == nil {
		return ""
	}
	return strings.TrimSpace(managerConfig.SteamAPIKey)
}

func GetSteamAPIKeyStatus() SteamAPIKeyStatus {
	if value := strings.TrimSpace(os.Getenv("STEAM_API_KEY")); value != "" {
		return SteamAPIKeyStatus{Configured: true, Source: "environment", MaskedKey: maskSecret(value)}
	}
	managerConfigMutex.RLock()
	defer managerConfigMutex.RUnlock()
	if managerConfig == nil || managerConfig.SteamAPIKey == "" {
		return SteamAPIKeyStatus{Source: "none"}
	}
	return SteamAPIKeyStatus{
		Configured: true,
		Source:     "panel",
		MaskedKey:  maskSecret(managerConfig.SteamAPIKey),
	}
}

func SetSteamAPIKey(value string) (string, error) {
	normalized, err := NormalizeSteamAPIKey(value)
	if err != nil {
		return "", err
	}
	managerConfigMutex.Lock()
	defer managerConfigMutex.Unlock()
	previous := managerConfig.SteamAPIKey
	managerConfig.SteamAPIKey = normalized
	if err := saveManagerConfig(); err != nil {
		managerConfig.SteamAPIKey = previous
		return "", err
	}
	return normalized, nil
}
