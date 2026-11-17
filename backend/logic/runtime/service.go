package runtime

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 任务动作名，与宿主机 Agent 的 action 白名单一一对应。
const (
	ActionProbe   = "probe"
	ActionInspect = "inspect"
	ActionStart   = "start"
	ActionStop    = "stop"
	ActionRestart = "restart"
	ActionLogs    = "logs"
	ActionDetect  = "detect"
	ActionAdopt   = "adopt"
	ActionExport  = "export"
	ActionImport  = "import"
	ActionMigrate = "migrate"
)

// 默认超时。迁移涉及停源+起目标+健康检查，必须比普通动作长。
const (
	probeTimeout   = 8 * time.Second
	inspectTimeout = 20 * time.Second
	actionTimeout  = 90 * time.Second
	detectTimeout  = 45 * time.Second
	logsTimeout    = 20 * time.Second
	migrateTimeout = 240 * time.Second
)

// modeProbeTimeout 只用于启动时判定 Agent 是否存在。
// 必须远小于 probeTimeout：Agent 正常时 200ms 内就会回包，
// 没有 Agent 时不能让 /platform 首屏等满 8 秒。
const modeProbeTimeout = 2500 * time.Millisecond

// Settings 是运行时子系统需要的面板侧配置。
type Settings struct {
	DataDir      string
	ControlDir   string
	DefaultUser  string
	AllowedRoots []string
	AgentMode    string
}

// LegacyProvider 由 logic 包注入：注册表为空时用它合成旧部署的默认运行时。
// 用函数注入而不是直接 import logic，避免循环依赖。
type LegacyProvider func() []Descriptor

var (
	legacyProvider   LegacyProvider
	legacyProviderMu sync.RWMutex
)

// RegisterLegacyProvider 由 logic 包在 init 中调用一次。
func RegisterLegacyProvider(provider LegacyProvider) {
	legacyProviderMu.Lock()
	defer legacyProviderMu.Unlock()
	legacyProvider = provider
}

func legacyDescriptors() []Descriptor {
	legacyProviderMu.RLock()
	provider := legacyProvider
	legacyProviderMu.RUnlock()
	if provider == nil {
		return nil
	}
	items := provider()
	normalized := make([]Descriptor, 0, len(items))
	for _, item := range items {
		value, err := item.Normalize()
		if err != nil {
			continue
		}
		value.Origin = OriginLegacy
		normalized = append(normalized, value)
	}
	return normalized
}

// Manager 是运行时的业务入口。一个进程只应有一个实例。
type Manager struct {
	mu            sync.RWMutex
	settings      Settings
	client        Client
	mode          string
	lastModeProbe time.Time
}

// modeReprobeInterval 控制多久允许重新探测一次 Agent。
//
// 为什么需要重探：面板容器往往比宿主机 Agent 先启动。
// 启动时探测失败就永久锁死在 local，用户装好 Agent 后必须重启面板才生效，
// 这是运维上最容易踩的坑。改为低频重探，装好后最多 30 秒自动恢复。
const modeReprobeInterval = 30 * time.Second

var (
	manager     *Manager
	managerOnce sync.Once
)

// Init 初始化全局 Manager。重复调用会覆盖配置但不重置注册表缓存。
func Init(settings Settings) *Manager {
	managerOnce.Do(func() {
		manager = &Manager{}
	})
	manager.configure(settings)
	return manager
}

// Default 返回全局 Manager，未初始化时用默认配置兜底。
func Default() *Manager {
	managerOnce.Do(func() {
		manager = &Manager{}
		manager.configure(Settings{})
	})
	return manager
}

func (m *Manager) configure(settings Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if settings.DataDir != "" {
		SetDataDir(settings.DataDir)
	}
	if settings.DefaultUser == "" {
		settings.DefaultUser = "ubuntu"
	}
	if len(settings.AllowedRoots) == 0 {
		settings.AllowedRoots = DefaultAllowedRoots()
	}
	m.settings = settings
	client, mode := selectClient(settings)
	m.client = client
	// 能力探测必须真的能和 Agent 通信，不能只看目录可写。
	// 面板容器里 /control 永远可写，但宿主机可能压根没装 Agent，
	// 只看可写会让 UI 显示"全能力"，点一下就超时。
	m.mode = mode
	m.lastModeProbe = time.Now()
}

