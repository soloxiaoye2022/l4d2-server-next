package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// DiscoveryCandidate 是宿主机扫描出的一个候选服务端（设计文档 §7.3）。
// 前端必须原样展示 Evidence，让用户自己判断"这条是不是我的服"。
type DiscoveryCandidate struct {
	Key         string     `json:"key"`
	Kind        Kind       `json:"kind"`
	Name        string     `json:"name"`
	Confidence  float64    `json:"confidence"`
	Running     bool       `json:"running"`
	PID         int        `json:"pid,omitempty"`
	User        string     `json:"user,omitempty"`
	Spec        LaunchSpec `json:"spec"`
	Target      Target     `json:"target"`
	GamePath    string     `json:"game_path"`
	Port        int        `json:"port"`
	Persistence string     `json:"persistence"`
	Evidence    []string   `json:"evidence"`
	Adoptable   bool       `json:"adoptable"`
	Reason      string     `json:"reason,omitempty"`
	AlreadyID   string     `json:"already_runtime_id,omitempty"`
}

// CandidateKey 生成稳定指纹：类型 + 主标识 + 游戏目录哈希。
// 同一台机器重复扫描必须得到同一个 key，接管列表才不会漂移。
// 目录必须规范化（去尾部斜杠、清理 . 与 ..），否则同一条目会生成两个候选。
func CandidateKey(kind Kind, identity string, gamePath string) string {
	normalizedPath := ""
	if strings.TrimSpace(gamePath) != "" {
		normalizedPath = cleanStyle(strings.TrimSpace(gamePath))
	}
	normalized := strings.ToLower(strings.TrimSpace(identity)) + "|" + normalizedPath
	sum := sha256.Sum256([]byte(string(kind) + "|" + normalized))
	return string(kind) + "-" + hex.EncodeToString(sum[:6])
}

// NormalizeCandidate 补齐 key、持久化标记与置信度边界。
func NormalizeCandidate(candidate DiscoveryCandidate) DiscoveryCandidate {
	value := candidate
	if value.Key == "" {
		value.Key = CandidateKey(value.Kind, candidateIdentity(value), value.GamePath)
	}
	if value.Persistence == "" {
		if IsPersistent(value.Kind) {
			value.Persistence = "persistent"
		} else {
			value.Persistence = "ephemeral"
		}
	}
	if value.Confidence < 0 {
		value.Confidence = 0
	}
	if value.Confidence > 1 {
		value.Confidence = 1
	}
	if value.Name == "" {
		value.Name = defaultCandidateName(value)
	}
	return value
}

func candidateIdentity(candidate DiscoveryCandidate) string {
	switch candidate.Kind {
	case KindSystemd, KindSystemdUser:
		return candidate.Target.UnitName
	case KindDocker:
		return candidate.Target.ContainerName
	case KindScreen, KindTmux:
		return candidate.Target.SessionName
	case KindMCSM:
		return candidate.Target.MCSMInstance
	case KindLGSM:
		return candidate.Target.LGSMScript
	default:
		if candidate.PID > 0 {
			return fmt.Sprintf("pid-%d", candidate.PID)
		}
		return candidate.Spec.Executable
	}
}

func defaultCandidateName(candidate DiscoveryCandidate) string {
	switch candidate.Kind {
	case KindSystemd, KindSystemdUser:
		if candidate.Target.UnitName != "" {
			return candidate.Target.UnitName
		}
	case KindDocker:
		if candidate.Target.ContainerName != "" {
			return "容器 " + candidate.Target.ContainerName
		}
	case KindScreen:
		return "screen " + candidate.Target.SessionName
	case KindTmux:
		return "tmux " + candidate.Target.SessionName
	}
	if candidate.GamePath != "" {
		return candidate.Kind.Label() + " · " + candidate.GamePath
	}
	return candidate.Kind.Label()
}

