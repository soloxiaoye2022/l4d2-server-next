package logic

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"l4d2-manager-next/consts"
	sourcetvsupport "l4d2-manager-next/resources/sourcetvsupport"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const SourceTVSupportVersion = sourcetvsupport.Version

type SourceTVSupportStatus struct {
	Version                 string   `json:"version"`
	Platform                string   `json:"platform"`
	Available               bool     `json:"available"`
	Installed               bool     `json:"installed"`
	SourceModPresent        bool     `json:"sourcemod_present"`
	MissingFiles            []string `json:"missing_files"`
	ModifiedFiles           []string `json:"modified_files"`
	InstalledAt             string   `json:"installed_at,omitempty"`
	RestartRequired         bool     `json:"restart_required"`
	Loaded                  bool     `json:"loaded"`
	LoadState               string   `json:"load_state"`
	StartupBroadcastEnabled bool     `json:"startup_broadcast_enabled"`
	StartupTVEnabled        bool     `json:"startup_tv_enabled"`
	StartupTVPort           int      `json:"startup_tv_port,omitempty"`
	RecommendedTVPort       int      `json:"recommended_tv_port"`
	RepositoryURL           string   `json:"repository_url"`
	KnownLimitation         string   `json:"known_limitation"`
}

type sourceTVInstallMarker struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	InstalledAt string `json:"installed_at"`
	LoadedAt    string `json:"loaded_at,omitempty"`
}

type sourceTVPackageFile struct {
	name string
	data []byte
	hash string
}

func sourceTVSupportAsset() ([]byte, string) {
	if runtime.GOOS == "windows" {
		return sourcetvsupport.Windows, "windows"
	}
	return sourcetvsupport.Linux, "linux"
}

func sourceTVSupportMarkerPath() string {
	return filepath.Join(consts.ManagerDataPath, "sourcetvsupport-install.json")
}

func sourceTVExpectedFiles(platform string) []string {
	extension := ".so"
	if platform == "windows" {
		extension = ".dll"
	}
	return []string{
		"addons/sourcetvsupport_vsp_l4d2.vdf",
		"addons/sourcemod/extensions/sourcetvsupport.autoload",
		"addons/sourcemod/extensions/sourcetvsupport.ext.2.l4d2" + extension,
		"addons/sourcemod/gamedata/sourcetvsupport.txt",
		"addons/sourcetvsupport/bin/vsp_l4d2" + extension,
	}
}

func GetSourceTVSupportStatus() (SourceTVSupportStatus, error) {
	asset, platform := sourceTVSupportAsset()
	files, err := readSourceTVPackage(asset)
	if err != nil {
		return SourceTVSupportStatus{}, err
	}
	status := SourceTVSupportStatus{
		Version:           SourceTVSupportVersion,
		Platform:          platform,
		Available:         len(asset) > 0,
		RecommendedTVPort: 27021,
		RepositoryURL:     "https://github.com/shqke/sourcetvsupport",
		KnownLimitation:   "SourceTV 中继功能仍不完整，建议使用直连观战端口。",
		MissingFiles:      []string{},
		ModifiedFiles:     []string{},
	}
	sourcemodDir := filepath.Join(consts.GamePath, "addons", "sourcemod")
	if info, statErr := os.Stat(sourcemodDir); statErr == nil && info.IsDir() {
		status.SourceModPresent = true
	}

	for _, entry := range files {
		target := filepath.Join(consts.GamePath, filepath.FromSlash(entry.name))
		data, readErr := os.ReadFile(target)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				status.MissingFiles = append(status.MissingFiles, entry.name)
				continue
			}
			return status, fmt.Errorf("读取 SourceTV 修复文件 %s 失败: %w", entry.name, readErr)
		}
		if sha256Hex(data) != entry.hash {
			status.ModifiedFiles = append(status.ModifiedFiles, entry.name)
		}
	}
	status.Installed = len(status.MissingFiles) == 0 && len(status.ModifiedFiles) == 0
	status.LoadState = "not_installed"
	if marker, markerErr := readSourceTVInstallMarker(); markerErr == nil {
		status.InstalledAt = marker.InstalledAt
		if status.Installed {
			known, loaded := sourceTVExtensionLoaded()
			switch {
			case known && loaded:
				status.Loaded = true
				status.LoadState = "loaded"
				if marker.LoadedAt == "" {
					marker.LoadedAt = time.Now().UTC().Format(time.RFC3339)
					if data, marshalErr := json.MarshalIndent(marker, "", "  "); marshalErr == nil {
						_ = writeManagerFileAtomic(sourceTVSupportMarkerPath(), data, 0644)
					}
				}
			case known:
				status.LoadState = "not_loaded"
				status.RestartRequired = true
			default:
				status.LoadState = "unknown"
				status.RestartRequired = true
			}
		}
	}
	status.StartupBroadcastEnabled, status.StartupTVEnabled, status.StartupTVPort = inspectSourceTVStartup()
	return status, nil
}

