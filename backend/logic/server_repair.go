package logic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"l4d2-manager-next/consts"

	"github.com/google/uuid"
)

const (
	RepairActionQuarantine           = "quarantine"
	RepairActionQuarantineAndReload  = "quarantine_and_reload"
	RepairActionQuarantineAndRestart = "quarantine_and_restart"
	RepairActionRestartService       = "restart_service"
	RepairActionKillPortAndRestart   = "kill_port_and_restart"

	RepairStatusOK      = "ok"
	RepairStatusPartial = "partial"
	RepairStatusNoop    = "noop"
	RepairStatusTimeout = "timeout"
	RepairStatusError   = "error"

	repairHistoryFileName = "repair-history.jsonl"
	repairHistoryKeep     = 300
	repairHistoryMax      = 500
)

var (
	serverRepairMu    sync.Mutex
	repairHistoryMu   sync.Mutex
	executeRepairLoad = ExecuteMapHotReloadCommand
)

type repairRequest struct {
	RequestID   string `json:"request_id"`
	Action      string `json:"action"`
	ServiceName string `json:"service_name"`
	RequestedAt string `json:"requested_at"`
}

type RepairQuarantinedVPK struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"`
}

type RepairCleanedReference struct {
	File    string `json:"file"`
	Removed int    `json:"removed"`
	Backup  string `json:"backup,omitempty"`
	Error   string `json:"error,omitempty"`
}

type RepairReloadState struct {
	Requested bool   `json:"requested"`
	OK        bool   `json:"ok"`
	Command   string `json:"command,omitempty"`
	Message   string `json:"message,omitempty"`
}

type RepairRestartState struct {
	Requested     bool   `json:"requested"`
	OK            bool   `json:"ok"`
	ActiveState   string `json:"active_state,omitempty"`
	Port          int    `json:"port,omitempty"`
	PortListening bool   `json:"port_listening"`
	Message       string `json:"message,omitempty"`
}

type RepairResult struct {
	RequestID         string                   `json:"request_id,omitempty"`
	Action            string                   `json:"action"`
	Status            string                   `json:"status"`
	Message           string                   `json:"message"`
	AddonsDir         string                   `json:"addons_dir,omitempty"`
	QuarantineDir     string                   `json:"quarantine_dir,omitempty"`
	Quarantined       []RepairQuarantinedVPK   `json:"quarantined"`
	CleanedReferences []RepairCleanedReference `json:"cleaned_references"`
	Remaining         []InvalidAddonVPK        `json:"remaining"`
	VerifyRemaining   []InvalidAddonVPK        `json:"verify_remaining"`
	Killed            []PortOwner              `json:"killed"`
	Reload            RepairReloadState        `json:"reload"`
	Restart           RepairRestartState       `json:"restart"`
	FinishedAt        string                   `json:"finished_at,omitempty"`
	Timeout           bool                     `json:"timeout"`
}

// RepairHistoryEntry is one persisted repair attempt. It is appended to
// data/repair-history.jsonl so the panel can show earlier actions.
type RepairHistoryEntry struct {
	ID                string                   `json:"id"`
	At                string                   `json:"at"`
	Operator          string                   `json:"operator,omitempty"`
	Action            string                   `json:"action"`
	Status            string                   `json:"status"`
	Message           string                   `json:"message"`
	QuarantineDir     string                   `json:"quarantine_dir,omitempty"`
	Quarantined       []RepairQuarantinedVPK   `json:"quarantined"`
	CleanedReferences []RepairCleanedReference `json:"cleaned_references"`
	Remaining         []InvalidAddonVPK        `json:"remaining"`
	Reload            RepairReloadState        `json:"reload"`
	Restart           RepairRestartState       `json:"restart"`
	DurationMS        int64                    `json:"duration_ms"`
}

func repairRequestPath() string {
	return filepath.Join(diagnosticsControlDir(), "repair-request.json")
}

func repairResultPath() string {
	return filepath.Join(diagnosticsControlDir(), "repair-result.json")
}

