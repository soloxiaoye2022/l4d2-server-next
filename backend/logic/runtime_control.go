package logic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runtimelogic "l4d2-manager-next/logic/runtime"
)

// RuntimeControlSnapshot 是面板写给宿主机脚本的"当前运行时"快照。
//
// 修复 D4：维护/修复脚本原来直接执行 systemctl，
// 接管 screen / tmux / nohup / docker 之后，更新一次就会把服停死且起不回来。
// 现在面板先落一份快照，脚本通过 runtime-control.sh 交给 Agent 执行，
// 所有运行时共用同一套启停实现。
type RuntimeControlSnapshot struct {
	Kind      runtimelogic.Kind       `json:"kind"`
	Target    runtimelogic.Target     `json:"target"`
	Spec      runtimelogic.LaunchSpec `json:"spec"`
	RuntimeID string                  `json:"runtime_id"`
	Origin    string                  `json:"origin"`
	WrittenAt string                  `json:"written_at"`
}

func runtimeControlPath() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_RUNTIME_CONTROL_PATH")); value != "" {
		return value
	}
	return filepath.Join(DefaultControlDir(), "runtime-control.json")
}

// activeRuntime 返回应当代表"当前游戏服"的运行时。
//
// 优先级：已启用且非 legacy 的条目 > legacy 合成项 > 无。
// 非 legacy 优先，是因为用户显式接管/创建过，说明他认可这条描述。
func activeRuntime() (runtimelogic.Descriptor, bool) {
	InitRuntimeManager()
	manager := runtimelogic.Default()
	items, _, err := manager.All()
	if err != nil {
		return runtimelogic.Descriptor{}, false
	}
	var legacy *runtimelogic.Descriptor
	for index := range items {
		item := items[index]
		if !item.Enabled || item.ArchiveAt != "" {
			continue
		}
		if item.Origin == runtimelogic.OriginLegacy {
			copied := item
			legacy = &copied
			continue
		}
		return item, true
	}
	if legacy != nil {
		return *legacy, true
	}
	return runtimelogic.Descriptor{}, false
}

// WriteRuntimeControlSnapshot 在提交维护/修复任务前刷新快照。
// 返回 handled=false 表示当前没有可用运行时，调用方应回退旧逻辑。
func WriteRuntimeControlSnapshot() (bool, error) {
	item, ok := activeRuntime()
	if !ok {
		return false, nil
	}
	snapshot := RuntimeControlSnapshot{
		Kind:      item.Kind,
		Target:    item.Target,
		Spec:      item.Spec,
		RuntimeID: item.ID,
		Origin:    item.Origin,
		WrittenAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return true, fmt.Errorf("生成运行时控制快照失败: %w", err)
	}
	if err := writeManagerFileAtomic(runtimeControlPath(), data, 0600); err != nil {
		return true, fmt.Errorf("写入运行时控制快照失败: %w", err)
	}
	return true, nil
}

// managedRuntimeAvailable 表示"有非 legacy 的运行时可用"。
//
// 用于把原本"只有 systemd 才能做"的功能扩展到任意运行时：
// 诊断采集、修复处置都不再需要宿主机有 systemd。
func managedRuntimeAvailable() bool {
	item, ok := activeRuntime()
	if !ok {
		return false
	}
	return item.Origin != runtimelogic.OriginLegacy
}
