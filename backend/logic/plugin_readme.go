package logic

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"l4d2-manager-next/resources/pluginreadme"
)

// 插件说明自动生成。
//
// 上传插件时插件包里往往只有 smx，没有 README，面板就会显示"此插件无说明文件"。
// 这里根据插件目录里真实存在的文件生成一份基础说明：
//   - .smx 文件清单
//   - cfg 里的 ConVar 与中文注释（词典命中即中文，未命中保留原文并记入待翻译清单）
//   - gamedata 签名文件
// 命令章节需要插件真正运行后才能通过 `sm cmds` 拿到，因此拆成两步：
// 上传时生成基础版，启用后调用 EnrichPluginReadme 补全。

const (
	// pluginReadmeFileName 是自动生成的说明文件名。
	pluginReadmeFileName = "README.md"
	// pluginReadmeMarker 标记该文件由面板生成，便于后续覆盖而不误删用户文档。
	pluginReadmeMarker = "<!-- generated-by: l4d2-manager -->"
	// pluginReadmeMissingMarker 标记当前说明缺少命令章节。
	pluginReadmeMissingMarker = "<!-- pending: runtime-commands -->"
	// pluginReadmeLegacyFooter 是早期批量脚本生成说明的固定结尾。
	// 那时候还没有版本标记，但内容同样来自面板，必须允许被后续刷新覆盖，
	// 否则线上已有的说明会被误判成作者手写文档而永远停在旧格式。
	pluginReadmeLegacyFooter = "本说明由面板根据插件目录内的实际文件与配置自动整理"
)

var (
	// cjkPattern 判断字符串是否已包含中文。
	cjkPattern = regexp.MustCompile(`[\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}]`)
	// cvarLinePattern 匹配 cfg 里的赋值行，允许大写（如 l4d2_Kill_enabled_health）。
	cvarLinePattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]{2,})\s+(?:"[^"]*"|\S+)\s*$`)
	// cvarOptionPattern 匹配 "1: xxx" / "0=yyy" 这类枚举说明行。
	cvarOptionPattern = regexp.MustCompile(`^\d+\s*[:=]`)
	// trailingParensPattern 抓目录名里所有括号内容。
	trailingParensPattern = regexp.MustCompile(`\(([^()]*)\)`)
	// pureNumberPattern 判断括号内容是否只是版本号。
	pureNumberPattern = regexp.MustCompile(`^[\d.\-+vV ]+$`)
	// englishProsePattern 判断文本是否仍以英文句子为主。
	englishProsePattern = regexp.MustCompile(`[A-Za-z]{3,}\s+[A-Za-z]{2,}`)
	// authorNamePattern 判断括号内容是否像人名/团队名。
	authorNamePattern = regexp.MustCompile(`[A-Za-z]{2,}`)
	// chineseAuthorPattern 匹配 2-8 个汉字的人名（如「洛琪」「豆瓣酱な」）。
	chineseAuthorPattern = regexp.MustCompile(`^[\x{4e00}-\x{9fff}·]{2,10}$`)
	// descriptionHintPattern 识别括号里其实是说明而不是作者（含功能描述、使用条件、效果说明）。
	descriptionHintPattern = regexp.MustCompile(
		`(必须|需要|支持|修复|兼容|推荐|注意|可选|必选|版本|说明|用于|适配|前置|无效|无特效|特效|客户端|服务端|测试|汉化|整合|优化|增强|修改|替换|自定义)`)
	// launchArgPattern 识别括号里是启动参数而不是作者，例如
	// "-tickrate 100 +sv_setmax 31"、"游戏运行时添加无效" 这类命令行开关。
	launchArgPattern = regexp.MustCompile(`^[+-]|-tickrate|\+sv_|^[a-zA-Z_][a-zA-Z0-9_]*\s+\d`)
)

// PluginReadmeResult 描述一次说明生成的结果，便于上层展示与统计。
type PluginReadmeResult struct {
	// Written 表示是否真的写了文件。
	Written bool `json:"written"`
	// HasCommands 表示本次是否已包含运行时命令章节。
	HasCommands bool `json:"has_commands"`
	// ConvarCount 是识别到的配置项数量。
	ConvarCount int `json:"convar_count"`
	// CommandCount 是写入的命令数量。
	CommandCount int `json:"command_count"`
	// Untranslated 是词典未覆盖、仍保留英文的注释条数。
	Untranslated int `json:"untranslated"`
	// Reason 说明为什么没有写文件。
	Reason string `json:"reason,omitempty"`
}

