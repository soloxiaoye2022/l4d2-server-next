package runtime

import (
	"strings"
	"time"
)

// InspectData 是 Agent 回传的观测结果。不同 provider 填不同字段，
// 但健康判定只看这里定义的最小集合。
type InspectData struct {
	State         string      `json:"state,omitempty"`
	PID           int         `json:"pid,omitempty"`
	PortListening bool        `json:"port_listening"`
	PortOwners    []PortOwner `json:"port_owners,omitempty"`
	LogTail       string      `json:"log_tail,omitempty"`
	Reason        string      `json:"reason,omitempty"`
	// ProcessRunning 是 provider 的原始判断，用来区分 stopped 与 starting。
	ProcessRunning bool `json:"process_running"`
}

// BuildSnapshot 按"进程 → 端口归属 → RCON"三层判定生成健康快照。
//
// 关键点：systemctl start 返回 0 只代表 systemd 接受请求，不代表服起来了；
// 端口被别的进程占用时 srcds 会静默换端口，所以必须校验 owner。
func BuildSnapshot(data InspectData, rconReachable bool, rconErr string, source string) HealthSnapshot {
	snapshot := HealthSnapshot{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		PID:           data.PID,
		PortListening: data.PortListening,
		PortOwners:    data.PortOwners,
		RCONReachable: rconReachable,
		RCONError:     strings.TrimSpace(rconErr),
		LogTail:       data.LogTail,
		Reason:        data.Reason,
		Source:        source,
	}

	foreign := ForeignPortOwners(data.PortOwners)
	switch {
	case data.State == StateStarting:
		snapshot.State = StateStarting
		snapshot.Reason = firstNonEmpty(snapshot.Reason, "服务端正在启动")
	case !data.ProcessRunning && !data.PortListening:
		snapshot.State = StateStopped
		if strings.EqualFold(data.State, StateCrashed) {
			snapshot.State = StateCrashed
		}
	case len(foreign) > 0:
		snapshot.State = StateDegraded
		snapshot.Reason = firstNonEmpty(snapshot.Reason, "游戏端口被非游戏进程占用")
	case data.ProcessRunning && !data.PortListening:
		snapshot.State = StateStarting
		snapshot.Reason = firstNonEmpty(snapshot.Reason, "进程存在但端口尚未监听")
	case data.PortListening && !rconReachable:
		// 端口通了但 RCON 不通：可能是启动中，也可能是密码错误。
		snapshot.State = StateDegraded
		snapshot.Reason = firstNonEmpty(snapshot.Reason, "端口已监听但 RCON 不可达")
	case data.PortListening && rconReachable:
		snapshot.State = StateRunning
	default:
		snapshot.State = StateUnknown
	}
	return snapshot
}

// ForeignPortOwners 过滤掉游戏进程，剩下的就是"抢端口的人"。
func ForeignPortOwners(owners []PortOwner) []PortOwner {
	foreign := make([]PortOwner, 0, len(owners))
	for _, owner := range owners {
		name := strings.ToLower(strings.TrimSpace(owner.Command))
		if strings.HasPrefix(name, "srcds") {
			continue
		}
		foreign = append(foreign, owner)
	}
	return foreign
}

// StateLabel 返回前端可直接展示的中文状态名。
func StateLabel(state string) string {
	switch state {
	case StateRunning:
		return "运行中"
	case StateStarting:
		return "启动中"
	case StateStopped:
		return "已停止"
	case StateDegraded:
		return "异常"
	case StateCrashed:
		return "已崩溃"
	default:
		return "未知"
	}
}
