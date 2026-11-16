package logic

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// localDiagnosticsTimeout caps the external commands. netstat and tasklist are
// fast, but a hung child process must never block the HTTP handler.
const localDiagnosticsTimeout = 4 * time.Second

// netstatListenPattern matches a listening TCP row from `netstat -ano`:
//
//	TCP    0.0.0.0:27015    0.0.0.0:0    LISTENING    1234
var netstatListenPattern = regexp.MustCompile(`^\s*TCP\s+\S+:(\d+)\s+\S+\s+LISTENING\s+(\d+)\s*$`)

// tasklistPattern matches `tasklist /FO CSV /NH` rows: "srcds.exe","1234",...
var tasklistPattern = regexp.MustCompile(`^"([^"]+)","(\d+)"`)

// CollectLocalDiagnostics gathers what the current host can observe without a
// systemd bridge. Linux keeps using the host path unit; Windows has no such
// mechanism, so the panel inspects the process table directly.
func CollectLocalDiagnostics(port int) ServerDiagnostics {
	diagnostics := ServerDiagnostics{
		ServiceName: serverStartupServiceName(),
		Port:        port,
	}
	if !IsWindows() {
		return diagnostics
	}

	if listeners, owners, ok := windowsListeners(port); ok {
		diagnostics.PortListening = len(listeners) > 0
		diagnostics.Listeners = listeners
		diagnostics.PortOwners = owners
	}
	if processes, ok := windowsSrcdsProcesses(); ok {
		diagnostics.Processes = processes
	}
	if free, total, ok := diskFreeBytes(DefaultControlDir()); ok {
		diagnostics.DiskFreeBytes = free
		diagnostics.DiskTotalBytes = total
	}
	diagnostics.Available = true
	return diagnostics
}

// windowsListeners runs netstat -ano and returns every listener on the target
// port plus the owning process names. srcds silently moves to the next port
// when binding fails, so the owner list matters more than the boolean.
func windowsListeners(port int) ([]string, []PortOwner, bool) {
	output, ok := runWindowsCommand("netstat", "-ano", "-p", "TCP")
	if !ok {
		return nil, nil, false
	}

	pidByPort := make(map[int]int)
	listeners := make([]string, 0, 2)
	for _, line := range strings.Split(output, "\n") {
		match := netstatListenPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		listenPort, err := strconv.Atoi(match[1])
		if err != nil || listenPort != port {
			continue
		}
		pid, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		listeners = append(listeners, strings.TrimSpace(line))
		pidByPort[pid] = pid
	}
	if len(listeners) == 0 {
		return listeners, nil, true
	}

	names := windowsProcessNames()
	owners := make([]PortOwner, 0, len(pidByPort))
	for pid := range pidByPort {
		owners = append(owners, PortOwner{Command: names[pid], PID: pid})
	}
	return listeners, owners, true
}

// windowsSrcdsProcesses lists running srcds instances. Used as a fallback
// signal when the port is not bound but the process exists (still booting).
func windowsSrcdsProcesses() ([]string, bool) {
	output, ok := runWindowsCommand("tasklist", "/FI", "IMAGENAME eq srcds.exe", "/FO", "CSV", "/NH")
	if !ok {
		return nil, false
	}
	processes := make([]string, 0, 2)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if !tasklistPattern.MatchString(line) {
			continue
		}
		processes = append(processes, strings.TrimSpace(line))
	}
	return processes, true
}

// windowsProcessNames maps PID to image name so port owners are readable.
func windowsProcessNames() map[int]string {
	names := make(map[int]string)
	output, ok := runWindowsCommand("tasklist", "/FO", "CSV", "/NH")
	if !ok {
		return names
	}
	for _, line := range strings.Split(output, "\n") {
		match := tasklistPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		pid, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		names[pid] = match[1]
	}
	return names
}

// runWindowsCommand executes a short-lived Windows CLI and returns stdout.
// Errors are swallowed into ok=false: diagnostics must degrade, not fail.
func runWindowsCommand(name string, args ...string) (string, bool) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = hiddenWindowAttr()

	done := make(chan struct{})
	var output []byte
	var err error
	go func() {
		output, err = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
		if err != nil {
			return "", false
		}
		return string(output), true
	case <-time.After(localDiagnosticsTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", false
	}
}