// GeneratePluginReadme 为指定插件生成或刷新说明文件。
//
// 行为约定：
//   - 插件根目录已有用户自己写的 README.md（不含生成标记）时不动它，避免覆盖人工文档；
//   - 其余情况写入或覆盖面板生成的 README.md；
//   - 命令章节为空时写入 pending 标记，供后续 EnrichPluginReadme 判断是否需要补全。
func GeneratePluginReadme(name string) (PluginReadmeResult, error) {
	result := PluginReadmeResult{}

	pluginDir := filepath.Join(getStorePath(), name)
	if info, err := os.Stat(pluginDir); err != nil || !info.IsDir() {
		return result, fmt.Errorf("插件目录不存在: %s", name)
	}

	target := filepath.Join(pluginDir, pluginReadmeFileName)
	if existing, err := os.ReadFile(target); err == nil {
		if !isReadmeGeneratedByManager(string(existing)) {
			// 用户自带的 README，不覆盖
			result.Reason = "插件已自带 README.md，未覆盖"
			return result, nil
		}
	}

	content, result, err := buildPluginReadme(name, pluginDir, result)
	if err != nil {
		return result, err
	}

	if err := writePluginReadmeFile(target, content); err != nil {
		return result, err
	}
	result.Written = true
	return result, nil
}

// EnrichPluginReadme 在插件启用并运行后补全命令章节。
//
// 只有已存在生成版 README、且标记为 pending 时才会重新生成，
// 避免每次启用插件都重写没有变化的文件。
func EnrichPluginReadme(name string) (PluginReadmeResult, error) {
	result := PluginReadmeResult{}

	pluginDir := filepath.Join(getStorePath(), name)
	target := filepath.Join(pluginDir, pluginReadmeFileName)
	existing, err := os.ReadFile(target)
	if err != nil {
		// 没有说明文件时直接完整生成一次
		return GeneratePluginReadme(name)
	}
	text := string(existing)
	if !isReadmeGeneratedByManager(text) {
		result.Reason = "插件已自带 README.md，未覆盖"
		return result, nil
	}

	content, result, err := buildPluginReadme(name, pluginDir, result)
	if err != nil {
		return result, err
	}
	if content == text {
		result.Reason = "说明内容无变化"
		return result, nil
	}
	if err := writePluginReadmeFile(target, content); err != nil {
		return result, err
	}
	result.Written = true
	return result, nil
}