// FuseCandidates 去重并排序：同一进程只保留证据最全的一条。
// 归属判定优先级 cgroup > session > docker inspect > ppid 链，
// 由 Agent 在生成候选时按 evidence 顺序体现。
func FuseCandidates(candidates []DiscoveryCandidate) []DiscoveryCandidate {
	byKey := make(map[string]DiscoveryCandidate, len(candidates))
	order := make([]string, 0, len(candidates))
	for _, raw := range candidates {
		candidate := NormalizeCandidate(raw)
		existing, exists := byKey[candidate.Key]
		if !exists {
			byKey[candidate.Key] = candidate
			order = append(order, candidate.Key)
			continue
		}
		if betterCandidate(candidate, existing) {
			byKey[candidate.Key] = candidate
		}
	}
	result := make([]DiscoveryCandidate, 0, len(order))
	for _, key := range order {
		result = append(result, byKey[key])
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Running != result[j].Running {
			return result[i].Running
		}
		if result[i].Confidence != result[j].Confidence {
			return result[i].Confidence > result[j].Confidence
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func betterCandidate(candidate, existing DiscoveryCandidate) bool {
	if candidate.Confidence != existing.Confidence {
		return candidate.Confidence > existing.Confidence
	}
	if len(candidate.Evidence) != len(existing.Evidence) {
		return len(candidate.Evidence) > len(existing.Evidence)
	}
	return candidate.Running && !existing.Running
}

// MarkAdoptionState 给候选补充"是否已被接管"与不可接管原因。
//
// legacy 合成项不参与"已接管"判定：它只是注册表为空时的只读占位，
// 用来让老部署在运行时页面上有个条目可看。如果连它也阻止接管，
// 用户点"接管"会得到"已被运行时 default 接管"，而 default 根本不可编辑、
// 不落盘，等于永远无法把老部署升级成真正的运行时。这正是接管的入口场景。
func MarkAdoptionState(candidates []DiscoveryCandidate, items []Descriptor) []DiscoveryCandidate {
	byGamePath := make(map[string]string, len(items))
	byTarget := make(map[string]string, len(items))
	for _, item := range items {
		if item.Origin == OriginLegacy {
			continue
		}
		if item.GamePath != "" {
			byGamePath[cleanStyle(item.GamePath)] = item.ID
		}
		byTarget[targetFingerprint(item.Kind, item.Target)] = item.ID
	}
	result := make([]DiscoveryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		value := candidate
		if id, ok := byTarget[targetFingerprint(value.Kind, value.Target)]; ok {
			value.AlreadyID = id
		} else if value.GamePath != "" {
			if id, ok := byGamePath[cleanStyle(value.GamePath)]; ok {
				value.AlreadyID = id
			}
		}
		if value.AlreadyID != "" {
			value.Adoptable = false
			value.Reason = "该服务端已被运行时 " + value.AlreadyID + " 接管"
		} else if value.Kind == KindBare && value.PID == 0 {
			value.Adoptable = false
			value.Reason = "未找到运行中的进程，无法接管"
		} else {
			value.Adoptable = true
		}
		result = append(result, value)
	}
	return result
}

func targetFingerprint(kind Kind, target Target) string {
	switch kind {
	case KindSystemd, KindSystemdUser:
		return string(kind) + "|" + target.UnitName
	case KindDocker:
		return string(kind) + "|" + target.ContainerName
	case KindScreen, KindTmux:
		return string(kind) + "|" + target.SessionName
	case KindMCSM:
		return string(kind) + "|" + target.MCSMNodeURL + "|" + target.MCSMInstance
	case KindLGSM:
		return string(kind) + "|" + target.LGSMScript
	default:
		return string(kind) + "|"
	}
}

// DetectResult 是 /runtime/detect 的返回体。
type DetectResult struct {
	Candidates []DiscoveryCandidate `json:"candidates"`
	ScannedAt  string               `json:"scanned_at"`
	Warnings   []string             `json:"warnings"`
}
