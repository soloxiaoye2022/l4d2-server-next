package runtime

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// runtimeIDPattern 约束运行时 ID：稳定 slug，前端可直接当 key 用。
var runtimeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// 运行时状态。状态由健康检查驱动，不由命令返回码驱动（设计文档 §8.4）。
const (
	StateUnknown  = "unknown"
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateDegraded = "degraded"
	StateCrashed  = "crashed"
)

// 运行时来源。adopted 表示是从宿主机扫描接管来的既有部署。
const (
	OriginManual    = "manual"
	OriginDetected  = "detected"
	OriginMigrated  = "migrated"
	OriginInstalled = "installed"
	OriginLegacy    = "legacy"
)

// Target 保存各运行时专属句柄。字段全部可空，由 Kind 决定使用哪些。
type Target struct {
	UnitName       string `json:"unit_name,omitempty"`
	UnitPath       string `json:"unit_path,omitempty"`
	Scope          string `json:"scope,omitempty"`
	ContainerName  string `json:"container_name,omitempty"`
	ComposeFile    string `json:"compose_file,omitempty"`
	ComposeService string `json:"compose_service,omitempty"`
	SessionName    string `json:"session_name,omitempty"`
	PidFile        string `json:"pid_file,omitempty"`
	MCSMNodeURL    string `json:"mcsm_node_url,omitempty"`
	MCSMInstance   string `json:"mcsm_instance,omitempty"`
	LGSMScript     string `json:"lgsm_script,omitempty"`
}

// Normalize 去掉首尾空白，并按 Kind 校验必填句柄。
func (t Target) Normalize(kind Kind) (Target, error) {
	normalized := Target{
		UnitName:       strings.TrimSpace(t.UnitName),
		UnitPath:       strings.TrimSpace(t.UnitPath),
		Scope:          strings.TrimSpace(t.Scope),
		ContainerName:  strings.TrimSpace(t.ContainerName),
		ComposeFile:    strings.TrimSpace(t.ComposeFile),
		ComposeService: strings.TrimSpace(t.ComposeService),
		SessionName:    strings.TrimSpace(t.SessionName),
		PidFile:        strings.TrimSpace(t.PidFile),
		MCSMNodeURL:    strings.TrimSpace(t.MCSMNodeURL),
		MCSMInstance:   strings.TrimSpace(t.MCSMInstance),
		LGSMScript:     strings.TrimSpace(t.LGSMScript),
	}
	switch kind {
	case KindSystemd, KindSystemdUser:
		if normalized.UnitName == "" {
			return Target{}, fmt.Errorf("systemd 运行时必须提供服务名")
		}
		if !validServiceName(normalized.UnitName) {
			return Target{}, fmt.Errorf("服务名无效，必须形如 l4d2.service")
		}
		if normalized.Scope == "" {
			if kind == KindSystemdUser {
				normalized.Scope = "user"
			} else {
				normalized.Scope = "system"
			}
		}
	case KindDocker:
		if normalized.ContainerName == "" {
			return Target{}, fmt.Errorf("Docker 运行时必须提供容器名")
		}
		if !validHandleName(normalized.ContainerName) {
			return Target{}, fmt.Errorf("容器名无效，只能包含字母、数字、下划线、点和连字符")
		}
	case KindScreen, KindTmux:
		if normalized.SessionName == "" {
			return Target{}, fmt.Errorf("%s 运行时必须提供会话名", kind.Label())
		}
		if !validHandleName(normalized.SessionName) {
			return Target{}, fmt.Errorf("会话名无效，只能包含字母、数字、下划线、点和连字符")
		}
	case KindMCSM:
		if normalized.MCSMNodeURL == "" || normalized.MCSMInstance == "" {
			return Target{}, fmt.Errorf("MCSManager 运行时必须提供节点地址与实例 ID")
		}
	case KindLGSM:
		if normalized.LGSMScript == "" {
			return Target{}, fmt.Errorf("LinuxGSM 运行时必须提供脚本路径")
		}
		if !IsAbsolutePath(normalized.LGSMScript) {
			return Target{}, fmt.Errorf("LinuxGSM 脚本路径必须是绝对路径")
		}
	}
	return normalized, nil
}

