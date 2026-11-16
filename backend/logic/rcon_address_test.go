package logic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRconURLUsesStartupPort(t *testing.T) {
	forceSystemdStartup(t)
	unitPath := filepath.Join(t.TempDir(), "l4d2.service")
	if err := os.WriteFile(unitPath, []byte(`[Service]
ExecStart=/l4d2/srcds_run -game left4dead2 \
  +hostport 12138 +exec server.cfg
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_RCON_URL", "127.0.0.1:27015")
	t.Setenv("L4D2_RCON_PORT", "")
	t.Setenv("L4D2_PORT", "")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)
	t.Setenv("L4D2_SERVER_UNIT_FILE", "")

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "127.0.0.1:12138" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "127.0.0.1:12138")
	}
}

func TestResolveRconURLSupportsLegacyStartupPath(t *testing.T) {
	forceSystemdStartup(t)
	unitPath := filepath.Join(t.TempDir(), "l4d2.service")
	if err := os.WriteFile(unitPath, []byte(`[Service]
ExecStart=/l4d2/srcds_run +hostport=12138
`), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_RCON_URL", "127.0.0.1:27015")
	t.Setenv("L4D2_RCON_PORT", "")
	t.Setenv("L4D2_PORT", "")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", "")
	t.Setenv("L4D2_SERVER_UNIT_FILE", unitPath)

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "127.0.0.1:12138" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "127.0.0.1:12138")
	}
}

func TestResolveRconURLSupportsGamePortAlias(t *testing.T) {
	forceSystemdStartup(t)
	t.Setenv("L4D2_RCON_URL", "127.0.0.1:27015")
	t.Setenv("L4D2_RCON_PORT", "")
	t.Setenv("L4D2_PORT", "12139")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", filepath.Join(t.TempDir(), "missing.service"))
	t.Setenv("L4D2_SERVER_UNIT_FILE", "")

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "127.0.0.1:12139" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "127.0.0.1:12139")
	}
}

func TestResolveRconURLExplicitPortOverride(t *testing.T) {
	forceSystemdStartup(t)
	unitPath := filepath.Join(t.TempDir(), "l4d2.service")
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/l4d2/srcds_run +hostport 12138\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_RCON_URL", "127.0.0.1:27015")
	t.Setenv("L4D2_RCON_PORT", "12140")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "127.0.0.1:12140" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "127.0.0.1:12140")
	}
}

func TestResolveRconURLFallsBackWhenUnitIsMissing(t *testing.T) {
	forceSystemdStartup(t)
	t.Setenv("L4D2_RCON_URL", "127.0.0.1:12141")
	t.Setenv("L4D2_RCON_PORT", "")
	t.Setenv("L4D2_PORT", "")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", filepath.Join(t.TempDir(), "missing.service"))
	t.Setenv("L4D2_SERVER_UNIT_FILE", "")

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "127.0.0.1:12141" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "127.0.0.1:12141")
	}
}

func TestResolveRconURLPreservesIPv6Host(t *testing.T) {
	forceSystemdStartup(t)
	unitPath := filepath.Join(t.TempDir(), "l4d2.service")
	if err := os.WriteFile(unitPath, []byte("[Service]\nExecStart=/l4d2/srcds_run +hostport 12138\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("L4D2_RCON_URL", "[::1]:27015")
	t.Setenv("L4D2_RCON_PORT", "")
	t.Setenv("L4D2_STARTUP_UNIT_PATH", unitPath)

	got, err := ResolveRconURL()
	if err != nil {
		t.Fatalf("ResolveRconURL() error = %v", err)
	}
	if got != "[::1]:12138" {
		t.Fatalf("ResolveRconURL() = %q, want %q", got, "[::1]:12138")
	}
}
