package runtime

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LaunchSpec 是运行时无关的启动规格。任意运行时之间迁移的本质就是
// 导出 LaunchSpec，再用另一个 provider 导入（设计文档 §5.2）。
type LaunchSpec struct {
	WorkingDir      string            `json:"working_dir"`
	Executable      string            `json:"executable"`
	Args            []string          `json:"args"`
	Env             map[string]string `json:"env,omitempty"`
	User            string            `json:"user,omitempty"`
	Port            int               `json:"port"`
	RCONAddress     string            `json:"rcon_address,omitempty"`
	RCONPasswordRef string            `json:"rcon_password_ref,omitempty"`
	AutoRestart     bool              `json:"auto_restart"`
	StopSignal      string            `json:"stop_signal,omitempty"`
	LogPath         string            `json:"log_path,omitempty"`
}

// CommandLine 把可执行文件与参数还原成一条可展示、可写入 systemd 的命令行。
// 含空白的参数会被双引号包裹，保证回读时能重新分词。
func (s LaunchSpec) CommandLine() string {
	parts := make([]string, 0, len(s.Args)+1)
	if s.Executable != "" {
		parts = append(parts, quoteArgument(s.Executable))
	}
	for _, arg := range s.Args {
		parts = append(parts, quoteArgument(arg))
	}
	return strings.Join(parts, " ")
}

// Normalize 校验并规范化启动规格。返回的错误直接展示给用户，
// 因此每条都必须说明"哪个字段、为什么不行"。
func (s LaunchSpec) Normalize() (LaunchSpec, error) {
	normalized := s
	normalized.WorkingDir = strings.TrimSpace(s.WorkingDir)
	normalized.Executable = strings.TrimSpace(s.Executable)
	normalized.User = strings.TrimSpace(s.User)
	normalized.StopSignal = strings.ToUpper(strings.TrimSpace(s.StopSignal))

	if err := validateAbsolutePath("工作目录", normalized.WorkingDir); err != nil {
		return LaunchSpec{}, err
	}
	if err := validateAbsolutePath("可执行文件", normalized.Executable); err != nil {
		return LaunchSpec{}, err
	}
	if len(normalized.Args) > 256 {
		return LaunchSpec{}, fmt.Errorf("启动参数不能超过 256 个")
	}
	args := make([]string, 0, len(normalized.Args))
	for _, arg := range normalized.Args {
		if utf8.RuneCountInString(arg) > 4096 {
			return LaunchSpec{}, fmt.Errorf("单个启动参数不能超过 4096 个字符")
		}
		if strings.ContainsAny(arg, "\r\n\x00") {
			return LaunchSpec{}, fmt.Errorf("启动参数不能包含换行或空字符")
		}
		args = append(args, arg)
	}
	normalized.Args = args

	if normalized.User != "" && !ValidUserName(normalized.User) {
		return LaunchSpec{}, fmt.Errorf("运行用户无效，只能包含小写字母、数字、下划线和连字符")
	}
	if normalized.Port < 0 || normalized.Port > 65535 {
		return LaunchSpec{}, fmt.Errorf("端口必须在 1-65535 之间")
	}
	if normalized.StopSignal == "" {
		normalized.StopSignal = "TERM"
	}
	if normalized.LogPath != "" {
		if err := validateAbsolutePath("日志路径", normalized.LogPath); err != nil {
			return LaunchSpec{}, err
		}
	}
	if len(normalized.Env) > 64 {
		return LaunchSpec{}, fmt.Errorf("环境变量不能超过 64 项")
	}
	for key, value := range normalized.Env {
		if !validEnvName(key) {
			return LaunchSpec{}, fmt.Errorf("环境变量名无效: %s", key)
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return LaunchSpec{}, fmt.Errorf("环境变量 %s 的值不能包含换行或空字符", key)
		}
	}
	return normalized, nil
}

// ParseCommandLine 把一条启动命令分词成 LaunchSpec 的 Executable + Args。
// 规则与前端一致：双引号分组、反斜杠转义、单引号原样保留。
func ParseCommandLine(command string) (string, []string, error) {
	tokens, err := SplitCommand(command)
	if err != nil {
		return "", nil, err
	}
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("启动命令不能为空")
	}
	return tokens[0], tokens[1:], nil
}

// SplitCommand 按 shell 风格分词，但不做任何变量展开与通配符处理。
func SplitCommand(command string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false
	started := false

	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}

	for _, char := range command {
		switch {
		case escaped:
			current.WriteRune(char)
			started = true
			escaped = false
		case char == '\\' && quote != '\'':
			escaped = true
			started = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			started = true
		case char == '"' || char == '\'':
			quote = char
			started = true
		case unicode.IsSpace(char):
			flush()
		default:
			current.WriteRune(char)
			started = true
		}
	}

	if escaped {
		return nil, fmt.Errorf("启动命令以未完成的转义符结尾")
	}
	if quote != 0 {
		return nil, fmt.Errorf("启动命令包含未闭合的引号")
	}
	flush()
	return tokens, nil
}

func quoteArgument(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"'\\") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func validateAbsolutePath(label, value string) error {
	if value == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if utf8.RuneCountInString(value) > 1024 {
		return fmt.Errorf("%s不能超过 1024 个字符", label)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%s不能包含换行或空字符", label)
	}
	if !IsAbsolutePath(value) {
		return fmt.Errorf("%s必须是绝对路径", label)
	}
	if !IsCleanPath(value) {
		return fmt.Errorf("%s必须是规范化路径，不能包含 . 或 .. 段", label)
	}
	return nil
}

// IsAbsolutePath 同时接受 POSIX 绝对路径与 Windows 盘符/UNC 路径。
// 面板可能跑在 Windows 上管理 Linux 路径，反之亦然。
func IsAbsolutePath(value string) bool {
	if strings.HasPrefix(value, "/") {
		return true
	}
	if len(value) >= 3 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return strings.HasPrefix(value, `\\`)
}

// IsCleanPath 拒绝含 . 或 .. 段的路径，防止越权写到白名单之外。
func IsCleanPath(value string) bool {
	separator := "/"
	if !strings.HasPrefix(value, "/") {
		separator = `\`
	}
	segments := strings.FieldsFunc(value, func(r rune) bool {
		return string(r) == "/" || string(r) == `\`
	})
	_ = separator
	for _, segment := range segments {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// ValidUserName 限定 Agent 可以 runuser 切换的目标用户。
func ValidUserName(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9':
		case char == '_' || char == '-':
		case char == '.':
		default:
			return false
		}
		if index == 0 && char >= '0' && char <= '9' {
			return false
		}
	}
	return true
}

func validEnvName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		switch {
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
		case char == '_':
		case index > 0 && char >= '0' && char <= '9':
		default:
			return false
		}
	}
	return true
}