var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.@:-]+\.service$`)
var handleNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func validServiceName(value string) bool {
	return serviceNamePattern.MatchString(value)
}

func validHandleName(value string) bool {
	return handleNamePattern.MatchString(value)
}

// Descriptor 是运行时注册表条目，也是 data/runtimes.json 的存储单元。
type Descriptor struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       Kind              `json:"kind"`
	Enabled    bool              `json:"enabled"`
	Adopted    bool              `json:"adopted"`
	Managed    bool              `json:"managed"`
	Target     Target            `json:"target"`
	Spec       LaunchSpec        `json:"spec"`
	GamePath   string            `json:"game_path"`
	RealPath   string            `json:"real_path,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
	LastHealth *HealthSnapshot   `json:"last_health,omitempty"`
	Origin     string            `json:"origin"`
	// ArchiveAt 非空表示已被迁移归档，保留期内可回滚。
	ArchiveAt string `json:"archive_at,omitempty"`
	// Capabilities 是能力矩阵的镜像，前端不必自己推断。
	Capabilities ProviderCapabilities `json:"capabilities"`
}

// Normalize 补齐默认值并校验必填字段。
func (d Descriptor) Normalize() (Descriptor, error) {
	normalized := d
	normalized.ID = strings.TrimSpace(d.ID)
	normalized.Name = strings.TrimSpace(d.Name)
	normalized.GamePath = strings.TrimSpace(d.GamePath)
	normalized.RealPath = strings.TrimSpace(d.RealPath)
	normalized.Origin = strings.TrimSpace(d.Origin)
	if normalized.ID == "" {
		return Descriptor{}, fmt.Errorf("运行时 ID 不能为空")
	}
	if !runtimeIDPattern.MatchString(normalized.ID) {
		return Descriptor{}, fmt.Errorf("运行时 ID 只能包含小写字母、数字和连字符，且不超过 32 位")
	}
	if !normalized.Kind.Valid() {
		return Descriptor{}, fmt.Errorf("不支持的运行时类型: %s", normalized.Kind)
	}
	if normalized.Name == "" {
		normalized.Name = normalized.Kind.Label()
	}
	if normalized.Origin == "" {
		normalized.Origin = OriginManual
	}
	target, err := normalized.Target.Normalize(normalized.Kind)
	if err != nil {
		return Descriptor{}, err
	}
	normalized.Target = target
	if normalized.Spec.Port < 0 || normalized.Spec.Port > 65535 {
		return Descriptor{}, fmt.Errorf("端口必须在 1-65535 之间")
	}
	if normalized.GamePath != "" {
		if !IsAbsolutePath(normalized.GamePath) || !IsCleanPath(normalized.GamePath) {
			return Descriptor{}, fmt.Errorf("游戏目录必须是规范化的绝对路径")
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if normalized.CreatedAt == "" {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now
	normalized.Capabilities = CapabilitiesOf(normalized.Kind)
	return normalized, nil
}

// ValidateID 单独校验 ID，供只带 id 的接口使用。
// 注意：这里必须按原始输入校验，不能先 ToLower，
// 否则 "Main" 会被静默接受并变成 "main"，前端与注册表 ID 不一致。
func ValidateID(id string) error {
	value := strings.TrimSpace(id)
	if value != strings.ToLower(value) {
		return fmt.Errorf("运行时 ID 只能使用小写字母")
	}
	if !runtimeIDPattern.MatchString(value) {
		return fmt.Errorf("运行时 ID 只能包含小写字母、数字和连字符，且不超过 32 位")
	}
	return nil
}