// refreshMode 在必要时重新探测 Agent，并在候选通道变化时成对切换 client 与 mode。
//
// 只有"显式配置"才跳过重探；auto 模式下一次探测失败不代表永远失败。
func (m *Manager) refreshMode() {
	m.mu.RLock()
	requested := strings.TrimSpace(strings.ToLower(m.settings.AgentMode))
	elapsed := time.Since(m.lastModeProbe)
	currentMode := m.mode
	m.mu.RUnlock()

	if requested != "" && requested != "auto" {
		return
	}
	if currentMode == AgentModeBridge {
		return
	}
	if elapsed < modeReprobeInterval {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// 二次检查：并发请求下只让一个真正去探测。
	if m.mode == AgentModeBridge || time.Since(m.lastModeProbe) < modeReprobeInterval {
		return
	}
	m.lastModeProbe = time.Now()
	client, mode := selectClient(m.settings)
	if mode == AgentModeBridge {
		m.client = client
		m.mode = mode
		fmt.Println("[runtime] 检测到宿主机 Agent，本次通道已切换为 bridge")
	}
}

// selectClient 同时决定 client 与 mode。
//
// 关键点：mode 与 client 必须成对切换。曾经只改 mode 不改 client，
// 结果 agent_mode 显示 local，但实际仍在等 Agent 结果文件，全部超时。
func selectClient(settings Settings) (Client, string) {
	bridge := &FileBridgeClient{ControlDir: controlDirOf(settings)}
	local := &LocalClient{}

	switch strings.TrimSpace(strings.ToLower(settings.AgentMode)) {
	case AgentModeBridge:
		return bridge, AgentModeBridge
	case AgentModeLocal:
		return local, AgentModeLocal
	case AgentModeDisabled:
		return &unavailableClient{}, AgentModeDisabled
	}

	if bridge.Available() && bridge.Probe(modeProbeTimeout) == nil {
		return bridge, AgentModeBridge
	}
	if local.Available() {
		return local, AgentModeLocal
	}
	return &unavailableClient{}, AgentModeDisabled
}

func controlDirOf(settings Settings) string {
	if value := strings.TrimSpace(settings.ControlDir); value != "" {
		return value
	}
	return defaultControlDir()
}

// unavailableClient 在既无 Agent 又无本地工具时使用，所有动作明确报错。
type unavailableClient struct{}

func (c *unavailableClient) Mode() string    { return AgentModeDisabled }
func (c *unavailableClient) Available() bool { return false }
func (c *unavailableClient) Probe(time.Duration) error {
	return fmt.Errorf("未检测到宿主机 Agent，也无法在本地执行进程控制")
}
func (c *unavailableClient) Submit(request JobRequest, _ time.Duration) (JobResult, error) {
	return JobResult{
		JobID:   request.JobID,
		Action:  request.Action,
		Status:  JobUnsupported,
		Message: "未检测到宿主机 Agent，该操作不可用",
	}, nil
}

// Settings 返回当前配置副本。
func (m *Manager) Settings() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings
}

// AgentMode 返回当前通道模式。
func (m *Manager) AgentMode() string {
	m.refreshMode()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mode
}

func (m *Manager) clientSnapshot() Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// Probe 验证 Agent 是否真的活着。
func (m *Manager) Probe() error {
	client := m.clientSnapshot()
	if client == nil {
		return fmt.Errorf("未配置宿主机通道")
	}
	return client.Probe(probeTimeout)
}

// All 返回全部运行时；注册表为空时补上 legacy 合成项（不落盘）。
func (m *Manager) All() ([]Descriptor, []string, error) {
	m.refreshMode()
	items, err := List()
	warnings := make([]string, 0, 2)
	if err != nil {
		warnings = append(warnings, err.Error())
	}
	if len(items) == 0 {
		items = legacyDescriptors()
		if len(items) > 0 {
			warnings = append(warnings, "当前使用旧部署自动识别的运行时，保存后才会写入注册表")
		}
	}
	return items, warnings, nil
}

// ListResult 组装 /runtime/list 的完整返回体。
func (m *Manager) ListResult(defaultID string) (ListResult, error) {
	items, warnings, err := m.All()
	if err != nil {
		return ListResult{}, err
	}
	// 每条运行时的能力必须按当前环境重新计算。
	// 注册表里存的是"该类型理论上能做什么"，若直接透出，
	// Windows 上的 systemd 条目会显示可启动，点下去必然失败。
	for index := range items {
		items[index].Capabilities = m.effectiveCapabilities(items[index].Kind)
	}
	capabilities := make(map[string]ProviderCapabilities, len(orderedKinds))
	for _, kind := range orderedKinds {
		capabilities[string(kind)] = m.effectiveCapabilities(kind)
	}
	if defaultID == "" {
		defaultID = pickDefaultID(items)
	}
	return ListResult{
		Runtimes:     items,
		DefaultID:    defaultID,
		AgentMode:    m.AgentMode(),
		Capabilities: capabilities,
		Warnings:     warnings,
	}, nil
}

