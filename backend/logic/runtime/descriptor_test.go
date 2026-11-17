package runtime

import (
	"strings"
	"testing"
)

func TestTargetNormalizePerKind(t *testing.T) {
	validCases := map[string]struct {
		kind   Kind
		target Target
	}{
		"systemd":      {KindSystemd, Target{UnitName: "l4d2.service"}},
		"systemd-user": {KindSystemdUser, Target{UnitName: "l4d2.service"}},
		"docker":       {KindDocker, Target{ContainerName: "l4d2"}},
		"screen":       {KindScreen, Target{SessionName: "l4d2-main"}},
		"tmux":         {KindTmux, Target{SessionName: "l4d2_main"}},
		"lgsm":         {KindLGSM, Target{LGSMScript: "/home/lgsm/l4d2server/l4d2server"}},
		"mcsm":         {KindMCSM, Target{MCSMNodeURL: "http://127.0.0.1:23333", MCSMInstance: "abc123"}},
		"nohup":        {KindNohup, Target{}},
		"bare":         {KindBare, Target{}},
	}
	for name, testCase := range validCases {
		t.Run(name, func(t *testing.T) {
			if _, err := testCase.target.Normalize(testCase.kind); err != nil {
				t.Fatalf("合法句柄被拒绝: %v", err)
			}
		})
	}

	invalidCases := map[string]struct {
		kind   Kind
		target Target
	}{
		"systemd 缺服务名":  {KindSystemd, Target{}},
		"systemd 服务名非法": {KindSystemd, Target{UnitName: "l4d2; rm -rf /"}},
		"systemd 缺后缀":   {KindSystemd, Target{UnitName: "l4d2"}},
		"docker 缺容器名":   {KindDocker, Target{}},
		"docker 容器名非法":  {KindDocker, Target{ContainerName: "-bad"}},
		"screen 缺会话名":   {KindScreen, Target{}},
		"lgsm 相对路径":     {KindLGSM, Target{LGSMScript: "l4d2server"}},
		"mcsm 缺实例":      {KindMCSM, Target{MCSMNodeURL: "http://127.0.0.1:23333"}},
	}
	for name, testCase := range invalidCases {
		t.Run(name, func(t *testing.T) {
			if _, err := testCase.target.Normalize(testCase.kind); err == nil {
				t.Fatal("非法句柄必须被拒绝")
			}
		})
	}
}

func TestTargetNormalizeFillsScope(t *testing.T) {
	target, err := Target{UnitName: "l4d2.service"}.Normalize(KindSystemdUser)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if target.Scope != "user" {
		t.Fatalf("systemd-user 默认作用域应为 user，实际 %q", target.Scope)
	}
	target, err = Target{UnitName: "l4d2.service"}.Normalize(KindSystemd)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if target.Scope != "system" {
		t.Fatalf("systemd 默认作用域应为 system，实际 %q", target.Scope)
	}
}

func TestDescriptorNormalize(t *testing.T) {
	valid := sampleDescriptor()
	normalized, err := valid.Normalize()
	if err != nil {
		t.Fatalf("合法描述符被拒绝: %v", err)
	}
	if normalized.Capabilities.CanStart != true {
		t.Fatal("能力矩阵必须被补齐")
	}
	if normalized.UpdatedAt == "" {
		t.Fatal("更新时间必须被写入")
	}
	if normalized.Origin != OriginManual {
		t.Fatalf("默认来源应为 manual，实际 %q", normalized.Origin)
	}

	invalidCases := map[string]Descriptor{
		"空 ID":     {ID: "", Kind: KindSystemd, Target: Target{UnitName: "a.service"}},
		"大写 ID":    {ID: "Main", Kind: KindSystemd, Target: Target{UnitName: "a.service"}},
		"非法类型":     {ID: "x", Kind: Kind("nope")},
		"游戏目录相对路径": {ID: "x", Kind: KindSystemd, Target: Target{UnitName: "a.service"}, GamePath: "l4d2"},
	}
	for name, descriptor := range invalidCases {
		t.Run(name, func(t *testing.T) {
			if _, err := descriptor.Normalize(); err == nil {
				t.Fatal("非法描述符必须被拒绝")
			}
		})
	}
}

func TestCapabilitiesOf(t *testing.T) {
	if !CapabilitiesOf(KindSystemd).CanWriteSpec {
		t.Fatal("systemd 必须支持写入启动规格")
	}
	if CapabilitiesOf(KindBare).CanStart {
		t.Fatal("裸进程不应支持启动")
	}
	if CapabilitiesOf(KindScreen).Persistent {
		t.Fatal("screen 不应标记为持久化")
	}
	if !CapabilitiesOf(KindDocker).Persistent {
		t.Fatal("docker 应标记为持久化")
	}
}

func TestValidateID(t *testing.T) {
	valid := []string{"main", "l4d2-1", "a", "abc-123"}
	for _, id := range valid {
		if err := ValidateID(id); err != nil {
			t.Fatalf("%q 应被接受: %v", id, err)
		}
	}
	invalid := []string{"", "Main", "a b", "-abc", "abc_", strings.Repeat("a", 40)}
	for _, id := range invalid {
		if err := ValidateID(id); err == nil {
			t.Fatalf("%q 应被拒绝", id)
		}
	}
}
