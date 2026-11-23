package logic

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"l4d2-manager-next/consts"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// HostnameSource 描述一个实际存在的服务器名配置来源。同一台服务器可能同时装了
// 多个服务器名插件，所以这里是列表而不是单值。
type HostnameSource struct {
	Plugin     string `json:"plugin"`
	File       string `json:"file"`
	Raw        string `json:"raw"`
	IsTemplate bool   `json:"is_template"`
	Format     string `json:"format"`
	Note       string `json:"note"`
}

// HostnameDetection 汇总一次检测的全部证据：RCON 生效值、已加载插件、配置文件。
// 面板据此展示真实生效的服务器名，避免只认单一插件的文件。
type HostnameDetection struct {
	Effective    string           `json:"effective"`
	EffectiveOK  bool             `json:"effective_ok"`
	EffectiveErr string           `json:"effective_error"`
	Plugins      []string         `json:"plugins"`
	Sources      []HostnameSource `json:"sources"`
}

type hostnameFileKind uint8

const (
	hostnameFilePlain hostnameFileKind = iota
	hostnameFileNekoCustom
	hostnameFileNested
)

type hostnameFileCandidate struct {
	rel    string
	plugin string
	format string
	note   string
	kind   hostnameFileKind
}

// 候选顺序即保存优先级：纯文本插件文件优先，模板文件最后。
// 新增插件兼容时只需在这里补一行，无需改动检测与保存主流程。
var hostnameFileCandidates = []hostnameFileCandidate{
	{
		rel:    "addons/sourcemod/configs/l4d2_hostname.txt",
		plugin: "L4D2 中文服务器名",
		note:   "纯文本文件，插件把整个文件内容当作服务器名",
		kind:   hostnameFilePlain,
	},
	{
		rel:    "addons/sourcemod/configs/l4d2_server_name.txt",
		plugin: "L4D2 服务器名",
		note:   "纯文本文件，插件把整个文件内容当作服务器名",
		kind:   hostnameFilePlain,
	},
	{
		rel:    "addons/sourcemod/configs/server_name.txt",
		plugin: "服务器名插件",
		note:   "纯文本文件，插件把整个文件内容当作服务器名",
		kind:   hostnameFilePlain,
	},
	{
		rel:    "addons/sourcemod/configs/neko_servername.txt",
		plugin: "Neko ServerName",
		note:   "纯文本文件，插件把整个文件内容当作服务器名",
		kind:   hostnameFilePlain,
	},
	{
		rel:    "addons/sourcemod/configs/hostname/hostname.txt",
		plugin: "hostname (SourceMod 服务器名)",
		format: "hostname",
		note:   "按端口分组存储，取第一个端口的 hostname 值",
		kind:   hostnameFileNested,
	},
	{
		rel:    "addons/sourcemod/data/nekocustom.cfg",
		plugin: "Neko ServerName",
		format: "ServerNameFormat",
		note:   "KeyValues 配置文件，值可能是带 {占位符} 的名字模板",
		kind:   hostnameFileNekoCustom,
	},
}

var (
	// 控制台输出形如 `"hostname" = "名字" ( def. "" )`，值里的引号以 \" 转义。
	rconHostnamePattern = regexp.MustCompile(`(?m)"hostname"\s*=\s*"((?:[^"\\]|\\.)*)"`)
	// `sm plugins list` 的行格式为 `  001 "插件名" (1.0.0) by 作者`，版本与作者可缺省。
	sourceModPluginLinePattern = regexp.MustCompile(`(?m)^\s*\d{3}\s+"([^"]+)"`)
	// 花括号里含字母才视为模板，避免把 `[4特]` 这类普通文本误判为占位符。
	hostnameTemplatePattern = regexp.MustCompile(`\{[^}]*[A-Za-z][^}]*\}`)
	keyValuesPairPattern    = regexp.MustCompile(`^\s*"([^"]+)"\s+"([^"]*)"\s*$`)
	keyValuesLinePattern    = regexp.MustCompile(`^(\s*)"([^"]+)"(\s*)"((?:[^"\\]|\\.)*)"(\s*)$`)
)

