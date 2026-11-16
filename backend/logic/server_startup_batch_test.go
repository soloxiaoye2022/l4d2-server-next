package logic

import (
	"strings"
	"testing"
)

func TestParseBatchStartupReadsExplicitVariables(t *testing.T) {
	content := strings.Join([]string{
		"@echo off",
		`set "L4D2_WORKDIR=D:\SteamCMD\l4d2"`,
		`set "L4D2_EXECSTART=D:\SteamCMD\l4d2\srcds.exe -game left4dead2 +hostport 27015"`,
		`cd /d "%L4D2_WORKDIR%"`,
		`%L4D2_EXECSTART%`,
	}, "\r\n")

	comments, workdir, execStart, err := parseBatchStartup(content)
	if err != nil {
		t.Fatalf("parseBatchStartup() error = %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("comments = %v, want empty", comments)
	}
	if workdir != `D:\SteamCMD\l4d2` {
		t.Fatalf("workdir = %q", workdir)
	}
	if !strings.Contains(execStart, "+hostport 27015") {
		t.Fatalf("execStart = %q", execStart)
	}
}

func TestParseBatchStartupFallsBackToCdAndLaunchCommand(t *testing.T) {
	content := strings.Join([]string{
		"@echo off",
		`cd /d "D:\L4D2"`,
		`srcds.exe -game left4dead2 +hostport 27016`,
	}, "\r\n")

	_, workdir, execStart, err := parseBatchStartup(content)
	if err != nil {
		t.Fatalf("parseBatchStartup() error = %v", err)
	}
	if workdir != `D:\L4D2` {
		t.Fatalf("workdir = %q, want D:\\L4D2", workdir)
	}
	if !strings.Contains(execStart, "27016") {
		t.Fatalf("execStart = %q", execStart)
	}
}

func TestParseBatchStartupReadsUnquotedSetForm(t *testing.T) {
	content := "set L4D2_WORKDIR=E:\\Games\\l4d2\r\nset L4D2_EXECSTART=srcds.exe -game left4dead2\r\n"
	_, workdir, execStart, err := parseBatchStartup(content)
	if err != nil {
		t.Fatalf("parseBatchStartup() error = %v", err)
	}
	if workdir != `E:\Games\l4d2` {
		t.Fatalf("workdir = %q", workdir)
	}
	if execStart != "srcds.exe -game left4dead2" {
		t.Fatalf("execStart = %q", execStart)
	}
}

func TestParseBatchStartupReadsCommentBlock(t *testing.T) {
	content := strings.Join([]string{
		"@echo off",
		batchCommentsBegin,
		"rem 启用 60 tick",
		"rem 端口 27015",
		batchCommentsEnd,
		`set "L4D2_WORKDIR=D:\l4d2"`,
		`set "L4D2_EXECSTART=srcds.exe -game left4dead2"`,
	}, "\r\n")

	comments, _, _, err := parseBatchStartup(content)
	if err != nil {
		t.Fatalf("parseBatchStartup() error = %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments = %v, want 2 entries", comments)
	}
	if comments[0] != "启用 60 tick" {
		t.Fatalf("comments[0] = %q", comments[0])
	}
}

func TestParseBatchStartupRejectsEmptyScript(t *testing.T) {
	content := "@echo off\r\necho nothing to launch\r\n"
	if _, _, _, err := parseBatchStartup(content); err == nil {
		t.Fatal("expected error for script without startup definition")
	}
}

func TestUpdateBatchStartupReplacesExistingVariables(t *testing.T) {
	content := strings.Join([]string{
		"@echo off",
		`set "L4D2_WORKDIR=old"`,
		`set "L4D2_EXECSTART=old.exe"`,
		"echo done",
	}, "\r\n")

	updated, err := updateBatchStartup(content, `D:\new\l4d2`, `D:\new\l4d2\srcds.exe -game left4dead2`, []string{"新注释"})
	if err != nil {
		t.Fatalf("updateBatchStartup() error = %v", err)
	}
	if !strings.Contains(updated, `set "L4D2_WORKDIR=D:\new\l4d2"`) {
		t.Fatalf("workdir not replaced:\n%s", updated)
	}
	if !strings.Contains(updated, `set "L4D2_EXECSTART=D:\new\l4d2\srcds.exe -game left4dead2"`) {
		t.Fatalf("execstart not replaced:\n%s", updated)
	}
	if !strings.Contains(updated, "echo done") {
		t.Fatalf("unrelated lines must survive:\n%s", updated)
	}
	if !strings.Contains(updated, "rem 新注释") {
		t.Fatalf("comment block missing:\n%s", updated)
	}
}

func TestUpdateBatchStartupInsertsMissingWorkdir(t *testing.T) {
	content := "@echo off\r\nset \"L4D2_EXECSTART=srcds.exe -game left4dead2\"\r\n"
	updated, err := updateBatchStartup(content, `D:\l4d2`, `D:\l4d2\srcds.exe -game left4dead2`, nil)
	if err != nil {
		t.Fatalf("updateBatchStartup() error = %v", err)
	}
	if !strings.Contains(updated, `set "L4D2_WORKDIR=D:\l4d2"`) {
		t.Fatalf("workdir not inserted:\n%s", updated)
	}
	// 变量对必须相邻，cd 才能正确解析
	workdirIndex := strings.Index(updated, "L4D2_WORKDIR=")
	execIndex := strings.Index(updated, "L4D2_EXECSTART=")
	if workdirIndex < 0 || execIndex < 0 || workdirIndex > execIndex {
		t.Fatalf("variable order wrong:\n%s", updated)
	}
}

func TestUpdateBatchStartupAppendsWhenNeitherExists(t *testing.T) {
	content := "@echo off\r\necho starting\r\n"
	updated, err := updateBatchStartup(content, `D:\l4d2`, `D:\l4d2\srcds.exe -game left4dead2`, nil)
	if err != nil {
		t.Fatalf("updateBatchStartup() error = %v", err)
	}
	if !strings.Contains(updated, "L4D2_WORKDIR=") || !strings.Contains(updated, "L4D2_EXECSTART=") {
		t.Fatalf("variables not appended:\n%s", updated)
	}
	if !strings.Contains(updated, "echo starting") {
		t.Fatalf("original content lost:\n%s", updated)
	}
}

func TestUpdateBatchStartupRejectsDuplicateVariables(t *testing.T) {
	content := strings.Join([]string{
		`set "L4D2_WORKDIR=a"`,
		`set "L4D2_WORKDIR=b"`,
	}, "\r\n")
	if _, err := updateBatchStartup(content, `D:\l4d2`, "srcds.exe", nil); err == nil {
		t.Fatal("expected error for duplicate L4D2_WORKDIR")
	}
}

func TestUpdateBatchStartupUsesCRLF(t *testing.T) {
	updated, err := updateBatchStartup("@echo off\n", `D:\l4d2`, "srcds.exe", nil)
	if err != nil {
		t.Fatalf("updateBatchStartup() error = %v", err)
	}
	if strings.Contains(strings.ReplaceAll(updated, "\r\n", ""), "\n") {
		t.Fatalf("batch output must be CRLF only:\n%q", updated)
	}
}

func TestBatchRoundTripPreservesValues(t *testing.T) {
	workdir := `D:\SteamCMD\l4d2`
	execStart := `D:\SteamCMD\l4d2\srcds.exe -game left4dead2 +hostport 27015 -tickrate 100`
	comments := []string{"中文注释", "第二条"}

	template := batchStartupTemplate(workdir, execStart, comments)
	gotComments, gotWorkdir, gotExec, err := parseBatchStartup(template)
	if err != nil {
		t.Fatalf("parseBatchStartup() error = %v", err)
	}
	if gotWorkdir != workdir {
		t.Fatalf("workdir = %q, want %q", gotWorkdir, workdir)
	}
	if gotExec != execStart {
		t.Fatalf("execStart = %q, want %q", gotExec, execStart)
	}
	if len(gotComments) != len(comments) {
		t.Fatalf("comments = %v, want %v", gotComments, comments)
	}

	updated, err := updateBatchStartup(template, workdir, execStart, comments)
	if err != nil {
		t.Fatalf("updateBatchStartup() error = %v", err)
	}
	_, workdir2, exec2, err := parseBatchStartup(updated)
	if err != nil {
		t.Fatalf("reparse error = %v", err)
	}
	if workdir2 != workdir || exec2 != execStart {
		t.Fatalf("round trip changed values: %q / %q", workdir2, exec2)
	}
}

func TestBatchTemplateIsLaunchable(t *testing.T) {
	template := batchStartupTemplate(`D:\l4d2`, `D:\l4d2\srcds.exe -game left4dead2`, nil)
	for _, want := range []string{"@echo off", "setlocal", `cd /d "%L4D2_WORKDIR%"`, "%L4D2_EXECSTART%"} {
		if !strings.Contains(template, want) {
			t.Fatalf("template missing %q:\n%s", want, template)
		}
	}
}

func TestIsBatchLaunchCommandFiltersScaffolding(t *testing.T) {
	launch := []string{
		`srcds.exe -game left4dead2`,
		`D:\l4d2\srcds.exe -game left4dead2 +hostport 27015`,
	}
	for _, line := range launch {
		if !isBatchLaunchCommand(line) {
			t.Fatalf("should be a launch command: %q", line)
		}
	}
	scaffold := []string{
		"@echo off", "set FOO=bar", `cd /d "D:\l4d2"`, "echo hi", "rem note",
		"if exist x goto y", ":label", "pause", "timeout /t 3", "setlocal",
	}
	for _, line := range scaffold {
		if isBatchLaunchCommand(line) {
			t.Fatalf("should not be a launch command: %q", line)
		}
	}
}
