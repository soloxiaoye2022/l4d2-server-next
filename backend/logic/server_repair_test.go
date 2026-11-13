package logic

import (
	"errors"
	"strings"
	"testing"
	"time"

	"l4d2-manager-next/consts"
)

func TestRequestServerRepairRejectsUnknownAction(t *testing.T) {
	if _, err := RequestServerRepair("rm-rf", "admin", time.Millisecond); err == nil {
		t.Fatal("RequestServerRepair() error = nil, want error for unknown action")
	}
}

func TestRequestServerRepairNoopWhenAddonsClean(t *testing.T) {
	originalPath := consts.AddonsBasePath
	originalData := consts.ManagerDataPath
	consts.AddonsBasePath = t.TempDir()
	consts.ManagerDataPath = t.TempDir()
	t.Cleanup(func() {
		consts.AddonsBasePath = originalPath
		consts.ManagerDataPath = originalData
	})

	result, err := RequestServerRepair(RepairActionQuarantine, "admin", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("RequestServerRepair() error = %v", err)
	}
	if result.Status != RepairStatusNoop {
		t.Fatalf("status = %q, want %q", result.Status, RepairStatusNoop)
	}
	if len(result.Quarantined) != 0 {
		t.Fatalf("quarantined = %+v, want empty", result.Quarantined)
	}
	if result.Timeout {
		t.Fatal("clean addons must not wait for the host bridge")
	}
}

func TestRequestServerRepairDefaultsToQuarantineOnly(t *testing.T) {
	originalPath := consts.AddonsBasePath
	originalData := consts.ManagerDataPath
	consts.AddonsBasePath = t.TempDir()
	consts.ManagerDataPath = t.TempDir()
	t.Cleanup(func() {
		consts.AddonsBasePath = originalPath
		consts.ManagerDataPath = originalData
	})

	result, err := RequestServerRepair("", "admin", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("RequestServerRepair() error = %v", err)
	}
	if result.Action != RepairActionQuarantine {
		t.Fatalf("action = %q, want %q (empty body must not restart the server)", result.Action, RepairActionQuarantine)
	}
}

func TestRepairSummaryMentionsQuarantineAndRestart(t *testing.T) {
	message := repairSummary(RepairResult{
		Status:      RepairStatusOK,
		Quarantined: []RepairQuarantinedVPK{{Name: "a.vpk", Size: 12, Reason: "目录树为空（空 VPK）"}},
		Restart:     RepairRestartState{Requested: true, PortListening: true, Port: 12138},
	})
	for _, want := range []string{"已隔离 1", "12138"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message = %q, want it to contain %q", message, want)
		}
	}
}

func TestRepairSummaryReportsRemainingFiles(t *testing.T) {
	message := repairSummary(RepairResult{
		Status:      RepairStatusPartial,
		Quarantined: []RepairQuarantinedVPK{{Name: "a.vpk", Size: 12}},
		Remaining:   []InvalidAddonVPK{{Name: "b.vpk", Size: 8}},
	})
	if !strings.Contains(message, "仍有 1 个文件异常") {
		t.Fatalf("message = %q, want it to mention remaining files", message)
	}
}

func TestRepairSummaryMentionsHotReload(t *testing.T) {
	message := repairSummary(RepairResult{
		Status:      RepairStatusOK,
		Quarantined: []RepairQuarantinedVPK{{Name: "a.vpk", Size: 12}},
		Reload:      RepairReloadState{Requested: true, OK: true},
	})
	if !strings.Contains(message, "已热重载") || !strings.Contains(message, "服务未重启") {
		t.Fatalf("message = %q, want hot reload wording", message)
	}
	if strings.Contains(message, "服务已重启") {
		t.Fatalf("message = %q, hot reload must not claim a restart", message)
	}
}

func TestRunRepairHotReloadSuccessAndFailure(t *testing.T) {
	original := executeRepairLoad
	t.Cleanup(func() { executeRepairLoad = original })

	executeRepairLoad = func() (string, error) { return "update_addon_paths", nil }
	state := runRepairHotReload()
	if !state.Requested || !state.OK || state.Message == "" {
		t.Fatalf("reload state = %+v, want successful reload", state)
	}

	executeRepairLoad = func() (string, error) { return "", errors.New("rcon offline") }
	state = runRepairHotReload()
	if state.OK {
		t.Fatalf("reload state = %+v, want failure", state)
	}
	if !strings.Contains(state.Message, "热重载失败") {
		t.Fatalf("message = %q, want failure wording", state.Message)
	}
}

func TestRepairHistoryAppendAndListNewestFirst(t *testing.T) {
	originalData := consts.ManagerDataPath
	consts.ManagerDataPath = t.TempDir()
	t.Cleanup(func() { consts.ManagerDataPath = originalData })

	for i := 0; i < 3; i++ {
		if err := appendRepairHistoryEntry(RepairHistoryEntry{
			ID:      string(rune('a' + i)),
			At:      time.Now().Format(time.RFC3339),
			Action:  RepairActionQuarantine,
			Status:  RepairStatusOK,
			Message: "已隔离 1 个损坏 VPK",
		}); err != nil {
			t.Fatalf("appendRepairHistoryEntry() error = %v", err)
		}
	}

	entries, err := ListRepairHistory(2)
	if err != nil {
		t.Fatalf("ListRepairHistory() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].ID != "c" || entries[1].ID != "b" {
		t.Fatalf("entries order = %q,%q, want c,b", entries[0].ID, entries[1].ID)
	}
}

func TestListRepairHistoryReturnsEmptyWhenMissing(t *testing.T) {
	originalData := consts.ManagerDataPath
	consts.ManagerDataPath = t.TempDir()
	t.Cleanup(func() { consts.ManagerDataPath = originalData })

	entries, err := ListRepairHistory(10)
	if err != nil {
		t.Fatalf("ListRepairHistory() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(entries))
	}
}
