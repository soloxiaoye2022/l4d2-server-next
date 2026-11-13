package logic

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"unicode"
)

var startupPortOptions = map[string]struct{}{
	"+hostport": {},
	"+port":     {},
	"-port":     {},
}

// ParseGameServerPort returns the last explicit game port in ExecStart.
// L4D2_PORT remains a compatibility fallback for deployments without a unit file.
func ParseGameServerPort(execStart string) int {
	if port, found, err := parseGameServerPort(execStart); err == nil && found {
		return port
	}
	if port := parsePort(os.Getenv("L4D2_PORT")); port > 0 {
		return port
	}
	return DefaultGameServerPort
}

func parseGameServerPort(execStart string) (int, bool, error) {
	tokens, err := splitStartupCommand(execStart)
	if err != nil {
		return 0, false, err
	}

	port := 0
	found := false
	for index := 0; index < len(tokens); index++ {
		option, inlineValue, hasInlineValue := splitStartupOption(tokens[index])
		if _, ok := startupPortOptions[strings.ToLower(option)]; !ok {
			continue
		}

		value := inlineValue
		if !hasInlineValue {
			if index+1 >= len(tokens) {
				return 0, false, fmt.Errorf("%s 缺少端口值", option)
			}
			index++
			value = tokens[index]
		}

		parsed := parsePort(value)
		if parsed == 0 {
			return 0, false, fmt.Errorf("%s 的端口值无效: %q", option, value)
		}
		port = parsed
		found = true
	}
	return port, found, nil
}

func splitStartupOption(token string) (string, string, bool) {
	if index := strings.IndexByte(token, '='); index >= 0 {
		return token[:index], token[index+1:], true
	}
	return token, "", false
}

func parsePort(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if host, portValue, err := net.SplitHostPort(value); err == nil && host != "" {
		value = portValue
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func splitStartupCommand(command string) ([]string, error) {
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
		if escaped {
			current.WriteRune(char)
			started = true
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			started = true
			continue
		}
		if char == '"' || char == '\'' {
			quote = char
			started = true
			continue
		}
		if unicode.IsSpace(char) {
			flush()
			continue
		}
		current.WriteRune(char)
		started = true
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

func systemdUnitLines(content string) []string {
	rawLines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	var continuation strings.Builder

	for _, rawLine := range rawLines {
		trimmedRight := strings.TrimRight(rawLine, " \t")
		if strings.HasSuffix(trimmedRight, "\\") {
			continuation.WriteString(strings.TrimSuffix(trimmedRight, "\\"))
			continuation.WriteByte(' ')
			continue
		}
		continuation.WriteString(rawLine)
		lines = append(lines, continuation.String())
		continuation.Reset()
	}
	if continuation.Len() > 0 {
		lines = append(lines, continuation.String())
	}
	return lines
}
