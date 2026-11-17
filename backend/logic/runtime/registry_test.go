package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempDataDir 把注册表数据目录指向临时目录，避免污染真实 data/。
func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetDataDir(dir)
	t.Cleanup(func() {
		SetDataDir("./data")
	})
	return dir
}

func sampleDescriptor() Descriptor {
	return Descriptor{
		ID:   "main",
		Name: "主服",
		Kind: KindSystemd,
		Target: Target{
			UnitName: "l4d2.service",
		},
		Spec: LaunchSpec{
			WorkingDir: "/home/ubuntu/l4d2",
			Executable: "/home/ubuntu/l4d2/srcds_run",
			Args:       []string{"-game", "left4dead2"},
			Port:       27015,
		},
		Enabled:  true,
		GamePath: "/home/ubuntu/l4d2",
	}
}

func TestRegistrySaveAndList(t *testing.T) {
	dir := withTempDataDir(t)

	item, err := sampleDescriptor().Normalize()
	if err != nil {
		t.Fatalf("规范化失败: %v", err)
	}
	if err := Save([]Descriptor{item}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, runtimesFileName)); err != nil {
		t.Fatalf("注册表文件未落盘: %v", err)
	}
	items, err := List()
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(items) != 1 || items[0].ID != "main" {
		t.Fatalf("读取结果错误: %#v", items)
	}
	if items[0].Capabilities.CanStart != true {
		t.Fatal("能力矩阵必须被补齐")
	}
}

func TestRegistryRejectsDuplicateID(t *testing.T) {
	withTempDataDir(t)
	item, _ := sampleDescriptor().Normalize()
	if err := Save([]Descriptor{item, item}); err == nil {
		t.Fatal("重复 ID 必须被拒绝")
	}
}

func TestRegistryRecoversFromCorruptFile(t *testing.T) {
	dir := withTempDataDir(t)
	path := filepath.Join(dir, runtimesFileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	items, err := List()
	if err == nil {
		t.Fatal("损坏文件必须返回错误")
	}
	if len(items) != 0 {
		t.Fatalf("损坏文件不应返回条目: %#v", items)
	}
	// 损坏文件必须被备份，而不是静默丢弃。
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Fatal("损坏文件必须被备份")
	}
}

func TestRegistrySkipsInvalidEntries(t *testing.T) {
	dir := withTempDataDir(t)
	path := filepath.Join(dir, runtimesFileName)
	content := `[
  {"id":"good","name":"好","kind":"systemd","target":{"unit_name":"l4d2.service"},"enabled":true},
  {"id":"BAD ID","name":"坏","kind":"systemd","target":{"unit_name":"l4d2.service"},"enabled":true}
]`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	items, err := List()
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(items) != 1 || items[0].ID != "good" {
		t.Fatalf("应跳过非法条目，实际 %#v", items)
	}
}

func TestUpsertPreservesCreatedAt(t *testing.T) {
	withTempDataDir(t)
	item, _ := sampleDescriptor().Normalize()
	saved, err := Upsert(item)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	created := saved.CreatedAt

	updated := saved
	updated.Name = "改名"
	result, err := Upsert(updated)
	if err != nil {
		t.Fatalf("再次写入失败: %v", err)
	}
	if result.CreatedAt != created {
		t.Fatalf("创建时间不应被刷新: %q -> %q", created, result.CreatedAt)
	}
	if result.Name != "改名" {
		t.Fatalf("名称未更新: %q", result.Name)
	}
}

func TestRemoveRuntime(t *testing.T) {
	withTempDataDir(t)
	item, _ := sampleDescriptor().Normalize()
	if _, err := Upsert(item); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := Remove(item.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	items, _ := List()
	if len(items) != 0 {
		t.Fatalf("删除后仍有条目: %#v", items)
	}
}

func TestEventsRolling(t *testing.T) {
	withTempDataDir(t)
	for index := 0; index < 5; index++ {
		AppendEvent(Event{RuntimeID: "main", Kind: KindSystemd, State: StateRunning})
	}
	events := ListEvents("main", 3)
	if len(events) != 3 {
		t.Fatalf("应返回最近 3 条，实际 %d", len(events))
	}
	if other := ListEvents("other", 10); len(other) != 0 {
		t.Fatalf("其他运行时不应有事件，实际 %d", len(other))
	}
}

func TestHealthCache(t *testing.T) {
	withTempDataDir(t)
	item, _ := sampleDescriptor().Normalize()
	if _, err := Upsert(item); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	snapshot := HealthSnapshot{State: StateRunning, PID: 123, PortListening: true}
	if err := UpdateHealth(item.ID, snapshot); err != nil {
		t.Fatalf("写入健康快照失败: %v", err)
	}
	cached, ok := CachedHealth(item.ID)
	if !ok {
		t.Fatal("应能读到缓存快照")
	}
	if cached.State != StateRunning || cached.PID != 123 {
		t.Fatalf("缓存内容错误: %#v", cached)
	}
}

func TestPickDefaultID(t *testing.T) {
	if id := pickDefaultID(nil); id != "" {
		t.Fatalf("空列表应返回空 ID，实际 %q", id)
	}
	items := []Descriptor{
		{ID: "disabled", Enabled: false},
		{ID: "old", Enabled: true, UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "new", Enabled: true, UpdatedAt: "2026-11-17T00:00:00Z"},
	}
	if id := pickDefaultID(items); id != "new" {
		t.Fatalf("应选择最新更新的启用项，实际 %q", id)
	}
	archived := []Descriptor{
		{ID: "archived", Enabled: true, ArchiveAt: "2026-11-01T00:00:00Z", UpdatedAt: "2026-11-02T00:00:00Z"},
		{ID: "active", Enabled: true, UpdatedAt: "2026-11-01T00:00:00Z"},
	}
	if id := pickDefaultID(archived); id != "active" {
		t.Fatalf("不应选择归档项，实际 %q", id)
	}
}
