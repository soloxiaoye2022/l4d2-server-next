package runtime

import (
	"reflect"
	"testing"
)

func TestSplitCommandHandlesQuotesAndEscapes(t *testing.T) {
	cases := []struct {
		name    string
		command string
		want    []string
	}{
		{"普通参数", "./srcds_run -game left4dead2 +port 27015", []string{"./srcds_run", "-game", "left4dead2", "+port", "27015"}},
		{"含空格路径", `"/opt/my games/srcds_run" -game left4dead2`, []string{"/opt/my games/srcds_run", "-game", "left4dead2"}},
		{"单引号", `'/opt/my games/srcds_run' -console`, []string{"/opt/my games/srcds_run", "-console"}},
		{"反斜杠转义空格", `/opt/my\ games/srcds_run`, []string{"/opt/my games/srcds_run"}},
		{"空参数", `./srcds_run "" +port 27015`, []string{"./srcds_run", "", "+port", "27015"}},
		{"多余空白", "  ./srcds_run   -game   left4dead2  ", []string{"./srcds_run", "-game", "left4dead2"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := SplitCommand(testCase.command)
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("解析结果不一致\n得到: %#v\n期望: %#v", got, testCase.want)
			}
		})
	}
}

func TestSplitCommandRejectsBrokenInput(t *testing.T) {
	if _, err := SplitCommand(`./srcds_run "unterminated`); err == nil {
		t.Fatal("未闭合引号必须报错")
	}
	if _, err := SplitCommand(`./srcds_run trailing\`); err == nil {
		t.Fatal("尾部孤立转义符必须报错")
	}
}

func TestCommandLineRoundTrip(t *testing.T) {
	spec := LaunchSpec{
		Executable: "/opt/my games/srcds_run",
		Args:       []string{"-game", "left4dead2", "+port", "27015"},
	}
	line := spec.CommandLine()
	tokens, err := SplitCommand(line)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	want := append([]string{spec.Executable}, spec.Args...)
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("往返不一致\n得到: %#v\n期望: %#v", tokens, want)
	}
}

func TestLaunchSpecNormalize(t *testing.T) {
	valid := LaunchSpec{
		WorkingDir: "/home/ubuntu/l4d2",
		Executable: "/home/ubuntu/l4d2/srcds_run",
		Args:       []string{"-game", "left4dead2"},
		Port:       27015,
	}
	normalized, err := valid.Normalize()
	if err != nil {
		t.Fatalf("合法规格被拒绝: %v", err)
	}
	if normalized.StopSignal != "TERM" {
		t.Fatalf("默认停止信号应为 TERM，得到 %q", normalized.StopSignal)
	}

	invalidCases := map[string]LaunchSpec{
		"相对工作目录":  {WorkingDir: "l4d2", Executable: "/x/srcds_run"},
		"相对可执行文件": {WorkingDir: "/home/ubuntu/l4d2", Executable: "srcds_run"},
		"含上跳段":    {WorkingDir: "/home/../etc", Executable: "/x/srcds_run"},
		"换行注入":    {WorkingDir: "/home/ubuntu/l4d2\nrm -rf /", Executable: "/x/srcds_run"},
		"端口越界":    {WorkingDir: "/home/ubuntu/l4d2", Executable: "/x/srcds_run", Port: 70000},
		"非法运行用户":  {WorkingDir: "/home/ubuntu/l4d2", Executable: "/x/srcds_run", User: "root; rm -rf /"},
		"参数含换行":   {WorkingDir: "/home/ubuntu/l4d2", Executable: "/x/srcds_run", Args: []string{"a\nb"}},
	}
	for name, spec := range invalidCases {
		t.Run(name, func(t *testing.T) {
			if _, err := spec.Normalize(); err == nil {
				t.Fatal("非法规格必须被拒绝")
			}
		})
	}
}

func TestParseCommandLine(t *testing.T) {
	executable, args, err := ParseCommandLine(`/opt/l4d2/srcds_run -game left4dead2 +port 27015`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if executable != "/opt/l4d2/srcds_run" {
		t.Fatalf("可执行文件错误: %s", executable)
	}
	if len(args) != 4 {
		t.Fatalf("参数数量错误: %#v", args)
	}
	if _, _, err := ParseCommandLine("   "); err == nil {
		t.Fatal("空命令必须报错")
	}
}

func TestValidUserName(t *testing.T) {
	valid := []string{"ubuntu", "l4d2", "game_user", "user-1"}
	for _, name := range valid {
		if !ValidUserName(name) {
			t.Fatalf("%q 应被接受", name)
		}
	}
	invalid := []string{"", "Root", "1abc", "user name", "user;rm", "a/b"}
	for _, name := range invalid {
		if ValidUserName(name) {
			t.Fatalf("%q 应被拒绝", name)
		}
	}
}
