package runtime

import (
	"testing"
)

func TestBuildSnapshotStates(t *testing.T) {
	cases := []struct {
		name          string
		data          InspectData
		rconReachable bool
		want          string
	}{
		{
			name:          "进程与端口都在且 RCON 可达 = running",
			data:          InspectData{ProcessRunning: true, PortListening: true, PortOwners: []PortOwner{{Command: "srcds_linux", PID: 100}}},
			rconReachable: true,
			want:          StateRunning,
		},
		{
			name: "进程在但端口未监听 = starting",
			data: InspectData{ProcessRunning: true, PortListening: false},
			want: StateStarting,
		},
		{
			name: "进程与端口都没有 = stopped",
			data: InspectData{ProcessRunning: false, PortListening: false},
			want: StateStopped,
		},
		{
			name: "端口被别的进程占用 = degraded",
			data: InspectData{
				ProcessRunning: true,
				PortListening:  true,
				PortOwners:     []PortOwner{{Command: "nginx", PID: 55}},
			},
			rconReachable: true,
			want:          StateDegraded,
		},
		{
			name: "端口通但 RCON 不通 = degraded",
			data: InspectData{ProcessRunning: true, PortListening: true, PortOwners: []PortOwner{{Command: "srcds_linux", PID: 7}}},
			want: StateDegraded,
		},
		{
			name: "provider 明确报告崩溃 = crashed",
			data: InspectData{State: StateCrashed, ProcessRunning: false, PortListening: false},
			want: StateCrashed,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := BuildSnapshot(testCase.data, testCase.rconReachable, "", "test")
			if snapshot.State != testCase.want {
				t.Fatalf("得到 %q，期望 %q（原因：%s）", snapshot.State, testCase.want, snapshot.Reason)
			}
		})
	}
}

func TestForeignPortOwners(t *testing.T) {
	owners := []PortOwner{
		{Command: "srcds_linux", PID: 1},
		{Command: "srcds_run", PID: 2},
		{Command: "nginx", PID: 3},
		{Command: "", PID: 4},
	}
	foreign := ForeignPortOwners(owners)
	if len(foreign) != 2 {
		t.Fatalf("应识别 2 个非游戏占用者，实际 %d", len(foreign))
	}
	if foreign[0].Command != "nginx" || foreign[1].PID != 4 {
		t.Fatalf("过滤结果错误: %#v", foreign)
	}
}

func TestStateLabel(t *testing.T) {
	cases := map[string]string{
		StateRunning:  "运行中",
		StateStarting: "启动中",
		StateStopped:  "已停止",
		StateDegraded: "异常",
		StateCrashed:  "已崩溃",
		"":            "未知",
	}
	for state, want := range cases {
		if got := StateLabel(state); got != want {
			t.Fatalf("状态 %q 得到 %q，期望 %q", state, got, want)
		}
	}
}