// writePluginReadmeFile 原子写入说明文件，保留已有权限。
func writePluginReadmeFile(target string, content string) error {
	mode := os.FileMode(0644)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	temporary := target + ".manager-tmp"
	if err := os.WriteFile(temporary, []byte(content), mode); err != nil {
		return fmt.Errorf("写入说明临时文件失败: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("替换说明文件失败: %w", err)
	}
	return nil
}

// buildPluginReadme 组装说明正文。所有内容来自插件目录的真实文件。
func buildPluginReadme(
	name string, pluginDir string, result PluginReadmeResult,
) (string, PluginReadmeResult, error) {
	sources, err := scanPluginReadmeSources(pluginDir)
	if err != nil {
		return "", result, err
	}
	kind := classifyPluginKind(sources)

	dictionary := pluginreadme.Translations()
	convars, untranslated := collectReadmeCvars(pluginDir, sources.cfg, dictionary)
	// 只有 SourceMod 插件才可能注册命令。内存补丁和 Metamod 扩展没有 smx，
	// 发 `sm cmds` 只会拿到 "not found"，把 pending 标记一直挂在那里。
	var scan PluginCommandScan
	if kind == pluginKindSourceMod {
		scan = queryPluginReadmeCommands(sources.smx)
	}
	commands := scan.Commands
	// 运行时查不到命令、但包里带了源码时，用源码里的 RegConsoleCmd 兜底。
	// 这样未加载的插件也能给出命令表，而不是永远停在「启用后再刷新」。
	commandFromSource := false
	// sourceChecked 表示确实读过源码并给出了确定结论（有命令或确认无命令）。
	// 只有读过源码才能断言"这个插件没有命令"，否则只能说"暂时读不到"。
	sourceChecked := false
	if len(sources.source) > 0 {
		sourceCommands := parseSourcePluginCommands(sources.source, func(rel string) ([]byte, error) {
			return os.ReadFile(filepath.Join(pluginDir, filepath.FromSlash(rel)))
		})
		sourceChecked = true
		if len(commands) == 0 && len(sourceCommands) > 0 {
			commands = sourceCommands
			commandFromSource = true
		}
	}

	result.ConvarCount = len(convars)
	result.CommandCount = len(commands)
	result.Untranslated = untranslated
	result.HasCommands = len(commands) > 0

	prefix, authors := parsePluginNameHints(name)

	var lines []string
	lines = append(lines, pluginReadmeMarker)
	lines = append(lines, "# "+name, "")
	lines = append(lines, "## 说明", "")
	lines = append(lines, describePluginKind(kind)...)
	if prefix == "必选" {
		lines = append(lines, "- 必选插件，建议保持启用")
	} else if prefix == "可选" {
		lines = append(lines, "- 可选插件，可按需启用")
	}
	if len(sources.smx) > 0 {
		lines = append(lines, fmt.Sprintf("- 包含 %d 个已编译插件（.smx）", len(sources.smx)))
	}
	if len(sources.extensions) > 0 {
		lines = append(lines, "- 附带 SourceMod 扩展，需与 .smx 一起放入 `addons/sourcemod/extensions`")
	}
	if len(sources.metamod) > 0 && kind == pluginKindMetamod {
		lines = append(lines, fmt.Sprintf("- 包含 %d 个 Metamod 扩展文件", len(sources.metamod)))
	}
	if len(sources.gamedata) > 0 {
		lines = append(lines, "- 附带 gamedata 签名，对游戏版本较敏感，升级游戏后可能需要更新")
	}
	if len(convars) > 0 {
		lines = append(lines, fmt.Sprintf("- 提供 %d 个可配置项", len(convars)))
	}
	if len(commands) > 0 {
		lines = append(lines, fmt.Sprintf("- 注册 %d 个控制台命令", len(commands)))
	}
	lines = append(lines, "")

	// 命令章节只对 SourceMod 插件有意义。
	if kind == pluginKindSourceMod {
		if len(commands) > 0 {
			lines = append(lines, buildPluginCommandSection(commands, commandFromSource)...)
		} else {
			lines = append(lines, buildPluginNoCommandSection(scan, sourceChecked)...)
		}
	}

	if len(authors) > 0 {
		lines = append(lines, "## 作者", "")
		for _, author := range authors {
			lines = append(lines, "- "+author)
		}
		lines = append(lines, "")
	}

	// 插件文件章节：smx 与补丁/扩展分别列出，让用户看清装的是什么。
	lines = append(lines, buildPluginFileSection(sources)...)

	if len(convars) > 0 {
		lines = append(lines, "## 主要配置", "")
		lines = append(lines, "| 配置项 | 说明 |")
		lines = append(lines, "| --- | --- |")
		for index, cvar := range convars {
			if index >= 25 {
				lines = append(lines, fmt.Sprintf("| … | 其余 %d 项见 cfg 文件 |", len(convars)-25))
				break
			}
			lines = append(lines, "| `"+cvar.Name+"` | "+escapeMarkdownCell(cvar.Description)+" |")
		}
		lines = append(lines, "")
	}

	if len(sources.cfg) > 0 {
		lines = append(lines, "## 配置文件", "")
		seen := map[string]bool{}
		listed := 0
		for _, rel := range sources.cfg {
			base := path.Base(rel)
			if seen[base] {
				continue
			}
			seen[base] = true
			if listed >= 8 {
				break
			}
			lines = append(lines, "- `"+base+"`")
			listed++
		}
		lines = append(lines, "")
	}

	lines = append(lines, "---", "")
	lines = append(lines, "本说明由面板根据插件目录内的实际文件与配置自动整理，未包含推测内容。")
	lines = append(lines, "")
	if untranslated > 0 {
		lines = append(lines, fmt.Sprintf("> 有 %d 条配置注释暂无中文翻译，已保留英文原文。", untranslated))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), result, nil
}

// describePluginKind 用一句话说明这个插件是什么形态、怎么生效。
//
// 这一段是给用户判断"启用后要不要重启/换图"用的，必须写清楚。
func describePluginKind(kind pluginKind) []string {
	switch kind {
	case pluginKindSourceMod:
		// SourceMod 插件是主流形态，说明由下面的文件与命令章节承担，不额外加话。
		return nil
	case pluginKindScramble:
		return []string{
			"- 这是 SourceScramble 内存补丁，不是 .smx 插件，因此不会注册任何聊天命令",
			"- 由 `sourcescramble_manager.smx` 在服务器启动时读取配置并改写内存指令",
			"- 不注册命令，也就没有聊天框用法；生效仅取决于地址签名是否匹配当前游戏版本",
		}
	case pluginKindMetamod:
		return []string{
			"- 这是 Metamod 扩展，加载层级在 SourceMod 之下，不是 .smx 插件",
			"- 由 Metamod 读取 `.vdf` 声明后加载 `.so` / `.dll`",
			"- 不注册 SourceMod 命令，也没有聊天框用法",
		}
	default:
		return []string{
			"- 目录里没有 .smx、内存补丁或 Metamod 扩展，只有说明或空壳",
			"- 作用需结合插件名理解",
		}
	}
}

// buildPluginFileSection 列出实际文件，按类型分组。
func buildPluginFileSection(sources pluginSources) []string {
	var lines []string

	appendGroup := func(title string, files []string, limit int) {
		if len(files) == 0 {
			return
		}
		lines = append(lines, "## "+title, "")
		for index, rel := range files {
			if index >= limit {
				lines = append(lines, fmt.Sprintf("- 其余 %d 个见插件目录", len(files)-limit))
				break
			}
			lines = append(lines, "- `"+path.Base(rel)+"`")
		}
		lines = append(lines, "")
	}

	appendGroup("插件文件", sources.smx, 8)
	appendGroup("SourceMod 扩展", sources.extensions, 8)
	appendGroup("内存补丁配置", sources.scramble, 8)
	// 这里装的是 Metamod 本体或扩展二进制，用「平台文件」比「扩展」准确：
	// 完整的 SourceMod 平台会同时带 metamod.vdf 和 metamod/bin/*.so。
	appendGroup("Metamod 平台文件", sources.metamod, 8)
	appendGroup("gamedata 签名", sources.gamedata, 8)

	return lines
}

// escapeMarkdownCell 转义表格里会破坏结构的竖线。
func escapeMarkdownCell(value string) string {
	return strings.ReplaceAll(value, "|", `\|`)
}

// pluginKind 是插件的落地形态，决定说明怎么写。
//
// 同叫"插件"，实际有三种完全不同的东西，不能混着写：
//   - sourcemod：有 .smx，靠 SourceMod 加载，才会注册控制台命令
//   - scramble：只有 sourcescramble 配置 + gamedata，是内存补丁，由
//     sourcescramble_manager.smx 在启动时读取并打补丁，本身不注册任何命令
//   - metamod：只有 .vdf + .so/.dll，是 Metamod 扩展，加载在 SourceMod 之下，
//     同样不注册 SourceMod 命令
//   - empty：什么都没有
type pluginKind string

const (
	pluginKindSourceMod pluginKind = "sourcemod"
	pluginKindScramble  pluginKind = "scramble"
	pluginKindMetamod   pluginKind = "metamod"
	pluginKindEmpty     pluginKind = "empty"
)

// classifyPluginKind 依据磁盘上的真实文件判断插件形态。
//
// 优先级：有 smx 就是 SourceMod 插件；否则看 sourcescramble 补丁；再看 Metamod 扩展。
func classifyPluginKind(sources pluginSources) pluginKind {
	switch {
	case len(sources.smx) > 0:
		return pluginKindSourceMod
	case len(sources.scramble) > 0:
		return pluginKindScramble
	case len(sources.metamod) > 0:
		return pluginKindMetamod
	default:
		return pluginKindEmpty
	}
}

// pluginSources 是一个插件目录里扫描到的各类文件（均为相对插件根的路径）。
type pluginSources struct {
	smx      []string
	cfg      []string
	gamedata []string
	// source 是 SourcePawn 源码（.sp / .inc），只在插件未加载时用来兜底解析命令。
	source []string
	// scramble 是 configs/sourcescramble 下的内存补丁配置。
	scramble []string
	// extensions 是 sourcemod/extensions 下的 SourceMod 扩展（.so/.dll）。
	// 它们和 smx 一起工作，不是 Metamod 扩展，不能混在一起说。
	extensions []string
	// metamod 是 addons 根下的 .vdf 声明，以及不被 SourceMod 管的 .so/.dll。
	metamod []string
}

// scanPluginReadmeSources 列出插件目录里的各类文件（相对插件根）。
func scanPluginReadmeSources(pluginDir string) (pluginSources, error) {
	var sources pluginSources

	walkErr := filepath.Walk(pluginDir, func(p string, info os.FileInfo, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(pluginDir, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		lower := strings.ToLower(rel)
		switch {
		case strings.HasSuffix(lower, ".smx"):
			sources.smx = append(sources.smx, rel)
		case strings.HasSuffix(lower, ".cfg"):
			sources.cfg = append(sources.cfg, rel)
		case strings.HasSuffix(lower, ".txt") && strings.Contains(lower, "gamedata"):
			sources.gamedata = append(sources.gamedata, rel)
		case strings.HasSuffix(lower, ".txt") && strings.Contains(lower, "sourcescramble"):
			sources.scramble = append(sources.scramble, rel)
		case strings.HasSuffix(lower, ".sp"), strings.HasSuffix(lower, ".inc"):
			sources.source = append(sources.source, rel)
		case strings.HasSuffix(lower, ".so"),
			strings.HasSuffix(lower, ".dll"),
			strings.HasSuffix(lower, ".autoload"):
			// sourcemod/extensions 下的是 SourceMod 扩展，其他位置才是 Metamod 扩展。
			if strings.Contains(lower, "addons/sourcemod/extensions/") {
				sources.extensions = append(sources.extensions, rel)
			} else {
				sources.metamod = append(sources.metamod, rel)
			}
		case strings.HasSuffix(lower, ".vdf"):
			sources.metamod = append(sources.metamod, rel)
		}
		return nil
	})
	if walkErr != nil {
		return pluginSources{}, fmt.Errorf("扫描插件目录失败: %w", walkErr)
	}
	sort.Strings(sources.smx)
	sort.Strings(sources.cfg)
	sort.Strings(sources.gamedata)
	sort.Strings(sources.source)
	sort.Strings(sources.scramble)
	sort.Strings(sources.extensions)
	sort.Strings(sources.metamod)
	return sources, nil
}

// collectReadmeCvars 解析 cfg 里的 ConVar 与说明，返回去重后的列表与未翻译条数。
func collectReadmeCvars(
	pluginDir string, cfgFiles []string, dictionary map[string]string,
) ([]CvarConfig, int) {
	seen := map[string]bool{}
	var convars []CvarConfig
	untranslated := 0

	for _, rel := range cfgFiles {
		content, err := os.ReadFile(filepath.Join(pluginDir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		parsed, missing := parseReadmeCvars(DecodeTextContent(content), dictionary)
		untranslated += missing
		for _, cvar := range parsed {
			if seen[cvar.Name] {
				continue
			}
			seen[cvar.Name] = true
			convars = append(convars, cvar)
		}
	}
	return convars, untranslated
}

// parseReadmeCvars 解析单个 cfg 文本。
//
// 结构是连续注释块后跟一行赋值：
//
//	// 说明
//	// --
//	// Default: "1"
//	sm_xxx "1"
//
// Default/Minimum/Maximum 与分割线不算说明；"1: xxx" 这类枚举行单独收集，
// 拼在描述后面，避免读起来断裂。
func parseReadmeCvars(content string, dictionary map[string]string) ([]CvarConfig, int) {
	var convars []CvarConfig
	untranslated := 0
	var mainDescriptions []string
	var options []string

	reset := func() {
		mainDescriptions = nil
		options = nil
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			reset()
			continue
		}
		if strings.HasPrefix(line, "//") {
			body := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			switch {
			case body == "", body == "-", body == "--":
				continue
			case strings.HasPrefix(body, "Default:"),
				strings.HasPrefix(body, "Minimum:"),
				strings.HasPrefix(body, "Maximum:"):
				continue
			case strings.HasPrefix(body, "ConVars for plugin"),
				strings.HasPrefix(body, "This file was auto-generated"):
				continue
			}
			if cvarOptionPattern.MatchString(body) {
				options = append(options, translateReadmeComment(body, dictionary))
				continue
			}
			text := translateReadmeComment(body, dictionary)
			if looksEnglish(text) {
				untranslated++
			}
			mainDescriptions = append(mainDescriptions, text)
			continue
		}

		match := cvarLinePattern.FindStringSubmatch(line)
		if match == nil {
			reset()
			continue
		}
		if len(mainDescriptions) == 0 && len(options) == 0 {
			reset()
			continue
		}

		var parts []string
		if len(mainDescriptions) > 0 {
			parts = append(parts, mainDescriptions[0])
		}
		if len(options) >= 2 {
			limit := len(options)
			if limit > 8 {
				limit = 8
			}
			joined := strings.Join(options[:limit], "")
			parts = append(parts, "（"+joined+"）")
		}
		description := strings.Join(parts, "")
		if description != "" && !isTruncatedDescription(description) {
			convars = append(convars, CvarConfig{Name: match[1], Description: description})
		}
		reset()
	}

	return convars, untranslated
}

// translateReadmeComment 把一行注释转成中文：先取现成中文，再查词典。
func translateReadmeComment(body string, dictionary map[string]string) string {
	if index := strings.Index(body, "｜"); index >= 0 {
		tail := strings.TrimSpace(body[index+len("｜"):])
		if cjkPattern.MatchString(tail) {
			return tail
		}
	}
	if cjkPattern.MatchString(body) {
		return body
	}
	if translated, ok := dictionary[body]; ok && translated != "" {
		return translated
	}
	return body
}

// looksEnglish 判断文本是否仍以英文为主（用于统计未翻译条数）。
func looksEnglish(text string) bool {
	if cjkPattern.MatchString(text) {
		return false
	}
	return englishProsePattern.MatchString(text)
}

// isTruncatedDescription 过滤被 cfg 折行截断的半句，避免展示半截说明。
//
// SourceMod 自动生成的 cfg 会把长注释折成多行，逐行翻译后就会留下
// 「……延迟最高且」「……subtracted from max」这类半句。宁可省略，也不误导。
func isTruncatedDescription(description string) bool {
	tail := strings.TrimRight(description, "）) ")
	// 中文半句：以连接词、介词或标点结尾
	chineseSuffixes := []string{
		"的", "且", "并", "和", "与", "或", "在", "为", "，", "、",
		"从", "向", "按", "由", "该", "此", "以", "使", "将", "对", "被",
	}
	for _, suffix := range chineseSuffixes {
		if strings.HasSuffix(tail, suffix) {
			return true
		}
	}
	// 英文半句：以连接词或介词结尾，或引号/括号未闭合
	englishSuffixes := []string{
		" and", " or", " the", " a", " an", " to", " of", " in", " on",
		" for", " with", " by", " from", " is", " are", " be", " that",
		" which", " when", " if", " than", " as", " at", ",",
	}
	lower := strings.ToLower(tail)
	for _, suffix := range englishSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	// 括号未闭合说明被截断
	if strings.Count(tail, "(") != strings.Count(tail, ")") {
		return true
	}
	return false
}

// PluginCommand 是插件注册到 SourceMod 的控制台命令。
type PluginCommand struct {
	Command string `json:"command"`
	Type    string `json:"type"`
	Help    string `json:"help"`
}

// PluginCommandScan 是一次命令扫描的结果。
//
// 必须区分「插件在跑但没有命令」和「插件根本没加载」：
// 前者是插件的正常形态（靠事件钩子生效），后者才需要提示用户去启用。
type PluginCommandScan struct {
	// Commands 是收集到的命令，已按命令名去重。
	Commands []PluginCommand
	// Loaded 是已加载（SourceMod 能查到该插件）的 smx 数量。
	Loaded int
	// NotLoaded 是未加载的 smx 数量。
	NotLoaded int
	// Unknown 是查询失败、无法判断的数量。
	Unknown int
}

// queryPluginReadmeCommands 通过 RCON 读取插件注册的命令。
//
// `sm cmds <smx 文件名>` 支持按文件名查询，不需要先拿插件序号。
// 三种返回都要认：
//
//	[SM] Listing commands for: xxx   → 已加载，有命令
//	[SM] No commands found for: xxx  → 已加载，但没有注册命令
//	[SM] Plugin "xxx.smx" was not found. → 未加载
func queryPluginReadmeCommands(smxFiles []string) PluginCommandScan {
	scan := PluginCommandScan{}
	seen := map[string]bool{}

	for _, rel := range smxFiles {
		base := path.Base(rel)
		output, err := executePluginRconCommand("sm cmds " + quoteSourceModArg(base))
		if err != nil {
			scan.Unknown++
			continue
		}
		parsed := parsePluginCommandsOutput(output)
		switch {
		case len(parsed) > 0:
			scan.Loaded++
		case strings.Contains(output, "No commands found for"):
			// 插件存在且已加载，只是它不注册命令
			scan.Loaded++
		case strings.Contains(output, "was not found"), strings.Contains(output, "is not loaded"):
			scan.NotLoaded++
		default:
			scan.Unknown++
		}
		for _, command := range parsed {
			if seen[command.Command] {
				continue
			}
			seen[command.Command] = true
			scan.Commands = append(scan.Commands, command)
		}
	}
	return scan
}

// 源码命令注册的正则。
//
// SourcePawn 的写法固定为：
//
//	RegConsoleCmd("sm_hp", Command_X, "说明")
//	RegAdminCmd("sm_limit", cmdSetLimit, ADMFLAG_RCON, "说明")
//
// 帮助文本可能缺失，也可能带中文，都是直接可用的说明。
var (
	sourceCommandPattern = regexp.MustCompile(
		`Reg(Console|Admin)Cmd\s*\(\s*"([^"]+)"\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*([^)]*)\)`,
	)
	sourceAdminFlagsPattern = regexp.MustCompile(`(ADMFLAG_[A-Z_]+)`)
	sourceHelpPattern       = regexp.MustCompile(`"([^"]*)"`)
)

// parseSourcePluginCommands 从 SourcePawn 源码里解析注册的控制台命令。
//
// 用途：插件未加载时 `sm cmds` 查不到任何东西，但作者常常把 .sp 源码一起打包。
// 源码里的 RegConsoleCmd / RegAdminCmd 是静态事实，可以据此补全命令表，
// 不必等插件被启用。
//
// 已知局限（都在说明里标注来源，不冒充运行时数据）：
//   - 只能看到直接写死字面量的命令名，拼接或宏定义的会漏
//   - 命令是否真的生效仍取决于插件能否加载成功
func parseSourcePluginCommands(files []string, readFile func(string) ([]byte, error)) []PluginCommand {
	var commands []PluginCommand
	seen := map[string]bool{}

	for _, rel := range files {
		content, err := readFile(rel)
		if err != nil {
			continue
		}
		text := DecodeTextContent(content)
		for _, match := range sourceCommandPattern.FindAllStringSubmatch(text, -1) {
			kind := match[1]
			name := match[2]
			rest := match[4]

			commandType := "console"
			help := ""
			if kind == "Admin" {
				commandType = "admin"
			}
			if flags := sourceAdminFlagsPattern.FindStringSubmatch(rest); flags != nil {
				// ADMFLAG_RCON → rcon，ADMFLAG_ROOT → root，供管理员章节展示。
				commandType = strings.ToLower(strings.TrimPrefix(flags[1], "ADMFLAG_"))
			}
			// 帮助文本取最后一个字符串字面量，避开权限常量。
			if helpMatches := sourceHelpPattern.FindAllStringSubmatch(rest, -1); helpMatches != nil {
				last := helpMatches[len(helpMatches)-1][1]
				if last != "" && !strings.HasPrefix(last, "ADMFLAG_") {
					help = last
				}
			}

			if seen[name] {
				continue
			}
			seen[name] = true
			commands = append(commands, PluginCommand{
				Command: name,
				Type:    commandType,
				Help:    help,
			})
		}
	}
	return commands
}

// parsePluginCommandsOutput 解析 `sm cmds` 输出。
//
// 格式：
//
//	[SM] Listing commands for: 插件名
//	  [Name]            [Type]   [Help]
//	  sm_tgvote         console      打开特感投票菜单
func parsePluginCommandsOutput(output string) []PluginCommand {
	var commands []PluginCommand
	inTable := false
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "[Name]") && strings.Contains(trimmed, "[Type]") {
			inTable = true
			continue
		}
		if !inTable || trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		commandType := fields[1]
		switch commandType {
		case "console", "admin", "server", "client":
		default:
			continue
		}
		help := ""
		if index := strings.Index(trimmed, commandType); index >= 0 {
			help = strings.TrimSpace(trimmed[index+len(commandType):])
		}
		commands = append(commands, PluginCommand{
			Command: fields[0],
			Type:    commandType,
			Help:    help,
		})
	}
	return commands
}

// buildPluginNoCommandSection 生成「没有可列命令」时的章节。
//
// 必须按扫描到的真实状态写，不能一律说「未启用」：
//   - 已加载但没有命令：插件靠事件触发，本来就没有玩家命令，这属于正常
//   - 未加载但源码已确认没有 RegConsoleCmd：源码是静态事实，不必再挂 pending
//   - 未加载且没有源码：需要启用后才能读到命令，这时才给 pending 标记等补全
//   - 读不到：服务器可能没运行，只说明情况，不下结论
func buildPluginNoCommandSection(scan PluginCommandScan, checkedSource bool) []string {
	var lines []string

	switch {
	case scan.Loaded > 0 && scan.NotLoaded == 0 && scan.Unknown == 0:
		lines = append(lines, "## 命令与用法", "")
		lines = append(lines, "> 该插件已在运行，但没有注册任何控制台命令。")
		lines = append(lines, "> 这类插件通常靠游戏内事件自动生效，启用后无需额外操作。")
		lines = append(lines, "")
	case scan.NotLoaded > 0 && scan.Loaded == 0 && scan.Unknown == 0 && checkedSource:
		// 源码里查过 RegConsoleCmd / RegAdminCmd，确认没有注册命令。
		// 这时的「没有命令」是事实，不是查询失败，不该继续挂 pending 诱导用户反复刷新。
		lines = append(lines, "## 命令与用法", "")
		lines = append(lines, "> 插件包内附带的源码中没有 `RegConsoleCmd` / `RegAdminCmd` 调用，")
		lines = append(lines, "> 说明它不注册任何聊天命令，只能靠游戏内事件自动生效。")
		lines = append(lines, "")
	case scan.NotLoaded > 0 && scan.Loaded == 0 && scan.Unknown == 0:
		lines = append(lines, pluginReadmeMissingMarker)
		lines = append(lines, "## 命令与用法", "")
		lines = append(lines, "> 插件当前未加载，SourceMod 查不到它注册的命令。")
		lines = append(lines, "> 启用插件后，在插件列表点「刷新说明」即可补全命令与聊天框用法。")
		lines = append(lines, "")
	case scan.Loaded > 0 && scan.NotLoaded > 0:
		lines = append(lines, "## 命令与用法", "")
		lines = append(lines, "> 该插件包含多个 smx：已加载的部分没有注册命令，未加载的部分暂时读不到。")
		lines = append(lines, "> 启用全部 smx 后点「刷新说明」可再补全一次。")
		lines = append(lines, "")
	default:
		// 读不到命令不等于插件没有命令，保留 pending 标记等下次再读
		lines = append(lines, pluginReadmeMissingMarker)
		lines = append(lines, "## 命令与用法", "")
		lines = append(lines, "> 暂时读不到该插件注册的命令（服务器可能未运行）。")
		lines = append(lines, "> 服务器运行时点「刷新说明」可以重新读取。")
		lines = append(lines, "")
	}
	return lines
}

// buildPluginCommandSection 生成命令与用法章节。
//
// 玩家命令写成 `!xxx` 聊天框输入形式：sm_xxx 是控制台命令，
// 但 SourceMod 会把 ! / 前缀转成对应命令，玩家通常不知道该对应关系。
//
// fromSource 为真表示命令表来自插件包里的 .sp 源码，不是运行时注册信息，
// 必须在结尾写明，避免读者误以为这些命令已在服务器上生效。
func buildPluginCommandSection(commands []PluginCommand, fromSource bool) []string {
	var lines []string
	lines = append(lines, "## 命令与用法", "")

	var playerCommands, adminCommands []PluginCommand
	for _, command := range commands {
		switch command.Type {
		case "console", "client":
			playerCommands = append(playerCommands, command)
		default:
			adminCommands = append(adminCommands, command)
		}
	}

	if len(playerCommands) > 0 {
		lines = append(lines, "### 玩家命令", "")
		lines = append(lines, "在游戏聊天框输入 `!命令名` 或 `/命令名` 即可触发，不需要开控制台。", "")
		lines = append(lines, "| 命令 | 聊天框输入 | 说明 |")
		lines = append(lines, "| --- | --- | --- |")
		for index, command := range playerCommands {
			if index >= 25 {
				lines = append(lines, fmt.Sprintf("| … | … | 其余 %d 个命令略 |", len(playerCommands)-25))
				break
			}
			chat := "!" + strings.TrimPrefix(command.Command, "sm_")
			help := command.Help
			if help == "" {
				help = "（插件未提供说明）"
			}
			lines = append(lines, "| `"+command.Command+"` | `"+chat+"` | "+escapeMarkdownCell(help)+" |")
		}
		lines = append(lines, "")
	}

	if len(adminCommands) > 0 {
		lines = append(lines, "### 管理员命令", "")
		lines = append(lines, "需要在服务器控制台或拥有对应管理权限的账号执行。", "")
		lines = append(lines, "| 命令 | 权限 | 说明 |")
		lines = append(lines, "| --- | --- | --- |")
		for index, command := range adminCommands {
			if index >= 20 {
				lines = append(lines, fmt.Sprintf("| … | … | 其余 %d 个命令略 |", len(adminCommands)-20))
				break
			}
			help := command.Help
			if help == "" {
				help = "（插件未提供说明）"
			}
			lines = append(lines,
				"| `"+command.Command+"` | "+command.Type+" | "+escapeMarkdownCell(help)+" |")
		}
		lines = append(lines, "")
	}

	if fromSource {
		lines = append(lines,
			"> 命令表来自插件包内的 `.sp` 源码（`RegConsoleCmd` / `RegAdminCmd`），"+
				"不是运行时注册信息。插件启用并加载成功后，点「刷新说明」可换成运行时数据。", "")
	} else {
		lines = append(lines, "> 命令表来自 SourceMod 运行时注册信息（`sm cmds`），不是文档抄录。", "")
	}
	return lines
}

// parsePluginNameHints 从插件目录名提取 必选/可选 前缀与括号内作者。
//
// 括号里可能是作者、版本号、说明或启动参数，只认：
//   - 纯版本号 → 跳过
//   - 含功能描述词（修复/必须/适配…）→ 跳过，那是说明不是作者
//   - 含启动参数（+/- 开头、含空格分隔的开关）→ 跳过
//   - 纯中文短词（2-10 字）或像人名的英文 → 视为作者
//
// 前缀同时认「可选」和「自选」：线上目录大多写成「自选-xxx」，
// 语义与「可选」一致，不能因为写法不同就丢掉整条信息。
func parsePluginNameHints(name string) (prefix string, authors []string) {
	cleaned := strings.TrimSpace(name)
	switch {
	case strings.HasPrefix(cleaned, "必选"):
		prefix = "必选"
		cleaned = strings.TrimPrefix(cleaned, "必选")
	case strings.HasPrefix(cleaned, "可选"):
		prefix = "可选"
		cleaned = strings.TrimPrefix(cleaned, "可选")
	case strings.HasPrefix(cleaned, "自选"):
		prefix = "可选"
		cleaned = strings.TrimPrefix(cleaned, "自选")
	}
	cleaned = strings.TrimLeft(cleaned, "-_ ")

	for _, candidate := range trailingParensPattern.FindAllStringSubmatch(cleaned, -1) {
		text := strings.TrimSpace(candidate[1])
		if text == "" || pureNumberPattern.MatchString(text) {
			continue
		}
		if descriptionHintPattern.MatchString(text) {
			continue
		}
		// 启动参数不是作者，如 "-tickrate 100 +sv_setmax 31"。
		if launchArgPattern.MatchString(text) {
			continue
		}
		if !authorNamePattern.MatchString(text) && !chineseAuthorPattern.MatchString(text) {
			continue
		}
		duplicated := false
		for _, existing := range authors {
			if existing == text {
				duplicated = true
				break
			}
		}
		if !duplicated {
			authors = append(authors, text)
		}
	}
	return prefix, authors
}

// isReadmeGeneratedByManager 判断已有说明是否由面板生成。
// 只有面板生成的文件才允许覆盖，避免冲掉插件作者或用户手写的文档。
//
// 两代格式都要认：
//   - 新格式首行带 pluginReadmeMarker
//   - 旧格式由批量脚本生成，没有标记但有固定结尾
func isReadmeGeneratedByManager(content string) bool {
	if strings.Contains(content, pluginReadmeMarker) {
		return true
	}
	return strings.Contains(content, pluginReadmeLegacyFooter)
}
