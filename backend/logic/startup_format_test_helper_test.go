package logic

import "testing"

// forceSystemdStartup pins the startup format to systemd for the duration of a
// test. The systemd parser tests must produce the same result on Linux and
// Windows hosts, where production code would otherwise pick the batch branch.
func forceSystemdStartup(t *testing.T) {
	t.Helper()
	previous := startupFormatOverride
	enabled := false
	startupFormatOverride = &enabled
	t.Cleanup(func() {
		startupFormatOverride = previous
	})
}

// forceBatchStartup pins the startup format to batch for the duration of a test.
func forceBatchStartup(t *testing.T) {
	t.Helper()
	previous := startupFormatOverride
	enabled := true
	startupFormatOverride = &enabled
	t.Cleanup(func() {
		startupFormatOverride = previous
	})
}
