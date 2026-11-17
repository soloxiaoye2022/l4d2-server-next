package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInstallPath(t *testing.T) {
	t.Setenv("L4D2_INSTALL_ROOT", "/data/l4d2")

	cases := []struct {
		name string
		raw  string
		home string
		want string
	}{
		{"空值走默认", "", "/home/ubuntu", "/data/l4d2/l4d2-new"},
		{"相对路径以 InstallRoot 为基准", "l4d2-test", "/home/ubuntu", "/data/l4d2/l4d2-test"},
		{"绝对路径原样保留", "/opt/l4d2-standalone", "/home/ubuntu", "/opt/l4d2-standalone"},
		{"波浪号展开", "~/l4d2", "/home/ubuntu", "/home/ubuntu/l4d2"},
		{"清理冗余斜杠", "/opt//l4d2/", "/home/ubuntu", "/opt/l4d2"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := ResolveInstallPath(testCase.raw, testCase.home)
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("得到 %q，期望 %q", got, testCase.want)
			}
		})
	}

	rejectCases := map[string]struct {
		raw  string
		home string
	}{
		"上跳逃逸":    {"../../etc", "/home/ubuntu"},
		"根目录本身":   {"/", "/home/ubuntu"},
		"顶层系统目录":  {"/home", "/home/ubuntu"},
		"换行注入":    {"/opt/l4d2\nrm", "/home/ubuntu"},
		"波浪号无家目录": {"~/l4d2", ""},
	}
	for name, testCase := range rejectCases {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveInstallPath(testCase.raw, testCase.home); err == nil {
				t.Fatal("非法路径必须被拒绝")
			}
		})
	}
}

func TestCheckAllowedRoot(t *testing.T) {
	allowed := []string{"/home", "/data"}
	if err := CheckAllowedRoot("/home/ubuntu/l4d2", allowed); err != nil {
		t.Fatalf("白名单内路径被拒绝: %v", err)
	}
	if err := CheckAllowedRoot("/opt/l4d2", allowed); err == nil {
		t.Fatal("白名单外路径必须被拒绝")
	}
	if err := CheckAllowedRoot("/home", allowed); err == nil {
		t.Fatal("不允许直接把根目录当目标")
	}
	if err := CheckAllowedRoot("/opt/l4d2", nil); err != nil {
		t.Fatalf("白名单为空时应不限制: %v", err)
	}
}

func TestIsAbsolutePathAndCleanPath(t *testing.T) {
	absolute := []string{"/home/ubuntu/l4d2", `D:\SteamCMD\l4d2`, `\\server\share`}
	for _, value := range absolute {
		if !IsAbsolutePath(value) {
			t.Fatalf("%q 应被判定为绝对路径", value)
		}
	}
	relative := []string{"l4d2", "./l4d2", "C:l4d2"}
	for _, value := range relative {
		if IsAbsolutePath(value) {
			t.Fatalf("%q 应被判定为相对路径", value)
		}
	}
	if IsCleanPath("/home/../etc") {
		t.Fatal("含 .. 的路径必须被判为不干净")
	}
	if !IsCleanPath("/home/ubuntu/l4d2") {
		t.Fatal("普通绝对路径应被判为干净")
	}
}

func TestDefaultLogPath(t *testing.T) {
	got := DefaultLogPath("/home/ubuntu/l4d2", "主服 A")
	want := "/home/ubuntu/l4d2/logs/主服-a.log"
	if got != want {
		t.Fatalf("得到 %q，期望 %q", got, want)
	}
	if DefaultLogPath("", "x") != "" {
		t.Fatal("工作目录为空时应返回空日志路径")
	}
}

func TestInstallRootFallsBackToDefault(t *testing.T) {
	t.Setenv("L4D2_INSTALL_ROOT", "")
	got := InstallRoot()
	if got == "" {
		t.Fatal("安装根目录不能为空")
	}
	if _, err := os.Stat(got); err == nil {
		// 存在也没关系，只是在验证函数可用。
		_ = filepath.Clean(got)
	}
}