// executeHostnameRconCommand 单独留出注入点，便于在不连服务器的情况下单测检测流程。
var executeHostnameRconCommand = ExecuteRconCommand

// parseRconHostname 从 RCON 回包中取出真实生效的服务器名。
// 既支持 `"hostname" = "值" ( def. "" )`，也兼容命令直接回显裸值的情况。
func parseRconHostname(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	if match := rconHostnamePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		return unescapeKeyValuesValue(match[1])
	}
	for _, raw := range strings.Split(trimmed, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		if len(line) >= 2 && strings.HasPrefix(line, `"`) && strings.HasSuffix(line, `"`) {
			line = line[1 : len(line)-1]
		}
		if line != "" {
			return line
		}
	}
	return ""
}

// parseSourceModPluginNames 提取 `sm plugins list` 里引号中的运行时插件名。
// 只认运行时名字，不认 smx 文件名，因为改名后的插件仍会被正确识别。
func parseSourceModPluginNames(output string) []string {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	matches := sourceModPluginLinePattern.FindAllStringSubmatch(output, -1)
	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// serverNamePluginKeywords 使用完整词组而不是 "server" 这种宽泛词，
// 避免把 "Server Message System" 之类的插件误判成服务器名插件。
var serverNamePluginKeywords = []string{
	"hostname",
	"server name",
	"servername",
	"server_name",
	"服名",
	"服务器名",
	"servertitle",
	"server title",
}

func isServerNamePlugin(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	for _, keyword := range serverNamePluginKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func isHostnameTemplate(value string) bool {
	return hostnameTemplatePattern.MatchString(value)
}

func unescapeKeyValuesValue(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	replacer := strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n", `\t`, "\t")
	return replacer.Replace(value)
}

func escapeKeyValuesValue(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return replacer.Replace(value)
}

// DecodeTextContent 把可能是 GBK 的字节解码成 UTF-8。
// 服务器上的 cfg/txt 常见 GBK 编码，直接按 UTF-8 读会出现乱码。
func DecodeTextContent(raw []byte) string {
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	if utf8.Valid(raw) {
		return string(raw)
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw); err == nil {
		return string(decoded)
	}
	return string(raw)
}

// readHostnameTextFile 读取配置文本，兼容 SourceMod 插件常见的 UTF-8 与 GBK。
func readHostnameTextFile(absPath string) (string, error) {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return DecodeTextContent(raw), nil
}

// extractKeyValuesValue 从 KeyValues 文件里找第一个同名键的值。
func extractKeyValuesValue(content, key string) string {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		match := keyValuesPairPattern.FindStringSubmatch(line)
		if len(match) != 3 || !strings.EqualFold(match[1], key) {
			continue
		}
		return unescapeKeyValuesValue(match[2])
	}
	return ""
}

// extractNestedHostnameValue 读取 `"hostname" { "27015" { "hostname" "值" } }`
// 这种按端口分组的配置，返回第一个端口分组的名字。
func extractNestedHostnameValue(content string) string {
	depth := 0
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch line {
		case "{":
			depth++
			continue
		case "}":
			depth--
			if depth < 0 {
				depth = 0
			}
			continue
		}
		// 端口分组名（如 "27015"）只有单引号串，不会匹配到键值对，这里直接跳过。
		if line == "" || depth != 2 {
			continue
		}
		if match := keyValuesPairPattern.FindStringSubmatch(line); len(match) == 3 && strings.EqualFold(match[1], "hostname") {
			return unescapeKeyValuesValue(match[2])
		}
	}
	return ""
}

