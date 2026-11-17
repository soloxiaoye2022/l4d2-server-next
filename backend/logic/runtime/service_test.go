package runtime

import (
	"strings"
	"testing"
	"time"
)

// newTestManager 构造一个不访问宿主机的 Manager。
func newTestManager(t *testing.T, mode string) *Manager {
	t.Helper()
	withTempDataDir(t)
	return &Manager{
		settings: Settings{DataDir: "./data", DefaultUser: "ubuntu"},
		client:   &unavailableClient{},
		mode:     mode,
	}
}

func TestEffectiveCapabilitiesDisabledWhenEnvironmentLacksKind(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)

	// 环境里没有 systemd 时，systemd 的能力必须全部为 false，
	// 否则前端会渲染出点了就失败的按钮。
	if HasSystemdOnHost() {
		t.Skip("当前环境有 systemd，跳过该断言")
	}
	capabilities := manager.effectiveCapabilities(KindSystemd)
	if capabilities.CanStart || capabilities.CanStop || capabilities.CanWriteSpec {
		t.Fatalf("无 systemd 环境下不应声明可启动: %#v", capabilities)
	}
	if reason := manager.unsupportedReason(KindSystemd); !strings.Contains(reason, "systemd") {
		t.Fatalf("原因必须说明缺少 systemd，实际 %q", reason)
	}
}

func TestEffectiveCapabilitiesBridgeIsUnrestricted(t *testing.T) {
	manager := newTestManager(t, AgentModeBridge)
	for _, kind := range []Kind{KindSystemd, KindScreen, KindTmux, KindMCSM, KindLGSM} {
		if manager.unsupportedReason(kind) != "" {
			t.Fatalf("Agent 模式不应对 %s 设限", kind)
		}
		if manager.effectiveCapabilities(kind).CanStart != CapabilitiesOf(kind).CanStart {
			t.Fatalf("%s 能力应与类型能力一致", kind)
		}
	}
}

func TestActionRejectsUnsupportedStart(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, err := sampleDescriptor().Normalize()
	if err != nil {
		t.Fatalf("构造运行时失败: %v", err)
	}
	item.Kind = KindBare
	item.Target = Target{}
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := manager.Action(item.ID, ActionStart); err == nil {
		t.Fatal("裸进程不支持启动，必须报错")
	}
	if _, err := manager.Action(item.ID, "rm -rf"); err == nil {
		t.Fatal("未知动作必须报错")
	}
}

func TestDeleteArchiveKeepsEntry(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := manager.Delete(item.ID, "archive"); err != nil {
		t.Fatalf("归档失败: %v", err)
	}
	stored, ok := Get(item.ID)
	if !ok {
		t.Fatal("归档后条目必须仍然存在")
	}
	if stored.ArchiveAt == "" {
		t.Fatal("归档标记必须被写入")
	}
	if stored.Enabled {
		t.Fatal("归档后必须禁用")
	}
}

func TestDeleteRemovesEntry(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := manager.Delete(item.ID, "detach"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, ok := Get(item.ID); ok {
		t.Fatal("删除后条目不应存在")
	}
}

func TestCreateRejectsDuplicateID(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if _, err := manager.Create(item, nil); err == nil {
		t.Fatal("重复 ID 必须被拒绝")
	}
}

func TestDetachRequiresAdopted(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := manager.Detach(item.ID); err == nil {
		t.Fatal("非接管运行时不允许撤销接管")
	}
}

func TestPlanMigrationBlocksSameKind(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := manager.PlanMigration(item.ID, KindSystemd, MigrationOptions{}); err == nil {
		t.Fatal("同类型迁移必须被拒绝")
	}
}

func TestPlanMigrationBlocksMissingPort(t *testing.T) {
	manager := newTestManager(t, AgentModeBridge)
	item, _ := sampleDescriptor().Normalize()
	item.Spec.Port = 0
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	plan, err := manager.PlanMigration(item.ID, KindScreen, MigrationOptions{})
	if err != nil {
		t.Fatalf("计划生成失败: %v", err)
	}
	found := false
	for _, blocker := range plan.Blockers {
		if strings.Contains(blocker, "端口") {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少端口必须被列为阻断项: %#v", plan.Blockers)
	}
}

func TestApplyMigrationRejectsUnknownToken(t *testing.T) {
	manager := newTestManager(t, AgentModeLocal)
	if _, err := manager.ApplyMigration("not-exist"); err == nil {
		t.Fatal("未知 token 必须被拒绝")
	}
}

func TestLogsClampsTail(t *testing.T) {
	manager := newTestManager(t, AgentModeDisabled)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// Agent 不可用时读取日志应返回明确错误，而不是空列表假装成功。
	if _, err := manager.Logs(item.ID, 999999); err == nil {
		t.Fatal("Agent 不可用时必须返回错误")
	}
}

func TestStatusFallsBackToCache(t *testing.T) {
	manager := newTestManager(t, AgentModeDisabled)
	item, _ := sampleDescriptor().Normalize()
	if _, err := manager.Create(item, nil); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	cached := HealthSnapshot{State: StateStopped, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := UpdateHealth(item.ID, cached); err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}
	snapshot, err := manager.Status(item.ID)
	if err != nil {
		t.Fatalf("应退回缓存而不是报错: %v", err)
	}
	if snapshot.Source != "cache" {
		t.Fatalf("来源应标记为 cache，实际 %q", snapshot.Source)
	}
	if snapshot.State != StateStopped {
		t.Fatalf("状态应来自缓存，实际 %q", snapshot.State)
	}
}
