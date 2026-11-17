package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// PortOwner 是占用端口的一个进程，用于判断"端口通但不是我方游戏"。
type PortOwner struct {
	Command string `json:"command"`
	PID     int    `json:"pid"`
}

// HealthSnapshot 是一次健康检查的完整结果。
type HealthSnapshot struct {
	CheckedAt     string      `json:"checked_at"`
	State         string      `json:"state"`
	PID           int         `json:"pid,omitempty"`
	PortListening bool        `json:"port_listening"`
	PortOwners    []PortOwner `json:"port_owners,omitempty"`
	RCONReachable bool        `json:"rcon_reachable"`
	RCONError     string      `json:"rcon_error,omitempty"`
	LogTail       string      `json:"log_tail,omitempty"`
	Reason        string      `json:"reason,omitempty"`
	// Source 记录快照来源：agent / local / cache。
	Source string `json:"source,omitempty"`
}

// JobStatus 是宿主机 Agent 返回的任务状态枚举。
const (
	JobSuccess     = "success"
	JobError       = "error"
	JobUnsupported = "unsupported"
	JobTimeout     = "timeout"
	JobDenied      = "denied"
	JobPending     = "pending"
	JobRunning     = "running"
)

// JobRequest 是面板写给宿主 Agent 的任务请求（设计文档 §6.3）。
type JobRequest struct {
	JobID       string         `json:"job_id"`
	Action      string         `json:"action"`
	RuntimeID   string         `json:"runtime_id,omitempty"`
	Kind        Kind           `json:"kind,omitempty"`
	Target      Target         `json:"target,omitempty"`
	Spec        *LaunchSpec    `json:"spec,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
	RequestedAt string         `json:"requested_at"`
	Requester   string         `json:"requester"`
}

// JobResult 是 Agent 回写的结果。
type JobResult struct {
	JobID        string         `json:"job_id"`
	Action       string         `json:"action"`
	Status       string         `json:"status"`
	Message      string         `json:"message,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
	StartedAt    string         `json:"started_at,omitempty"`
	FinishedAt   string         `json:"finished_at,omitempty"`
	AgentVersion string         `json:"agent_version,omitempty"`
}

