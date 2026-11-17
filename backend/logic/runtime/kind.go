package runtime

// Kind 标识游戏服务端的运行方式。新增运行时必须同时补齐：
// 1) 本文件的常量与能力矩阵；2) 宿主机 Agent 的 provider；3) 前端显示文案。
type Kind string

const (
	KindSystemd     Kind = "systemd"      // systemctl 管理的 system unit
	KindSystemdUser Kind = "systemd-user" // systemctl --user 管理的用户单元
	KindDocker      Kind = "docker"       // docker run / docker compose
	KindScreen      Kind = "screen"       // GNU screen 会话
	KindTmux        Kind = "tmux"         // tmux 会话
	KindNohup       Kind = "nohup"        // nohup + 后台重定向日志
	KindBare        Kind = "bare"         // 无守护直接 fork 的进程
	KindMCSM        Kind = "mcsm"         // MCSManager 节点
	KindLGSM        Kind = "lgsm"         // LinuxGSM 脚本
)

var orderedKinds = []Kind{
	KindSystemd,
	KindSystemdUser,
	KindDocker,
	KindScreen,
	KindTmux,
	KindNohup,
	KindBare,
	KindMCSM,
	KindLGSM,
}

// AllKinds 返回全部运行时类型，前端据此渲染选择器。
func AllKinds() []Kind {
	result := make([]Kind, len(orderedKinds))
	copy(result, orderedKinds)
	return result
}

// KindStrings 返回全部运行时类型的字符串形式，供 /platform 接口直接使用。
func KindStrings() []string {
	result := make([]string, 0, len(orderedKinds))
	for _, kind := range orderedKinds {
		result = append(result, string(kind))
	}
	return result
}

func (k Kind) Valid() bool {
	for _, candidate := range orderedKinds {
		if candidate == k {
			return true
		}
	}
	return false
}

// Label 是前端展示用的中文名称，后端也用它生成默认运行时名。
func (k Kind) Label() string {
	switch k {
	case KindSystemd:
		return "systemd 服务"
	case KindSystemdUser:
		return "systemd 用户服务"
	case KindDocker:
		return "Docker 容器"
	case KindScreen:
		return "screen 会话"
	case KindTmux:
		return "tmux 会话"
	case KindNohup:
		return "nohup 后台进程"
	case KindBare:
		return "裸进程"
	case KindMCSM:
		return "MCSManager 实例"
	case KindLGSM:
		return "LinuxGSM 脚本"
	default:
		return string(k)
	}
}

// ProviderCapabilities 描述某运行时实际能做哪些事。
// 前端据此禁用按钮而不是假装可用后报错。
type ProviderCapabilities struct {
	CanStart     bool `json:"can_start"`
	CanStop      bool `json:"can_stop"`
	CanRestart   bool `json:"can_restart"`
	CanWriteSpec bool `json:"can_write_spec"`
	CanLogs      bool `json:"can_logs"`
	CanAdopt     bool `json:"can_adopt"`
	NeedsRoot    bool `json:"needs_root"`
	Persistent   bool `json:"persistent"`
}

// CapabilitiesOf 返回运行时能力矩阵（设计文档 §5.5）。
func CapabilitiesOf(kind Kind) ProviderCapabilities {
	switch kind {
	case KindSystemd, KindSystemdUser:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: true, CanLogs: true, CanAdopt: true,
			NeedsRoot: true, Persistent: true,
		}
	case KindDocker:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: false, CanLogs: true, CanAdopt: true,
			NeedsRoot: false, Persistent: true,
		}
	case KindScreen, KindTmux:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: false, CanLogs: true, CanAdopt: true,
			NeedsRoot: true, Persistent: false,
		}
	case KindNohup:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: true, CanLogs: true, CanAdopt: true,
			NeedsRoot: true, Persistent: false,
		}
	case KindBare:
		return ProviderCapabilities{
			CanStart: false, CanStop: true, CanRestart: false,
			CanWriteSpec: false, CanLogs: false, CanAdopt: true,
			NeedsRoot: true, Persistent: false,
		}
	case KindMCSM:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: true, CanLogs: true, CanAdopt: true,
			NeedsRoot: false, Persistent: true,
		}
	case KindLGSM:
		return ProviderCapabilities{
			CanStart: true, CanStop: true, CanRestart: true,
			CanWriteSpec: true, CanLogs: true, CanAdopt: true,
			NeedsRoot: false, Persistent: true,
		}
	default:
		return ProviderCapabilities{}
	}
}

// IsPersistent 表示宿主机重启后该运行时是否能自行恢复。
func IsPersistent(kind Kind) bool {
	return CapabilitiesOf(kind).Persistent
}
