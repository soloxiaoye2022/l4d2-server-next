package runtime

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// 默认安装根目录。相对路径一律以它为基准（设计文档 §9.5.6 / C7）。
const (
	defaultInstallRootLinux = "/data/l4d2"
	defaultInstallRootWin   = "l4d2"
)

// DefaultAllowedRoots 是 Agent 允许写入的根目录白名单默认值。
func DefaultAllowedRoots() []string {
	if os.PathSeparator == '\\' {
		return []string{}
	}
	return []string{"/home", "/data", "/opt", "/srv"}
}

// InstallRoot 返回安装根目录：优先环境变量，其次平台默认值。
func InstallRoot() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_INSTALL_ROOT")); value != "" {
		return value
	}
	if os.PathSeparator == '\\' {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, defaultInstallRootWin)
		}
		return defaultInstallRootWin
	}
	return defaultInstallRootLinux
}

// DefaultInstallDir 生成安装目录默认值（设计文档 §9.5.6 优先级链的兜底项）。
func DefaultInstallDir() string {
	return joinStyle(InstallRoot(), "l4d2-new")
}

// ResolveInstallPath 把用户输入解析成宿主机绝对路径。
//
// 规则（顺序即优先级）：
//  1. 空 → 默认值
//  2. ~ 开头 → 展开为 home
//  3. POSIX 绝对 / Windows 盘符 → 原样 realpath
//  4. 其他相对路径 → 以 InstallRoot 为基准拼接
//
// 禁止用面板进程 cwd 作基准：容器 cwd 与宿主机无关。
func ResolveInstallPath(raw string, home string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultInstallDir(), nil
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("安装目录不能包含换行或空字符")
	}

	switch {
	case strings.HasPrefix(value, "~"):
		if home == "" {
			return "", fmt.Errorf("无法展开 ~：未提供目标用户家目录")
		}
		rest := strings.TrimPrefix(value, "~")
		rest = strings.TrimPrefix(rest, "/")
		rest = strings.TrimPrefix(rest, `\`)
		if rest == "" {
			value = home
		} else {
			value = joinStyle(home, rest)
		}
	case IsAbsolutePath(value):
		// 已经是绝对路径，保持原样。
	default:
		value = joinStyle(InstallRoot(), value)
	}

	if !IsCleanPath(value) {
		return "", fmt.Errorf("安装目录不能包含 . 或 .. 段")
	}
	resolved := cleanStyle(value)
	if isRootOnly(resolved) {
		return "", fmt.Errorf("安装目录不能是根目录本身，请至少下探一层，例如 %s", DefaultInstallDir())
	}
	return resolved, nil
}

// CheckAllowedRoot 校验目标路径落在白名单根目录内。
// 白名单为空表示不限制（Windows 原生部署）。
func CheckAllowedRoot(target string, allowed []string) error {
	if len(allowed) == 0 {
		return nil
	}
	target = cleanStyle(target)
	for _, root := range allowed {
		root = cleanStyle(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		if target == root {
			return fmt.Errorf("目标目录不能是白名单根目录本身: %s", root)
		}
		if strings.HasPrefix(target, strings.TrimSuffix(root, "/")+"/") {
			return nil
		}
	}
	sorted := make([]string, len(allowed))
	copy(sorted, allowed)
	sort.Strings(sorted)
	return fmt.Errorf("目标目录必须在允许的根目录内（%s）", strings.Join(sorted, "、"))
}

// JoinStyle 按基准路径的风格拼接，供外部调用。
func JoinStyle(base string, elems ...string) string {
	return joinStyle(base, elems...)
}

func joinStyle(base string, elems ...string) string {
	if strings.HasPrefix(base, "/") {
		parts := append([]string{base}, elems...)
		return path.Join(parts...)
	}
	parts := append([]string{base}, elems...)
	return filepath.Join(parts...)
}

func cleanStyle(value string) string {
	if strings.HasPrefix(value, "/") {
		return path.Clean(value)
	}
	return filepath.Clean(value)
}

func isRootOnly(value string) bool {
	if value == "/" {
		return true
	}
	// Windows 盘符根：D:\ 或 D:/
	if len(value) == 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	// 顶层系统目录不允许直接当安装目录。
	switch value {
	case "/home", "/data", "/opt", "/srv", "/usr", "/var", "/etc", "/root", "/tmp":
		return true
	}
	return false
}

// DefaultLogPath 推导默认日志文件路径，供 nohup/tmux provider 使用。
func DefaultLogPath(workingDir string, name string) string {
	if strings.TrimSpace(workingDir) == "" {
		return ""
	}
	fileName := sanitizeFileName(name)
	if fileName == "" {
		fileName = "l4d2"
	}
	return joinStyle(workingDir, "logs", fileName+".log")
}

func sanitizeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, char := range value {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			builder.WriteRune(unicode.ToLower(char))
		case char == '-' || char == '_':
			builder.WriteRune(char)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
