package logic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimelogic "l4d2-manager-next/logic/runtime"
)

// prepareRuntimeRegistry 把运行时注册表指向临时目录，避免污染真实 data/。
func prepareRuntimeRegistry(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	runtimelogic.SetDataDir(dataDir)
	t.Cleanup(func() {
		runtimelogic.SetDataDir("./data")
	})
	controlPath := filepath.Join(t.TempDir(), "runtime-control.json")
	t.Setenv("L4D2_RUNTIME_CONTROL_PATH", controlPath)
	return controlPath
}

func TestWriteRuntimeControlSnapshotWithoutRuntime(t *testing.T) {
	controlPath := prepareRuntimeRegistry(t)

	handled, err := WriteRuntimeControlSnapshot()
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	// 空注册表 + 无启动项时没有任何可用运行时，调用方应回退旧链路。
	if handled {
		t.Fatal("没有运行时时不应返回 handled=true")
	}
	if _, statErr := os.Stat(controlPath); statErr == nil {
		t.Fatal("没有运行时时不应写快照文件")
	}
}

func TestWriteRuntimeControlSnapshotWithManagedRuntime(t *testing.T) {
	controlPath := prepareRuntimeRegistry(t)

	descriptor, err := runtimelogic.Descriptor{
		ID:      "main",
		Name:    "主服",
		Kind:    runtimelogic.KindScreen,
		Enabled: true,
		Target:  runtimelogic.Target{SessionName: "l4d2-main"},
		Spec: runtimelogic.LaunchSpec{
			WorkingDir: "/home/ubuntu/l4d2",
			Executable: "/home/ubuntu/l4d2/srcds_run",
			Args:       []string{"-game", "left4dead2"},
			Port:       27015,
		},
		GamePath: "/home/ubuntu/l4d2",
		Origin:   runtimelogic.OriginDetected,
	}.Normalize()
	if err != nil {
		t.Fatalf("构造运行时失败: %v", err)
	}
	if _, err := runtimelogic.Upsert(descriptor); err != nil {
		t.Fatalf("写入注册表失败: %v", err)
	}

	handled, err := WriteRuntimeControlSnapshot()
	if err != nil {
		t.Fatalf("写入快照失败: %v", err)
	}
	if !handled {
		t.Fatal("有运行时时必须返回 handled=true")
	}

	data, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatalf("快照文件缺失: %v", err)
	}
	var snapshot RuntimeControlSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("快照格式错误: %v", err)
	}
	if snapshot.Kind != runtimelogic.KindScreen {
		t.Fatalf("kind 错误: %s", snapshot.Kind)
	}
	if snapshot.Target.SessionName != "l4d2-main" {
		t.Fatalf("target 未写入: %#v", snapshot.Target)
	}
	if snapshot.Spec.Port != 27015 {
		t.Fatalf("spec 未写入: %#v", snapshot.Spec)
	}
	if snapshot.RuntimeID != "main" {
		t.Fatalf("runtime_id 错误: %s", snapshot.RuntimeID)
	}
}

func TestActiveRuntimePrefersManagedOverLegacy(t *testing.T) {
	prepareRuntimeRegistry(t)

	legacy := runtimelogic.Descriptor{
		ID:      "default",
		Name:    "旧部署",
		Kind:    runtimelogic.KindSystemd,
		Enabled: true,
		Target:  runtimelogic.Target{UnitName: "l4d2.service"},
		Origin:  runtimelogic.OriginLegacy,
	}
	managed := runtimelogic.Descriptor{
		ID:      "docker-main",
		Name:    "容器主服",
		Kind:    runtimelogic.KindDocker,
		Enabled: true,
		Target:  runtimelogic.Target{ContainerName: "l4d2"},
		Origin:  runtimelogic.OriginDetected,
	}
	if _, err := runtimelogic.Upsert(legacy); err != nil {
		t.Fatalf("写入 legacy 失败: %v", err)
	}
	if _, err := runtimelogic.Upsert(managed); err != nil {
		t.Fatalf("写入 managed 失败: %v", err)
	}

	item, ok := activeRuntime()
	if !ok {
		t.Fatal("应返回一个运行时")
	}
	if item.ID != "docker-main" {
		t.Fatalf("必须优先返回非 legacy 运行时，实际 %s", item.ID)
	}
	if !managedRuntimeAvailable() {
		t.Fatal("存在非 legacy 运行时时 managedRuntimeAvailable 必须为 true")
	}
}

func TestActiveRuntimeSkipsDisabledAndArchived(t *testing.T) {
	prepareRuntimeRegistry(t)

	disabled := runtimelogic.Descriptor{
		ID:      "disabled",
		Name:    "停用",
		Kind:    runtimelogic.KindDocker,
		Enabled: false,
		Target:  runtimelogic.Target{ContainerName: "a"},
		Origin:  runtimelogic.OriginManual,
	}
	active := runtimelogic.Descriptor{
		ID:      "active",
		Name:    "在用",
		Kind:    runtimelogic.KindDocker,
		Enabled: true,
		Target:  runtimelogic.Target{ContainerName: "b"},
		Origin:  runtimelogic.OriginManual,
	}
	for _, item := range []runtimelogic.Descriptor{disabled, active} {
		if _, err := runtimelogic.Upsert(item); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}
	item, ok := activeRuntime()
	if !ok || item.ID != "active" {
		t.Fatalf("应跳过停用项，实际 %#v ok=%v", item.ID, ok)
	}
}

func TestCollectRuntimeDiagnosticsWithoutManagedRuntime(t *testing.T) {
	prepareRuntimeRegistry(t)
	if _, ok := CollectRuntimeDiagnostics(27015); ok {
		t.Fatal("没有托管运行时时必须回退旧链路")
	}
}

func TestRuntimeServiceLabel(t *testing.T) {
	cases := []struct {
		name string
		item runtimelogic.Descriptor
		want string
	}{
		{"systemd", runtimelogic.Descriptor{Kind: runtimelogic.KindSystemd, Target: runtimelogic.Target{UnitName: "l4d2.service"}}, "l4d2.service"},
		{"docker", runtimelogic.Descriptor{Kind: runtimelogic.KindDocker, Target: runtimelogic.Target{ContainerName: "l4d2"}}, "容器 l4d2"},
		{"screen", runtimelogic.Descriptor{Kind: runtimelogic.KindScreen, Target: runtimelogic.Target{SessionName: "main"}}, "screen 会话 main"},
		{"兜底", runtimelogic.Descriptor{Name: "自定义", Kind: runtimelogic.KindBare}, "自定义"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := runtimeServiceLabel(testCase.item); got != testCase.want {
				t.Fatalf("得到 %q，期望 %q", got, testCase.want)
			}
		})
	}
}

func TestBuildRuntimeStatusText(t *testing.T) {
	item := runtimelogic.Descriptor{Name: "主服", Kind: runtimelogic.KindTmux}
	text := buildRuntimeStatusText(item, runtimelogic.HealthSnapshot{
		State:         runtimelogic.StateRunning,
		PID:           4321,
		PortListening: true,
	})
	for _, want := range []string{"主服", "tmux 会话", "运行中", "4321", "端口已监听"} {
		if !strings.Contains(text, want) {
			t.Fatalf("状态文本缺少 %q: %s", want, text)
		}
	}
}