func repairHistoryPath() string {
	return filepath.Join(consts.ManagerDataPath, repairHistoryFileName)
}

// RequestServerRepair asks the host-side path unit to quarantine broken addon
// VPKs. The host script re-scans addons itself, so the request carries no file
// names and cannot be used to move arbitrary paths. Reload runs through RCON
// inside the container; restart is performed by the host script.
func RequestServerRepair(action string, operator string, wait time.Duration) (RepairResult, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		action = RepairActionQuarantine
	}
	if !isRepairAction(action) {
		return RepairResult{Action: action, Status: RepairStatusError}, fmt.Errorf("不支持的处置动作: %s", action)
	}

	serverRepairMu.Lock()
	defer serverRepairMu.Unlock()

	startedAt := time.Now()
	if !isServiceRepairAction(action) && len(FindInvalidAddonVPKs()) == 0 {
		result := RepairResult{
			Action:            action,
			Status:            RepairStatusNoop,
			Message:           "addons 目录下没有发现损坏的 VPK，无需处置",
			Quarantined:       []RepairQuarantinedVPK{},
			CleanedReferences: []RepairCleanedReference{},
			Remaining:         []InvalidAddonVPK{},
			VerifyRemaining:   []InvalidAddonVPK{},
		}
		appendRepairHistory(result, operator, startedAt)
		return result, nil
	}

	request := repairRequest{
		RequestID:   uuid.NewString(),
		Action:      action,
		ServiceName: serverStartupServiceName(),
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return RepairResult{Action: action, Status: RepairStatusError}, fmt.Errorf("生成处置请求失败: %w", err)
	}
	if err := writeManagerFileAtomic(repairRequestPath(), data, 0600); err != nil {
		result := RepairResult{
			Action:  action,
			Status:  RepairStatusError,
			Message: fmt.Sprintf("提交处置请求失败: %v", err),
		}
		appendRepairHistory(result, operator, startedAt)
		return result, nil
	}

	deadline := time.Now().Add(wait)
	for {
		if result, err := readServerRepairResult(); err == nil && result.RequestID == request.RequestID {
			result.VerifyRemaining = FindInvalidAddonVPKs()
			if result.VerifyRemaining == nil {
				result.VerifyRemaining = []InvalidAddonVPK{}
			}
			finalizeRepairResult(&result, action)
			appendRepairHistory(result, operator, startedAt)
			return result, nil
		}
		if time.Now().After(deadline) {
			result := RepairResult{
				Action:  action,
				Status:  RepairStatusTimeout,
				Message: "等待宿主机处置结果超时，请确认 l4d2-repair.path 已启用且 repair-addons.sh 存在",
				Timeout: true,
			}
			appendRepairHistory(result, operator, startedAt)
			return result, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func isRepairAction(action string) bool {
	switch action {
	case RepairActionQuarantine, RepairActionQuarantineAndReload, RepairActionQuarantineAndRestart,
		RepairActionRestartService, RepairActionKillPortAndRestart:
		return true
	default:
		return false
	}
}

// isServiceRepairAction marks actions that only touch the service, never the
// addons directory, so the "no broken VPK" short circuit must not apply.
func isServiceRepairAction(action string) bool {
	return action == RepairActionRestartService || action == RepairActionKillPortAndRestart
}

// finalizeRepairResult runs the container-side follow-up (hot reload) and
// composes the user-facing message.
func finalizeRepairResult(result *RepairResult, action string) {
	if action == RepairActionQuarantineAndReload && len(result.Quarantined) > 0 {
		result.Reload = runRepairHotReload()
		if !result.Reload.OK && result.Status == RepairStatusOK {
			result.Status = RepairStatusPartial
		}
	}
	if result.Message == "" {
		result.Message = repairSummary(*result)
	} else if result.Reload.Requested {
		result.Message = repairSummary(*result)
	}
}

func runRepairHotReload() RepairReloadState {
	state := RepairReloadState{Requested: true, Command: GetMapHotReloadCommand()}
	output, err := executeRepairLoad()
	if err != nil {
		state.Message = fmt.Sprintf("热重载失败: %v", err)
		return state
	}
	state.OK = true
	state.Message = strings.TrimSpace(output)
	if state.Message == "" {
		state.Message = "热重载指令已执行"
	}
	return state
}

func readServerRepairResult() (RepairResult, error) {
	var result RepairResult
	data, err := os.ReadFile(repairResultPath())
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// repairSummary builds the user-facing message. The host script also fills a
// message, but the panel owns reload wording because reload happens here.
func repairSummary(result RepairResult) string {
	switch result.Status {
	case RepairStatusNoop:
		return "addons 目录下没有发现损坏的 VPK"
	case RepairStatusError:
		return "处置失败，请查看宿主机 repair-result.json"
	}
	if isServiceRepairAction(result.Action) {
		if result.Restart.PortListening {
			return fmt.Sprintf("服务已重启，端口 %d 已监听", result.Restart.Port)
		}
		return "服务已重启，但端口未在 30 秒内监听"
	}
	message := fmt.Sprintf("已隔离 %d 个损坏 VPK", len(result.Quarantined))
	if len(result.CleanedReferences) > 0 {
		message += "，并清理了 addonlist/maplist 中的引用"
	}
	if len(result.Remaining) > 0 {
		message += fmt.Sprintf("，仍有 %d 个文件异常", len(result.Remaining))
	}
	if result.Reload.Requested {
		if result.Reload.OK {
			message += "，已热重载（服务未重启）"
		} else {
			message += fmt.Sprintf("，但热重载失败：%s", result.Reload.Message)
		}
	}
	if result.Restart.Requested && result.Restart.PortListening {
		message += fmt.Sprintf("，服务已重启，端口 %d 已监听", result.Restart.Port)
	}
	return message
}

func appendRepairHistory(result RepairResult, operator string, startedAt time.Time) {
	entry := RepairHistoryEntry{
		ID:                uuid.NewString(),
		At:                time.Now().Format(time.RFC3339),
		Operator:          strings.TrimSpace(operator),
		Action:            result.Action,
		Status:            result.Status,
		Message:           result.Message,
		QuarantineDir:     result.QuarantineDir,
		Quarantined:       result.Quarantined,
		CleanedReferences: result.CleanedReferences,
		Remaining:         result.Remaining,
		Reload:            result.Reload,
		Restart:           result.Restart,
		DurationMS:        time.Since(startedAt).Milliseconds(),
	}
	if entry.Quarantined == nil {
		entry.Quarantined = []RepairQuarantinedVPK{}
	}
	if entry.CleanedReferences == nil {
		entry.CleanedReferences = []RepairCleanedReference{}
	}
	if entry.Remaining == nil {
		entry.Remaining = []InvalidAddonVPK{}
	}
	if err := appendRepairHistoryEntry(entry); err != nil {
		fmt.Printf("[repair] 写入处置记录失败: %v\n", err)
	}
}

func appendRepairHistoryEntry(entry RepairHistoryEntry) error {
	repairHistoryMu.Lock()
	defer repairHistoryMu.Unlock()

	path := repairHistoryPath()
	entries, err := readRepairHistoryEntries()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entries = append(entries, entry)
	if len(entries) > repairHistoryMax {
		entries = entries[len(entries)-repairHistoryKeep:]
	}

	var builder strings.Builder
	for _, item := range entries {
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	return writeManagerFileAtomic(path, []byte(builder.String()), 0644)
}

func readRepairHistoryEntries() ([]RepairHistoryEntry, error) {
	file, err := os.Open(repairHistoryPath())
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]RepairHistoryEntry, 0, 32)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry RepairHistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return entries, err
	}
	return entries, nil
}

// ListRepairHistory returns the newest entries first.
func ListRepairHistory(limit int) ([]RepairHistoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	entries, err := readRepairHistoryEntries()
	if err != nil {
		if os.IsNotExist(err) {
			return []RepairHistoryEntry{}, nil
		}
		return nil, err
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}