func detectHostnameSourceFile(candidate hostnameFileCandidate) (HostnameSource, bool) {
	absPath := filepath.Join(consts.GamePath, filepath.FromSlash(candidate.rel))
	if info, err := os.Stat(absPath); err != nil || info.IsDir() {
		return HostnameSource{}, false
	}
	content, err := readHostnameTextFile(absPath)
	if err != nil {
		return HostnameSource{}, false
	}

	var value string
	switch candidate.kind {
	case hostnameFilePlain:
		value = strings.TrimSpace(content)
	case hostnameFileNekoCustom:
		value = extractKeyValuesValue(content, candidate.format)
	case hostnameFileNested:
		value = extractNestedHostnameValue(content)
	}
	if value == "" {
		return HostnameSource{}, false
	}

	source := HostnameSource{
		Plugin: candidate.plugin,
		File:   candidate.rel,
		Raw:    value,
		Format: candidate.format,
		Note:   candidate.note,
	}
	if isHostnameTemplate(value) {
		source.IsTemplate = true
		source.Note = "这是名字模板，面板显示的是运行时展开后的实际值"
	}
	return source, true
}

// DetectHostnameFiles 只扫描配置文件，不发 RCON。保存流程用它挑选目标文件。
func DetectHostnameFiles() []HostnameSource {
	sources := make([]HostnameSource, 0, len(hostnameFileCandidates))
	for _, candidate := range hostnameFileCandidates {
		if source, ok := detectHostnameSourceFile(candidate); ok {
			sources = append(sources, source)
		}
	}
	return sources
}

func queryLoadedServerNamePlugins() []string {
	output, err := executeHostnameRconCommand("sm plugins list")
	if err != nil {
		return nil
	}
	plugins := make([]string, 0, 4)
	for _, name := range parseSourceModPluginNames(output) {
		if isServerNamePlugin(name) {
			plugins = append(plugins, name)
		}
	}
	return plugins
}

// DetectHostnameSources 汇总 RCON 生效值、已加载插件和配置文件三类证据。
// 任何一步失败都只降级为“缺少该类证据”，不中断检测。
func DetectHostnameSources() *HostnameDetection {
	detection := &HostnameDetection{
		Plugins: []string{},
		Sources: []HostnameSource{},
	}
	if output, err := executeHostnameRconCommand("hostname"); err != nil {
		detection.EffectiveErr = err.Error()
	} else {
		detection.EffectiveOK = true
		detection.Effective = parseRconHostname(output)
	}
	if plugins := queryLoadedServerNamePlugins(); len(plugins) > 0 {
		detection.Plugins = plugins
	}
	detection.Sources = DetectHostnameFiles()
	return detection
}

// ResolveHostname 按 RCON 生效值 > 非模板文件值 > 模板值 的优先级给出面板展示值，
// 顺带说明来源；三者都没有才返回错误文案。
func ResolveHostname(detection *HostnameDetection) (value, source, errMsg string) {
	if detection == nil {
		return "", "", "未检测到服务器名插件。已加载插件的名字里没有 hostname / 服务器名 相关项，也没有找到已知配置文件。"
	}
	if detection.Effective != "" {
		return detection.Effective, "rcon", ""
	}
	for _, candidate := range detection.Sources {
		if !candidate.IsTemplate && candidate.Raw != "" {
			return candidate.Raw, "file", ""
		}
	}
	for _, candidate := range detection.Sources {
		if candidate.Raw != "" {
			return candidate.Raw, "file", ""
		}
	}
	if detection.EffectiveErr != "" {
		return "", "", "未能从服务器读取生效名称（" + detection.EffectiveErr + "），也没有找到已知的服务器名配置文件。"
	}
	return "", "", "未检测到服务器名插件。已加载插件的名字里没有 hostname / 服务器名 相关项，也没有找到已知配置文件。"
}

func replaceKeyValuesValue(content, key, value string) (string, bool) {
	lines := strings.Split(content, "\n")
	replaced := false
	for index, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		match := keyValuesLinePattern.FindStringSubmatch(line)
		if len(match) != 6 || !strings.EqualFold(match[2], key) {
			continue
		}
		lines[index] = match[1] + `"` + match[2] + `"` + match[3] + `"` + escapeKeyValuesValue(value) + `"` + match[5]
		replaced = true
		break
	}
	return strings.Join(lines, "\n"), replaced
}