func InstallSourceTVSupport() (SourceTVSupportStatus, error) {
	asset, platform := sourceTVSupportAsset()
	files, err := readSourceTVPackage(asset)
	if err != nil {
		return SourceTVSupportStatus{}, err
	}
	sourcemodDir := filepath.Join(consts.GamePath, "addons", "sourcemod")
	if info, statErr := os.Stat(sourcemodDir); statErr != nil || !info.IsDir() {
		return SourceTVSupportStatus{}, fmt.Errorf("未检测到 SourceMod，请先安装 SourceMod 和 Metamod:Source")
	}

	if err := backupSourceTVSupportFiles(files); err != nil {
		return SourceTVSupportStatus{}, err
	}
	for _, entry := range files {
		target := filepath.Join(consts.GamePath, filepath.FromSlash(entry.name))
		if err := writeManagerFileAtomic(target, entry.data, 0644); err != nil {
			return SourceTVSupportStatus{}, fmt.Errorf("安装 %s 失败: %w", entry.name, err)
		}
	}

	marker := sourceTVInstallMarker{
		Version:     SourceTVSupportVersion,
		Platform:    platform,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(marker, "", "  ")
	if err := writeManagerFileAtomic(sourceTVSupportMarkerPath(), data, 0644); err != nil {
		return SourceTVSupportStatus{}, fmt.Errorf("写入 SourceTV 安装记录失败: %w", err)
	}
	status, err := GetSourceTVSupportStatus()
	if err != nil {
		return status, err
	}
	status.RestartRequired = true
	status.LoadState = "not_loaded"
	return status, nil
}

func ConfigureSourceTVStartup() (ServerStartupConfig, error) {
	config, err := GetServerStartupConfig()
	if err != nil {
		return ServerStartupConfig{}, err
	}
	if !config.Available {
		return ServerStartupConfig{}, fmt.Errorf("启动项文件不可用")
	}
	tokens := tokenizeCommandLineForInspection(config.ExecStart)
	if len(tokens) == 0 {
		return ServerStartupConfig{}, fmt.Errorf("启动命令为空")
	}
	hasBroadcast := false
	tvEnabled := false
	tvPort := false
	for index, token := range tokens {
		switch token {
		case "-hltv":
			hasBroadcast = true
		case "+tv_enable":
			tvEnabled = true
			if index+1 < len(tokens) {
				tokens[index+1] = "1"
			} else {
				tokens = append(tokens, "1")
			}
		case "+tv_port":
			tvPort = true
			if index+1 >= len(tokens) {
				tokens = append(tokens, "27021")
			} else if tokens[index+1] == "27020" {
				tokens[index+1] = "27021"
			}
		}
	}
	if !hasBroadcast {
		tokens = append(tokens, "-hltv")
	}
	if !tvEnabled {
		tokens = append(tokens, "+tv_enable", "1")
	}
	if !tvPort {
		tokens = append(tokens, "+tv_port", "27021")
	}
	comments := append([]string{}, config.StartupComments...)
	if !containsStartupComment(comments, "SourceTV") {
		comments = append(comments,
			"SourceTV 广播：启用 L4D2 SourceTV 观战和录像",
			"SourceTV 端口：固定使用 27021，避免与 27020 管理面板冲突",
		)
	}
	return UpdateServerStartup(UpdateServerStartupRequest{
		WorkingDirectory: config.WorkingDirectory,
		ExecStart:        strings.Join(tokens, " "),
		StartupComments:  comments,
	})
}

func containsStartupComment(comments []string, text string) bool {
	for _, comment := range comments {
		if strings.Contains(comment, text) {
			return true
		}
	}
	return false
}

func sourceTVExtensionLoaded() (bool, bool) {
	if strings.TrimSpace(os.Getenv("L4D2_RCON_URL")) == "" ||
		strings.TrimSpace(os.Getenv("L4D2_RCON_PASSWORD")) == "" {
		return false, false
	}
	output, err := ExecuteRconCommand("sm exts list")
	if err != nil {
		return false, false
	}
	return true, strings.Contains(strings.ToLower(output), "sourcetvsupport")
}

func readSourceTVPackage(data []byte) ([]sourceTVPackageFile, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("SourceTV 修复包不可用")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("打开 SourceTV 修复包失败: %w", err)
	}
	expected := map[string]struct{}{}
	for _, name := range sourceTVExpectedFiles(runtimePlatformName()) {
		expected[name] = struct{}{}
	}
	files := make([]sourceTVPackageFile, 0, len(expected))
	var total int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if _, ok := expected[name]; !ok {
			continue
		}
		if entry.UncompressedSize64 > 16<<20 {
			return nil, fmt.Errorf("SourceTV 修复文件过大: %s", name)
		}
		total += int64(entry.UncompressedSize64)
		if total > 32<<20 {
			return nil, fmt.Errorf("SourceTV 修复包解压后超过限制")
		}
		readerAt, openErr := entry.Open()
		if openErr != nil {
			return nil, fmt.Errorf("读取 SourceTV 修复文件 %s 失败: %w", name, openErr)
		}
		content, readErr := io.ReadAll(io.LimitReader(readerAt, 16<<20))
		closeErr := readerAt.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取 SourceTV 修复文件 %s 失败: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("关闭 SourceTV 修复文件 %s 失败: %w", name, closeErr)
		}
		files = append(files, sourceTVPackageFile{name: name, data: content, hash: sha256Hex(content)})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].name < files[right].name })
	if len(files) != len(expected) {
		return nil, fmt.Errorf("SourceTV 修复包缺少预期文件")
	}
	return files, nil
}

