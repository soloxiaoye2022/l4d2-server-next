package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"l4d2-manager-next/consts"
	"os"
	"path/filepath"
	"strings"
)

type SteamCMDUpdateConfig struct {
	Available         bool   `json:"available"`
	SteamDirectory    string `json:"steam_directory"`
	InstallDirectory  string `json:"install_directory"`
	ScriptPath        string `json:"script_path"`
	ScriptExists      bool   `json:"script_exists"`
	ScriptSHA256      string `json:"script_sha256,omitempty"`
	ScriptContent     string `json:"script_content,omitempty"`
	GeneratedAt       string `json:"generated_at,omitempty"`
	AutoupdateEnabled bool   `json:"autoupdate_enabled"`
}

type GenerateSteamCMDUpdateRequest struct {
	SteamDirectory string `json:"steam_directory"`
}

func GetSteamCMDUpdateConfig() (SteamCMDUpdateConfig, error) {
	startup, err := GetServerStartupConfig()
	if err != nil {
		return SteamCMDUpdateConfig{}, err
	}
	config := SteamCMDUpdateConfig{
		InstallDirectory:  startup.WorkingDirectory,
		AutoupdateEnabled: containsCommandToken(startup.ExecStart, "-autoupdate"),
	}
	if startup.WorkingDirectory == "" {
		return config, nil
	}
	config.Available = true
	config.SteamDirectory = dirOfPathStyle(startup.WorkingDirectory)
	config.ScriptPath = joinPathStyle(startup.WorkingDirectory, "left4dead2", "update_l4d2.txt")
	localScriptPath := filepath.Join(consts.GamePath, "update_l4d2.txt")
	data, readErr := os.ReadFile(localScriptPath)
	if readErr == nil {
		config.ScriptExists = true
		config.ScriptContent = string(data)
		sum := sha256.Sum256(data)
		config.ScriptSHA256 = hex.EncodeToString(sum[:])
		if stat, statErr := os.Stat(localScriptPath); statErr == nil {
			config.GeneratedAt = stat.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	return config, nil
}

func containsCommandToken(command, token string) bool {
	for _, field := range strings.Fields(command) {
		if strings.Trim(field, `"'`) == token {
			return true
		}
	}
	return false
}

func GenerateSteamCMDUpdateScript(request GenerateSteamCMDUpdateRequest) (SteamCMDUpdateConfig, error) {
	startup, err := GetServerStartupConfig()
	if err != nil {
		return SteamCMDUpdateConfig{}, err
	}
	if !startup.Available || startup.WorkingDirectory == "" {
		return SteamCMDUpdateConfig{}, fmt.Errorf("启动项中的游戏安装目录不可用")
	}
	steamDirectory := strings.TrimSpace(request.SteamDirectory)
	if steamDirectory == "" {
		steamDirectory = dirOfPathStyle(startup.WorkingDirectory)
	}
	steamDirectory, err = normalizeSystemdWorkingDirectory(steamDirectory)
	if err != nil {
		return SteamCMDUpdateConfig{}, fmt.Errorf("SteamCMD 目录无效: %w", err)
	}
	scriptPath := joinPathStyle(startup.WorkingDirectory, "left4dead2", "update_l4d2.txt")
	content := fmt.Sprintf(`force_install_dir %s
login anonymous
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType windows
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860 validate
quit
`, startup.WorkingDirectory)
	if err := writeManagerFileAtomic(
		filepath.Join(consts.GamePath, "update_l4d2.txt"),
		[]byte(content),
		0644,
	); err != nil {
		return SteamCMDUpdateConfig{}, fmt.Errorf("写入 SteamCMD 更新脚本失败: %w", err)
	}
	config, err := GetSteamCMDUpdateConfig()
	if err != nil {
		return config, err
	}
	config.SteamDirectory = steamDirectory
	config.ScriptPath = scriptPath
	config.ScriptContent = content
	config.ScriptExists = true
	sum := sha256.Sum256([]byte(content))
	config.ScriptSHA256 = hex.EncodeToString(sum[:])
	return config, nil
}
