package runtime

import (
	"testing"
)

func TestCandidateKeyIsStable(t *testing.T) {
	first := CandidateKey(KindSystemd, "l4d2.service", "/home/ubuntu/l4d2")
	second := CandidateKey(KindSystemd, "L4D2.Service", "/home/ubuntu/l4d2/")
	if first != second {
		t.Fatalf("同一服务端两次扫描 key 必须一致: %q vs %q", first, second)
	}
	other := CandidateKey(KindScreen, "l4d2.service", "/home/ubuntu/l4d2")
	if first == other {
		t.Fatal("不同类型必须产生不同 key")
	}
}

func TestFuseCandidatesPrefersStrongerEvidence(t *testing.T) {
	candidates := []DiscoveryCandidate{
		{
			Kind:       KindSystemd,
			Name:       "弱证据",
			Confidence: 0.6,
			Target:     Target{UnitName: "l4d2.service"},
			GamePath:   "/home/ubuntu/l4d2",
			Evidence:   []string{"ps"},
		},
		{
			Kind:       KindSystemd,
			Name:       "强证据",
			Confidence: 0.95,
			Running:    true,
			Target:     Target{UnitName: "l4d2.service"},
			GamePath:   "/home/ubuntu/l4d2",
			Evidence:   []string{"systemctl", "cgroup"},
		},
		{
			Kind:       KindDocker,
			Name:       "容器",
			Confidence: 0.9,
			Target:     Target{ContainerName: "l4d2"},
			GamePath:   "/data/l4d2",
		},
	}
	fused := FuseCandidates(candidates)
	if len(fused) != 2 {
		t.Fatalf("应合并为 2 条，实际 %d", len(fused))
	}
	if fused[0].Name != "强证据" {
		t.Fatalf("运行中的高置信度候选应排在最前，实际 %q", fused[0].Name)
	}
}

func TestNormalizeCandidateFillsDefaults(t *testing.T) {
	candidate := NormalizeCandidate(DiscoveryCandidate{
		Kind:     KindScreen,
		Target:   Target{SessionName: "l4d2"},
		GamePath: "/home/ubuntu/l4d2",
	})
	if candidate.Key == "" {
		t.Fatal("必须自动生成 key")
	}
	if candidate.Persistence != "ephemeral" {
		t.Fatalf("screen 应标记为 ephemeral，实际 %q", candidate.Persistence)
	}
	if candidate.Name == "" {
		t.Fatal("必须自动生成名称")
	}
	if candidate.Confidence != 0 {
		t.Fatalf("置信度应被限制在 [0,1]，实际 %v", candidate.Confidence)
	}
}

func TestMarkAdoptionState(t *testing.T) {
	items := []Descriptor{
		{
			ID:   "main",
			Kind: KindSystemd,
			Target: Target{
				UnitName: "l4d2.service",
				Scope:    "system",
			},
			GamePath: "/home/ubuntu/l4d2",
		},
	}
	candidates := []DiscoveryCandidate{
		{
			Kind:     KindSystemd,
			Target:   Target{UnitName: "l4d2.service"},
			GamePath: "/home/ubuntu/l4d2",
		},
		{
			Kind:     KindScreen,
			Target:   Target{SessionName: "other"},
			GamePath: "/opt/other",
		},
	}
	marked := MarkAdoptionState(candidates, items)
	if marked[0].Adoptable {
		t.Fatal("已被接管的候选不应可再次接管")
	}
	if marked[0].AlreadyID != "main" {
		t.Fatalf("应标记已接管的运行时 ID，实际 %q", marked[0].AlreadyID)
	}
	if !marked[1].Adoptable {
		t.Fatal("未接管的候选应可接管")
	}
}

// legacy 合成项只是注册表为空时的只读占位，不能阻止真实接管。
// 否则老部署永远无法升级成可编辑、可落盘的运行时。
func TestMarkAdoptionStateIgnoresLegacyPlaceholder(t *testing.T) {
	items := []Descriptor{
		{
			ID:       "default",
			Kind:     KindSystemd,
			Origin:   OriginLegacy,
			Target:   Target{UnitName: "l4d2.service"},
			GamePath: "/home/ubuntu/l4d2",
		},
	}
	candidates := []DiscoveryCandidate{
		{
			Kind:     KindSystemd,
			Target:   Target{UnitName: "l4d2.service"},
			GamePath: "/home/ubuntu/l4d2",
		},
	}
	marked := MarkAdoptionState(candidates, items)
	if !marked[0].Adoptable {
		t.Fatalf("legacy 占位项不应阻止接管，原因: %q", marked[0].Reason)
	}
	if marked[0].AlreadyID != "" {
		t.Fatalf("不应把 legacy 项标为已接管，实际 %q", marked[0].AlreadyID)
	}
}

func TestMarkAdoptionStateRejectsBareWithoutPid(t *testing.T) {
	candidates := []DiscoveryCandidate{
		{Kind: KindBare, GamePath: "/opt/l4d2"},
	}
	marked := MarkAdoptionState(candidates, nil)
	if marked[0].Adoptable {
		t.Fatal("没有 PID 的裸进程不应可接管")
	}
}