// effectiveCapabilities 把"类型能力"与"环境能力"取交集。
func (m *Manager) effectiveCapabilities(kind Kind) ProviderCapabilities {
	value := CapabilitiesOf(kind)
	if m.unsupportedReason(kind) != "" {
		return ProviderCapabilities{NeedsRoot: value.NeedsRoot}
	}
	return value
}

// pickDefaultID 取"已启用、最新更新"的一条，没有就取第一条。
func pickDefaultID(items []Descriptor) string {
	if len(items) == 0 {
		return ""
	}
	candidates := make([]Descriptor, 0, len(items))
	for _, item := range items {
		if item.Enabled && item.ArchiveAt == "" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return items[0].ID
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].UpdatedAt > candidates[j].UpdatedAt
	})
	return candidates[0].ID
}

// Resolve 按 ID 取运行时，空 ID 时返回默认项。
func (m *Manager) Resolve(id string) (Descriptor, error) {
	items, _, err := m.All()
	if err != nil {
		return Descriptor{}, err
	}
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		id = pickDefaultID(items)
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return Descriptor{}, fmt.Errorf("运行时不存在: %s", id)
}

// Create 新建运行时。只登记描述符，不改变宿主机进程归属。
//
// enabled 为 nil 时默认启用：用户点"新建/接管"就是想让它立刻生效，
// 若默认停用，诊断与维护会继续走旧链路，而界面看起来已经接管成功。
func (m *Manager) Create(input Descriptor, enabled *bool) (Descriptor, error) {
	value, err := input.Normalize()
	if err != nil {
		return Descriptor{}, err
	}
	if enabled != nil {
		value.Enabled = *enabled
	} else {
		value.Enabled = true
	}
	if value.Origin == "" {
		value.Origin = OriginManual
	}
	if _, exists := Get(value.ID); exists {
		return Descriptor{}, fmt.Errorf("运行时 ID 已存在: %s", value.ID)
	}
	saved, err := Upsert(value)
	if err != nil {
		return Descriptor{}, err
	}
	AppendEvent(Event{
		RuntimeID: saved.ID,
		Kind:      saved.Kind,
		State:     StateUnknown,
		Message:   "创建运行时",
	})
	return saved, nil
}

// Update 修改名称、启动规格与启用状态。
func (m *Manager) Update(id string, name string, spec *LaunchSpec, enabled *bool) (Descriptor, error) {
	current, ok := Get(id)
	if !ok {
		// 允许对 legacy 合成项执行保存，第一次保存即落盘。
		resolved, err := m.Resolve(id)
		if err != nil {
			return Descriptor{}, err
		}
		current = resolved
	}
	if strings.TrimSpace(name) != "" {
		current.Name = strings.TrimSpace(name)
	}
	if enabled != nil {
		current.Enabled = *enabled
	}
	if spec != nil {
		if !m.effectiveCapabilities(current.Kind).CanWriteSpec {
			return Descriptor{}, fmt.Errorf("%s 运行时不支持修改启动规格", current.Kind.Label())
		}
		normalized, err := spec.Normalize()
		if err != nil {
			return Descriptor{}, err
		}
		current.Spec = normalized
		current.GamePath = normalized.WorkingDir
	}
	if err := m.pushSpec(current); err != nil {
		return Descriptor{}, err
	}
	return Upsert(current)
}

// pushSpec 把启动规格写回宿主机（systemd 重写 unit / nohup 重写 wrapper）。
func (m *Manager) pushSpec(descriptor Descriptor) error {
	if !m.effectiveCapabilities(descriptor.Kind).CanWriteSpec {
		return nil
	}
	spec := descriptor.Spec
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      ActionImport,
		RuntimeID:   descriptor.ID,
		Kind:        descriptor.Kind,
		Target:      descriptor.Target,
		Spec:        &spec,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := m.clientSnapshot().Submit(request, actionTimeout)
	if err != nil {
		return err
	}
	if result.Status != JobSuccess {
		return fmt.Errorf("写入启动配置失败: %s", firstNonEmpty(result.Message, result.Status))
	}
	return nil
}

