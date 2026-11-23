package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"l4d2-manager-next/consts"
)

// withGamePath 把 consts.GamePath 指向临时目录，测试结束后恢复。
func withGamePath(t *testing.T, root string) {
	t.Helper()
	previous := consts.GamePath
	consts.GamePath = root
	t.Cleanup(func() { consts.GamePath = previous })
}

// stubRcon 替换 RCON 执行函数，返回固定的命令映射。
func stubRcon(t *testing.T, responses map[string]string, err error) {
	t.Helper()
	previous := executeHostnameRconCommand
	executeHostnameRconCommand = func(command string) (string, error) {
		if err != nil {
			return "", err
		}
		return responses[command], nil
	}
	t.Cleanup(func() { executeHostnameRconCommand = previous })
}

func writeHostnameTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseRconHostnameQuotedForm(t *testing.T) {
	output := "\"hostname\" = \"CC偏纯净多特155服[4特10秒][重启:0|路程:0%][计时:00m:03s]\" ( def. \"\" )\n"
	want := "CC偏纯净多特155服[4特10秒][重启:0|路程:0%][计时:00m:03s]"
	if got := parseRconHostname(output); got != want {
		t.Fatalf("parseRconHostname() = %q, want %q", got, want)
	}
}

func TestParseRconHostnameBareValue(t *testing.T) {
	if got := parseRconHostname("我的服务器\n"); got != "我的服务器" {
		t.Fatalf("裸值解析失败，得到 %q", got)
	}
	if got := parseRconHostname(`"带引号的服务器名"`); got != "带引号的服务器名" {
		t.Fatalf("引号包裹解析失败，得到 %q", got)
	}
	if got := parseRconHostname(""); got != "" {
		t.Fatalf("空输入应返回空串，得到 %q", got)
	}
}

func TestParseSourceModPluginNamesReadsRuntimeNames(t *testing.T) {
	output := "  001 \"L4D2 中文服务器名\" (1.0) by 豆瓣酱\n" +
		"  002 \"Neko ServerName\" (2.1) by Neko\n" +
		"  003 \"Server Message System\" (1.0) by someone\n"
	names := parseSourceModPluginNames(output)
	want := []string{"L4D2 中文服务器名", "Neko ServerName", "Server Message System"}
	if len(names) != len(want) {
		t.Fatalf("解析到 %d 个名字 %v，期望 %v", len(names), names, want)
	}
	for index, name := range want {
		if names[index] != name {
			t.Fatalf("第 %d 个名字 = %q，期望 %q", index, names[index], name)
		}
	}
}