// replaceNestedHostnameValue 只改第一个端口分组的 hostname，返回改动行数，
// 让调用方能提示多端口服务器还有哪些分组没被更新。
func replaceNestedHostnameValue(content, value string) (string, int, bool) {
	lines := strings.Split(content, "\n")
	depth := 0
	replaced := 0
	for index, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		switch line {
		case "{":
			depth++
			continue
		case "}":
			depth--
			if depth < 0 {
				depth = 0
			}
			continue
		}
		if line == "" || depth != 2 || replaced > 0 {
			continue
		}
		match := keyValuesLinePattern.FindStringSubmatch(line)
		if len(match) != 6 || !strings.EqualFold(match[2], "hostname") {
			continue
		}
		lines[index] = match[1] + `"` + match[2] + `"` + match[3] + `"` + escapeKeyValuesValue(value) + `"` + match[5]
		replaced++
	}
	return strings.Join(lines, "\n"), replaced, replaced > 0
}

func writeHostnameConfig(absPath, content string) error {
	mode := os.FileMode(0644)
	if info, err := os.Stat(absPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(absPath, []byte(content), mode)
}

// SaveServerHostname 按候选优先级写入第一个存在的配置文件。
// 模板型配置会保留 KeyValues 结构，只替换值；纯文本文件直接整文件写入。
func SaveServerHostname(value string) (written, warning string, err error) {
	for _, candidate := range hostnameFileCandidates {
		absPath := filepath.Join(consts.GamePath, filepath.FromSlash(candidate.rel))
		if _, statErr := os.Stat(absPath); statErr != nil {
			continue
		}
		content, readErr := readHostnameTextFile(absPath)
		if readErr != nil {
			return "", "", readErr
		}
		switch candidate.kind {
		case hostnameFilePlain:
			if err = writeHostnameConfig(absPath, value); err != nil {
				return "", "", err
			}
		case hostnameFileNekoCustom:
			updated, ok := replaceKeyValuesValue(content, candidate.format, value)
			if !ok {
				// 文件里没有目标键时补一行，保证用户的修改不会静默丢失。
				updated = strings.TrimRight(content, "\r\n") + "\n" +
					`"` + candidate.format + `"` + "\t" + `"` + escapeKeyValuesValue(value) + `"` + "\n"
			}
			if err = writeHostnameConfig(absPath, updated); err != nil {
				return "", "", err
			}
		case hostnameFileNested:
			updated, replaced, ok := replaceNestedHostnameValue(content, value)
			if !ok {
				if err = writeHostnameConfig(absPath, value); err != nil {
					return "", "", err
				}
			} else {
				if err = writeHostnameConfig(absPath, updated); err != nil {
					return "", "", err
				}
				if replaced > 0 {
					warning = "只更新了第一个端口分组（" + candidate.format + " 按端口分别保存）"
				}
			}
		}
		written = candidate.rel
		break
	}

	if written == "" {
		// 没有任何已知配置文件时退回到旧插件的纯文本文件，避免保存动作什么都不做。
		written = path.Join("addons", "sourcemod", "configs", "l4d2_hostname.txt")
		absPath := filepath.Join(consts.GamePath, filepath.FromSlash(written))
		if err = writeHostnameConfig(absPath, value); err != nil {
			return "", "", err
		}
		warning = "未检测到已知的服务器名插件，已写入 L4D2 中文服务器名插件使用的 l4d2_hostname.txt"
	}

	effective, rconErr := QueryEffectiveHostname()
	if rconErr == nil && effective != "" {
		notice := "当前生效的服务器名由插件管理（实际值：" + effective + "），修改配置文件后通常需要重启服务器或等待插件刷新才会生效。"
		if warning != "" {
			warning += "；" + notice
		} else {
			warning = notice
		}
	}
	return written, warning, nil
}

// QueryEffectiveHostname 读取运行时真实生效的服务器名。
func QueryEffectiveHostname() (string, error) {
	output, err := executeHostnameRconCommand("hostname")
	if err != nil {
		return "", err
	}
	return parseRconHostname(output), nil
}
