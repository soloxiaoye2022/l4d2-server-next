package logic

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const defaultRconAddress = "127.0.0.1:27015"

// ResolveRconURL keeps the configured RCON host while using the active
// startup definition as the single source of the game/RCON port.
func ResolveRconURL() (string, error) {
	rawURL := strings.TrimSpace(os.Getenv("L4D2_RCON_URL"))
	if rawURL == "" {
		rawURL = defaultRconAddress
	}

	host, rawPort, err := net.SplitHostPort(rawURL)
	if err != nil {
		return "", fmt.Errorf("L4D2_RCON_URL 无效: %w", err)
	}
	port := parsePort(rawPort)
	if port == 0 {
		return "", fmt.Errorf("L4D2_RCON_URL 端口无效: %q", rawPort)
	}

	if rawOverride := strings.TrimSpace(os.Getenv("L4D2_RCON_PORT")); rawOverride != "" {
		port = parsePort(rawOverride)
		if port == 0 {
			return "", fmt.Errorf("L4D2_RCON_PORT 无效: %q", rawOverride)
		}
	} else {
		endpoint := GetServerEndpoint()
		if endpoint.Available || endpoint.Port != DefaultGameServerPort {
			port = endpoint.Port
		}
	}

	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
