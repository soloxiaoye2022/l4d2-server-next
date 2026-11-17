package logic

import (
	"os"
	"strings"

	"l4d2-manager-next/consts"
	runtimelogic "l4d2-manager-next/logic/runtime"
)

// runtimeInitialized 保证 runtime 子系统只初始化一次。
var runtimeInitialized bool

// InitRuntimeManager 把面板侧配置注入运行时子系统，并注册旧部署兜底。
//
// 为什么需要兜底：老用户升级上来时 data/runtimes.json 不存在，
// 但宿主机上已经有 systemd 服务在跑。如果注册表为空就直接报"没有运行时"，
// 用户会以为面板坏了。这里把旧链路的配置合成一个只读的 legacy 运行时，
// 用户点"保存"后才真正落盘。
func InitRuntimeManager() {
	if runtimeInitialized {
		return
	}
	runtimeInitialized = true

	runtimelogic.Init(runtimelogic.Settings{
		DataDir:      consts.ManagerDataPath,
		ControlDir:   DefaultControlDir(),
		DefaultUser:  defaultRuntimeUser(),
		AllowedRoots: runtimeAllowedRoots(),
	})
	runtimelogic.RegisterLegacyProvider(buildLegacyRuntime)
}

// defaultRuntimeUser 是迁移/新建运行时默认的运行用户。
// 面板容器内 UID 1000 常见于 ubuntu 用户，允许用环境变量覆盖。
func defaultRuntimeUser() string {
	if value := strings.TrimSpace(os.Getenv("L4D2_RUNTIME_USER")); value != "" {
		return value
	}
	return "ubuntu"
}

// runtimeAllowedRoots 返回 Agent 允许写入的根目录。
// 分隔符与 Agent 侧一致：使用 os.PathListSeparator（Linux ":"，Windows ";"）。
func runtimeAllowedRoots() []string {
	raw := strings.TrimSpace(os.Getenv("L4D2_ALLOWED_ROOTS"))
	if raw == "" {
		return runtimelogic.DefaultAllowedRoots()
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value != "" {
			roots = append(roots, value)
		}
	}
	return roots
}

// buildLegacyRuntime 把现有 systemd / batch 部署合成一个运行时描述符。
// 它只在注册表为空时出现，不写入 data/runtimes.json。
func buildLegacyRuntime() []runtimelogic.Descriptor {
	unitPath := serverStartupUnitPath()
	serviceName := serverStartupServiceName()

	kind := runtimelogic.KindSystemd
	origin := runtimelogic.OriginLegacy
	if IsWindows() {
		// Windows 没有 systemd，旧部署是 bat 启动器，无法由 Agent 管理。
		kind = runtimelogic.KindBare
	}

	descriptor := runtimelogic.Descriptor{
		ID:       "default",
		Name:     "当前服务端（旧部署）",
		Kind:     kind,
		Enabled:  true,
		Adopted:  true,
		Managed:  false,
		Origin:   origin,
		GamePath: consts.GamePath,
		Labels: map[string]string{
			"unit_path": unitPath,
			"source":    "legacy",
		},
	}

	if kind == runtimelogic.KindSystemd {
		descriptor.Target = runtimelogic.Target{
			UnitName: serviceName,
			UnitPath: unitPath,
			Scope:    "system",
		}
		if cfg, err := GetServerStartupConfig(); err == nil {
			descriptor.Spec = runtimelogic.LaunchSpec{
				WorkingDir: cfg.WorkingDirectory,
				Executable: executableFromExecStart(cfg.ExecStart),
				Args:       argsFromExecStart(cfg.ExecStart),
				Port:       cfg.Port,
			}
			if cfg.WorkingDirectory != "" {
				descriptor.GamePath = cfg.WorkingDirectory
			}
		}
	}

	return []runtimelogic.Descriptor{descriptor}
}

// executableFromExecStart 从完整命令行里取出可执行文件。
func executableFromExecStart(command string) string {
	tokens, err := runtimelogic.SplitCommand(strings.TrimSpace(command))
	if err != nil || len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

// argsFromExecStart 从完整命令行里取出参数列表。
func argsFromExecStart(command string) []string {
	tokens, err := runtimelogic.SplitCommand(strings.TrimSpace(command))
	if err != nil || len(tokens) <= 1 {
		return nil
	}
	return tokens[1:]
}

// ControlViaRuntime 尝试用运行时子系统执行动作。
//
// 返回值 handled=false 表示"当前部署不该走新链路"，调用方必须退回旧逻辑：
//   - 注册表为空、只有 legacy 合成项（老用户还没接管任何运行时）
//   - 宿主机 Agent 不可用（面板无法控制进程）
//
// 这样能保证老部署升级后行为完全不变（设计文档 §3 原则 7）。
func ControlViaRuntime(action string) (bool, string, error) {
	InitRuntimeManager()
	manager := runtimelogic.Default()
	if manager.AgentMode() == runtimelogic.AgentModeDisabled {
		return false, "", nil
	}
	items, _, err := manager.All()
	if err != nil {
		return false, "", nil
	}
	target := ""
	for _, item := range items {
		if !item.Enabled || item.ArchiveAt != "" {
			continue
		}
		if item.Origin == runtimelogic.OriginLegacy {
			continue
		}
		target = item.ID
		break
	}
	if target == "" {
		return false, "", nil
	}
	result, err := manager.Action(target, action)
	if err != nil {
		return true, target, err
	}
	if result.Status != runtimelogic.JobSuccess {
		message := result.Message
		if strings.TrimSpace(message) == "" {
			message = result.Status
		}
		return true, target, errRuntimeActionFailed(message)
	}
	return true, target, nil
}

// errRuntimeActionFailed 把 Agent 的非成功状态转成 error，避免上层忽略。
type runtimeActionError struct {
	message string
}

func (e *runtimeActionError) Error() string {
	return e.message
}

func errRuntimeActionFailed(message string) error {
	return &runtimeActionError{message: message}
}