// Delete 删除运行时。mode=archive 时保留归档标记，便于回滚。
func (m *Manager) Delete(id string, mode string) error {
	current, ok := Get(id)
	if !ok {
		return fmt.Errorf("运行时不存在: %s", id)
	}
	if mode == "archive" {
		current.ArchiveAt = time.Now().UTC().Format(time.RFC3339)
		current.Enabled = false
		_, err := Upsert(current)
		return err
	}
	if err := Remove(id); err != nil {
		return err
	}
	AppendEvent(Event{
		RuntimeID: current.ID,
		Kind:      current.Kind,
		State:     StateUnknown,
		Message:   "删除运行时",
	})
	return nil
}

// Action 执行 start/stop/restart，并刷新健康快照。
func (m *Manager) Action(id string, action string) (JobResult, error) {
	switch action {
	case ActionStart, ActionStop, ActionRestart:
	default:
		return JobResult{}, fmt.Errorf("不支持的动作: %s", action)
	}
	current, err := m.Resolve(id)
	if err != nil {
		return JobResult{}, err
	}
	capabilities := m.effectiveCapabilities(current.Kind)
	switch action {
	case ActionStart:
		if !capabilities.CanStart {
			return JobResult{}, fmt.Errorf("%s 运行时无法启动，请先迁移到可写运行时", current.Kind.Label())
		}
	case ActionStop:
		if !capabilities.CanStop {
			return JobResult{}, fmt.Errorf("%s 运行时不支持停止", current.Kind.Label())
		}
	case ActionRestart:
		if !capabilities.CanRestart {
			return JobResult{}, fmt.Errorf("%s 运行时不支持重启", current.Kind.Label())
		}
	}

	spec := current.Spec
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      action,
		RuntimeID:   current.ID,
		Kind:        current.Kind,
		Target:      current.Target,
		Spec:        &spec,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := m.clientSnapshot().Submit(request, actionTimeout)
	if err != nil {
		return JobResult{}, err
	}
	AppendEvent(Event{
		RuntimeID: current.ID,
		Kind:      current.Kind,
		State:     result.Status,
		Message:   fmt.Sprintf("%s: %s", action, firstNonEmpty(result.Message, result.Status)),
	})
	if result.Status == JobSuccess {
		_, _ = m.Status(current.ID)
	}
	return result, nil
}

// Status 采集一次健康快照。Agent 不可用时退回缓存值。
func (m *Manager) Status(id string) (HealthSnapshot, error) {
	current, err := m.Resolve(id)
	if err != nil {
		return HealthSnapshot{}, err
	}
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      ActionInspect,
		RuntimeID:   current.ID,
		Kind:        current.Kind,
		Target:      current.Target,
		Spec:        &current.Spec,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, submitErr := m.clientSnapshot().Submit(request, inspectTimeout)
	if submitErr != nil || result.Status != JobSuccess {
		if cached, ok := CachedHealth(current.ID); ok {
			cached.Source = "cache"
			if submitErr != nil {
				cached.Reason = firstNonEmpty(cached.Reason, submitErr.Error())
			} else {
				cached.Reason = firstNonEmpty(cached.Reason, result.Message)
			}
			return cached, nil
		}
		if submitErr != nil {
			return HealthSnapshot{}, submitErr
		}
		return HealthSnapshot{
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
			State:     StateUnknown,
			Reason:    firstNonEmpty(result.Message, "无法获取运行时状态"),
			Source:    "agent",
		}, nil
	}

	data := inspectDataFrom(result.Data)
	snapshot := BuildSnapshot(data, boolFrom(result.Data, "rcon_reachable"), stringFrom(result.Data, "rcon_error"), "agent")
	if err := UpdateHealth(current.ID, snapshot); err != nil {
		fmt.Printf("[runtime] 缓存健康快照失败: %v\n", err)
	}
	return snapshot, nil
}

// Logs 读取运行时日志尾部。
func (m *Manager) Logs(id string, tail int) (map[string]any, error) {
	current, err := m.Resolve(id)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      ActionLogs,
		RuntimeID:   current.ID,
		Kind:        current.Kind,
		Target:      current.Target,
		Spec:        &current.Spec,
		Options:     map[string]any{"tail": tail},
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := m.clientSnapshot().Submit(request, logsTimeout)
	if err != nil {
		return nil, err
	}
	if result.Status != JobSuccess {
		return nil, fmt.Errorf("读取日志失败: %s", firstNonEmpty(result.Message, result.Status))
	}
	lines := stringSliceFrom(result.Data, "lines")
	return map[string]any{
		"lines":  lines,
		"source": stringFrom(result.Data, "source"),
	}, nil
}

