package logic

import (
	"strings"
	"testing"
)

func TestNetstatListenPatternMatchesWindowsRow(t *testing.T) {
	cases := []struct {
		line string
		port string
		pid  string
	}{
		{"  TCP    0.0.0.0:27015          0.0.0.0:0              LISTENING       1234", "27015", "1234"},
		{"  TCP    [::]:27015             [::]:0                 LISTENING       5678", "27015", "5678"},
		{"  TCP    127.0.0.1:27020        0.0.0.0:0              LISTENING       9012", "27020", "9012"},
	}
	for _, tc := range cases {
		match := netstatListenPattern.FindStringSubmatch(tc.line)
		if match == nil {
			t.Fatalf("no match for %q", tc.line)
		}
		if match[1] != tc.port {
			t.Fatalf("port = %q, want %q", match[1], tc.port)
		}
		if match[2] != tc.pid {
			t.Fatalf("pid = %q, want %q", match[2], tc.pid)
		}
	}
}

func TestNetstatListenPatternIgnoresNonListeningRows(t *testing.T) {
	lines := []string{
		"  TCP    0.0.0.0:27015          0.0.0.0:0              ESTABLISHED     1234",
		"  TCP    0.0.0.0:27015          0.0.0.0:0              TIME_WAIT       0",
		"  UDP    0.0.0.0:27015          *:*                                    1234",
		"",
	}
	for _, line := range lines {
		if netstatListenPattern.MatchString(line) {
			t.Fatalf("should not match %q", line)
		}
	}
}

func TestTasklistPatternParsesCsvRow(t *testing.T) {
	line := `"srcds.exe","4321","Console","1","120,000 K"`
	match := tasklistPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("no match for tasklist row")
	}
	if match[1] != "srcds.exe" {
		t.Fatalf("name = %q, want srcds.exe", match[1])
	}
	if match[2] != "4321" {
		t.Fatalf("pid = %q, want 4321", match[2])
	}
}

func TestTasklistPatternIgnoresInfoMessage(t *testing.T) {
	line := "INFO: No tasks are running which match the specified criteria."
	if tasklistPattern.MatchString(line) {
		t.Fatal("info message must not match")
	}
}

func TestCollectLocalDiagnosticsOnLinuxKeepsPortOnly(t *testing.T) {
	if IsWindows() {
		t.Skip("linux-specific expectations")
	}
	diagnostics := CollectLocalDiagnostics(27015)
	if diagnostics.Port != 27015 {
		t.Fatalf("port = %d, want 27015", diagnostics.Port)
	}
	if diagnostics.Available {
		t.Fatal("local diagnostics should stay unavailable on Linux")
	}
	if diagnostics.PortListening {
		t.Fatal("Linux must not claim a listening port without the host bridge")
	}
}

func TestCollectServerDiagnosticsDoesNotBlockWithoutSystemd(t *testing.T) {
	if DiagnosticsAvailable() {
		t.Skip("requires a host without systemd")
	}
	// A zero wait would still loop once; the unsupported branch must return
	// immediately instead of waiting for the bridge.
	response := CollectServerDiagnostics(0)
	if len(response.Hints) == 0 {
		t.Fatal("expected at least one hint")
	}
	found := false
	for _, hint := range response.Hints {
		if strings.Contains(hint.Title, "不支持") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an unsupported-platform hint, got %+v", response.Hints)
	}
}
