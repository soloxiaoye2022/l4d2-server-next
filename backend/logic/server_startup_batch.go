package logic

import (
	"fmt"
	"regexp"
	"strings"
)

// Windows has no systemd unit, so the game server startup definition lives in a
// batch file. It carries the same two pieces of information as the unit:
// a working directory and a command line. Two conventional variables hold them,
// and everything else in the file is preserved verbatim so users can keep their
// own wrapper logic.
const (
	batchWorkdirVar   = "L4D2_WORKDIR"
	batchExecStartVar = "L4D2_EXECSTART"

	// Batch has no "#" comment syntax: cmd.exe would try to run that line and
	// print "'#' is not recognized". The marker must be a real `rem` line so the
	// generated launcher stays executable.
	batchCommentsBegin = "rem L4D2 Manager startup options: begin"
	batchCommentsEnd   = "rem L4D2 Manager startup options: end"
)

// batchSetPattern matches both `set "NAME=value"` and `set NAME=value`. The
// quoted form is preferred because unquoted values break on trailing spaces.
var batchSetPattern = regexp.MustCompile(`(?i)^\s*set\s+(?:"\s*([A-Za-z0-9_]+)\s*=\s*([^"]*)"|([A-Za-z0-9_]+)\s*=\s*(.*?))\s*$`)

// batchCdPattern matches `cd /d "path"` or `cd "path"`, the usual way a batch
// launcher switches to the game directory before starting srcds.
var batchCdPattern = regexp.MustCompile(`(?i)^\s*cd\s+(?:/d\s+)?(?:"([^"]+)"|(\S+))\s*$`)

// batchCommentPattern matches `rem ...` and `:: ...` comment lines.
var batchCommentPattern = regexp.MustCompile(`(?i)^\s*(?:rem\b\s*|::\s*)(.*)$`)

// parseBatchStartup extracts the startup definition from a batch launcher.
//
// Order of precedence:
//  1. explicit L4D2_WORKDIR / L4D2_EXECSTART assignments
//  2. `cd /d "path"` for the directory and the first launch command for ExecStart
//
// The fallback keeps hand-written batch files usable without forcing users to
// rewrite them in the manager's format.
func parseBatchStartup(content string) (comments []string, workdir string, execStart string, err error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	comments = make([]string, 0, 4)
	inCommentBlock := false
	firstCommand := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.EqualFold(trimmed, batchCommentsBegin):
			inCommentBlock = true
			comments = comments[:0]
			continue
		case strings.EqualFold(trimmed, batchCommentsEnd):
			inCommentBlock = false
			continue
		case inCommentBlock:
			if match := batchCommentPattern.FindStringSubmatch(trimmed); match != nil {
				if text := strings.TrimSpace(match[1]); text != "" {
					comments = append(comments, text)
				}
			}
			continue
		}

		if match := batchSetPattern.FindStringSubmatch(trimmed); match != nil {
			name, quotedValue, bareName, bareValue := match[1], match[2], match[3], match[4]
			if name == "" {
				name, quotedValue = bareName, bareValue
			}
			switch strings.ToUpper(name) {
			case batchWorkdirVar:
				workdir = strings.TrimSpace(quotedValue)
			case batchExecStartVar:
				execStart = strings.TrimSpace(quotedValue)
			}
			continue
		}

		if workdir == "" {
			if match := batchCdPattern.FindStringSubmatch(trimmed); match != nil {
				value := match[1]
				if value == "" {
					value = match[2]
				}
				workdir = strings.TrimSpace(value)
				continue
			}
		}

		if firstCommand == "" && isBatchLaunchCommand(trimmed) {
			firstCommand = trimmed
		}
	}

	if execStart == "" {
		execStart = firstCommand
	}
	if workdir == "" && execStart == "" {
		return comments, "", "", fmt.Errorf("启动脚本中既没有 %s/%s 变量，也没有可识别的启动命令", batchWorkdirVar, batchExecStartVar)
	}
	return comments, workdir, execStart, nil
}

// isBatchLaunchCommand reports whether a line looks like the actual srcds
// launch rather than batch scaffolding.
func isBatchLaunchCommand(line string) bool {
	if line == "" || strings.HasPrefix(line, "@") {
		return false
	}
	lower := strings.ToLower(line)
	for _, prefix := range []string{
		"set ", "cd ", "echo", "rem ", "::", "if ", "goto ", ":",
		"title ", "color ", "cls", "pause", "timeout ", "setlocal", "endlocal",
		"chcp ",
	} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return strings.Contains(lower, "srcds") || strings.Contains(lower, "left4dead2")
}