// Detect 触发一次宿主机扫描（仅手动，不后台轮询）。
func (m *Manager) Detect() (DetectResult, error) {
	request := JobRequest{
		JobID:       uuid.NewString(),
		Action:      ActionDetect,
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := m.clientSnapshot().Submit(request, detectTimeout)
	if err != nil {
		return DetectResult{}, err
	}
	if result.Status != JobSuccess {
		return DetectResult{}, fmt.Errorf("扫描失败: %s", firstNonEmpty(result.Message, result.Status))
	}
	candidates := candidatesFrom(result.Data)
	items, _, _ := m.All()
	fused := FuseCandidates(candidates)
	marked := MarkAdoptionState(fused, items)
	return DetectResult{
		Candidates: marked,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		Warnings:   stringSliceFrom(result.Data, "warnings"),
	}, nil
}

// Adopt 接管一个已检测到的服务端，只登记不改变进程归属。
func (m *Manager) Adopt(candidateKey string, id string, name string) (Descriptor, error) {
	if err := ValidateID(id); err != nil {
		return Descriptor{}, err
	}
	detected, err := m.Detect()
	if err != nil {
		return Descriptor{}, err
	}
	var matched *DiscoveryCandidate
	for index := range detected.Candidates {
		if detected.Candidates[index].Key != candidateKey {
			continue
		}
		candidate := detected.Candidates[index]
		matched = &candidate
		break
	}
	if matched == nil {
		return Descriptor{}, fmt.Errorf("未找到候选服务端，可能已被其他运行时接管，请重新扫描")
	}
	if !matched.Adoptable {
		return Descriptor{}, fmt.Errorf("该服务端不可接管: %s", matched.Reason)
	}

	descriptor := Descriptor{
		ID:       strings.ToLower(strings.TrimSpace(id)),
		Name:     strings.TrimSpace(name),
		Kind:     matched.Kind,
		Enabled:  true,
		Adopted:  true,
		Managed:  CapabilitiesOf(matched.Kind).CanWriteSpec,
		Target:   matched.Target,
		Spec:     matched.Spec,
		GamePath: matched.GamePath,
		Origin:   OriginDetected,
	}
	if descriptor.Name == "" {
		descriptor.Name = matched.Name
	}
	normalized, err := descriptor.Normalize()
	if err != nil {
		return Descriptor{}, err
	}
	if _, exists := Get(normalized.ID); exists {
		return Descriptor{}, fmt.Errorf("运行时 ID 已存在: %s", normalized.ID)
	}
	saved, err := Upsert(normalized)
	if err != nil {
		return Descriptor{}, err
	}
	AppendEvent(Event{
		RuntimeID: saved.ID,
		Kind:      saved.Kind,
		State:     StateUnknown,
		Message:   "接管宿主机既有服务端",
	})
	return saved, nil
}

// Detach 撤销接管，仅删除登记，不停止进程。
func (m *Manager) Detach(id string) error {
	current, ok := Get(id)
	if !ok {
		return fmt.Errorf("运行时不存在: %s", id)
	}
	if !current.Adopted {
		return fmt.Errorf("该运行时不是接管而来的，请使用删除")
	}
	if err := Remove(id); err != nil {
		return err
	}
	AppendEvent(Event{
		RuntimeID: current.ID,
		Kind:      current.Kind,
		State:     StateUnknown,
		Message:   "撤销接管（进程未受影响）",
	})
	return nil
}

// unsupportedReason 判断某运行时类型在当前环境是否可用。
// 返回空字符串表示可用，否则返回给用户看的原因。
//
// 这是"迁移计划必须在生成时就拦下不可行目标"的实现：
// Windows 上没有 systemd，若允许计划生成，执行时必然失败并回滚。
func (m *Manager) unsupportedReason(kind Kind) string {
	mode := m.AgentMode()
	if mode == AgentModeBridge {
		// 宿主机 Agent 全能力，由 Agent 侧再校验具体命令是否存在。
		return ""
	}
	switch kind {
	case KindSystemd, KindSystemdUser:
		if !HasSystemdOnHost() {
			return "当前环境没有 systemd，不能作为迁移目标"
		}
	case KindScreen, KindTmux:
		return "screen/tmux 需要宿主机 Agent 支持"
	case KindMCSM, KindLGSM:
		return kind.Label() + " 需要宿主机 Agent 支持"
	}
	return ""
}

// MigrationPlan 是迁移前的完整方案，前端必须展示并二次确认。
type MigrationPlan struct {
	Token       string   `json:"token"`
	SourceID    string   `json:"source_id"`
	SourceKind  Kind     `json:"source_kind"`
	TargetID    string   `json:"target_id"`
	TargetKind  Kind     `json:"target_kind"`
	Steps       []string `json:"steps"`
	Risks       []string `json:"risks"`
	Blockers    []string `json:"blockers"`
	GeneratedAt string   `json:"generated_at"`
}

// MigrationOptions 是迁移目标的可选参数。
type MigrationOptions struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Session   string `json:"session_name,omitempty"`
	UnitName  string `json:"unit_name,omitempty"`
	Container string `json:"container_name,omitempty"`
	LogPath   string `json:"log_path,omitempty"`
}