func TestIsServerNamePluginIgnoresUnrelatedNames(t *testing.T) {
	cases := map[string]bool{
		"L4D2 中文服务器名":           true,
		"Neko ServerName":       true,
		"hostname":              true,
		"服务器名称":                 true,
		"Server Message System": false,
		"L4D2 Ready Up":         false,
		"":                      false,
	}
	for name, want := range cases {
		if got := isServerNamePlugin(name); got != want {
			t.Errorf("isServerNamePlugin(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsHostnameTemplate(t *testing.T) {
	template := "CC偏纯净多特{servernum}服[{specials}特{times}秒]"
	if !isHostnameTemplate(template) {
		t.Fatal("含占位符的名字应判定为模板")
	}
	plain := "CC偏纯净多特155服[4特10秒][重启:0|路程:0%]"
	if isHostnameTemplate(plain) {
		t.Fatal("普通名字不应判定为模板")
	}
	// 方括号里的中文数字不是占位符
	if isHostnameTemplate("服务器[4特]") {
		t.Fatal("方括号内容不应判定为模板")
	}
}

func TestExtractKeyValuesValueAndNested(t *testing.T) {
	neko := `"ServerNameFormat"	"CC偏纯净多特{servernum}服"
"ServerName_AutoUpdate"	"1"
`
	if got := extractKeyValuesValue(neko, "ServerNameFormat"); got != "CC偏纯净多特{servernum}服" {
		t.Fatalf("extractKeyValuesValue = %q", got)
	}

	nested := `"hostname"
{
	"27015"
	{
		"hostname"		"偏纯净三方多特tick100"
	}
	"27016"
	{
		"hostname"		"第二实例"
	}
}
`
	if got := extractNestedHostnameValue(nested); got != "偏纯净三方多特tick100" {
		t.Fatalf("extractNestedHostnameValue = %q", got)
	}
}

func TestDetectHostnameFilesFindsMultiplePlugins(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	writeHostnameTestFile(t, root, "addons/sourcemod/configs/l4d2_hostname.txt", "中文服名插件\n")
	writeHostnameTestFile(t, root, "addons/sourcemod/data/nekocustom.cfg",
		"\"ServerNameFormat\"\t\"Neko模板{servernum}\"\n")
	writeHostnameTestFile(t, root, "addons/sourcemod/configs/hostname/hostname.txt",
		"\"hostname\"\n{\n\t\"27015\"\n\t{\n\t\t\"hostname\"\t\"按端口存的服名\"\n\t}\n}\n")

	sources := DetectHostnameFiles()
	if len(sources) != 3 {
		t.Fatalf("应检测到 3 个来源，实际 %d: %+v", len(sources), sources)
	}
	if sources[0].Raw != "中文服名插件" || sources[0].IsTemplate {
		t.Fatalf("第一个来源应为纯文本插件名: %+v", sources[0])
	}
	// Neko 模板必须被标记为模板
	foundTemplate := false
	for _, source := range sources {
		if source.IsTemplate {
			foundTemplate = true
		}
	}
	if !foundTemplate {
		t.Fatalf("含 {servernum} 的来源应标记为模板: %+v", sources)
	}
}

func TestDetectHostnameSourcesUsesRconForEffectiveValue(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	writeHostnameTestFile(t, root, "addons/sourcemod/data/nekocustom.cfg",
		"\"ServerNameFormat\"\t\"CC偏纯净多特{servernum}服\"\n")
	stubRcon(t, map[string]string{
		"hostname":        "\"hostname\" = \"运行时真实名字\" ( def. \"\" )\n",
		"sm plugins list": "  001 \"L4D2 中文服务器名\" (1.0) by 豆瓣酱\n  002 \"Neko ServerName\" (2.1) by Neko\n",
	}, nil)

	detection := DetectHostnameSources()
	if !detection.EffectiveOK || detection.Effective != "运行时真实名字" {
		t.Fatalf("RCON 生效值错误: %+v", detection)
	}
	if len(detection.Plugins) != 2 {
		t.Fatalf("应识别 2 个服务器名插件，实际 %v", detection.Plugins)
	}

	value, source, errMsg := ResolveHostname(detection)
	if errMsg != "" {
		t.Fatalf("ResolveHostname 不应报错: %s", errMsg)
	}
	if value != "运行时真实名字" || source != "rcon" {
		t.Fatalf("应优先使用 RCON 值，得到 value=%q source=%q", value, source)
	}
}

func TestResolveHostnamePrefersNonTemplateFileWhenRconFails(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	writeHostnameTestFile(t, root, "addons/sourcemod/configs/l4d2_hostname.txt", "文件里的服名\n")
	writeHostnameTestFile(t, root, "addons/sourcemod/data/nekocustom.cfg",
		"\"ServerNameFormat\"\t\"模板{servernum}\"\n")
	stubRcon(t, nil, os.ErrDeadlineExceeded)

	detection := DetectHostnameSources()
	value, source, errMsg := ResolveHostname(detection)
	if errMsg != "" {
		t.Fatalf("不应报错: %s", errMsg)
	}
	if value != "文件里的服名" || source != "file" {
		t.Fatalf("应优先非模板文件，得到 value=%q source=%q", value, source)
	}
}

func TestResolveHostnameReportsMissingEvidence(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	stubRcon(t, map[string]string{"sm plugins list": "  001 \"L4D2 Ready Up\" (1.0)\n"}, nil)

	detection := DetectHostnameSources()
	_, _, errMsg := ResolveHostname(detection)
	if errMsg == "" {
		t.Fatal("没有任何证据时应返回说明文案")
	}
}

func TestSaveServerHostnameWritesFirstExistingTarget(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	writeHostnameTestFile(t, root, "addons/sourcemod/configs/l4d2_hostname.txt", "旧名字\n")
	stubRcon(t, map[string]string{"hostname": "\"hostname\" = \"旧名字\" ( def. \"\" )\n"}, nil)

	written, _, err := SaveServerHostname("新名字")
	if err != nil {
		t.Fatalf("SaveServerHostname() error = %v", err)
	}
	if written != "addons/sourcemod/configs/l4d2_hostname.txt" {
		t.Fatalf("写入文件 = %q", written)
	}
	content, err := os.ReadFile(filepath.Join(root, "addons", "sourcemod", "configs", "l4d2_hostname.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "新名字" {
		t.Fatalf("文件内容 = %q", string(content))
	}
}

func TestSaveServerHostnamePreservesNekoKeyValuesStructure(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	writeHostnameTestFile(t, root, "addons/sourcemod/data/nekocustom.cfg",
		"\"ServerName_AutoUpdate\"\t\"1\"\n\"ServerNameFormat\"\t\"旧模板{servernum}\"\n")
	stubRcon(t, nil, os.ErrDeadlineExceeded)

	written, _, err := SaveServerHostname("新模板{servernum}")
	if err != nil {
		t.Fatalf("SaveServerHostname() error = %v", err)
	}
	if written != "addons/sourcemod/data/nekocustom.cfg" {
		t.Fatalf("写入文件 = %q", written)
	}
	content, err := os.ReadFile(filepath.Join(root, "addons", "sourcemod", "data", "nekocustom.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "ServerName_AutoUpdate") {
		t.Fatalf("其他键应保留:\n%s", text)
	}
	if !strings.Contains(text, "新模板{servernum}") {
		t.Fatalf("目标键应更新:\n%s", text)
	}
}

func TestSaveServerHostnameFallsBackToLegacyFile(t *testing.T) {
	root := t.TempDir()
	withGamePath(t, root)
	stubRcon(t, nil, os.ErrDeadlineExceeded)

	written, warning, err := SaveServerHostname("保底名字")
	if err != nil {
		t.Fatalf("SaveServerHostname() error = %v", err)
	}
	if written != "addons/sourcemod/configs/l4d2_hostname.txt" {
		t.Fatalf("兜底写入路径 = %q", written)
	}
	if warning == "" {
		t.Fatal("兜底写入应给出提示")
	}
	content, err := os.ReadFile(filepath.Join(root, "addons", "sourcemod", "configs", "l4d2_hostname.txt"))
	if err != nil {
		t.Fatalf("兜底文件未生成: %v", err)
	}
	if string(content) != "保底名字" {
		t.Fatalf("兜底文件内容 = %q", string(content))
	}
}

func TestDecodeTextContentHandlesGBKAndBOM(t *testing.T) {
	if got := DecodeTextContent([]byte("\ufeff中文服名")); got != "中文服名" {
		t.Fatalf("BOM 未去除: %q", got)
	}
	// GBK 编码的“中文”是 0xD6 0xD0 0xCE 0xC4
	if got := DecodeTextContent([]byte{0xD6, 0xD0, 0xCE, 0xC4}); got != "中文" {
		t.Fatalf("GBK 解码失败: %q", got)
	}
}