// updateBatchStartup rewrites the startup definition in place. Unknown lines are
// kept so custom wrappers survive; only the comment block and the two variables
// are touched.
func updateBatchStartup(content, workdir, execStart string, comments []string) (string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	filtered := make([]string, 0, len(lines)+4)
	inCommentBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, batchCommentsBegin) {
			inCommentBlock = true
			continue
		}
		if inCommentBlock {
			if strings.EqualFold(trimmed, batchCommentsEnd) {
				inCommentBlock = false
			}
			continue
		}
		filtered = append(filtered, line)
	}

	block := buildBatchCommentBlock(comments)
	workdirLine := fmt.Sprintf(`set "%s=%s"`, batchWorkdirVar, workdir)
	execLine := fmt.Sprintf(`set "%s=%s"`, batchExecStartVar, execStart)

	workdirIndex := -1
	execIndex := -1
	for index, line := range filtered {
		match := batchSetPattern.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		name := match[1]
		if name == "" {
			name = match[3]
		}
		switch strings.ToUpper(name) {
		case batchWorkdirVar:
			if workdirIndex >= 0 {
				return "", fmt.Errorf("启动脚本包含多个 %s，暂不支持自动编辑", batchWorkdirVar)
			}
			workdirIndex = index
		case batchExecStartVar:
			if execIndex >= 0 {
				return "", fmt.Errorf("启动脚本包含多个 %s，暂不支持自动编辑", batchExecStartVar)
			}
			execIndex = index
		}
	}

	switch {
	case workdirIndex >= 0:
		filtered[workdirIndex] = workdirLine
	case execIndex >= 0:
		filtered = insertLines(filtered, execIndex, workdirLine)
		execIndex++
	default:
		// Neither variable exists: append both at the end so the rest of the
		// script keeps its original ordering.
		filtered = append(filtered, workdirLine, execLine)
		workdirIndex = len(filtered) - 2
		execIndex = len(filtered) - 1
	}
	if workdirIndex >= 0 && workdirIndex < len(filtered) {
		filtered[workdirIndex] = workdirLine
	}
	if execIndex >= 0 && execIndex < len(filtered) {
		filtered[execIndex] = execLine
	}

	// Comment block sits right above the variable pair.
	anchor := workdirIndex
	if anchor < 0 {
		anchor = execIndex
	}
	if anchor < 0 {
		anchor = len(filtered)
	}
	result := insertLines(filtered, anchor, block...)

	text := strings.Join(result, "\n")
	// Batch files must stay CRLF or cmd.exe mis-parses labels and continuations.
	return strings.ReplaceAll(text, "\n", "\r\n"), nil
}

func buildBatchCommentBlock(comments []string) []string {
	block := make([]string, 0, len(comments)+2)
	block = append(block, batchCommentsBegin)
	for _, comment := range comments {
		block = append(block, "rem "+comment)
	}
	block = append(block, batchCommentsEnd)
	return block
}

// batchStartupTemplate is used when the manager has to create the launcher from
// scratch. It mirrors what start_manager.bat does for the manager itself.
func batchStartupTemplate(workdir, execStart string, comments []string) string {
	lines := []string{
		"@echo off",
		"setlocal",
	}
	lines = append(lines, buildBatchCommentBlock(comments)...)
	lines = append(lines,
		fmt.Sprintf(`set "%s=%s"`, batchWorkdirVar, workdir),
		fmt.Sprintf(`set "%s=%s"`, batchExecStartVar, execStart),
		`cd /d "%`+batchWorkdirVar+`%"`,
		`%`+batchExecStartVar+`%`,
		"",
	)
	return strings.ReplaceAll(strings.Join(lines, "\n"), "\n", "\r\n")
}

func insertLines(lines []string, index int, extra ...string) []string {
	if index < 0 || index > len(lines) {
		index = len(lines)
	}
	result := make([]string, 0, len(lines)+len(extra))
	result = append(result, lines[:index]...)
	result = append(result, extra...)
	result = append(result, lines[index:]...)
	return result
}