var planCache = struct {
	mu    sync.Mutex
	plans map[string]MigrationPlan
}{plans: make(map[string]MigrationPlan)}

// PlanMigration 生成迁移计划。任何 blocker 都必须在 apply 前拦下。
func (m *Manager) PlanMigration(sourceID string, targetKind Kind, options MigrationOptions) (MigrationPlan, error) {
	source, err := m.Resolve(sourceID)
	if err != nil {
		return MigrationPlan{}, err
	}
	if !targetKind.Valid() {
		return MigrationPlan{}, fmt.Errorf("不支持的目标运行时类型: %s", targetKind)
	}
	if targetKind == source.Kind {
		return MigrationPlan{}, fmt.Errorf("源运行时与目标运行时类型相同，无需迁移")
	}
	capabilities := m.effectiveCapabilities(targetKind)
	// 迁移只需要"能启动"。screen/tmux 没有配置文件，启动规格保存在面板注册表，
	// 它们的 CanWriteSpec 是 false，但仍然可以作为迁移目标。
	if !capabilities.CanStart {
		return MigrationPlan{}, fmt.Errorf("%s 不能作为迁移目标（当前环境无法启动该类型）", targetKind.Label())
	}

	targetID := strings.TrimSpace(options.ID)
	if targetID == "" {
		targetID = source.ID + "-" + string(targetKind)
	}
	if err := ValidateID(targetID); err != nil {
		return MigrationPlan{}, err
	}
	if _, exists := Get(targetID); exists {
		return MigrationPlan{}, fmt.Errorf("目标运行时 ID 已存在: %s", targetID)
	}

	blockers := make([]string, 0, 4)
	if source.Spec.Executable == "" || source.Spec.WorkingDir == "" {
		blockers = append(blockers, "源运行时缺少可执行文件或工作目录，无法导出启动规格")
	}
	if source.Spec.Port <= 0 {
		blockers = append(blockers, "源运行时未解析到游戏端口，迁移后会与现有服冲突")
	}
	if m.AgentMode() == AgentModeDisabled {
		blockers = append(blockers, "未检测到宿主机 Agent，无法执行停机与启动")
	}
	// 目标类型必须在当前环境里真的能跑，否则计划看起来可行、执行必失败。
	if reason := m.unsupportedReason(targetKind); reason != "" {
		blockers = append(blockers, reason)
	}

	target := Descriptor{
		ID:       targetID,
		Name:     firstNonEmpty(strings.TrimSpace(options.Name), source.Name+"（"+targetKind.Label()+"）"),
		Kind:     targetKind,
		Enabled:  true,
		Managed:  true,
		Spec:     source.Spec,
		GamePath: source.GamePath,
		Origin:   OriginMigrated,
		Target:   migrationTarget(targetKind, targetID, options),
	}
	if targetKind == KindNohup && target.Spec.LogPath == "" {
		target.Spec.LogPath = DefaultLogPath(target.Spec.WorkingDir, targetID)
	}
	normalizedTarget, normalizeErr := target.Normalize()
	if normalizeErr != nil {
		blockers = append(blockers, normalizeErr.Error())
	}

	steps := []string{
		fmt.Sprintf("备份源运行时配置（%s）到 staging 目录", source.Kind.Label()),
		fmt.Sprintf("导出启动规格：%s", source.Spec.CommandLine()),
		fmt.Sprintf("生成目标配置：%s", targetKind.Label()),
		fmt.Sprintf("停止源运行时（%s）并等待端口 %d 释放", source.Kind.Label(), source.Spec.Port),
		fmt.Sprintf("启动目标运行时（%s）", targetKind.Label()),
		"健康检查：进程存在 + 端口监听 + RCON 可达",
	}
	risks := []string{"迁移期间服务端会短暂离线，预计 15-40 秒"}
	if !IsPersistent(targetKind) {
		risks = append(risks, fmt.Sprintf("%s 不是持久化运行时，宿主机重启后不会自动恢复", targetKind.Label()))
	}
	if source.Kind == KindSystemd || source.Kind == KindSystemdUser {
		risks = append(risks, "源 systemd 单元将被停止并禁用，失败会自动回滚")
	}

	plan := MigrationPlan{
		Token:       uuid.NewString(),
		SourceID:    source.ID,
		SourceKind:  source.Kind,
		TargetID:    targetID,
		TargetKind:  targetKind,
		Steps:       steps,
		Risks:       risks,
		Blockers:    blockers,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	planCache.mu.Lock()
	// 限制缓存规模，避免长期运行累积。
	if len(planCache.plans) > 64 {
		planCache.plans = make(map[string]MigrationPlan)
	}
	planCache.plans[plan.Token] = plan
	planCache.mu.Unlock()
	_ = normalizedTarget
	return plan, nil
}

func migrationTarget(kind Kind, id string, options MigrationOptions) Target {
	switch kind {
	case KindSystemd, KindSystemdUser:
		unit := strings.TrimSpace(options.UnitName)
		if unit == "" {
			unit = id + ".service"
		}
		scope := "system"
		if kind == KindSystemdUser {
			scope = "user"
		}
		return Target{UnitName: unit, Scope: scope}
	case KindDocker:
		container := strings.TrimSpace(options.Container)
		if container == "" {
			container = id
		}
		return Target{ContainerName: container}
	case KindScreen, KindTmux:
		session := strings.TrimSpace(options.Session)
		if session == "" {
			session = id
		}
		return Target{SessionName: session}
	case KindNohup:
		return Target{PidFile: DefaultLogPath("", id)}
	default:
		return Target{}
	}
}

// ApplyMigration 执行已生成的迁移计划。
func (m *Manager) ApplyMigration(token string) (JobResult, error) {
	planCache.mu.Lock()
	plan, ok := planCache.plans[token]
	planCache.mu.Unlock()
	if !ok {
		return JobResult{}, fmt.Errorf("迁移计划不存在或已过期，请重新生成")
	}
	if len(plan.Blockers) > 0 {
		return JobResult{}, fmt.Errorf("迁移被阻止: %s", strings.Join(plan.Blockers, "；"))
	}
	source, err := m.Resolve(plan.SourceID)
	if err != nil {
		return JobResult{}, err
	}

	target := Descriptor{
		ID:       plan.TargetID,
		Name:     source.Name + "（" + plan.TargetKind.Label() + "）",
		Kind:     plan.TargetKind,
		Enabled:  true,
		Managed:  true,
		Spec:     source.Spec,
		GamePath: source.GamePath,
		Origin:   OriginMigrated,
		Target:   migrationTarget(plan.TargetKind, plan.TargetID, MigrationOptions{}),
	}
	if plan.TargetKind == KindNohup && target.Spec.LogPath == "" {
		target.Spec.LogPath = DefaultLogPath(target.Spec.WorkingDir, plan.TargetID)
	}
	normalizedTarget, err := target.Normalize()
	if err != nil {
		return JobResult{}, err
	}

	spec := source.Spec
	targetSpec := normalizedTarget.Spec
	request := JobRequest{
		JobID:     uuid.NewString(),
		Action:    ActionMigrate,
		RuntimeID: source.ID,
		Kind:      source.Kind,
		Target:    source.Target,
		Spec:      &spec,
		Options: map[string]any{
			"target_id":   normalizedTarget.ID,
			"target_kind": string(normalizedTarget.Kind),
			"target":      normalizedTarget.Target,
			"target_spec": targetSpec,
			"plan_token":  token,
		},
		RequestedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Requester:   "panel",
	}
	result, err := m.clientSnapshot().Submit(request, migrateTimeout)
	if err != nil {
		return JobResult{}, err
	}

	if result.Status == JobSuccess {
		archived := source
		archived.Enabled = false
		archived.ArchiveAt = time.Now().UTC().Format(time.RFC3339)
		if _, err := Upsert(archived); err != nil {
			fmt.Printf("[runtime] 归档源运行时失败: %v\n", err)
		}
		if _, err := Upsert(normalizedTarget); err != nil {
			fmt.Printf("[runtime] 保存目标运行时失败: %v\n", err)
		}
		AppendEvent(Event{
			RuntimeID: source.ID,
			Kind:      source.Kind,
			State:     StateStopped,
			Message:   "迁移完成，源运行时已归档",
		})
		AppendEvent(Event{
			RuntimeID: normalizedTarget.ID,
			Kind:      normalizedTarget.Kind,
			State:     StateRunning,
			Message:   "迁移完成，目标运行时已接管",
		})
	} else {
		AppendEvent(Event{
			RuntimeID: source.ID,
			Kind:      source.Kind,
			State:     StateCrashed,
			Message:   "迁移失败: " + firstNonEmpty(result.Message, result.Status),
		})
	}
	return result, nil
}

// ExportSpec 读取运行时的启动规格（供前端展示与复制）。
func (m *Manager) ExportSpec(id string) (LaunchSpec, error) {
	current, err := m.Resolve(id)
	if err != nil {
		return LaunchSpec{}, err
	}
	return current.Spec, nil
}

func inspectDataFrom(data map[string]any) InspectData {
	result := InspectData{
		State:          stringFrom(data, "state"),
		PID:            intFrom(data, "pid"),
		PortListening:  boolFrom(data, "port_listening"),
		LogTail:        stringFrom(data, "log_tail"),
		Reason:         stringFrom(data, "reason"),
		ProcessRunning: boolFrom(data, "process_running"),
	}
	result.PortOwners = portOwnersFrom(data)
	if !result.ProcessRunning && result.PID > 0 {
		result.ProcessRunning = true
	}
	return result
}

func portOwnersFrom(data map[string]any) []PortOwner {
	raw, ok := data["port_owners"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	owners := make([]PortOwner, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		owners = append(owners, PortOwner{
			Command: stringFrom(entry, "command"),
			PID:     intFrom(entry, "pid"),
		})
	}
	return owners
}

func candidatesFrom(data map[string]any) []DiscoveryCandidate {
	raw, ok := data["candidates"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	candidates := make([]DiscoveryCandidate, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidate := DiscoveryCandidate{
			Key:        stringFrom(entry, "key"),
			Kind:       Kind(stringFrom(entry, "kind")),
			Name:       stringFrom(entry, "name"),
			Confidence: floatFrom(entry, "confidence"),
			Running:    boolFrom(entry, "running"),
			PID:        intFrom(entry, "pid"),
			User:       stringFrom(entry, "user"),
			GamePath:   stringFrom(entry, "game_path"),
			Port:       intFrom(entry, "port"),
			Evidence:   stringSliceFrom(entry, "evidence"),
		}
		candidate.Spec = launchSpecFrom(entry["spec"])
		candidate.Target = targetFrom(entry["target"])
		candidates = append(candidates, candidate)
	}
	return candidates
}

func launchSpecFrom(raw any) LaunchSpec {
	entry, ok := raw.(map[string]any)
	if !ok {
		return LaunchSpec{}
	}
	return LaunchSpec{
		WorkingDir:  stringFrom(entry, "working_dir"),
		Executable:  stringFrom(entry, "executable"),
		Args:        stringSliceFrom(entry, "args"),
		User:        stringFrom(entry, "user"),
		Port:        intFrom(entry, "port"),
		RCONAddress: stringFrom(entry, "rcon_address"),
		AutoRestart: boolFrom(entry, "auto_restart"),
		LogPath:     stringFrom(entry, "log_path"),
	}
}

func targetFrom(raw any) Target {
	entry, ok := raw.(map[string]any)
	if !ok {
		return Target{}
	}
	return Target{
		UnitName:       stringFrom(entry, "unit_name"),
		UnitPath:       stringFrom(entry, "unit_path"),
		Scope:          stringFrom(entry, "scope"),
		ContainerName:  stringFrom(entry, "container_name"),
		ComposeFile:    stringFrom(entry, "compose_file"),
		ComposeService: stringFrom(entry, "compose_service"),
		SessionName:    stringFrom(entry, "session_name"),
		PidFile:        stringFrom(entry, "pid_file"),
		MCSMNodeURL:    stringFrom(entry, "mcsm_node_url"),
		MCSMInstance:   stringFrom(entry, "mcsm_instance"),
		LGSMScript:     stringFrom(entry, "lgsm_script"),
	}
}

func stringFrom(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func boolFrom(data map[string]any, key string) bool {
	if data == nil {
		return false
	}
	value, ok := data[key].(bool)
	return ok && value
}

func intFrom(data map[string]any, key string) int {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func floatFrom(data map[string]any, key string) float64 {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func stringSliceFrom(data map[string]any, key string) []string {
	if data == nil {
		return nil
	}
	items, ok := data[key].([]any)
	if !ok {
		if direct, ok := data[key].([]string); ok {
			return direct
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func defaultControlDir() string {
	if value := strings.TrimSpace(getenv("L4D2_CONTROL_DIR")); value != "" {
		return value
	}
	if isWindowsHost() {
		return "control"
	}
	return "/control"
}
