package logic

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeFakeProc 在临时目录里伪造 /proc/<pid>/cmdline 与 cgroup。
func writeFakeProc(t *testing.T, root string, pid int, cmdline []string, cgroup string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	payload := ""
	for _, arg := range cmdline {
		payload += arg + "\x00"
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(payload), 0644); err != nil {
		t.Fatal(err)
	}
	if cgroup != "" {
		if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(cgroup), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindServiceProcessPrefersCgroupMember(t *testing.T) {
	root := t.TempDir()
	// 无关进程
	writeFakeProc(t, root, 100, []string{"/usr/bin/nginx", "-g", "daemon off;"}, "0::/system.slice/nginx.service")
	// 包装 shell
	writeFakeProc(t, root, 200,
		[]string{"/bin/sh", "/data/l4d2-server/srcds_run", "-game", "left4dead2"},
		"0::/system.slice/l4d2.service")
	// 真正服务端，参数更长
	writeFakeProc(t, root, 201,
		[]string{"./srcds_linux", "-game", "left4dead2", "-insecure", "-tickrate", "100", "+hostport", "27015"},
		"0::/system.slice/l4d2.service")

	t.Setenv(hostProcDirEnv, root)

	process, ok := FindServiceProcess("l4d2.service")
	if !ok {
		t.Fatal("FindServiceProcess() 未找到进程")
	}
	if process.PID != 201 {
		t.Fatalf("应选参数最长的 srcds_linux，实际 PID=%d", process.PID)
	}
	if !process.ViaCgroup {
		t.Fatal("通过 cgroup 匹配的进程应标记 ViaCgroup")
	}
	if got := ParseGameServerPort(process.Command); got != 27015 {
		t.Fatalf("端口解析 = %d，期望 27015", got)
	}
}

func TestFindServiceProcessRejectsAmbiguousFallback(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, 300, []string{"./srcds_linux", "-game", "left4dead2", "+hostport", "27015"}, "")
	writeFakeProc(t, root, 301, []string{"./srcds_linux", "-game", "left4dead2", "+hostport", "27016"}, "")

	t.Setenv(hostProcDirEnv, root)

	if _, ok := FindServiceProcess("l4d2.service"); ok {
		t.Fatal("多个无归属候选时必须拒绝返回，避免取错实例")
	}
}

func TestFindServiceProcessSingleFallback(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, 400,
		[]string{"./srcds_linux", "-game", "left4dead2", "+hostport", "27018"}, "")

	t.Setenv(hostProcDirEnv, root)

	process, ok := FindServiceProcess("l4d2.service")
	if !ok {
		t.Fatal("唯一候选应被采纳")
	}
	if process.PID != 400 || process.ViaCgroup {
		t.Fatalf("兜底匹配结果错误: %+v", process)
	}
}

func TestIsWrapperExecStart(t *testing.T) {
	cases := map[string]bool{
		"/data/l4d2-server/start_server.sh":                            true,
		"/bin/sh /data/l4d2-server/start_server.sh":                    true,
		"/data/l4d2-server/srcds_run -game left4dead2 +hostport 27015": false,
		"/home/ubuntu/steamcmd/l4d2/srcds_run":                         false,
	}
	for execStart, want := range cases {
		if got := isWrapperExecStart(execStart); got != want {
			t.Errorf("isWrapperExecStart(%q) = %v, want %v", execStart, got, want)
		}
	}
}
