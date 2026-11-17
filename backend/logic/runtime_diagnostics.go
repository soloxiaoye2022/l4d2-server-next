package logic

import (
	"strconv"
	"strings"

	runtimelogic "l4d2-manager-next/logic/runtime"
)

// CollectRuntimeDiagnostics 通过运行时子系统采集诊断快照。
//
// 修复 D5：旧实现只有 systemd 用宿主 path unit 采集，
// 接管 screen / tmux / nohup / docker 后诊断页整片空白。
// 现在统一走 Manager：inspect 拿状态与端口归属，logs 拿日志尾部，
// 再复用同一套 BuildServerDiagnosticHints 规则。
//
// 返回 ok=false 表示"当前没有可管理的运行时"，调用方应回退旧链路。
func CollectRuntimeDiagnostics(port int) (ServerDiagnostics, bool) {
	InitRuntimeManager()
	manager := runtimelogic.Default()
	if manager.AgentMode() == runtimelogic.AgentModeDisabled {
		return ServerDiagnostics{}, false
	}
	item, ok := activeRuntime()
	if !ok || item.Origin == runtimelogic.OriginLegacy {
		// legacy 走原有 systemd 诊断桥接，保持老部署行为不变。
		return ServerDiagnostics{}, false
	}
	if item.Spec.Port > 0 {
		port = item.Spec.Port
	}

	diagnostics := ServerDiagnostics{
		Available:   true,
		ServiceName: runtimeServiceLabel(item),
		Port:        port,
	}

	snapshot, err := manager.Status(item.ID)
	if err != nil {
		diagnostics.StatusText = "无法获取运行时状态: " + err.Error()
		diagnostics.Message = err.Error()
		return diagnostics, true
	}

	diagnostics.ActiveState = snapshot.State
	diagnostics.SubState = runtimelogic.StateLabel(snapshot.State)
	diagnostics.Port = port
	diagnostics.PortListening = snapshot.PortListening
	diagnostics.MainPID = intToString(snapshot.PID)
	diagnostics.StatusText = buildRuntimeStatusText(item, snapshot)
	if snapshot.Reason != "" {
		diagnostics.Message = snapshot.Reason
	}
	for _, owner := range snapshot.PortOwners {
		diagnostics.PortOwners = append(diagnostics.PortOwners, PortOwner{
			Command: owner.Command,
			PID:     owner.PID,
		})
	}
	if snapshot.PID > 0 {
		diagnostics.Processes = append(diagnostics.Processes, item.Kind.Label()+" pid="+intToString(snapshot.PID))
	}

	if lines, logSource, logErr := collectRuntimeLogLines(manager, item.ID); logErr == nil {
		diagnostics.Journal = lines
		diagnostics.JournalRecent = lines
		if logSource != "" {
			diagnostics.StatusText += "\n日志来源: " + logSource
		}
	}

	diskPath := item.Spec.WorkingDir
	if diskPath == "" {
		diskPath = item.GamePath
	}
	if diskPath != "" {
		if free, total, ok := diskFreeBytes(diskPath); ok {
			diagnostics.DiskFreeBytes = free
			diagnostics.DiskTotalBytes = total
		}
	}
	return diagnostics, true
}

// collectRuntimeLogLines 读取运行时日志尾部。
// 日志读不到不算致命错误：状态与端口信息仍然有效。
func collectRuntimeLogLines(manager *runtimelogic.Manager, id string) ([]string, string, error) {
	payload, err := manager.Logs(id, 200)
	if err != nil {
		return nil, "", err
	}
	lines := make([]string, 0, 200)
	if raw, ok := payload["lines"].([]string); ok {
		lines = append(lines, raw...)
	}
	source, _ := payload["source"].(string)
	return lines, source, nil
}

func runtimeServiceLabel(item runtimelogic.Descriptor) string {
	if item.Target.UnitName != "" {
		return item.Target.UnitName
	}
	if item.Target.ContainerName != "" {
		return "容器 " + item.Target.ContainerName
	}
	if item.Target.SessionName != "" {
		return item.Kind.Label() + " " + item.Target.SessionName
	}
	return item.Name
}

func buildRuntimeStatusText(item runtimelogic.Descriptor, snapshot runtimelogic.HealthSnapshot) string {
	parts := []string{
		"运行时: " + item.Name,
		"类型: " + item.Kind.Label(),
		"状态: " + runtimelogic.StateLabel(snapshot.State),
	}
	if snapshot.PID > 0 {
		parts = append(parts, "PID: "+intToString(snapshot.PID))
	}
	if snapshot.PortListening {
		parts = append(parts, "端口已监听")
	} else {
		parts = append(parts, "端口未监听")
	}
	if snapshot.Reason != "" {
		parts = append(parts, "原因: "+snapshot.Reason)
	}
	return strings.Join(parts, "\n")
}

func intToString(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}