// Event 是状态变更事件，写入 runtime_events 供回溯"什么时候挂的"。
type Event struct {
	At        string `json:"at"`
	RuntimeID string `json:"runtime_id"`
	Kind      Kind   `json:"kind"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
}

// ListResult 是 /runtime/list 的返回体。
type ListResult struct {
	Runtimes     []Descriptor                    `json:"runtimes"`
	DefaultID    string                          `json:"default_id"`
	AgentMode    string                          `json:"agent_mode"`
	Capabilities map[string]ProviderCapabilities `json:"capabilities"`
	Warnings     []string                        `json:"warnings,omitempty"`
}

// 注册表持久化。写入采用"临时文件 + fsync + rename"，避免半截 JSON。
const (
	runtimesFileName      = "runtimes.json"
	runtimeStateFileName  = "runtime_state.json"
	runtimeEventsFileName = "runtime-events.jsonl"
	runtimeEventsKeep     = 2000
)

var (
	registryMu      sync.RWMutex
	registryCache   []Descriptor
	registryLoaded  bool
	registryLoadErr error
	registryDataDir = "./data"
	stateFileMu     sync.Mutex
	eventMu         sync.Mutex
)

// SetDataDir 让调用方（logic 包）注入面板数据目录。
// runtime 包不直接依赖 consts，避免循环导入。
func SetDataDir(dir string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if strings.TrimSpace(dir) == "" {
		return
	}
	registryDataDir = dir
	registryLoaded = false
	registryCache = nil
	registryLoadErr = nil
}

func runtimesPath() string {
	return filepath.Join(registryDataDir, runtimesFileName)
}

func runtimeStatePath() string {
	return filepath.Join(registryDataDir, runtimeStateFileName)
}

func runtimeEventsPath() string {
	return filepath.Join(registryDataDir, runtimeEventsFileName)
}

// List 返回注册表快照。legacy 合成项不落盘，只在内存里补上。
func List() ([]Descriptor, error) {
	registryMu.RLock()
	if registryLoaded {
		items := cloneDescriptors(registryCache)
		err := registryLoadErr
		registryMu.RUnlock()
		return items, err
	}
	registryMu.RUnlock()

	registryMu.Lock()
	defer registryMu.Unlock()
	if registryLoaded {
		return cloneDescriptors(registryCache), registryLoadErr
	}
	items, err := loadRuntimesLocked()
	registryCache = items
	registryLoaded = true
	registryLoadErr = err
	return cloneDescriptors(items), err
}

func loadRuntimesLocked() ([]Descriptor, error) {
	path := runtimesPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Descriptor{}, nil
		}
		return []Descriptor{}, fmt.Errorf("读取运行时注册表失败: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []Descriptor{}, nil
	}
	var items []Descriptor
	if err := json.Unmarshal(data, &items); err != nil {
		// 损坏的注册表不能静默丢弃，备份一份便于人工修复。
		backup := path + ".corrupt-" + time.Now().Format("20060102-150405")
		_ = os.Rename(path, backup)
		return []Descriptor{}, fmt.Errorf("运行时注册表格式损坏，已备份到 %s: %w", filepath.Base(backup), err)
	}
	normalized := make([]Descriptor, 0, len(items))
	for _, item := range items {
		value, normalizeErr := item.Normalize()
		if normalizeErr != nil {
			// 单条损坏不影响整体，但要在日志里可见。
			fmt.Printf("[runtime] 跳过无效运行时 %s: %v\n", item.ID, normalizeErr)
			continue
		}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

// Save 全量替换注册表并落盘。调用方负责先做业务校验。
func Save(items []Descriptor) error {
	registryMu.Lock()
	defer registryMu.Unlock()

	normalized := make([]Descriptor, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		value, err := item.Normalize()
		if err != nil {
			return err
		}
		if _, duplicated := seen[value.ID]; duplicated {
			return fmt.Errorf("运行时 ID 重复: %s", value.ID)
		}
		seen[value.ID] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	if err := writeJSONAtomic(runtimesPath(), normalized, 0600); err != nil {
		return err
	}
	registryCache = normalized
	registryLoaded = true
	registryLoadErr = nil
	return nil
}

// Get 按 ID 取单个运行时。
func Get(id string) (Descriptor, bool) {
	items, err := List()
	if err != nil {
		return Descriptor{}, false
	}
	id = strings.TrimSpace(strings.ToLower(id))
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return Descriptor{}, false
}

// Upsert 新增或替换一个运行时。
func Upsert(item Descriptor) (Descriptor, error) {
	value, err := item.Normalize()
	if err != nil {
		return Descriptor{}, err
	}
	items, err := List()
	if err != nil {
		return Descriptor{}, err
	}
	replaced := false
	for index := range items {
		if items[index].ID != value.ID {
			continue
		}
		// 保留原始创建时间，避免每次保存都刷新。
		if items[index].CreatedAt != "" {
			value.CreatedAt = items[index].CreatedAt
		}
		items[index] = value
		replaced = true
		break
	}
	if !replaced {
		items = append(items, value)
	}
	if err := Save(items); err != nil {
		return Descriptor{}, err
	}
	return value, nil
}

// Remove 删除一个运行时。
func Remove(id string) error {
	id = strings.TrimSpace(strings.ToLower(id))
	items, err := List()
	if err != nil {
		return err
	}
	remaining := make([]Descriptor, 0, len(items))
	for _, item := range items {
		if item.ID == id {
			continue
		}
		remaining = append(remaining, item)
	}
	return Save(remaining)
}

// UpdateHealth 把最新健康快照写回注册表，并单独缓存一份供首屏使用。
func UpdateHealth(id string, snapshot HealthSnapshot) error {
	items, err := List()
	if err != nil {
		return err
	}
	found := false
	for index := range items {
		if items[index].ID != id {
			continue
		}
		value := snapshot
		items[index].LastHealth = &value
		found = true
		break
	}
	if !found {
		return fmt.Errorf("运行时不存在: %s", id)
	}
	if err := Save(items); err != nil {
		return err
	}
	return saveStateCache(id, snapshot)
}

// CachedHealth 读取上次健康快照，避免首屏空白。
func CachedHealth(id string) (HealthSnapshot, bool) {
	stateFileMu.Lock()
	defer stateFileMu.Unlock()
	var cache map[string]HealthSnapshot
	data, err := os.ReadFile(runtimeStatePath())
	if err != nil {
		return HealthSnapshot{}, false
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return HealthSnapshot{}, false
	}
	snapshot, ok := cache[id]
	return snapshot, ok
}

func saveStateCache(id string, snapshot HealthSnapshot) error {
	stateFileMu.Lock()
	defer stateFileMu.Unlock()
	cache := make(map[string]HealthSnapshot)
	if data, err := os.ReadFile(runtimeStatePath()); err == nil {
		_ = json.Unmarshal(data, &cache)
	}
	cache[id] = snapshot
	return writeJSONAtomic(runtimeStatePath(), cache, 0600)
}

// AppendEvent 追加一条状态变更事件，超过上限后滚动丢弃最旧的。
func AppendEvent(event Event) {
	if event.At == "" {
		event.At = time.Now().UTC().Format(time.RFC3339)
	}
	eventMu.Lock()
	defer eventMu.Unlock()

	events, _ := readEventsLocked()
	events = append(events, event)
	if len(events) > runtimeEventsKeep {
		events = events[len(events)-runtimeEventsKeep:]
	}
	var builder strings.Builder
	for _, item := range events {
		data, err := json.Marshal(item)
		if err != nil {
			continue
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	if err := writeFileAtomic(runtimeEventsPath(), []byte(builder.String()), 0600); err != nil {
		fmt.Printf("[runtime] 写入运行时事件失败: %v\n", err)
	}
}

// ListEvents 返回指定运行时的最近事件（最新在后）。
func ListEvents(id string, limit int) []Event {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	all, _ := readEventsLocked()
	filtered := make([]Event, 0, len(all))
	id = strings.TrimSpace(strings.ToLower(id))
	for _, item := range all {
		if id != "" && item.RuntimeID != id {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return filtered
}

func readEventsLocked() ([]Event, error) {
	file, err := os.Open(runtimeEventsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []Event{}, nil
		}
		return nil, err
	}
	defer file.Close()

	events := make([]Event, 0, 64)
	decoder := json.NewDecoder(file)
	for {
		var item Event
		if err := decoder.Decode(&item); err != nil {
			break
		}
		events = append(events, item)
	}
	return events, nil
}

func cloneDescriptors(items []Descriptor) []Descriptor {
	result := make([]Descriptor, len(items))
	copy(result, items)
	return result
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, mode)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".runtime-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("替换文件失败: %w", err)
	}
	return nil
}