func backupSourceTVSupportFiles(files []sourceTVPackageFile) error {
	existing := make(map[string][]byte)
	for _, entry := range files {
		target := filepath.Join(consts.GamePath, filepath.FromSlash(entry.name))
		data, err := os.ReadFile(target)
		if err == nil {
			existing[entry.name] = data
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if len(existing) == 0 {
		return nil
	}
	backupDir := filepath.Join(consts.ManagerDataPath, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}
	backupPath := filepath.Join(
		backupDir,
		"sourcetvsupport-"+time.Now().Format("20060102-150405")+".zip",
	)
	file, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	names := make([]string, 0, len(existing))
	for name := range existing {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, createErr := writer.Create(name)
		if createErr == nil {
			_, createErr = entry.Write(existing[name])
		}
		if createErr != nil {
			writer.Close()
			file.Close()
			return createErr
		}
	}
	if err := writer.Close(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readSourceTVInstallMarker() (sourceTVInstallMarker, error) {
	var marker sourceTVInstallMarker
	data, err := os.ReadFile(sourceTVSupportMarkerPath())
	if err != nil {
		return marker, err
	}
	err = json.Unmarshal(data, &marker)
	return marker, err
}

func inspectSourceTVStartup() (bool, bool, int) {
	config, err := GetServerStartupConfig()
	if err != nil || !config.Available {
		return false, false, 0
	}
	tokens := tokenizeCommandLineForInspection(config.ExecStart)
	broadcast := false
	tvEnabled := false
	tvPort := 0
	for index, token := range tokens {
		switch token {
		case "-hltv":
			broadcast = true
		case "+tv_enable":
			if index+1 < len(tokens) && strings.Trim(tokens[index+1], `"'`) == "1" {
				tvEnabled = true
			}
		case "+tv_port":
			if index+1 < len(tokens) {
				tvPort = parsePort(strings.Trim(tokens[index+1], `"'`))
			}
		}
	}
	return broadcast, tvEnabled, tvPort
}

func tokenizeCommandLineForInspection(command string) []string {
	fields := strings.Fields(command)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens = append(tokens, strings.Trim(field, `"'`))
	}
	return tokens
}

func runtimePlatformName() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "linux"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeManagerFileAtomic(target string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".manager-file-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}
