# 多运行时适配架构设计（2026-11-17）

> 本文是改造前的设计基线。后续代码必须严格按本文实现；实现与本文不一致时，先改本文再改代码。
> 涉及范围：后端 `backend/logic`、`backend/controller`、`backend/main.go`、`manifest/`、前端 `frontend/src`。

---

## 1. 背景与目标

### 1.1 现状一句话

面板目前只有一条"管理游戏服"的路径：**读宿主机的 systemd unit 文件**（容器内是 `/control/l4d2.service` 快照），重启走 `docker restart` 或 RCON，诊断/维护/修复全部通过 `/control/*.json` 请求文件交给宿主机 path unit 执行。

### 1.2 用户提出的目标

1. 支持任意部署/启动方式：`systemctl`、`screen`、`tmux`、`nohup`、`MCSM`、原有 Docker，以及裸进程。
2. 用户可显式选择运行时类型，而不是被动依赖 systemd。
3. 自动检测宿主机上任意方式部署的游戏服务端。
4. 任意运行时之间可切换，支持"一键接管"。
5. 内置服务端下载/安装能力（当前只有"更新"）。

### 1.3 本文额外补全的目标（用户未提但必须做）

| 补全项 | 原因 |
|---|---|
| 启动规格（LaunchSpec）归一化 | 没有它就无法在 systemd 与 screen 之间迁移 |
| 状态机 + 健康检查 | "启动成功"不等于"玩家能连"，必须用端口 + RCON 双判定 |
| 接管（detach/adopt）可逆 | 用户接管错了要能还原，不能吃掉别人的进程归属 |
| 迁移前快照与失败回滚 | 切错运行时会把服搞挂，必须能 30 秒内退回 |
| 宿主 Agent 权限模型与参数白名单 | 面板能执行命令 = 最高危面，必须禁止 shell 拼接 |
| 日志来源抽象 | 不同运行时的日志位置完全不同，否则接管后看不到日志 |
| 端口归属判定（cgroup 证明） | `ps` 看到的 srcds 不一定是 systemd 的，必须靠 cgroup 归因 |
| 服务端安装的磁盘/依赖预检 | L4D2 服务端约 20-30 GB，装到一半失败最难处理 |
| 审计与并发锁 | 两个标签页同时点"切换"必须有唯一胜者 |
| 单机多实例隔离 | 运行时注册表天然就是实例注册表，两个能力必须共用一套模型 |

---

## 2. 现状取证（源码级）

### 2.1 已有能力矩阵

| 能力 | 当前实现 | 位置 | 瓶颈 |
|---|---|---|---|
| 读启动配置 | 解析 systemd unit / Windows bat | `backend/logic/server_startup.go:135-263` | 只认 `WorkingDirectory=` + 单个 `ExecStart=`；多 ExecStart 直接报错 |
| 写启动配置 | 原子替换 unit + 触发重载 | `server_startup.go:205-263`、`manifest/systemd/apply-startup.sh` | 只能写 `/control/l4d2.service` 快照，靠 path unit 落盘 |
| 触发重载 | `L4D2_STARTUP_RELOAD_CMD` 或写 `/control/systemd-reload` | `server_startup.go:577-606` | `/control/systemd-reload` **没有任何 path unit 监听**，实际生效靠 `l4d2-startup.path` 盯 unit 文件本身 —— 命名不一致的遗留缺陷 |
| 重启 | `L4D2_RESTART_BY_RCON` → RCON `_restart`；否则 `L4D2_RESTART_CMD`；兜底 `docker restart ${L4D2_CONTAINER_NAME:-l4d2}` | `backend/controller/restart.go:13-45` | 只有重启，**没有 start / stop / status** |
| 诊断 | 写 `diagnostics-request.json`，轮询 `diagnostics-result.json` | `logic/server_diagnostics.go:152-212` + `manifest/systemd/diagnose-server.sh` | 全部字段围绕 systemd（LoadState/ActiveState/MainPID），非 systemd 运行时无数据 |
| 维护（更新） | 写 `maintenance-request.json`，宿主脚本 `systemctl stop` → steamcmd → `systemctl start` | `logic/server_maintenance.go` + `manifest/systemd/run-maintenance.sh:174-221` | 停机/启动硬编码 `systemctl` |
| 修复 | 写 `repair-request.json`，宿主脚本重启/杀端口 | `logic/server_repair.go` + `manifest/systemd/repair-addons.sh` | 同上 |
| 平台能力 | `HasSystemd() = IsLinux()` | `logic/platform.go:58-60` | **语义错误**：Linux 不等于 systemd（容器、Alpine、WSL 都可能没有） |
| 前端平台分支 | `stores/platform.ts` 二态 `systemd` / `batch` | `frontend/src/stores/platform.ts` | `FALLBACK` 写死 `systemd: true`，接口失败会伪装成 systemd 环境 |
| 服务端安装 | **不存在** | — | 只能"更新"已安装目录 |
| 运行控制路由 | 只有 `POST /restart` | `backend/main.go:139` | 无 `/start`、`/stop`、`/status` |

### 2.2 现有链路图

```
前端 ──POST──> Gin controller ──> logic（拼 JSON 请求）──> /control/xxx-request.json
                                                            │
                                          systemd .path unit 触发（宿主 root）
                                                            ▼
                                                  manifest/systemd/*.sh
                                                （systemctl / steamcmd / journalctl）
                                                            │
                                                            ▼
                                                  /control/xxx-result.json
                                                            │
前端 <──轮询 status── controller <──读结果文件──────────────┘
```

**结论**：现有架构已经是"面板不直接操作宿主机"的桥接模型，这是对的，必须保留。缺的是**桥接协议太窄**（只有 4 种固定请求）和**语义绑死 systemd**。

### 2.3 必须修复的既有缺陷

| 编号 | 缺陷 | 影响 |
|---|---|---|
| D1 | `HasSystemd()` 返回 `IsLinux()` | 容器化 Linux 无 systemd 时，前端仍显示 systemd 专属功能 |
| D2 | `/control/systemd-reload` 无监听者 | 保存启动项后"，等待重载"可能永远等不到 |
| D3 | 无 start/stop/status | 无法覆盖 screen/tmux/nohup 场景 |
| D4 | 维护脚本硬编码 `systemctl stop/start` | 接管非 systemd 服后，更新功能会把服停掉再也起不来 |
| D5 | 诊断字段强依赖 systemd 属性 | 非 systemd 运行时诊断页空白 |
| D6 | 前端 `FALLBACK` 假定 systemd | 平台接口失败时 UI 撒谎 |
| D7 | `manifest/systemd/run-maintenance.sh:180-215` 的更新逻辑是"先单平台 `app_update 222860 validate`，失败且日志含 `Invalid platform` 才补 windows 段" | 匿名账号在 Linux 上单平台拉不全，靠 `Invalid platform` 关键字兜底不可靠（部分残缺场景不打印该关键字）；必须无条件三段式（§9.5.2） |
| D8 | SteamCMD 模板分散两处：`backend/logic/steamcmd_update.go:84-96` 已是三段式，`run-maintenance.sh` 却是"单平台+兜底" | 两处行为不一致，实际执行的是 shell 那份；必须收敛为单一生成器 |
| D9 | `steamcmd_update.go:98` 把脚本写到 `consts.GamePath`（容器内 `/left4dead2`），而 `ScriptPath` 上报为 `<working_dir>/left4dead2/update_l4d2.txt` | 写入位置 ≠ 上报位置 ≠ SteamCMD 目录；用户按上报路径在宿主机找不到脚本，或改到容器内无效路径 |
| D10 | 安装目录默认值有 4 处隐式假设（`consts.GamePath` / `steamcmd_update.go:42-43` / `diagnose-server.sh:154` / `manage.sh:10`） | 四处不一致，用户无法预期默认装到哪；必须收敛为 `ResolveInstallPath()` |

---

## 3. 设计原则

1. **桥接不破**：面板（容器，UID 1000）永远不假设自己有 root；一切特权操作经宿主 Agent。
2. **能力声明而非环境推断**：不猜"是不是 systemd"，而是让每个 provider 声明 `Capabilities`，由 Agent 实测后上报。
3. **规格归一**：所有运行时最终都表达为同一个 `LaunchSpec`，切换运行时 = 导出 `LaunchSpec` + 用另一个 provider 导入。
4. **可逆**：接管可 detach，迁移有快照，失败自动回滚。
5. **无 shell 拼接**：Agent 只接受 `argv` 数组 + 枚举动作，禁止把用户输入拼进 `sh -c`。
6. **单一事实来源**：运行时注册表 `data/runtimes.json` 是唯一真相，环境变量只作为"默认实例"的兼容输入。
7. **向后兼容**：旧接口 `/restart`、`/server-startup/*`、`/server-maintenance/*` 全部保留，内部转发到默认运行时。

---

## 4. 总体架构

```
┌──────────────────────────── 前端 (Vue3) ────────────────────────────┐
│  /runtime 运行时管理页   /runtime/install 服务端安装页               │
│  stores/runtime.ts    services/api.ts（全部 POST）                   │
└───────────────────────────────┬─────────────────────────────────────┘
                                │ Bearer + X-L4D2-Runtime(可选)
┌───────────────────────────────▼─────────────────────────────────────┐
│ 后端 controller/runtime*.go   （参数校验、管理员校验、审计）           │
│ 后端 logic/runtime/*.go       （注册表、Provider 抽象、检测、切换、   │
│                               状态机、健康检查、安装任务）             │
└───────────────────────────────┬─────────────────────────────────────┘
                                │ RuntimeClient（接口，两种实现）
        ┌───────────────────────┴────────────────────────┐
        │                                                │
┌───────▼─────────────┐                        ┌─────────▼──────────────┐
│ FileBridgeClient     │                        │ LocalClient             │
│ 写 /control/runtime/ │                        │ 直接在面板进程内 exec    │
│ requests/<id>.json   │                        │ （Windows 原生部署/开发） │
│ 轮询 results/<id>    │                        │                        │
└───────┬─────────────┘                        └────────────────────────┘
        │ l4d2-runtime.path（宿主 systemd，root）
┌───────▼──────────────────────────────────────────────────────────────┐
│ 宿主机 Agent：manifest/runtime-agent/l4d2_runtime_agent.py（Python3）│
│ 动作：detect / inspect / start / stop / restart / logs / adopt /     │
│       import / export / install / health                             │
│ Provider 插件：systemd / docker / screen / tmux / nohup / bare /     │
│                mcsm / lgsm                                          │
└──────────────────────────────────────────────────────────────────────┘
```

### 4.1 为什么 Agent 用 Python 而不是 Go

- 宿主不需要额外构建产物，Ubuntu 22.04 / Debian 12 自带 `python3`。
- 现有 `diagnose-server.sh`、`run-maintenance.sh`、`apply-startup.sh` 已经在用内联 Python，运维心智一致。
- Agent 只做"进程与 systemd/docker 的薄封装"，没有性能要求。
- 不选 Go 的唯一代价是没有单文件二进制，用 `python3 -m py_compile` 在 CI 里做语法门禁即可。

> Agent 代码必须符合 ruff 规范（用户规则），导入全部置顶。

### 4.2 两种部署形态

| 形态 | Agent 位置 | 能力 |
|---|---|---|
| Docker 面板（默认） | 宿主机 `/opt/l4d2-manager/`，root，systemd path unit 触发 | 全能力 |
| Windows 原生 | 无 Agent，走 `LocalClient`（进程内 exec + tasklist/netstat） | systemd/screen/tmux/nohup/MCSM 不可用；docker/bare 可用 |
| 无 Agent 的 Linux 容器 | `LocalClient` 降级 | 只有 docker（若有 socket）+ RCON |

### 4.3 三条访问通道（关键：面板容器看不到宿主机文件系统）

面板容器只挂载了 `/left4dead2`、`/plugins`、`/data`、docker socket、`/host/proc`（`install.sh:90-97`）。因此对宿主机的任何操作必须走以下三条通道之一：

| 通道 | 能力 | 前置 | 具体用途 |
|---|---|---|---|
| **C1 Docker daemon** | 起一次性容器 + `-v` 任意宿主路径 | docker socket | SteamCMD 下载/安装、文件中转、docker runtime 管理 |
| **C2 宿主 Agent** | 在宿主命名空间执行任意白名单动作 | Agent 已安装（root） | systemd/screen/tmux/nohup 的生命周期、进程扫描、日志、systemd unit 读写 |
| **C3 直接文件系统** | 只能读写已挂载路径 | 已有挂载 | 面板自身 cfg/地图/插件/日志功能（仅限 `/left4dead2`） |

**选择优先级**：需要文件落地到任意宿主路径 → C1；需要控制宿主进程 → C2；读写已挂载的游戏目录 → C3（最快）。

**关键结论**：`docker run -v` 是绕过"容器看不见宿主机"的唯一通用手段，且不需要新增任何 compose 挂载、不需要重启面板。这条设计决定了"安装到任意目录"和"接管宿主机服务端"都无需重新映射目录。

---

## 5. 核心抽象

### 5.1 RuntimeKind

```go
type RuntimeKind string

const (
    RuntimeSystemd RuntimeKind = "systemd"  // systemctl 管理的 system unit
    RuntimeSystemdUser RuntimeKind = "systemd-user" // --user 单元
    RuntimeDocker  RuntimeKind = "docker"   // docker run / docker compose
    RuntimeScreen  RuntimeKind = "screen"
    RuntimeTmux    RuntimeKind = "tmux"
    RuntimeNohup   RuntimeKind = "nohup"    // nohup + 后台 + 重定向日志
    RuntimeBare    RuntimeKind = "bare"     // 无守护，直接 fork 的进程
    RuntimeMCSM    RuntimeKind = "mcsm"     // MCSManager 节点
    RuntimeLGSM    RuntimeKind = "lgsm"     // LinuxGSM 脚本
)
```

### 5.2 LaunchSpec（运行时无关的启动规格）

```go
type LaunchSpec struct {
    WorkingDir   string            `json:"working_dir"`    // 绝对路径，POSIX 或 Windows 风格
    Executable   string            `json:"executable"`     // 绝对路径
    Args         []string          `json:"args"`           // 已分词，不再二次拼接
    Env          map[string]string `json:"env,omitempty"`  // 追加环境变量
    User         string            `json:"user,omitempty"` // 运行用户（agent 用 runuser 切换）
    Port         int               `json:"port"`
    RCONAddress  string            `json:"rcon_address,omitempty"`
    RCONPasswordRef string         `json:"rcon_password_ref,omitempty"` // 密钥引用，不存明文
    AutoRestart  bool              `json:"auto_restart"`   // 崩溃自拉起
    StopSignal   string            `json:"stop_signal,omitempty"` // 默认 TERM
    LogPath      string            `json:"log_path,omitempty"`
}
```

约束：

- `Args` 由后端分词得到（复用前端同款规则：双引号分组、`\` 转义），**禁止** Agent 再做 shell 解析。
- `WorkingDir` / `Executable` 必须绝对且规范化（沿用 `normalizeSystemdWorkingDirectory` 的校验，改名 `normalizeLaunchPath`）。
- `User` 为空表示沿用 Agent 属主；非空时 Agent 用 `runuser -u <user> --` 启动，且 `<user>` 必须在白名单正则 `^[a-z_][a-z0-9_-]{0,31}$` 内。

### 5.3 RuntimeDescriptor（注册表条目）

```go
type RuntimeDescriptor struct {
    ID          string            `json:"id"`            // 稳定 slug：^[a-z0-9][a-z0-9-]{0,31}$
    Name        string            `json:"name"`
    Kind        RuntimeKind       `json:"kind"`
    Enabled     bool              `json:"enabled"`
    Adopted     bool              `json:"adopted"`        // 是否由面板接管
    Managed     bool              `json:"managed"`        // 面板是否拥有写权限（可改配置/重启）
    Target      RuntimeTarget     `json:"target"`         // 各 kind 专属句柄
    Spec        LaunchSpec        `json:"spec"`
    GamePath    string            `json:"game_path"`      // 直接映射 consts.GamePath 的实例化版本
    Labels      map[string]string `json:"labels,omitempty"`
    CreatedAt   string            `json:"created_at"`
    UpdatedAt   string            `json:"updated_at"`
    LastHealth  *HealthSnapshot   `json:"last_health,omitempty"`
    Origin      string            `json:"origin"`         // manual | detected | migrated | installed
}

type RuntimeTarget struct {
    UnitName      string `json:"unit_name,omitempty"`       // systemd
    UnitPath      string `json:"unit_path,omitempty"`       // 快照路径
    Scope         string `json:"scope,omitempty"`           // system | user
    ContainerName string `json:"container_name,omitempty"`  // docker
    ComposeFile   string `json:"compose_file,omitempty"`
    ComposeService string `json:"compose_service,omitempty"`
    SessionName   string `json:"session_name,omitempty"`    // screen / tmux
    PidFile       string `json:"pid_file,omitempty"`        // nohup / bare
    MCSMNodeURL   string `json:"mcsm_node_url,omitempty"`
    MCSMInstance  string `json:"mcsm_instance,omitempty"`
    LGSMScript    string `json:"lgsm_script,omitempty"`
}
```

### 5.4 Provider 接口（Agent 侧实现，后端侧对应同名能力声明）

```go
type RuntimeProvider interface {
    Kind() RuntimeKind
    Capabilities() ProviderCapabilities

    // 检测与视察
    Probe(target RuntimeTarget, spec LaunchSpec) (ProbeResult, error)
    Inspect(target RuntimeTarget) (InspectorResult, error)
    Logs(target RuntimeTarget, tail int) (LogResult, error)

    // 生命周期
    Start(target RuntimeTarget, spec LaunchSpec) error
    Stop(target RuntimeTarget, spec LaunchSpec) error
    Restart(target RuntimeTarget, spec LaunchSpec) error

    // 规格导入/导出
    Export(target RuntimeTarget) (LaunchSpec, error)
    Import(target RuntimeTarget, spec LaunchSpec) (RuntimeTarget, error)

    // 接管
    Adopt(candidate DiscoveryCandidate) (RuntimeDescriptor, error)
    Detach(descriptor RuntimeDescriptor) error
}

type ProviderCapabilities struct {
    CanStart     bool `json:"can_start"`
    CanStop      bool `json:"can_stop"`
    CanRestart   bool `json:"can_restart"`
    CanWriteSpec bool `json:"can_write_spec"`   // 能否把 panel 的修改写回
    CanLogs      bool `json:"can_logs"`
    CanAdopt     bool `json:"can_adopt"`
    NeedsRoot    bool `json:"needs_root"`
    Persistent   bool `json:"persistent"`       // 宿主机重启后是否自恢复
}
```

### 5.5 能力矩阵

| Kind | Start | Stop | Restart | 写规格 | 日志源 | 接管 | 开机自启 |
|---|---|---|---|---|---|---|---|
| systemd | ✅ | ✅ | ✅ | ✅ unit | `journalctl -u` | ✅ | ✅ |
| systemd-user | ✅ | ✅ | ✅ | ✅ | `journalctl --user-unit` | ✅ | ✅（需 linger） |
| docker | ✅ `docker start` | ✅ `docker stop` | ✅ | ⚠️ 需重建容器 | `docker logs` | ✅ | ✅ restart policy |
| screen | ✅ | ✅ `screen -S .. -X quit` | ✅ | ⚠️ 仅重写启动脚本 | `screen -L` hardcopy / 进程 stdout 重定向 | ✅ | ❌ 需 cron/@reboot |
| tmux | ✅ `new-session -d` | ✅ `kill-session` | ✅ | ⚠️ 同上 | `pipe-pane` 日志文件 | ✅ | ❌ 需 cron/@reboot |
| nohup | ✅ | ✅ `kill -TERM` | ✅ | ✅ 重写 wrapper 脚本 | 重定向文件 | ✅ | ❌ 需 cron/@reboot |
| bare | ⚠️ 只能 stop | ⚠️ | ⚠️ | ❌ | 无（建议接管后迁移） | ✅ | ❌ |
| mcsm | ✅ API | ✅ API | ✅ API | ✅ 实例配置 | 实例日志 API | ✅（需节点密钥） | ✅ |
| lgsm | ✅ `./l4d2server start` | ✅ | ✅ | ✅ `_default.cfg` | `log/server/` | ✅ | ✅（cron） |

> `Persistent=false` 的运行时（screen/tmux/nohup）在接管时**必须**提示"宿主机重启后不会自恢复"，并提供一键迁移到 systemd / docker 的入口。

---

## 6. 宿主机 Agent

### 6.1 目录布局

```
/opt/l4d2-manager/
├── runtime-agent/
│   ├── l4d2_runtime_agent.py          # 入口（ruff 规范）
│   ├── providers/                      # 各 provider 实现
│   │   ├── systemd.py  docker.py  screen.py
│   │   ├── tmux.py     nohup.py   bare.py
│   │   └── mcsm.py     lgsm.py
│   └── lib/{jobs.py,exec.py,paths.py,health.py}
├── control/
│   ├── runtime/
│   │   ├── requests/<job_id>.json     # 面板写
│   │   ├── results/<job_id>.json      # Agent 写
│   │   ├── staging/<job_id>/          # 快照、临时 unit
│   │   └── .lock                      # flock 全局互斥（安装/迁移）
│   ├── l4d2.service                   # 兼容旧链路
│   └── *.json                         # 旧链路请求/结果（保留）
```

### 6.2 触发方式

沿用现有 path unit 模式，新增：

```
manifest/systemd/l4d2-runtime.path
  PathExistsGlob=/opt/l4d2-manager/control/runtime/requests/*.json
manifest/systemd/l4d2-runtime.service
  Type=oneshot
  ExecStart=/usr/bin/python3 /opt/l4d2-manager/runtime-agent/l4d2_runtime_agent.py --drain
```

`--drain` 语义：扫描 `requests/`，逐个执行并写 `results/`，然后删除 request。单个 job 失败不阻塞后续 job（但不是并发执行，用 `.lock` 保护）。

> 高频轮询类动作（status/health）不进队列，用 `l4d2-runtime-probe.path`？**不**。健康检查直接在 Agent 的 `inspect` 动作里跑，由面板按需触发（前端 5 秒轮询一次 job），避免引入常驻守护进程。若后续延迟不可接受，再升级为 Unix socket 常驻模式（见 §13 阶段 4）。

### 6.3 Job 协议

请求：

```json
{
  "job_id": "uuid-v4",
  "action": "inspect",
  "runtime_id": "l4d2-main",
  "kind": "systemd",
  "target": { "unit_name": "l4d2.service" },
  "spec": { "working_dir": "/home/ubuntu/steamcmd/l4d2", "executable": "...", "args": ["-game","left4dead2"], "port": 27015 },
  "options": { "tail": 200, "wait_seconds": 20 },
  "requested_at": "2026-11-17T10:00:00Z",
  "requester": "panel"
}
```

结果：

```json
{
  "job_id": "uuid-v4",
  "action": "inspect",
  "status": "success",
  "message": "",
  "data": { "...": "各 action 专属结构" },
  "started_at": "...",
  "finished_at": "...",
  "agent_version": "1.0.0"
}
```

`status` 取值：`success` / `error` / `unsupported` / `timeout` / `denied`。

### 6.4 安全校验（Agent 侧硬性要求）

1. `action` 必须在枚举白名单内，否则 `denied`。
2. `kind` 必须合法，且与 `target` 字段组合匹配（例如 `systemd` 必须有合法 `unit_name`）。
3. `unit_name` 必须匹配 `^[A-Za-z0-9_.@:-]+\.service$`。
4. `container_name` / `session_name` 必须匹配 `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`。
5. 所有路径必须是规范化绝对路径、不含 `..`、不含 `\n\r\0`；写入目标必须在允许根目录内（`/etc/systemd/system`、`/opt/l4d2-manager/control`、`/home/*`、`/data/*`，可配置）。
6. `args` 每个元素不得包含 `\n\r\0`；长度上限 4096；元素数量上限 256。
7. 执行一律 `subprocess.run([...], shell=False)`；**禁止** `shell=True`。
8. 所有文件写入使用"临时文件 + fsync + `os.replace`"，权限 0600/0644 显式指定。
9. Agent 以 root 运行时，`spec.user` 决定最终进程属主；未指定则 `runuser -u` 到 `L4D2_RUNTIME_DEFAULT_USER`（默认 `ubuntu`）。
10. 每个 job 记录执行前的关键状态快照到 `staging/<job_id>/before.json`，便于回滚与审计。

### 6.5 幂等与并发

- 同一 `job_id` 重复投递：Agent 若发现 `results/<job_id>.json` 已存在则直接返回旧结果，不重复执行。
- 全局 `flock`（`runtime/.lock`）保护 `install`、`switch`、`import`、`adopt` 等破坏性动作；`inspect` / `logs` 不持锁。
- 后端侧对同一 `runtime_id` 的操作做进程内串行化（`map[string]*sync.Mutex`），并对"10 秒内重复的等价请求"做去重返回。

---

## 7. 自动检测

### 7.1 信号源

| 信号源 | 采集方式 | 能识别 |
|---|---|---|
| systemd | `systemctl list-units --type=service --all --no-legend --plain`，再对候选 `systemctl show -p ExecStart,MainPID,FragmentPath` | unit 名、ExecStart、主 PID、unit 路径 |
| 进程树 | `ps -eo pid,ppid,user,lstart,args --no-headers` + 过滤 `srcds`/`l4d2`/`222860` | 裸进程、shell wrapper、父进程 |
| cgroup 归因 | `/proc/<pid>/cgroup`：`/system.slice/l4d2.service` → systemd；`/docker/<id>` → 容器；`/user.slice/.../session-N.scope` → 登录会话 | **唯一可靠的归属证明** |
| docker | `docker ps --no-trunc --format '{{json .}}'` + `docker inspect` | 容器名、镜像、Cmd、端口映射、restart policy |
| compose | 容器 label `com.docker.compose.project` / `service` / `working_dir` | compose 文件与服务名 |
| screen | `screen -ls` 解析 `pid.name (state)` + `screen -S <name> -Q windows`（不可用时回退进程树 ppid 链） | session 名、窗口 |
| tmux | `tmux list-sessions -F '#{session_name}|#{session_windows}|#{pane_pid}'` + `tmux list-panes -a -F ...` | session、pane pid |
| nohup | 进程 ppid=1、无 cgroup 单元、stdout 指向文件（`/proc/<pid>/fd/1`） | 日志文件路径 |
| MCSM | 进程名 `node` + cwd 命中 `mcsm`/`MCSManager`；或探测 `127.0.0.1:23333` 的 `/api/instance/list` | 实例列表（需 API Key） |
| LinuxGSM | 存在 `~/l4d2server` 或 `/home/*/l4d2server` 且可执行 | LGSM 脚本路径 |
| Steam 库 | `steamapps/appmanifest_222860.acf` 的 `installdir` | 未运行但已安装的服务端目录（供"导入为运行时"用） |

### 7.2 融合算法

```
候选集合 = systemd候选 ∪ docker候选 ∪ screen/tmux会话候选 ∪ ps裸进程候选 ∪ mcsm候选 ∪ lgsm候选
对每个候选：
  - 用 PID 集合做去重键（同一进程只保留证据最全的一条）
  - 归属判定优先级：cgroup > screen/tmux session pid > docker inspect > ps ppid 链
  - 置信度 = 基础分(0.4) + 归属证据(+0.3) + 命令行含 srcds/l4d2(+0.2) + 端口命中(+0.1)
  - 端口：从 args 解析 +hostport/+port/-port；缺失则用 ss -lntup 反查 PID→端口
  - 状态：running / stopped / unknown（已安装未运行）
```

### 7.3 输出结构

```go
type DiscoveryCandidate struct {
    Key         string            `json:"key"`          // 稳定指纹：kind + 主标识 + game_path 哈希
    Kind        RuntimeKind       `json:"kind"`
    Name        string            `json:"name"`
    Confidence  float64           `json:"confidence"`
    Running     bool              `json:"running"`
    PID         int               `json:"pid,omitempty"`
    User        string            `json:"user,omitempty"`
    Spec        LaunchSpec        `json:"spec"`
    Target      RuntimeTarget     `json:"target"`
    GamePath    string            `json:"game_path"`
    Port        int               `json:"port"`
    Persistence string            `json:"persistence"`  // persistent | ephemeral
    Evidence    []string          `json:"evidence"`     // 人类可读证据
    Adoptable   bool              `json:"adoptable"`
    Reason      string            `json:"reason,omitempty"` // 不可接管原因
    AlreadyID   string            `json:"already_runtime_id,omitempty"`
}
```

前端必须把 `evidence` 原样展示 —— 用户要能判断"这条是不是我的服"，而不是盲信置信度。

### 7.4 检测的边界

- 检测只在**有一次 Agent 权限**时有效；纯 Docker 面板只能看到 docker 容器与 RCON。
- `screen -ls` 只能看到当前用户（root）的 session；其他用户的 session 需要 `runuser` 逐个尝试（仅在 `L4D2_RUNTIME_SCREEN_USERS` 配置后启用）。
- tmux 同理（`tmux -L <socket>` 多 server 场景只在配置了 socket 名后探测）。
- MCSM 检测默认只做"目录/端口探测"，实际读取实例列表必须用户提供 API Key，且 Key 只存 Agent 侧 `0600` 文件，不回传前端。

---

## 8. 接管与切换

### 8.1 必须先区分的两种"切换"

| 语义 | 名称 | 是否停机 | 说明 |
|---|---|---|---|
| 面板从"不管"→"管" | **接管 adopt** | 不停机 | 只登记描述符，不改变进程归属。`detach` 可撤销 |
| 启动方式 A → B | **迁移 migrate** | 需要停机（或蓝绿） | 真正改变进程由谁拉起。必须有快照与回滚 |

UI 上必须分成两个入口，文案不能混用"切换"。

### 8.2 接管流程（adopt）

```
1. 用户在检测列表点「接管」
2. 后端 POST /runtime/adopt {candidate_key, runtime_id, name}
3. Agent adopt：
   a. 重新采集该候选的实时状态（防止列表过期）
   b. 校验：端口唯一、game_path 未被其他已启用运行时占用
   c. 生成描述符：adopted=true, managed=<provider 是否可写>
   d. systemd  → 记录 unit_name/unit_path，可选把 unit 快照拉到 /control/runtimes/<id>/
      docker   → 记录 container_name/compose 信息
      screen   → 记录 session_name + 从 /proc 抓完整 cmdline 存为 spec
      nohup/bare → 记录 pid_file（面板生成），并把 /proc/<pid>/cmdline 存为 spec
   e. 写入 data/runtimes.json（面板侧），Agent 侧写 /control/runtime/runtimes/<id>.json 备份
4. 返回描述符，前端刷新
```

接管后的行为差异：

- `managed=true`：面板可以改启动参数、重启、更新、诊断。
- `managed=false`（bare 进程无启动脚本）：只能 stop/诊断，"保存启动项"按钮禁用并提示迁移。

### 8.3 迁移流程（migrate / switch）

```
POST /runtime/migrate/plan  {source_id, target_kind, target_options}
  → 返回 MigrationPlan（前端必须展示并二次确认）
     {
       "steps": [
         "预检：端口 27015 可用 / 磁盘剩余 ≥ 5GB / 目标用户 ubuntu 可写游戏目录",
         "导出源规格：/home/ubuntu/steamcmd/l4d2/srcds_run -game left4dead2 ...",
         "生成目标配置：/etc/systemd/system/l4d2-main.service",
         "停机源运行时（systemctl stop l4d2.service）",
         "启动目标运行时（systemctl start l4d2-main.service）",
         "健康检查：端口监听 + RCON status"
       ],
       "risks": ["源 unit 将被 disable", "停机窗口约 15-40 秒"],
       "blockers": []
     }

POST /runtime/migrate/apply {plan_token}
  → 创建 job（Agent 执行），返回 job_id
前端轮询 GET(实际 POST) /runtime/jobs/{job_id}

Agent 执行序列（全程持 flock）：
  1. snapshot: 备份源配置到 staging/<job_id>/（unit 文件 / compose 文件 / session cmdline / pidfile）
  2. 生成目标配置（写入 staging，不落盘）
  3. 预检二次确认（端口、磁盘、权限）
  4. 停源 → 等待端口释放（最长 30s，超时则 SIGTERM→SIGKILL）
  5. 落盘目标配置并启动
  6. 健康检查（最多 90s）：
     - 端口监听
     - 进程存在
     - RCON 可连（若配置了密码）→ `status` 返回有效行
  7. 成功：更新 runtimes.json，源标记 archived（保留 7 天可回滚）
     失败：自动回滚（恢复源配置并启动源运行时），结果 status=error 且带 rollback=success
```

### 8.4 状态机

```
            ┌──────────┐
            │ unknown  │
            └────┬─────┘
       inspect   │
      ┌──────────┼──────────┐
      ▼          ▼          ▼
 ┌─────────┐ ┌────────┐ ┌─────────┐
 │ stopped │ │starting│ │running  │
 └────┬────┘ └───┬────┘ └────┬────┘
      │ start    │ health ok  │ 健康检查失败
      └─────────►│◄───────────┘
                 │            ▼
                 │      ┌──────────┐
                 │      │degraded  │（端口通但 RCON 不通 / 端口被占）
                 │      └────┬─────┘
                 ▼           │ 连续 3 次失败
            ┌─────────┐      ▼
            │ crashed │◄─────┘
            └─────────┘
```

规则：

- 状态由**健康检查结果**驱动，不由"命令返回码"驱动。`systemctl start` 返回 0 只代表 systemd 接受请求。
- `degraded` 与 `crashed` 必须携带 `reason` 与 `last_log_tail`。
- 每次状态变更写入 `data/runtime_events.db`（SQLite，滚动保留 7 天），用于"什么时候挂的"回溯。

### 8.5 健康检查

```go
type HealthSnapshot struct {
    CheckedAt   string `json:"checked_at"`
    State       string `json:"state"`       // stopped|starting|running|degraded|crashed|unknown
    PID         int    `json:"pid,omitempty"`
    PortListening bool `json:"port_listening"`
    PortOwners  []PortOwner `json:"port_owners,omitempty"`
    RCONReachable bool `json:"rcon_reachable"`
    RCONError   string `json:"rcon_error,omitempty"`
    LogTail     string `json:"log_tail,omitempty"`
    Reason      string `json:"reason,omitempty"`
}
```

判定顺序：进程存在 → 端口监听且 owner 是游戏进程 → RCON `status` 返回含 `hostname` 或 `players` 的行。三层全通才是 `running`。

---

## 9. 内置服务端下载 / 安装

### 9.1 与"更新"的区别

| 维度 | 更新（已有） | 安装（新增） |
|---|---|---|
| 前置 | 目录已存在 `appmanifest_222860.acf` | 目录为空或不含 `srcds_run` |
| SteamCMD 方式 | **三段式 runscript**（与安装同一模板，见 §9.5.2） | **三段式 runscript**（同上，仅 `force_install_dir` 指向新目录） |
| 停机 | 必须停服 | 无需停服（目标目录无运行中进程） |
| 后续 | 启动服务 | 生成 cfg、生成启动配置、可选创建运行时、可选写 systemd unit |
| 失败处理 | 已停服必须重新启动 | 目录隔离，失败可整目录回滚 |

> 更新与安装的**唯一区别**是「是否需要先停服」和「装完之后的收尾动作」。SteamCMD 的调用方式必须完全一致，禁止更新走单平台直拉、安装走三段式这种分裂实现。

### 9.2 安装向导（前端 5 步）

1. **环境预检**：SteamCMD 路径（自动探测 `~/steamcmd/steamcmd.sh`、`/home/*/steamcmd`）、目标目录、磁盘剩余（**要求 ≥ 45 GB**，三段式会同时保留 Windows + Linux 内容）、目标用户、端口可用性、32 位运行库。
2. **安装参数**：app id 固定 222860（只读展示）、目标平台（`linux` / `windows`，默认 linux，仅影响最后一段 validate 的平台）、目录名。**不提供「单平台安装」选项**——匿名拉取必须三段式。
3. **执行**：后台任务，轮询展示 SteamCMD 输出尾部 + 阶段（建立 depot / 补齐 Windows 内容 / Linux 校验 / 完成）。
4. **初始化配置**：选择 tick（30/60/100/128）→ 从 `manifest/docker/cfg/server.cfg.<tick>` 模板写入 `left4dead2/cfg/server.cfg`；写 `hostname`、`rcon_password`、端口。
5. **创建运行时**：选择 systemd / docker / screen / nohup / 暂不创建 → 生成对应配置并启动 → 健康检查。

### 9.3 安装任务模型

```go
type InstallJob struct {
    ID          string   `json:"id"`
    Status      string   `json:"status"`       // pending|running|success|error|canceled
    Phase       string   `json:"phase"`        // precheck|download|validate|configure|runtime|done
    SteamDir    string   `json:"steam_dir"`
    InstallDir  string   `json:"install_dir"`
    Platform    string   `json:"platform"`
    LogTail     []string `json:"log_tail"`
    Progress    float64  `json:"progress"`     // 0-100，按 SteamCMD 输出行数与阶段估算
    StartedAt   string   `json:"started_at"`
    FinishedAt  string   `json:"finished_at"`
    Error       string   `json:"error,omitempty"`
}
```

约束：

- 与更新共用同一个 `flock`（`/run/lock/l4d2-manager-steamcmd.lock`），SteamCMD 不允许并发。
- 安装目录必须为空或不含 `srcds_run`；否则拒绝并提示走"更新"。
- 安装完成后写 `data/install-history.json`（目录、build id、时间、操作者），供"这是新装的还是老服"判定。
- 取消：写 `install-cancel` 标记，Agent 在下一个输出行检查并 `SIGTERM` SteamCMD 子进程，保留半成品目录并提示清理。

### 9.4 依赖预检（Linux）

32 位运行库缺失是 L4D2 服务端最常见的启动失败原因。**安装前**能查的：

- `dpkg --print-architecture` 含 `i386`（Ubuntu 已启用 `multiarch`），或 `/lib/ld-linux.so.2` 存在。
- 磁盘剩余 ≥ 45 GB。
- 目标用户对安装目录父级可写。
- Docker ≥ 29.3 时，游戏容器必须有 `security_opt: seccomp:unconfined`（32 位 srcds 兼容），否则启动即失败。

**安装后**才能查的（装完立即校验，失败即终止，不让用户拉起一个必然崩的服）：

- `ldd left4dead2/srcds_linux` 无 `not found`。
- `left4dead2/srcds_linux` 是 32 位 ELF（`file` 输出含 `ELF 32-bit`）。
- `left4dead2/srcds_run` 具备可执行位。

以上任一项不通过，向导必须给出可复制的修复命令（例如 `dpkg --add-architecture i386 && apt-get update && apt-get install -y lib32gcc-s1 lib32stdc++6`），而不是只报"预检失败"。

### 9.5 三种安装来源（已确认全支持）

统一入口 `POST /server-install/start`，`source` 字段决定分支：

```go
type InstallSource string

const (
    InstallSourceSteamCMD InstallSource = "steamcmd" // 在线下载
    InstallSourceArchive  InstallSource = "archive"  // 上传压缩包离线安装
    InstallSourceCopy     InstallSource = "copy"     // 从已有目录复制
)
```

#### 9.5.1 统一准入规则（三种来源共用）

1. `install_dir` 由用户输入，**相对路径与绝对路径都接受**（解析规则见 §9.5.6）。
2. 解析后的真实绝对路径必须在白名单根目录内（`RuntimeSettings.AllowedRoots`）。
3. `install_dir` 必须满足以下之一才允许安装：不存在 / 是空目录 / 不含 `left4dead2/srcds_run` 与 `left4dead2/srcds_linux`。
4. 安装期间对该目录加独占锁（文件锁 `<install_dir>/.l4d2-install.lock`），防止两个任务写同一目录。
5. 安装完成后写入 `data/install_history.json`：`source`、`install_dir`（用户原始输入）、`resolved_dir`（解析后绝对路径）、`build_id`、`finished_at`、`operator`。
6. 任何来源失败后都保留现场目录 + `install.log`，由用户决定「重试 / 清理」，不自动删除。

#### 9.5.2 SteamCMD 在线安装（必须三段式 runscript）

> **硬约束（用户实测确认）**：L4D2 专用服务端 `app 222860` 在 **Linux 主机上用匿名账号无法单平台直拉**。
> 直接在命令行写 `+app_update 222860 validate` 会拿不到完整内容（常见表现是 `Invalid platform` 或文件残缺、`srcds_linux` 缺失）。
> **必须**按「linux 先拉 → windows 再拉 → linux 最后 validate」三段顺序执行，并且必须写成 runscript 脚本文件用 `+runscript` 跑。

标准脚本模板（`force_install_dir` 用**用户输入并解析后的路径**，其余固定）：

```text
force_install_dir <resolved_install_dir>
login anonymous
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType windows
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860 validate
quit
```

执行方式（**不需要宿主机安装 SteamCMD**，用已有游戏镜像起一次性容器，把目标目录 bind mount 进容器）：

```text
docker run --rm \
  -v <resolved_install_dir>:/target \
  -v <resolved_install_dir>/update_l4d2.txt:/root/steamcmd/update_l4d2.txt:ro \
  --user <uid>:<gid> \
  <l4d2-pure 游戏镜像> \
  /root/steamcmd/steamcmd.sh +runscript /root/steamcmd/update_l4d2.txt
```

脚本内容里的 `force_install_dir` 写**容器内路径** `/target`：

```text
force_install_dir /target
login anonymous
...
```

理由与依据：

- 游戏镜像 `manifest/docker/l4d2.Dockerfile:36-39` 已经把 SteamCMD 装进 `/root/steamcmd`，宿主机**无需**再装一份。
- 容器内的 `/target` 通过 bind mount 直接指向宿主机目标目录，SteamCMD 写入即落盘到宿主机，**不存在"下载完再复制"这一步**，也不存在复制 30 GB 的耗时与磁盘双倍占用。
- 面板容器已有 `/var/run/docker.sock`（`install.sh:94`、`manage.sh:380`），具备起一次性容器的能力。
- 该方式对**任意宿主机路径**都成立，因为 `-v` 的源路径由 docker daemon（宿主 root）解析，不受面板容器自身挂载限制。

降级路径（按可用性自动选择，见 §4.3）：

| 优先级 | 执行方式 | 前置条件 | 适用 |
|---|---|---|---|
| 1 | `docker run --rm` 一次性容器（上面这条） | 面板有 docker socket + 本地已有游戏镜像 | **默认，覆盖全部部署形态** |
| 2 | 宿主 Agent 直接执行 `steamcmd.sh` | Agent 可用 + 宿主机已装 SteamCMD | 老部署（宿主已有 `/home/ubuntu/steamcmd`） |
| 3 | 面板进程内直接执行 | 面板镜像内置 SteamCMD（当前 alpine 镜像**没有**） | 仅开发机 / Windows 原生 |

要点：

1. **脚本落盘位置**：`<resolved_install_dir>/update_l4d2.txt`（落在目标目录里，宿主机可直接查看、便于排障），权限 `0644`，属主为目标用户。
   > 现有 `steamcmd_update.go` 把脚本写到 `consts.GamePath`（容器内 `/left4dead2`），却把 `ScriptPath` 报告成 `<working_dir>/left4dead2/update_l4d2.txt`——写入位置与上报位置**不一致**，且都不等于 SteamCMD 目录。这是 D9，必须在阶段 0 一并修正。
2. **三段缺一不可**：第一段负责建立 depot 与目录骨架，第二段补齐 Windows 侧共享内容（匿名账号在 Linux 段的可见内容不完整），第三段 `validate` 才把 Linux 可执行文件与校验和补全。
3. **不要用「先单平台、失败再 workaround」的策略**：现有 `manifest/systemd/run-maintenance.sh:180-215` 是"先 `+app_update 222860 validate`，失败且日志含 `Invalid platform` 才补 windows 段"，这段逻辑要**删除并改为无条件三段式**，否则每次更新都会先浪费一轮失败等待，且部分残缺场景不会触发 `Invalid platform` 关键字、直接漏掉。
4. **安装与更新共用同一个脚本生成器**：后端 `BuildSteamCMDRunScript(installDir string) string` 单一实现，`GenerateSteamCMDUpdateScript`（更新）与 `/server-install`（安装）都调用它，禁止两处各写一份模板造成漂移。
5. **平台差异**：
   - 目标平台 `linux`（默认）：最后一段为 `@sSteamCmdForcePlatformType linux` + `app_update 222860 validate`。
   - 目标平台 `windows`：前两段保持 `linux → windows` 不变，仅把最后一段的 `ForcePlatformType` 改为 `windows`。
   - 不允许出现「只跑一段」的分支。
6. **进度识别**（按日志行推进，不依赖 SteamCMD 退出码猜测）：
   - `Update state (0x61) downloading` / `Downloading item` → `phase=download`
   - `Update state (0x5) verifying install` / `Validating` → `phase=validate`
   - `Success! App '222860' fully installed.` → `phase=done`
   - 出现第二次 `@sSteamCmdForcePlatformType windows` 相关的下载段落 → 前端进度文案切换为「补齐 Windows 共享内容」
7. **重试与幂等**：`app_update` 本身幂等，失败后整段 runscript 可原样重跑；重试次数上限 3，退避 10/30/90 秒，全部失败才置 `error`。
8. **取消**：写 `install-cancel` 标记，Agent 每读到一行输出检查一次，命中则 `SIGTERM` 整个 SteamCMD 进程组（`kill -TERM -<pgid>`，SteamCMD 会派生 `steamcmd` 子进程），状态 `canceled`；runscript 文本文件保留不影响下次执行。
9. **安装完成校验**（任一缺失即判失败，不能只看 SteamCMD 退出码）：
   - `left4dead2/srcds_run`（shell wrapper）
   - `left4dead2/srcds_linux`（真实可执行文件，32 位 ELF）
   - `left4dead2/bin/`（引擎库目录非空）
   - `left4dead2/gameinfo.txt`
   - `steamapps/appmanifest_222860.acf`（含 `buildid`，写入 `install_history.json`）
10. **磁盘余量**：三段式会产生 Windows + Linux 双份内容，峰值占用显著高于单平台，预检门槛从 30 GB 提高到 **45 GB**。

#### 9.5.3 上传压缩包离线安装

- 复用现有分片上传管线（5 MiB 分片、`uploadId` 续传、2 GiB 上限），新增前缀 `/server-install/archive/init|chunk|merge`，产物落在 `data/tmp/install-<jobId>/`。
- 支持 `.zip` / `.tar.gz` / `.tar.zst`（后两者需要宿主有 `tar`；`.zst` 缺 `zstd` 时明确报错，不静默降级）。
- 解压安全（必须，且不能复用地图上传的宽松逻辑）：
  - 拒绝绝对路径、`..`、符号链接指向根外、硬链接、设备文件。
  - 单文件上限 4 GiB，总解压上限 60 GiB，条目数上限 200000（防 zip bomb）。
  - 解压到 `install_dir` 的同级临时目录 `<install_dir>.partial-<jobId>`，校验通过后 `rename` 到目标，保证原子性。
- 结构校验（解压后必须命中其一）：
  - `<root>/left4dead2/srcds_run`（标准结构）
  - `<root>/srcds_run` + `<root>/left4dead2/`（部分打包工具会压平）
  - 命中第二形态时自动上移一层，保证最终结构统一。
- 完整性校验：`left4dead2/bin/` 存在、`left4dead2/gameinfo.txt` 可读、`appmanifest_222860.acf` 存在（无则标注"build id 未知"）。

#### 9.5.4 从已有目录复制

- 参数：`source_dir`、`install_dir`、`new_port`。
- 前置校验：
  1. `source_dir` 命中 §9.5.1 的结构校验；
  2. 反查 `/proc/*/cwd` 与 `/proc/*/exe`，确认**没有运行中进程**以 `source_dir` 为工作目录（避免复制出脏数据与锁文件）；
  3. `install_dir` 不与 `source_dir` 相同或互为父子目录；
  4. `new_port` 未被占用，且不与已注册运行时冲突。
- 复制：`cp -a --reflink=auto <source_dir>/. <install_dir>/`（`reflink` 支持则秒级，不支持自动回退普通复制）。
- 复制后的收尾（必须全部执行，否则两个服会互相干扰）：
  1. 删除 `left4dead2/cfg/server.cfg` 中的 `rcon_password` 行，由向导重新写入新密码；
  2. 重写 `hostport`（写回启动规格或 systemd unit 的 `+hostport`）；
  3. 清空 `left4dead2/addons/sourcemod/logs/*`（避免日志串台）；
  4. 复制 SQLite 类插件数据（如 `sourcemod/data/sqlite/*.sq3`）时保留，但提示用户"新实例与旧实例共享同一份玩家数据，需要时请自行清理"；
  5. 生成新的 `appmanifest_222860.acf` 副本保持原 build id（后续可走"更新"）。

#### 9.5.5 安装向导 UI 分支

第 1 步先选来源（三张卡片：在线下载 / 上传压缩包 / 复制已有目录），后续步骤按来源动态变化：

| 步骤 | steamcmd | archive | copy |
|---|---|---|---|
| 环境预检 | SteamCMD + 磁盘 + 32 位库 | 磁盘 + 目标目录 | 源目录校验 + 运行中检测 + 端口 |
| 参数 | 安装目录（相对/绝对）+ 目标平台 | 压缩包上传 + 安装目录 | 源目录 + 安装目录 + 新端口 / 新 RCON 密码 |
| 执行 | SteamCMD 日志流（三段阶段） | 上传进度 + 解压进度 | 复制进度（按字节） |
| 初始化配置 | ✔ | ✔ | ✔（强制） |
| 创建运行时 | ✔ | ✔ | ✔ |

#### 9.5.6 安装目录：默认值、相对/绝对路径、以及"容器看不见宿主机"的根本问题

**先明确一件事：面板跑在容器里，它看不到宿主机的任意目录。**

当前 compose 只挂了这些（`install.sh:90-97`、`manage.sh:376-383`）：

| 挂载 | 容器内路径 | 用途 |
|---|---|---|
| `l4d2-data` 具名卷 | `/left4dead2` | 游戏目录（只有这一份） |
| `l4d2-plugins` | `/plugins` | 插件 |
| `l4d2-manager-data` | `/data` | 面板自身数据 |
| `/var/run/docker.sock` | 同名 | 能操作 docker |
| `/proc` | `/host/proc` | 只读宿主进程 |

所以：

- 面板进程内 `os.Stat("/home/ubuntu/steamcmd/l4d2")` **永远失败**，因为该路径不在容器命名空间里。
- 但 `docker run -v /home/ubuntu/xxx:/target` **可以成功**，因为 `-v` 的源路径由宿主机 docker daemon 解析，绕过了面板容器的挂载限制。

这是整个方案的关键分界线：

| 操作类型 | 谁执行 | 能否访问任意宿主路径 |
|---|---|---|
| 下载安装服务端（SteamCMD） | `docker run --rm` 一次性容器 | 能（`-v` 由 daemon 解析） |
| 扫描宿主进程 / 端口 | 宿主 Agent（root，在宿主命名空间） | 能 |
| 读日志 / 改 systemd unit | 宿主 Agent | 能 |
| 面板直接读写游戏文件（cfg、地图、插件） | 面板进程内 | 否，只能访问 `/left4dead2` 这一个挂载点 |

**默认值（关键）**：安装向导的「安装目录」输入框**必须预填**，用户可直接下一步，不需要自己拼路径。默认值按以下优先级取：

| 优先级 | 来源 | 示例 |
|---|---|---|
| 1 | 当前默认运行时（`RuntimeSettings.DefaultRuntimeID`）的 `spec.working_dir` 的**同级新目录** | 现有 `/home/ubuntu/steamcmd/l4d2` → 默认 `/home/ubuntu/steamcmd/l4d2-new` |
| 2 | `L4D2_GAME_PATH` 对应的**宿主机真实路径** | `/left4dead2` → `/data/l4d2/left4dead2` |
| 3 | 已探测到的 SteamCMD 目录下 + `l4d2` | SteamCMD 在 `/home/ubuntu/steamcmd` → `/home/ubuntu/steamcmd/l4d2` |
| 4 | 兜底 | `/data/l4d2/l4d2-new`（Docker 部署）/ `$HOME/l4d2`（原生部署） |

> 第 2 条要特别注意：容器内 `/left4dead2` 在宿主机上是 `/var/lib/docker/volumes/l4d2_l4d2-data/_data`（`install.sh:133-134` 已建软链接 `/data/l4d2/left4dead2 → 该目录`）。默认值必须展示**宿主机路径**，不能展示容器内路径，否则用户按默认值去宿主机找会找不到。

> 现状说明：代码里**没有统一的安装目录默认值**，只有 4 处各自为政的隐式假设：
> - `consts.GamePath` 默认 Linux `/left4dead2`、Windows `left4dead2`（`consts/consts.go:46-66`）；
> - `steamcmd_update.go:42-43` 把 SteamCMD 目录推导为「启动项 WorkingDirectory 的父目录」；
> - `diagnose-server.sh:154` 把磁盘探测目标硬编码为 `/home/ubuntu/steamcmd/l4d2`；
> - `manage.sh:10` 多实例根目录固定 `/data/l4d2`。
>
> 本次改造要把这些隐式默认**收敛为 `ResolveInstallPath()` 单一函数**。

UI 上默认值旁提供两个快捷按钮：「沿用现有服务端同级目录」「填到安装根目录下」，并实时显示**解析后的宿主机绝对路径**与磁盘可用空间。

**解析规则**：

```
输入 raw
├─ 为空            → 用默认值（§上表）
├─ 以 ~ 开头       → 展开为目标用户（spec.user）的家目录
├─ 以 / 开头（POSIX 绝对）
│   └─ 直接 realpath，不做任何拼接
├─ Windows 盘符（D:\ 或 D:/，仅 Windows 主机）
│   └─ 直接 realpath
└─ 其他（相对路径）
    ├─ 基准目录 = InstallRoot（安装根目录，默认 /data/l4d2）
    ├─ 拼成 InstallRoot/raw
    └─ realpath 解析（必须处理符号链接）
```

> **关于"基准"的更正**：上一版写"相对路径以 `steam_dir` 为基准"是错的。默认安装方式已不需要宿主机装 SteamCMD（§9.5.2 用一次性容器），`steam_dir` 在宿主机上可能根本不存在。正确基准是 **`InstallRoot`**，它永远存在、语义清晰、用户可预期——填 `l4d2-2` 就得到 `/data/l4d2/l4d2-2`。

硬性要求：

1. **禁止用面板进程的 cwd 作为相对路径基准**。面板容器 cwd 与宿主机完全无关，用它会解析到容器内的 `/app/...`，写出去必然是错的。基准只能是 `InstallRoot`（宿主侧路径）。
2. **解析必须在宿主侧执行一次并回传 `resolved_dir`**，面板侧只用回传值做展示与后续校验；面板不做独立解析，避免两套逻辑不一致。
3. `resolved_dir` 必须落在 `RuntimeSettings.AllowedRoots` 内（默认：`/home`、`/data`、`/opt`、`/srv`，可配置），否则 `denied`。
4. `resolved_dir` 不允许是 `/`、`/home`、`/data` 等根级目录本身（必须至少下探一层），防止误把整个数据盘当成安装目录。
5. 目标目录已存在且含 `srcds_run` → 拒绝并提示改走「更新」。
6. 相对路径解析结果与用户直觉不符时（例如 `l4d2-2` 落到 `/data/l4d2/l4d2-2`），UI 必须在输入框下方**醒目展示最终宿主机绝对路径**，让用户在点「开始安装」前就确认。
7. 脚本里的 `force_install_dir` 一律写容器内路径 `/target`（跑在一次性容器里），`/target` 由 `-v <resolved_dir>:/target` 映射；**`resolved_dir` 只出现在 `-v` 参数里**。
8. `install_history.json` 同时记录 `install_dir`（原始输入）与 `resolved_dir`（宿主机绝对路径），便于审计与问题复现。

#### 9.5.7 安装完成后如何让面板"看得见"新目录

新装目录不在 `/left4dead2` 挂载点内时，面板的地图/插件/配置页读不到文件。三种处理方式：

| 方案 | 做法 | 优点 | 缺点 |
|---|---|---|---|
| **A. 装到默认挂载点内（本轮默认）** | 默认安装目录固定在 `/data/l4d2/` 下，面板天然可见 | 零额外配置，立即可用 | 单实例场景够用 |
| **B. 装完提示重启面板并追加挂载** | 生成 `docker-compose.override.yml` 片段，提示 `docker compose up -d` 重启面板，把新目录挂到 `/left4dead2-<id>` | 支持任意路径 | 需重启面板（约 10 秒），地图/插件页需支持多根目录 |
| **C. 只做生命周期管理** | 面板只负责启停/日志/RCON，文件类功能标为"不可用" | 无需重启 | 功能残缺 |

UI 上安装目录输入框旁必须显示当前方案：默认位置显示绿色「面板可直接管理」，其他位置显示黄色「需重启面板才能管理文件」。

#### 9.5.8 扫描并接管宿主机已有服务端，需要加挂载吗？

分三种能力，答案不同：

| 能力 | 是否需要加挂载 | 说明 |
|---|---|---|
| **检测**（发现宿主机上有哪些服） | **不需要** | 宿主 Agent 在宿主命名空间执行 `ps` / `systemctl` / `docker ps` / `screen -ls`，结果 JSON 回传 |
| **生命周期管理**（启停/重启/日志/健康检查） | **不需要** | 全部走 Agent（systemctl / docker API / kill），不涉及文件读写 |
| **文件类功能**（改 cfg、装插件、传地图、看 SourceMod 日志） | **需要** | 面板必须能读到该服务端的 `left4dead2/` 目录 |

所以接管必须区分两档：

- **完全接管**（`managed=true`）：目标目录已在面板可见范围内，文件类功能全开。
- **受限接管**（`managed=false`）：只能启停 + 日志 + RCON，文件类页面显示「该运行时目录未挂载到面板，无法管理文件」，并提供两个按钮：「追加挂载」（生成 override 片段 + 重启指引）、「迁移到面板可见目录」（走 §8.3 迁移流程）。

接管前自动判定：

```
candidate.game_path（宿主机绝对路径）
  ├─ 在 /left4dead2 卷的宿主真实路径下   → 完全接管
  ├─ 在任一已挂载卷的宿主真实路径下       → 完全接管
  └─ 其他                               → 受限接管（附原因与补救按钮）
```

宿主真实路径获取方式：`docker inspect <manager容器> -f '{{json .Mounts}}'`，把容器内路径映射回宿主路径。**不能拿容器内路径直接和宿主机路径比较**。


---

## 10. 数据模型与持久化

### 10.1 文件清单

| 文件 | 内容 | 权限 |
|---|---|---|
| `data/runtimes.json` | 运行时注册表（`[]RuntimeDescriptor`） | 0600 |
| `data/runtime_state.json` | 最近一次健康快照缓存（避免前端首屏空白） | 0600 |
| `data/runtime_events.db` | 状态变更事件（SQLite，7 天滚动） | 0600 |
| `data/install_history.json` | 安装记录 | 0600 |
| `data/manager_config.json` | 新增字段 `runtime`（见 §10.3） | 0600（已有） |
| `/control/runtime/runtimes/<id>.json` | Agent 侧描述符备份 | 0644 |

### 10.2 向后兼容与迁移

启动时 `LoadRuntimes()`：

1. 若 `data/runtimes.json` 不存在：
   - 调用现有 `GetServerStartupConfig()` 合成一个 `origin=legacy` 的默认运行时：
     - Linux：`kind=systemd`，`target.unit_name = L4D2_STARTUP_SERVICE_NAME || l4d2.service`，`target.unit_path = serverStartupUnitPath()`
     - Windows：`kind=bare`，`target` 为空，`spec` 来自 bat 解析
   - 若 `L4D2_CONTAINER_NAME` 有值 → 追加 `kind=docker` 的默认运行时
   - 合成结果**只在内存**中返回，用户点"保存"后才落盘，避免污染老部署
2. 若存在但缺少新字段：用默认值补齐（`json.Unmarshal` 的零值 + `normalizeRuntimes()` 修正），不做破坏性重写。
3. `default runtime` 的选择规则：`enabled && adopted && kind != archived` 中 `updated_at` 最新的一条；若无，取 legacy 合成项。

### 10.3 `ManagerConfig` 新增字段

```go
type RuntimeSettings struct {
    DefaultRuntimeID string `json:"default_runtime_id"`
    DefaultUser      string `json:"default_user"`        // agent 启动进程的默认用户
    AutoDetect       bool   `json:"auto_detect"`         // 打开运行时页时是否自动触发一次扫描
    DetectIntervalSec int   `json:"detect_interval_sec"` // 已确认固定为 0：不做后台定时扫描，仅手动触发
    AgentMode        string `json:"agent_mode"`          // auto | bridge | local | disabled
    AllowedRoots     []string `json:"allowed_roots"`     // Agent 写入允许根目录
}
```

`AgentMode=auto` 时的探测顺序：`FileBridgeClient`（`<control>/runtime/` 可写且 30 秒内能拿到任一 probe 结果）→ `LocalClient`（能直接 exec `systemctl`/`docker`）→ `disabled`（只读展示）。

---

## 11. 接口与前端设计

### 11.1 后端接口（全部 POST，除 `/platform`）

| 路由 | 请求 | 返回 | 管理员 |
|---|---|---|---|
| `/runtime/list` | — | `{runtimes:[], default_id, agent_mode, capabilities}` | 否（只读） |
| `/runtime/get` | `{id}` | `RuntimeDescriptor` | 否 |
| `/runtime/detect` | `{refresh:bool}` | `{candidates:[], scanned_at, warnings:[]}` | 是 |
| `/runtime/adopt` | `{candidate_key, id, name}` | `RuntimeDescriptor` | 是 |
| `/runtime/detach` | `{id}` | `{ok}` | 是 |
| `/runtime/create` | `{id,name,kind,target,spec}` | `RuntimeDescriptor` | 是 |
| `/runtime/update` | `{id,name,spec,enabled}` | `RuntimeDescriptor` | 是 |
| `/runtime/delete` | `{id,mode:"detach"|"archive"}` | `{ok}` | 是 |
| `/runtime/action` | `{id,action:"start"|"stop"|"restart"}` | `{job_id}` | 是 |
| `/runtime/status` | `{id}` | `HealthSnapshot` | 否 |
| `/runtime/logs` | `{id,tail}` | `{lines:[], source}` | 否 |
| `/runtime/migrate/plan` | `{source_id,target_kind,target_options}` | `MigrationPlan` | 是 |
| `/runtime/migrate/apply` | `{plan_token}` | `{job_id}` | 是 |
| `/runtime/job` | `{job_id}` | `JobResult` | 是 |
| `/runtime/events` | `{id,limit}` | `{events:[]}` | 否 |
| `/server-install/precheck` | `{source,steam_dir,install_dir,source_dir,platform,new_port}` | `PrecheckResult`（含 `resolved_dir`、`default_dir`、`suggestions[]`） | 是 |
| `/server-install/archive/init` | `{filename,size}` | `{upload_id}` | 是 |
| `/server-install/archive/chunk` | `multipart` | `{received}` | 是 |
| `/server-install/archive/merge` | `{upload_id,install_dir,job_id}` | `{ok}` | 是 |
| `/server-install/start` | `{source,...params}` | `{job_id}` | 是 |
| `/server-install/status` | `{job_id}` | `InstallJob` | 是 |
| `/server-install/cancel` | `{job_id}` | `{ok}` | 是 |
| `/server-install/configure` | `{job_id,tick,hostname,port,rcon}` | `{ok}` | 是 |
| `/server-install/create-runtime` | `{job_id,kind,target}` | `{runtime_id}` | 是 |

旧接口兼容：

- `POST /restart` → 内部改为 `runtimeAction(defaultID, "restart")`，保留原有环境变量兜底（当 Agent 不可用时仍走 `L4D2_RESTART_CMD`）。
- `GET /platform` → 追加字段（不删除旧字段）：

```json
{
  "runtime_kinds": ["systemd","docker","screen","tmux","nohup","bare","mcsm","lgsm"],
  "agent_mode": "bridge",
  "agent_version": "1.0.0",
  "detected_runtime": { "id": "l4d2-main", "kind": "systemd", "state": "running" },
  "systemd": true
}
```

`systemd` 字段改为**实测值**（`systemctl is-system-running` 成功且 `/run/systemd/system` 存在），修复缺陷 D1。前端 `FALLBACK` 改为 `systemd:false, agent_mode:"unknown"`。

### 11.2 前端页面

新增 `frontend/src/views/Runtime.vue`（菜单：服务器设置 → 运行时管理，管理员可见）：

- **顶部**：当前默认运行时卡片（类型徽标、状态灯、PID、端口、RCON 可达、重启/停止/启动按钮）。
- **运行时列表**：名称、类型、状态、游戏目录、端口、是否接管、是否持久化；行内操作（接管详情、停止、迁移、卸载）。
- **自动检测抽屉**：扫描按钮 → 候选卡片（证据列表、置信度条、已接管标记）；不可接管项显示原因。
- **迁移向导弹窗**：源 → 目标选择 → `MigrationPlan` 步骤与风险展示 → 输入 `runtime_id` 确认 → 进度（阶段 + 日志尾部）→ 成功/失败/已回滚。
- **状态轮询**：页面可见时 5 秒一次 `/runtime/status`，不可见时暂停（`document.visibilitychange`）。

新增 `frontend/src/views/ServerInstall.vue`（菜单：服务器设置 → 服务端安装，管理员可见）：5 步向导，对应 §9.2。

`frontend/src/stores/runtime.ts`：缓存 `runtimes`、`defaultId`、`agentMode`，提供 `kindLabel(kind)`、`stateColor(state)`、`isPersistent(kind)`。

### 11.3 需要改造的现有页面

| 页面 | 改造 |
|---|---|
| `Home.vue` | 状态卡增加"运行时"行；重启按钮改为调用 `/runtime/action`；诊断入口按 `runtime.kind` 决定是否可用 |
| `ServerStartup.vue` | 顶部增加运行时选择器（多运行时场景）；保存时按 `kind` 走不同 provider；非持久化运行时显示"重启后不会自恢复"警告 |
| `ServerMaintenance.vue` | 更新前后调用 provider 的 stop/start，而不是硬编码 systemctl |
| `ServerDiagnosticsModal.vue` | 支持非 systemd 字段（进程/端口/日志），systemd 专属字段折叠 |
| `stores/platform.ts` | 修复 `FALLBACK`；新增 runtime 相关派生属性 |

---

## 12. 安全、失败恢复与盲区清单

### 12.1 安全边界

- 所有 `/runtime/*` 写操作与 `/server-install/*` 必须后端校验管理员角色（不能只靠前端 `requiresAdmin`）。
- 所有写操作 `defer LogOp(c, "...")()` 审计；审计详情记录 `runtime_id`、`kind`、`action`、`job_id`，**不记录** RCON 密码与 MCSM Key。
- RCON 密码、MCSM API Key 只以"引用"形式存储（环境变量名或 `0600` 密钥文件路径），API 返回时脱敏为 `***`。
- Agent 请求文件 0600；结果文件 0644（面板需要可读）。
- 禁止把用户输入拼进命令：post-start/stop 钩子只允许枚举预设动作，不支持自定义 shell。

### 12.2 失败恢复矩阵

| 失败点 | 表现 | 处理 |
|---|---|---|
| Agent 未安装 | probe 超时 | `agent_mode=disabled`，UI 降级为只读 + 旧链路兜底 |
| Agent 崩溃 | 结果文件不产生 | job 超时（默认 60s，迁移 180s）→ `timeout`，允许重试 |
| 请求文件格式错 | Agent 拒绝 | 返回 `denied` + 原因，不执行任何命令 |
| 迁移中停源成功但启目标失败 | 服挂了 | 自动回滚：恢复源配置并启动源运行时 |
| 回滚也失败 | 双重故障 | 结果 `error` + `rollback=failed`，返回精确的手工恢复命令（含快照路径） |
| 端口被残留进程占用 | 启动失败 | 提供"终止占用并重启"（沿用 `repair-addons.sh` 的 SIGTERM→SIGKILL 逻辑） |
| 磁盘满 | SteamCMD 失败 | 预检拦截；运行中失败则返回剩余空间与需清理项 |
| SteamCMD 并发 | 两个任务互相破坏 | 全局 flock + 后端任务去重 |
| 注册表写坏 | 启动时解析失败 | 保留 `runtimes.json.bak`，解析失败自动回退并告警 |

### 12.3 用户没想到但必须处理的盲区

1. **srcds 自动换端口**：`+hostport` 被占时 srcds 会静默改用下一个端口，`systemctl status` 仍显示 active。健康检查必须校验"监听端口的 owner 是游戏进程"，不能只看布尔值。
2. **`srcds_run` 是 shell wrapper**：`ps` 里看到的 PID 可能是 shell，真正进程是子进程 `srcds_linux`。Stop 必须杀整个进程组（`kill -TERM -<pgid>`），否则留下孤儿进程继续占端口。
3. **screen/tmux 会话名冲突**：同名 session 已存在时 `screen -S` 会静默接管而不是报错。创建前必须 `screen -ls` 校验。
4. **tmux server 共享**：多个实例共用同一个 tmux server，`kill-server` 会杀掉所有实例。只允许 `kill-session`，禁止 `kill-server`。
5. **systemd unit 的 `User=` 与游戏目录属主不一致**：这是 `Permission denied` 的头号原因。迁移/接管时必须校验属主并给出 `chown` 建议。
6. **systemd `ExecStart` 多行/续行**：`systemd` 支持 `ExecStart` 续行（反斜杠）与多个 `ExecStart`。当前解析器直接报错，接管时应支持"只读接管"（不改写），而不是拒绝接管。
7. **`WorkingDirectory` 软链接**：游戏目录常是软链接到数据盘，`filepath.Clean` 后仍可能指向不同真实路径。注册表要同时存 `path` 与 `real_path`（`os.path.realpath`），端口/实例冲突按 `real_path` 判重。
8. **容器内看不到宿主进程**：面板容器 PID namespace 隔离，`ps` 只能看到自己。所有进程级能力**必须**由 Agent 执行，面板侧不得假装能读宿主 `/proc`。
9. **Docker socket 权限**：面板容器以 1000:1001 运行，需要 `docker` 组 GID 匹配。若 socket 不可用，`docker` provider 必须 `unsupported` 而不是报 500。
10. **32 位 srcds 与 seccomp**：Docker 29.3+ 默认 seccomp 会让 srcds 直接段错误。docker provider 创建容器时必须注入 `security_opt: seccomp:unconfined`。
11. **MCSM 实例与本地文件系统**：MCSM 可能管理远程节点，`game_path` 在面板视角不可见。此时面板只能走 MCSM API 做生命周期，地图/插件文件操作必须标记为"不可用"。
12. **Windows 的"运行时"**：Windows 没有 systemd/screen/tmux。`bare` provider 用 `taskkill` + 计划任务实现重启，能力矩阵必须如实标注（`Persistent=false`）。
13. **时区与时间**：`systemctl show` 的时间戳是本地时间字符串，`journalctl -o short-iso` 带时区。统一转 UTC RFC3339 再比较，避免"刚刚重启"显示成 8 小时前。
14. **长任务与前端关闭**：安装/迁移是后台任务，前端关闭不应中断。任务状态持久化在 `data/`，重开后继续轮询。
15. **首次接管 systemd 服时的 unit 路径**：`systemctl show -p FragmentPath` 可能指向 `/etc/systemd/system/l4d2.service` 或 `/lib/systemd/system/...`。接管只读时不要假设可写；可写判定用 `os.access(path, W_OK)`。
16. **面板自身被重启**：Agent 与面板生命周期解耦，面板重启后根据 `data/runtimes.json` + `runtime_state.json` 恢复，不需要重新检测。

---

## 13. 分阶段实施计划

> 每个阶段结束都必须：`go -C backend fmt ./...`、`go -C backend test ./...`、`go -C backend build .`、`npm --prefix frontend run build` 全绿；并在远端真机做一次端到端验证。

### 阶段 0：地基（不改用户可见行为）

- 新增 `backend/logic/runtime/`：`kind.go`、`spec.go`、`descriptor.go`、`registry.go`、`client.go`（`FileBridgeClient` + `LocalClient`）。
- 新增 `data/runtimes.json` 读写与 legacy 合成（§10.2）。
- 修复 D1（`HasSystemd` 实测）、D2（补 `l4d2-startup.path` 对 `systemd-reload` 的监听或删除该文件写入，二选一并写文档）、D6（前端 `FALLBACK`）。
- **修复 D7/D8/D9**：抽出 `BuildSteamCMDRunScript(installDir)`，把 `run-maintenance.sh` 的"单平台先试 + `Invalid platform` 兜底"改为**无条件三段式 runscript**，并让 Go 侧 `GenerateSteamCMDUpdateScript` 复用同一模板（§9.5.2）；同时修正脚本落盘位置为 `<steam_dir>/update_l4d2.txt`，让写入位置与上报 `ScriptPath` 一致。
- **修复 D10**：新增 `ResolveInstallPath(raw, steamDir, user)` 单一函数，替换 `consts.GamePath` / `steamcmd_update.go` / `diagnose-server.sh` / `manage.sh` 四处隐式默认（§9.5.6）。
- 新增 `GET /platform` 的 runtime 字段（向后兼容追加）。
- 验收：旧部署升级后行为完全不变；`/platform` 返回 `agent_mode`；在远端真机执行一次更新，日志能完整出现「linux 拉取 → windows 补齐 → linux validate」三段。

### 阶段 1：Agent + systemd/docker provider

- 新增 `manifest/runtime-agent/`（Python3 + ruff）与 `manifest/systemd/l4d2-runtime.{path,service}`。
- 实现 `probe / inspect / start / stop / restart / logs`，覆盖 `systemd`、`docker`、`bare`。
- 新增 `/runtime/list|get|status|logs|action|job`。
- 前端新增 `Runtime.vue` 只读视图 + 手动新增运行时。
- 验收：远端机器上能对现有 systemd 服做启停、看日志、看健康状态。

### 阶段 2：自动检测 + 接管

- 实现 `detect`（§7）与 `adopt/detach`。
- 前端检测抽屉 + 接管向导。
- 补齐 `screen / tmux / nohup` provider 的 inspect/start/stop/restart。
- 验收：在装有 screen/tmux/nohup 三种部署的测试机上，能全部检测出来并接管，接管后能重启。

### 阶段 3：迁移 + 回滚

- `export/import` 规格、`migrate/plan|apply`、快照与回滚、`runtime_events.db`。
- 前端迁移向导。
- 把 `server_maintenance`、`server_repair`、`server_startup` 内部改为通过 provider 执行。
- 验收：systemd → screen → systemd 往返迁移成功；人为制造启动失败能自动回滚。

### 阶段 4：服务端安装 + MCSM/LGSM

- `/server-install/*` 五步向导，三条来源（SteamCMD / 压缩包 / 目录复制）、依赖预检、安装任务、取消。
- `mcsm`、`lgsm` provider。
- 验收：三条来源分别从零产出一个可连的服务端，并直接创建运行时启动成功。

### 阶段 5（可选）：常驻 Agent 与多实例

- Agent 增加 Unix socket 模式（`/run/l4d2-manager/agent.sock`），面板通过挂载的目录访问，降低轮询延迟。
- 与 `docs/operations/multi-instance-panel.md` 合并：`runtimes.json` 即实例注册表，前端顶部实例切换器。

---

## 14. 测试计划

### 14.1 单元测试（Go）

| 目标 | 用例 |
|---|---|
| `LaunchSpec` 分词/序列化 | 含空格路径、引号、反斜杠、空参数、超长参数 |
| 注册表读写 | 空文件、损坏 JSON、缺字段补齐、并发读写、legacy 合成 |
| 路径校验 | `..`、相对路径、`\0`、Windows 与 POSIX 混用 |
| 检测融合 | 同一进程多重证据去重、cgroup 归因优先、置信度排序 |
| 健康判定 | 端口通但 owner 非游戏进程 → degraded；进程在但端口未监听 → starting |
| 迁移计划 | 端口冲突拦截、磁盘不足拦截、源=目标拦截 |
| 安装准入 | 目标目录含 `srcds_run` 拒绝；目录锁并发拒绝；`source=copy` 时父子目录拒绝 |
| 离线包解压 | 绝对路径/`..`/符号链接越界/设备文件/条目超限/解压总量超限 全部拒绝 |
| 安装来源分派 | `steamcmd` / `archive` / `copy` 三条分支的参数校验与 job 结构 |
| SteamCMD 脚本生成 | `BuildSteamCMDRunScript` 输出必须严格包含三段（linux → windows → linux/validate）、顺序正确、`force_install_dir` 为 `/target`、无多余参数 |
| SteamCMD 结果校验 | 缺少 `srcds_linux` / `bin/` / `appmanifest_222860.acf` 任一 → 判定失败，即使退出码为 0 |
| 安装路径解析 | 相对路径以 `InstallRoot` 为基准（不是进程 cwd、不是 steam_dir）；`~` 展开；绝对路径不改写；`..` 逃逸拒绝；`realpath` 跟随符号链接后仍在白名单内；解析结果为 `/`、`/home` 时拒绝；默认值优先级链 4 级全部覆盖 |
| 挂载可见性判定 | `docker inspect .Mounts` 容器路径→宿主路径映射正确；候选目录在卷内 → `managed=true`；卷外 → `managed=false` 且带原因 |
| 一次性容器安装参数 | 生成的 `docker run` argv 含 `--rm`、`-v <resolved_dir>:/target`、`--user <uid>:<gid>`、正确的镜像名；`force_install_dir` 为 `/target` |
| 旧接口兼容 | `/restart` 在 Agent 不可用时回退 `L4D2_RESTART_CMD` |

### 14.2 Agent 单元测试（Python，pytest）

- `subprocess` 调用一律 mock，断言 argv 形态（禁止 shell）。
- 路径越界、非法 unit 名、非法 session 名必须抛 `denied`。
- job 幂等：同一 `job_id` 二次执行返回缓存结果。

### 14.3 端到端（远端真机 101.43.105.208）

1. 现有 systemd 服：检测 → 接管 → 状态 running → 重启 → 健康检查通过。
2. 手工起一个 screen 服：检测置信度 ≥ 0.8 → 接管 → 停止 → 启动 → 状态 running。
3. 迁移：systemd → screen → 回滚到 systemd；比对迁移前后 `ExecStart` 与端口一致。
4. 安装（三条来源各验一次）：
   - `steamcmd`：装到 `/data/l4d2/l4d2-test` → 日志必须完整出现三段 → `srcds_linux` 存在且 `file` 显示 `ELF 32-bit` → **验证宿主机上文件属主正确**（`ls -ln` 的 uid/gid = 目标用户）→ 生成 systemd unit → 启动 → RCON `status` 有输出；
   - `archive`：上传一份约 1 GB 的 zip 包 → 解压 → 结构校验通过 → 启动成功；
   - `copy`：从运行中的 `l4d2` 目录复制到 `l4d2-copy`（此时源目录有进程，必须先验证被拦截）→ 停源后复制 → 改端口 27017 → 启动成功且两个服可同时 RCON 连上。
4.1 **验证"无需新增挂载"**：全程不修改 `docker-compose.yaml`、不重启面板，三条来源都能装到 `/data/l4d2/` 下；装完后地图/插件页面立即可读到新目录内容。
4.2 **验证受限接管**：在 `/opt/l4d2-standalone` 手工起一个 screen 服 → 扫描能发现 → 接管后状态为"受限接管"，启停正常，文件页显示未挂载提示与补救按钮。
4.3 更新回归（验证 D7/D8 修复）：对现有服执行一次「检查更新 → 应用更新」，日志必须是无条件三段式，不允许出现"先失败再补 windows 段"的痕迹。
5. 失败注入：迁移时人为占用目标端口 → 计划阶段拦截；apply 阶段制造启动失败 → 自动回滚。
6. 兼容性回归：旧页面 `ServerStartup`、`ServerMaintenance`、`Home` 全部功能正常。

### 14.4 回归门禁

- `go vet ./...` 无新增告警。
- 前端 `vue-tsc` 零错误。
- 所有新增接口必须有对应 controller 测试（至少覆盖权限与参数校验）。

---

## 15. 风险与已确认决策

### 15.1 已确认决策（2026-11-17）

| 编号 | 决策项 | 结论 |
|---|---|---|
| C1 | Agent 形态 | **Python3 脚本**，随镜像分发到宿主机 `/opt/l4d2-manager/runtime-agent/`，由 systemd path unit 触发，代码符合 ruff 规范 |
| C2 | 自动检测时机 | **仅手动扫描**。`DetectIntervalSec` 固定 0，不做后台定时任务；结果在内存缓存 30 秒防抖 |
| C3 | 服务端安装来源 | **三种全支持**：① SteamCMD 在线安装 ② 上传压缩包离线安装 ③ 从已有目录复制新实例 |
| C4 | 多实例范围 | **本轮只做单实例多运行时**。`runtimes.json` 结构预留多实例字段（`ID`/`GamePath`/`Target` 均为实例级），前端不做实例切换器，下一轮再开 |
| C5 | SteamCMD 执行位置 | **不需要宿主机安装 SteamCMD**。默认用游戏镜像起 `docker run --rm` 一次性容器，`-v` 挂载目标目录，SteamCMD 直接写入宿主路径（§9.5.2）。宿主 Agent 执行仅作降级 |
| C6 | 是否新增 compose 挂载 | **不新增**。`docker run -v` 由宿主 daemon 解析路径，可覆盖任意宿主目录；仅当用户要求面板直接编辑非挂载目录时，才提示追加 override 并重启（§9.5.7 方案 B） |
| C7 | 相对路径基准 | **`InstallRoot`（默认 `/data/l4d2`）**，不是 SteamCMD 目录、不是容器 cwd（§9.5.6） |

### 15.2 风险清单

| 编号 | 风险 | 建议 |
|---|---|---|
| R1 | Agent 以 root 运行，攻击面大 | 参数白名单 + 无 shell + 路径根限制；后续可改为 sudoers 白名单 + 非 root |
| R2 | 迁移停机窗口不可控（SteamCMD 校验可能很久） | 迁移只做"快速停起"，校验放维护任务；迁移前提示预估窗口 |
| R3 | screen/tmux 非持久化，用户误以为能自恢复 | UI 强制黄条警告 + 一键迁移 systemd |
| R4 | MCSM 远程节点文件不可见 | 能力矩阵按"节点是否本地"动态降级 |
| R5 | 检测在大型宿主机上慢（进程多） | `ps` 全量扫描限 3 秒超时；结果缓存 30 秒 |
| R6 | 面板容器无 `/control` 写权限 | `agent_mode` 降级 + 明确提示需要挂载/安装 Agent |
| R7 | 现有 `L4D2_STARTUP_RELOAD_CMD` 等环境变量语义重叠 | 阶段 0 统一收敛到 `RuntimeSettings`，保留旧变量读取但标记 deprecated |
| R8 | 三种安装来源的目录判定互相干扰 | 统一"目标目录必须为空或不含 `srcds_run`"的准入规则；离线包解压走同一套完整性校验（见 §9.5） |
| R9 | 目录复制可能复制出正在运行的服务端 | 复制前校验源目录无运行中进程占用（`/proc/*/cwd` 反查）；复制用 `cp -a --reflink=auto`，完成后重写端口/RCON 避免冲突 |
| R10 | 匿名账号单平台拉取必然残缺 | 强制三段式 runscript，禁止任何"单平台安装/更新"分支；预检磁盘门槛提到 45 GB（§9.5.2） |
| R11 | 三段式耗时更长，用户以为卡死 | 分阶段进度文案（建立 depot / 补齐 Windows 内容 / Linux 校验），并显示已运行时长 |
| R12 | 一次性容器安装时，宿主机目标目录属主与容器内 UID 不一致 → 装完文件属主错误 | `docker run` 必须带 `--user <uid>:<gid>`（取目标用户 uid/gid），装完再 `chown -R` 兜底 |
| R13 | 一次性容器依赖本地已有游戏镜像，镜像未拉取时安装失败 | 安装前 `docker image inspect` 预检，缺失则先 `docker pull` 或明确报错并提供拉取命令 |
| R14 | 装到非挂载目录后，面板文件功能不可用，用户困惑 | 安装目录旁实时显示"面板可直接管理 / 需重启面板"，并在安装完成后弹提示（§9.5.7） |

### 15.3 本轮不做（明确排除）

- 单面板多实例切换器（`X-L4D2-Instance` 请求头、顶部实例选择）→ 下一轮，见 `docs/operations/multi-instance-panel.md`
- Agent 常驻 Unix socket 模式 → 阶段 5 可选
- MCSM 远程节点上的地图/插件文件操作 → 仅提供生命周期管理

---

## 16. 变更文件清单（预估）

### 后端新增

```
backend/logic/runtime/{kind,spec,descriptor,registry,client,health,detect,migrate,install,events,paths}.go
backend/controller/runtime.go
backend/controller/server_install.go
backend/model/runtime_event.go
backend/db/runtime_events.go
```

> `paths.go` 承载 `ResolveInstallPath()`、`ResolveSteamDir()`、`DefaultInstallDir()`，是 §9.5.6 的唯一实现，禁止在其他文件重复实现路径解析。

### 后端修改

```
backend/main.go                  # 注册 /runtime/* 与 /server-install/*
backend/controller/restart.go    # 转发到 runtime action，保留兜底
backend/logic/platform.go        # HasSystemd 实测；PlatformCapabilities 扩展
backend/logic/server_startup.go  # 抽出 LaunchSpec 转换，按 kind 分派
backend/logic/server_maintenance.go # 停机/启动改走 provider
backend/logic/server_repair.go   # 同上
backend/logic/server_diagnostics.go # 支持非 systemd 字段
backend/logic/manager_config.go  # 新增 RuntimeSettings
backend/logic/steamcmd_update.go # 抽出 BuildSteamCMDRunScript 单一模板（三段式），更新与安装共用
backend/consts/consts.go         # 新增运行数据路径常量
```

### 部署层修改（SteamCMD 三段式）

```
manifest/systemd/run-maintenance.sh  # 删除"单平台先试 + Invalid platform 兜底"，改为无条件三段式 runscript
```

### 部署层新增

```
manifest/runtime-agent/l4d2_runtime_agent.py
manifest/runtime-agent/providers/*.py
manifest/systemd/l4d2-runtime.path
manifest/systemd/l4d2-runtime.service
manifest/install/install.sh        # 安装 Agent 与 path unit
manifest/docker/manager.Dockerfile # 打包 runtime-agent 到镜像（供安装脚本分发）
```

### 前端新增

```
frontend/src/views/Runtime.vue
frontend/src/views/ServerInstall.vue
frontend/src/stores/runtime.ts
frontend/src/components/runtime/*.vue
```

### 前端修改

```
frontend/src/services/api.ts        # 新增接口与类型
frontend/src/router/index.ts        # 两条路由（requiresAdmin）
frontend/src/components/AppNavigation.vue # 两个菜单项（adminOnly）
frontend/src/stores/platform.ts     # 修 FALLBACK + runtime 字段
frontend/src/views/Home.vue
frontend/src/views/ServerStartup.vue
frontend/src/views/ServerMaintenance.vue
frontend/src/components/ServerDiagnosticsModal.vue
```

---

## 17. 实施前置动作（用户规则）

1. 备份：把 §16 列出的**所有已存在文件**复制到
   `_archive/l4d2-backups/20261117-runtime-adapter/`，保留相对目录结构。
2. 本文归档后不再随意改动；实现中发现设计缺陷，先在本文件追加"修订记录"再改代码。
3. 每个阶段单独提交，提交信息含阶段号与本文件锚点（例如 `feat(runtime): 阶段1 Agent 桥接`）。

---

## 18. 修订记录

### 2026-11-17 修订 1：SteamCMD 匿名拉取必须三段式

**触发**：用户实测确认 —— SteamCMD 支持 Windows 匿名下载服务端，但 **Linux 单平台匿名拉取拿不到完整内容**。必须按以下流程才能成功：

```bash
cat > update_l4d2.txt << 'EOF'
force_install_dir ./l4d2
login anonymous
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType windows
app_info_update 1
app_update 222860
@sSteamCmdForcePlatformType linux
app_info_update 1
app_update 222860 validate
quit
EOF
./steamcmd.sh +runscript update_l4d2.txt
```

**修订内容**：

| 章节 | 变更 |
|---|---|
| §2.3 | 新增缺陷 D7（单平台拉取不可靠）、D8（模板分散两处）、D9（脚本写入位置与上报不一致）、D10（默认安装目录四处隐式假设） |
| §9.1 | 更新与安装的 SteamCMD 方式统一为三段式 runscript |
| §9.2 | 预检磁盘门槛 30 GB → 45 GB；删除"单平台安装"选项；执行阶段文案改为三段式 |
| §9.4 | 拆成"安装前可查"与"安装后必查"两组；补充 32 位 ELF 校验 |
| §9.5.2 | 重写为完整三段式规范（脚本模板、argv 调用、10 条要点、结果校验、重试、取消） |
| §13 阶段 0 | 增加 D7/D8 修复任务与更新回归验收 |
| §14.1 / §14.3 | 新增 SteamCMD 脚本生成与结果校验用例；新增更新回归测试项 |
| §15.2 | 新增风险 R10（单平台残缺）、R11（三段式耗时长易误判卡死） |
| §16 | 后端修改新增 `steamcmd_update.go`；部署层修改新增 `run-maintenance.sh` |
| §18 修订 2 | 安装目录默认值与相对/绝对路径解析（见下） |

### 2026-11-17 修订 2：安装目录支持相对/绝对路径 + 必须有默认值

**触发**：用户指出「肯定是按实际安装的时候用户输入的路径，支持相对或者绝对路径，还有就是我们没有默认路径吗」。

**核实结论**：代码里确实**没有统一的安装目录默认值**，只有 4 处各自为政的隐式假设：

| 位置 | 隐式假设 |
|---|---|
| `backend/consts/consts.go:46-66` | `GamePath` 默认 Linux `/left4dead2`、Windows `left4dead2` |
| `backend/logic/steamcmd_update.go:42-43` | SteamCMD 目录 = 启动项 WorkingDirectory 的父目录；脚本路径 = `<working_dir>/left4dead2/update_l4d2.txt` |
| `manifest/systemd/diagnose-server.sh:154` | 磁盘探测硬编码 `/home/ubuntu/steamcmd/l4d2` |
| `manifest/install/manage.sh:10` | 多实例根目录固定 `/data/l4d2` |

**修订内容**：

| 章节 | 变更 |
|---|---|
| §9.5.1 | 准入规则改为「接受相对与绝对路径」，新增 `resolved_dir` 记录 |
| §9.5.2 | `force_install_dir` 一律写 `resolved_dir`（绝对路径），不写用户原始输入 |
| §9.5.5 | UI 分支表补充「安装目录（相对/绝对）」参数行 |
| §9.5.6（新增） | 完整定义默认值优先级（4 级）与路径解析规则（`~` / POSIX 绝对 / Windows 盘符 / 相对以 `steam_dir` 为基准）、8 条硬性要求 |
| §11.1 | `/server-install/precheck` 返回值增加 `resolved_dir`、`default_dir`、`suggestions[]` |
| §14.1 | 新增「安装路径解析」单测行 |

**关键决策**：

1. **相对路径的基准是 `steam_dir`，不是进程 cwd**。面板跑在容器里，cwd 与宿主机无关，用 cwd 解析必然落到容器内 `/app/...`，写出去是错的。
2. **解析只在 Agent 侧做一次**，回传 `resolved_dir` 给面板；面板不重复实现解析，避免两套逻辑漂移。
3. **UI 必须预填默认值 + 实时展示最终绝对路径**，用户填相对路径时也能立刻看到会落到哪。
4. 默认值收敛为 `ResolveInstallPath()` 单一函数，替换现有 4 处隐式假设。

**影响面**：安装向导 UI、`/server-install/precheck` 返回结构、Agent 的 `install` 动作参数校验。不改动现有更新链路的行为。

### 2026-11-17 修订 3：澄清"容器看不见宿主机"，SteamCMD 改走一次性容器

**触发**：用户提问「什么叫基准是宿主机上的 steamcmd…宿主机上得有 steamcmd 才能下载安装服务端吗？不能做到容器内置吗？然后下完容器内指令复制到宿主机那边吗？那这样容器还需要重新映射目录吗？扫描宿主机求生服务端选择接管的时候需要重新添加映射吗？」

**核实结论**（源码取证）：

| 事实 | 证据 |
|---|---|
| 游戏镜像里**已经有** SteamCMD | `manifest/docker/l4d2.Dockerfile:36-39`（`/root/steamcmd/steamcmd.sh`） |
| 面板镜像里**没有** SteamCMD | `manifest/docker/manager.Dockerfile`（alpine + docker cli，无 steamcmd） |
| 面板容器只挂了 `/left4dead2`、`/plugins`、`/data`、docker.sock、`/host/proc` | `install.sh:90-97`、`manage.sh:376-383` |
| 面板容器有 docker socket，可起容器 | `install.sh:94`、`manage.sh:380` |

**结论与修订**：

1. **不需要宿主机装 SteamCMD**。改用游戏镜像起 `docker run --rm` 一次性容器，`-v <resolved_dir>:/target`，SteamCMD 直接写宿主路径，**不存在"下载完再复制"这一步**，也没有 30 GB 双份占用。
2. **不需要新增任何 compose 挂载**。`-v` 的源路径由宿主 docker daemon 解析，不受面板容器挂载限制。
3. **相对路径基准从 `steam_dir` 改为 `InstallRoot`（默认 `/data/l4d2`）**——因为默认方案下宿主机可能根本没有 `steam_dir`。
4. 新增 **§4.3 三条访问通道**：C1 docker daemon / C2 宿主 Agent / C3 已挂载文件系统，明确"什么操作走哪条路"。
5. 新增 **§9.5.7**：装到非挂载目录后面板文件功能不可用的三种处理方案（默认装到挂载点内）。
6. 新增 **§9.5.8**：接管宿主机已有服务端时，检测与生命周期**不需要挂载**，只有文件类功能需要；接管分"完全接管 / 受限接管"两档，判定依据是 `docker inspect .Mounts` 映射回宿主真实路径。
7. 新增风险 R12（一次性容器 UID 不一致）、R13（本地镜像缺失）、R14（非挂载目录文件功能不可用）。
8. 新增决策 C5/C6/C7。

**影响面**：§4.3、§9.5.2、§9.5.6、§9.5.7、§9.5.8、§15.1、§15.2。核心变化是"安装不再依赖宿主 SteamCMD"，这让方案能在**任意现有部署**上直接生效，无需用户改 compose 或重装环境。



---

## 19. 实施记录（2026-11-17）

本轮按用户指令「更新下载这个功能先不管，把其他功能全部做出来」实施，**§9 服务端下载/安装整章跳过**（SteamCMD 三段式、一次性容器、安装向导、路径解析全部未落地）。以下为已落地内容与实现中修正的设计偏差。

### 19.1 已实现清单

| 层 | 文件 | 内容 |
|---|---|---|
| 后端 | `backend/logic/runtime/kind.go` | 9 种 RuntimeKind、能力矩阵、中文标签 |
| 后端 | `backend/logic/runtime/spec.go` | LaunchSpec 归一化、shell 风格分词、路径/用户名校验 |
| 后端 | `backend/logic/runtime/descriptor.go` | Descriptor/Target 校验、按 Kind 校验句柄 |
| 后端 | `backend/logic/runtime/registry.go` | `data/runtimes.json` 原子读写、健康快照缓存、事件流滚动 |
| 后端 | `backend/logic/runtime/client.go` | FileBridgeClient（请求/结果文件）、LocalClient 抽象 |
| 后端 | `backend/logic/runtime/local_provider.go` | 本地执行 docker/systemd/nohup/bare，无 Agent 时降级 |
| 后端 | `backend/logic/runtime/health.go` | 进程 → 端口归属 → RCON 三层健康判定 |
| 后端 | `backend/logic/runtime/detect.go` | 候选指纹、去重融合、接管状态标注 |
| 后端 | `backend/logic/runtime/service.go` | Manager：CRUD、动作、检测、接管、迁移计划与执行 |
| 后端 | `backend/logic/runtime/paths.go` | `ResolveInstallPath()`、白名单校验（为 §9 预留） |
| 后端 | `backend/logic/runtime_bridge.go` | legacy 合成运行时、`ControlViaRuntime()` 兼容旧链路 |
| 后端 | `backend/controller/runtime.go` | 14 个运行时接口，全部管理员鉴权 + 审计 |
| 后端 | `backend/controller/restart.go` | 新增 `/start`、`/stop`，`/restart` 优先走运行时 |
| 后端 | `backend/logic/platform.go` | 修 D1（`HasSystemd` 实测）、`/platform` 追加运行时字段 |
| 后端 | `backend/logic/server_startup.go` | 修 D2（重载通道不再写死无监听文件） |
| 宿主 | `manifest/runtime-agent/l4d2_runtime_agent.py` | 任务分发、白名单动作、文件锁、结构化错误 |
| 宿主 | `manifest/runtime-agent/agentlib/common.py` | argv 执行、路径白名单、端口归属、进程扫描 |
| 宿主 | `manifest/runtime-agent/providers/*.py` | systemd / docker / screen / tmux / nohup / bare / mcsm / lgsm |
| 宿主 | `manifest/systemd/l4d2-runtime.{path,service}` | path unit 触发 Agent |
| 部署 | `manifest/install/install.sh` | 挂载 `/control`、`/host/systemd`、安装 Agent |
| 部署 | `manifest/docker/manager.Dockerfile` | 镜像内置 Agent 与 systemd 单元 |
| 前端 | `frontend/src/services/api.ts` | 运行时类型定义 + 16 个 API 方法 |
| 前端 | `frontend/src/stores/runtime.ts` | 运行时状态、健康、轮询 |
| 前端 | `frontend/src/views/Runtime.vue` | 概览、列表、扫描抽屉、迁移向导、日志、事件 |
| 前端 | `frontend/src/stores/platform.ts` | 修 D6（FALLBACK 不再假定 systemd） |
| 前端 | `frontend/src/router/index.ts`、`AppNavigation.vue` | `/runtime` 路由与菜单 |

### 19.2 实施中发现并修正的设计偏差

| 编号 | 偏差 | 修正 |
|---|---|---|
| I1 | 原设计"桥接目录可写即认为 Agent 可用" | 改为真实 `probe` 往返：`/control` 在容器内永远可写，只看可写会让 UI 谎报全能力，点一次超时 45 秒 |
| I2 | 探测失败后只把 `agent_mode` 改成 local，`client` 仍指向桥接 | mode 与 client 必须成对切换（`selectClient`），否则 local 模式下所有请求仍等结果文件 |
| I3 | 迁移目标只校验 `CanStart && CanWriteSpec` | screen/tmux 的 `CanWriteSpec=false` 但完全可作为迁移目标，改为只校验 `CanStart` |
| I4 | 注册表里的 `Capabilities` 直接透出 | 改为按当前环境重新计算（`effectiveCapabilities`），Windows 上的 systemd 条目不再显示可启动 |
| I5 | 迁移计划不校验目标类型在本机是否可行 | 新增 `unsupportedReason`，计划阶段就列出 blocker，避免执行时必然回滚 |
| I6 | `ValidateID` 先 `ToLower` 再校验 | 会让 `Main` 被静默接受成 `main`，与前端提交值不一致；改为按原始值校验 |
| I7 | `CandidateKey` 用原始目录字符串 | 尾部斜杠会让同一服务端生成两个候选，改为先 `cleanStyle` |
| I8 | Agent 白名单用 `":"` 硬切 | Windows 路径 `C:\...` 会被切断，改用 `os.pathsep`；Go 侧同步用 `os.PathListSeparator` |
| I9 | Agent 顶层 `import pwd` / `import fcntl` | 仅 POSIX 存在，Windows 上无法跑测试；改为兼容导入 + 判空 |
| I10 | 端口解析、shell 引号、spec 读取在多个 provider 重复 | 收敛到 `agentlib/common.py` 的 `parse_port` / `quote_shell_arg`，`_spec_from_pid` 复用 |
| I11 | 安装脚本写死镜像名 | 国内镜像源场景下写死 `laoyutang/...` 会拉到不存在的镜像，改为 `docker inspect` 读取运行中容器镜像 |

### 19.3 兼容性保证

- 旧接口 `/restart`、`/server-startup/*`、`/server-maintenance/*`、`/server-repair/*` 全部保留。
- `/restart` 只在"注册表里有非 legacy 的启用运行时 且 Agent 可用"时才走新链路，否则原样执行旧的 `RCON → L4D2_RESTART_CMD → docker restart`。
- 注册表为空时 `/runtime/list` 返回一个内存合成的 `default`（origin=legacy），用户保存后才落盘，不改变任何既有行为。
- `l4d2-startup.path` 仍监听 unit 文件本身，保存启动项行为不变。

### 19.4 测试结果

| 类型 | 命令 | 结果 |
|---|---|---|
| Go 单元测试 | `go -C backend test ./...` | 全部通过（runtime 包 45 个用例） |
| Go 静态检查 | `go -C backend vet ./...` | 无告警 |
| Go 格式 | `gofmt -l backend` | 无输出 |
| Python 语法 | `python -m compileall manifest/runtime-agent` | 通过 |
| Python 规范 | `python -m ruff check manifest/runtime-agent` | All checks passed |
| Python 单元测试 | `python -m unittest discover -s tests`（runtime-agent 目录） | 17 个用例通过 |
| 前端构建 | `npm --prefix frontend run build` | 通过 |
| 端到端 | 真实启动后端 + HTTP 调用 14 个接口 | 见 19.5 |

### 19.5 端到端验证记录（Windows 本机，local 模式）

| 用例 | 结果 |
|---|---|
| `GET /platform` 首次响应 | 42ms（探测已移到进程启动阶段） |
| `GET /platform` 运行时字段 | `agent_mode=local`、9 种类型、`systemd=false`（实测无 systemd） |
| `GET /runtime/list` 空注册表 | 返回 legacy 合成项与 warning，不报错 |
| `POST /runtime/create` docker | 成功，`capabilities.persistent=true` |
| `GET /runtime/status` | 返回 docker daemon 真实错误，1.4s 内返回而非超时 |
| `POST /runtime/action` restart | 502 + 结构化 `job_result`，含真实原因 |
| `GET /runtime/logs`（nohup + 日志文件） | 正确返回 `line1/line2/line3` 与来源路径 |
| `GET /runtime/events` | 正确返回创建/动作事件时间线 |
| `POST /runtime/migrate/plan` 到 systemd | `blockers=["当前环境没有 systemd，不能作为迁移目标"]` |
| `POST /runtime/migrate/plan` 同类型 | 400「源运行时与目标运行时类型相同」 |
| `POST /runtime/migrate/apply` 无效 token | 502「迁移计划不存在或已过期」 |
| `POST /runtime/detach` 非接管项 | 400「该运行时不是接管而来的」 |
| `POST /runtime/delete` | 成功，list 数量正确递减 |
| 能力矩阵 | `systemd.canStart=false`、`nohup.canStart=true`、`docker.canStart=true` |

### 19.6 本轮未做（明确跳过）

- §9 全部：SteamCMD 下载、三段式脚本生成、一次性容器安装、上传压缩包、目录复制、安装向导页面、`/server-install/*` 接口。
- §12 前端"服务端安装页"。
- 服务端"更新"链路的 provider 化（`run-maintenance.sh` 仍硬编码 `systemctl`，D4 未修）：因为更新属于本轮跳过范围，改了会与后续安装实现冲突。
- `server_maintenance.go` / `server_repair.go` / `server_diagnostics.go` 内部改为走 provider（D5 未修）：诊断字段仍围绕 systemd，非 systemd 运行时在诊断页显示为空。
- 多实例注册表字段虽已预留（`ID`/`Name`），但 UI 仍以单实例为主视角。

---

## 20. D4 / D5 补做记录（2026-11-17）

用户指出：**D4 与 D5 并不依赖安装/更新链路，属于独立链路，完全可以做**。此前把它们归入"本轮跳过"是判断错误，现补做。

### 20.1 D4：维护/修复脚本硬编码 systemctl

**问题**：`run-maintenance.sh` 与 `repair-addons.sh` 直接执行 `systemctl stop/start/restart`。用户接管 screen / tmux / nohup / docker 后，点一次"更新"会把服停掉，而且**再也起不来**——systemd 里根本没有这个 unit。

**修复方案（三层）**：

| 层 | 文件 | 变更 |
|---|---|---|
| 面板 | `backend/logic/runtime_control.go`（新增） | `WriteRuntimeControlSnapshot()` 把当前默认运行时的 kind/target/spec 写到 `<control>/runtime-control.json`；`activeRuntime()` 优先返回非 legacy 条目 |
| 面板 | `backend/logic/server_maintenance.go` | `resolveMaintenanceTarget()` 优先用运行时的工作目录，回退启动项解析；`GetServerMaintenanceStatus()` 同步改造；提交任务前写快照 |
| 面板 | `backend/logic/server_repair.go` | 提交处置前写快照；`HasSystemd()` 判断改为 `HasSystemd() \|\| managedRuntimeAvailable()` |
| 宿主 | `manifest/systemd/runtime-control.sh`（新增） | 读快照 → 解析 kind/target/spec → 调 Agent `--exec`；无快照返回退出码 2 让调用方回退 |
| 宿主 | `manifest/runtime-agent/l4d2_runtime_agent.py` | 新增 `run_inline()`：`--exec <action> --kind <kind> --target <json> --spec <json>` 同步执行并输出 JSON |
| 宿主 | `manifest/systemd/run-maintenance.sh` | 新增 `service_control()`：优先 `runtime-control.sh`，退出码 2 才回退 systemctl |
| 宿主 | `manifest/systemd/repair-addons.sh` | 新增 `service_control()`，`restart_service()` 改为走它 |
| 部署 | `manifest/install/install.sh` | 安装时把 `runtime-control.sh` 复制到 `/opt/l4d2-manager/` 并 `chmod +x` |

**实现中发现的真实缺陷**：

1. **`$?` 陷阱**：把子命令写在 `if output=$(...)` 条件里，`$?` 拿到的是 `if` 的结果而不是子命令退出码，导致"退出码 2 回退"逻辑永远失效。改为先执行再判 `$?`。
2. **前置校验顺序**：`RequestServerMaintenance` 在写快照前就校验启动项，接管 screen 后启动项不可用会直接 500，快照永远写不出去。改为先解析目标再写快照。
3. **状态页同样写死启动项**：`GetServerMaintenanceStatus` 返回的 `install_directory` 为空、`service_name` 恒为 `l4d2.service`。改为同样走 `resolveMaintenanceTarget()`。

### 20.2 D5：诊断字段围绕 systemd

**问题**：`CollectServerDiagnostics` 只有 systemd 走宿主 path unit；`DiagnosticsAvailable()` 返回 `HasSystemd()`。接管 screen / tmux / nohup / docker 后诊断页整片空白，且提示"当前平台不支持 systemd 诊断"。

**修复方案**：

| 层 | 文件 | 变更 |
|---|---|---|
| 面板 | `backend/logic/runtime_diagnostics.go`（新增） | `CollectRuntimeDiagnostics()`：走 Manager 的 `Status()` + `Logs()`，把运行时状态、PID、端口归属、日志尾部填进同一个 `ServerDiagnostics` 结构，复用现有 `BuildServerDiagnosticHints` 全部规则 |
| 面板 | `backend/logic/server_diagnostics.go` | `CollectServerDiagnostics` 优先调用 `CollectRuntimeDiagnostics`；`DiagnosticsAvailable()` 改为 `HasSystemd() \|\| managedRuntimeAvailable()` |
| 前端 | `frontend/src/views/ServerMaintenance.vue` | 更新提示文案：`hasSystemd \|\| supportsRuntimeManagement` 时说明"自动识别 systemd/screen/tmux/nohup/docker" |

**关键设计**：legacy 合成项仍走原 systemd 桥接，保证老部署行为完全不变；只有用户显式接管过的运行时才切到新通道。

### 20.3 实现中发现的额外缺陷

| 编号 | 缺陷 | 修正 |
|---|---|---|
| I12 | `Create()` 不传 `enabled` 时 `Enabled` 默认 false | 用户点"新建/接管"就是想让它立刻生效；默认 false 会导致诊断/维护继续走旧链路，而界面看起来已接管成功。改为默认启用，controller 用 `*bool` 区分"没传"与"传了 false" |
| I13 | `agentlib` 与 `providers` 之间空行导致 ruff I001 反复报错 | 两个包都被判定为第一方，必须同组无空行；新增 `manifest/runtime-agent/ruff.toml` 显式声明 `known-first-party`，保证从仓库根和从子目录执行结果一致 |

### 20.4 测试结果

| 类型 | 命令 | 结果 |
|---|---|---|
| Go 单元测试 | `go -C backend test ./...` | 全部通过 |
| Go 静态检查 | `go -C backend vet ./...` | 无告警 |
| Go 格式 | `gofmt -l backend` | 无输出 |
| Python 规范 | `python -m ruff check manifest/runtime-agent` | All checks passed（仓库根与子目录一致） |
| Python 单元测试 | `python -m unittest discover -s tests` | 22 个用例通过（新增 5 个 `--exec` 用例） |
| 前端构建 | `npm --prefix frontend run build` | 通过 |

新增 Go 测试（`backend/logic/runtime_control_test.go`，8 个）：
- 无运行时 → 不写快照、返回 handled=false
- 有托管运行时 → 快照含 kind/target/spec/runtime_id
- 同时存在 legacy 与托管项 → 优先返回托管项
- 跳过停用/归档项
- 无托管运行时 → 诊断回退旧链路
- `runtimeServiceLabel` 四种句柄
- `buildRuntimeStatusText` 内容完整性

### 20.5 端到端验证（Windows 本机，local 模式）

| 用例 | 结果 |
|---|---|
| 创建 nohup 运行时（不传 enabled） | `enabled=True`（I12 修复生效） |
| `POST /server-startup/diagnostics` | `available=True`、`service=诊断测试`（运行时名称）、`active=stopped`、`port=27019`（运行时端口）、`journal=2 行`（运行时日志） |
| `POST /server-maintenance/update/check` | 成功写入 `runtime-control.json`，`kind=nohup`、`target.pid_file`、`spec.port=27019` 全部正确 |
| `POST /server-maintenance/status` | `install_directory` 为运行时工作目录、`service_name` 为运行时名称（不再是 `l4d2.service`） |

### 20.6 仍未做

- `diagnose-server.sh` 本身仍只采集 systemd 数据。当前实现是"面板侧改走运行时"，宿主机脚本只在 legacy 路径下被调用，因此不影响非 systemd 场景；若后续要让 legacy 之外的路径也复用该脚本，需要再改脚本。
- §9 安装/下载整章（SteamCMD 三段式、一次性容器、安装向导）仍按用户指令跳过。

---

## 21. 远端部署与缺陷闭环（2026-11-17）

部署目标：`101.43.105.208`（ubuntu，SSH 密码由用户提供），面板入口 `http://101.43.105.208:27020`。
镜像 `l4d2-manager-next:runtime-20261117`，容器 `l4d2-manager`；回滚容器 `l4d2-manager-prev-20261117` 保留。

### 21.1 远端部署结果

| 项 | 结果 |
|---|---|
| 镜像构建 | `l4d2-manager-next:runtime-20261117`（基于 `webfix-20261117` 增量） |
| 容器 | `l4d2-manager` Up，挂载 `/opt/l4d2-manager/control→/control`、`/run/systemd/system→/host/systemd:ro`、`/proc→/host/proc:ro` |
| 环境变量 | `L4D2_HOST_SYSTEMD_DIR=/host/systemd`、`L4D2_GAME_PATH=/left4dead2` |
| 宿主机 Agent | `/opt/l4d2-manager/runtime-agent/`，`l4d2-runtime.path` 已 `enable --now` |
| 宿主机脚本 | `/opt/l4d2-manager/runtime-control.sh`、`run-maintenance.sh`（10188 字节，0755）、`repair-addons.sh` |
| 前端 | `assets/app-CUqgKHT7.js`（284,339 字节）含 `runtime/detect`、`runtime/adopt`、`runtime/migrate/plan`、`runtime_management` |
| Agent 单测 | 远端 27 个用例通过（含 `parse_port_hostport`、`rejects_panel_helper_units`、DockerDetect ×3） |

### 21.2 远端端到端验证（真实生产服）

| 接口 | 结果 |
|---|---|
| `GET /platform` | 200，`systemd:true`、`agent_mode:"bridge"`、`runtime_management:true`、9 种 `runtime_kinds` |
| `POST /runtime/detect` | 从 7 条候选收敛到 1 条真实游戏服（`l4d2.service`，port=12138）；排除面板 6 个辅助 unit、user scope 重复、docker null 崩溃 |
| `POST /runtime/adopt` | 接管成功，`id=main`、`kind=systemd`、`origin=detected` |
| `GET /runtime/list` | 200，`adopted=true`、`managed=true`、`target.unit_name=l4d2.service`、`spec` 完整（含 `+hostport 12138`） |
| `GET /runtime/status?id=main` | 200，`state=running`、`port_listening=true`、`port_owners=[srcds_linux]`、`rcon_reachable=true`、`source=agent` |
| `POST /server-startup/diagnostics` | 200，`status_text` 为"运行时: 主服 / 类型: systemd 服务 / 状态: 运行中 / PID / 端口已监听 / 日志来源: journalctl" |
| `POST /server-maintenance/update/check` | 202 → 34 秒后 `status=success`、`installed_build_id=23990100`、`latest_build_id=23990100`、`update_available=false` |
| `POST /runtime/action` (restart) | 200，`message="已重启: l4d2.service"`、`data.pid=2289103`、`agent_version=1.0.0` |
| 重启后 `GET /runtime/status` | 200，新 PID 2289103、端口监听、RCON 可达（D4 自动重启链路闭环） |
| `runtime-control.json` | `kind=systemd`、`target.unit_name=l4d2.service`、`spec` 完整、`runtime_id=main`、`origin=detected` |

**验证前置条件**：`POST /rcon/getstatus` 显示 `Players: "0/16"`，确认无玩家在线后才执行重启，避免打断真实玩家。

### 21.3 本轮远端缺陷清单（6 个）

| 编号 | 缺陷 | 证据 | 修正 |
|---|---|---|---|
| R1 | `run-maintenance.sh` 把 SteamCMD 客户端自更新塞进 `timeout 180` 同一条命令 | `maintenance-result.json` = `[16%] Downloading update (5,858 of 36,566 KB)`；`journalctl -u l4d2-maintenance` 7 次 `status=1/FAILURE`；`appmanifest_222860.acf` `buildid 23990100 / UpdateResult 0` 说明游戏本体已最新 | 自更新与查询分阶段计时；`L4D2_STEAMCMD_SELF_UPDATE_TIMEOUT` / `L4D2_STEAMCMD_QUERY_TIMEOUT` 可覆盖。**超时值后续在 §29 从 1200 调到 3600** |
| R2 | check 与 update 共用 `l4d2-update.log`，客户端自更新的 "Downloading update" 让用户误判为游戏本体在更新 | `l4d2-update.log` 停在 12:03，`l4d2-check.log` 12:39 新增 3622 字节 | 新增 `CHECK_LOG`，check 全部写入 `l4d2-check.log` |
| R3 | 业务失败 `exit 1` 被 systemd 标记 `failed`，前端只显示"服务失败"看不到真实 message | 7 次 `status=1/FAILURE` | 全部 `exit 1` 改 `exit 0`，结论只走 `maintenance-result.json` |
| R4 | `verify_remote.py` 用固定 `/tmp/api_out.json`，root 先创建 0644 后普通用户写入静默失败 → 所有接口返回同一份旧 JSON（假证据） | 每个接口返回内容完全相同 | 改 `mktemp /tmp/api_XXXXXX.json` 每请求独占 + 用完删除 |
| R5 | `bash -c 'cd -- "$1" && shift && shift && timeout "$2" ...'`：`$2` 在 `shift` 前已展开，`timeout` 拿到目录路径 | 自更新后查询必然失败 | 改 `'dir="$1"; limit="$2"; shift 2; cd -- "$dir" && timeout "$limit" ...'`，显式 `_ "$steam_dir" "$timeout"` 传参 |
| R6 | `remote_cli.main()` 用 `" ".join(argv[1:])` 直传 shell，含空格/引号的参数被二次解析 | `probe_api.py` 传 JSON 时 `{\"id\":\"main\"...}` 被转义破坏 → HTTP 400 | `probe_api.py` 内部对 body 做 `'` 转义后包单引号；`remote_cli.py` 本身仍待改为 `shlex.quote` |

### 21.4 build/ 验证脚本族（仅本地开发用，不随镜像分发）

| 脚本 | 用途 |
|---|---|
| `remote_cli.py` | paramiko 连接、`run()` / `sudo_run()`（`sudo -S -p ''` 走 stdin 传密码） |
| `check_brief.py` | 快速探活：steamcmd 进程、预热日志尾、维护服务状态、游戏服状态、12138 监听 |
| `inspect_update.py` | control 目录清单与时间戳、`update.log` 头尾、maintenance request/result、全部 `l4d2*` unit（含 `.path`） |
| `inspect_failure.py` | 完整打印 request/result JSON、日志 grep 错误、`systemctl status l4d2-maintenance` |
| `inspect_steam.py` | `appmanifest_222860.acf`、`/home/ubuntu/Steam` 各子目录、bootstrap 日志、`steamcmd/package` 暂存件、游戏目录大小、`srcds_linux` 权限 |
| `warm_steamcmd.py` | 后台启动一次 SteamCMD 客户端自更新预热（`nohup ... &`，不阻塞） |
| `sync_maintenance.py` | SFTP 上传 `run-maintenance.sh` → 远端 `bash -n` 校验 → `sudo install -m 0755` |
| `verify_remote.py` | 面板全链路：`/platform`、`/runtime/list`、`/runtime/status`、诊断、维护状态、快照文件 |
| `verify_static.py` | 容器挂载与环境变量、`docker ps`、首页 bundle 名 |
| `verify_bundle.py` | 抓远端 bundle 并 grep endpoint 标记（中文需用 `\uXXXX` 转义字面量） |
| `probe_api.py` | 从容器 `printenv` 读真实管理员密码后调任意接口，body 用单引号包裹并转义 |

### 21.5 教训清单

1. **`$?` 在 `if` 条件里不可靠**：必须 `output=$(cmd); code=$?` 再判断。
2. **`bash -c` 里先取值再 `shift`**：位置参数在展开时已确定，`shift` 不会改变已展开的 `$n`。
3. **固定 `/tmp` 路径是假证据制造机**：root 创建的文件普通用户写不进去，`curl -o` 静默失败。一律 `mktemp`。
4. **esbuild 会把中文转成 `\uXXXX`**：远端 grep 中文字面量必然 0 命中，要 grep 英文 endpoint 标记或用转义形态。
5. **WSL 无 `/bin/bash`**：本地 `bash -n` 不可用，shell 语法校验必须在远端做。
6. **SteamCMD 客户端自更新与业务查询必须分阶段分超时**：合并超时会导致自更新永远完不成，形成永久失败循环，且每次从 0% 重下。
7. **验证生产服前先查在线人数**：`/rcon/getstatus` 的 `Players` 为 `0/16` 才允许重启类操作。
8. **"下载 37MB"不等于游戏更新**：SteamCMD 自更新包 36,566 KB；游戏本体更新是 GB 级。判定要看 `appmanifest` 的 `SizeOnDisk` / `BytesDownloaded` 与 `srcds_linux` 是否仍在监听。

### 21.6 仍未做

- 前端「运行时管理」页面在远端未做人工点击演练（接口层已全绿，检测抽屉/接管向导/迁移向导的 UI 交互待用户刷新验证）。
- `POST /runtime/migrate/plan|apply` 未在远端真实执行（迁移会停服并改写 unit，需用户在窗口期授权）。
- screen / tmux / nohup / bare provider 远端无环境可测（远端只有 systemd + docker）。
- `remote_cli.py` 参数直传 shell 的问题未修（R6），当前通过 `probe_api.py` 绕开。

---

## 22. 第二台机器部署：cc雨云（2026-11-22）

部署目标：`123.99.201.252:39322`（root），Ubuntu 24.04.1，Docker 29.7.2。
镜像 `l4d2-manager-next:enhanced-20261122`，容器 `l4d2-manager`；回滚容器 `l4d2-manager-prev-20261122`。

### 22.1 与 101.43 的部署差异

这台机器不是 Compose 部署，没有 `/opt/l4d2-manager` 目录，游戏服是**宿主机原生 systemd**（`l4d2.service`），不是 Docker 容器。

| 项 | 101.43.105.208 | 123.99.201.252 |
|---|---|---|
| 面板部署 | Compose（`/opt/l4d2-manager/compose.yaml`） | `docker run` 裸命令 |
| 游戏服 | systemd `l4d2.service`，路径 `/home/ubuntu/steamcmd/l4d2` | systemd `l4d2.service`，路径 `/data/l4d2-server` |
| 启动方式 | `srcds_run` 直接作为 ExecStart | `ExecStart=/data/l4d2-server/start_server.sh`（包装脚本） |
| 面板运行用户 | UID 1000（l4d2） | root |
| SteamCMD | 有（`/home/ubuntu/steamcmd`） | **没有**（只有 Steam 客户端下载好的游戏文件） |
| 原有卷 | control/data/plugins/game | `l4d2_l4d2-plugins`、`l4d2_l4d2-manager-data` |

因为游戏服是原生 systemd，运行时的 `systemd` provider 直接可用，不需要 Docker 通道。

### 22.2 部署步骤

镜像增量构建（不传源码和 plugins，`/plugins` 是独立卷）：

```dockerfile
FROM l4d2-manager-next:custom
RUN rm -rf /static && mkdir -p /static
COPY static/. /static/
COPY l4d2-manager /l4d2-manager
COPY systemd /manager-systemd
COPY runtime-agent /manager-runtime-agent
```

容器重建时在原有参数上补 3 项挂载和 4 个环境变量：

| 新增 | 用途 |
|---|---|
| `-v /run/systemd/system:/host/systemd:ro` | 面板判定宿主机有 systemd |
| `-v /etc/systemd/system:/host/systemd-units:ro` | 读取真实 unit 文件解析启动项 |
| `-v /opt/l4d2-manager/control:/control` | 与宿主机 Agent 交换任务 |
| `L4D2_HOST_SYSTEMD_DIR=/host/systemd` | 宿主 systemd 目录 |
| `L4D2_RUNTIME_USER=root` | 游戏服以 root 运行 |
| `L4D2_STARTUP_SERVICE_NAME=l4d2.service` | 服务名 |
| `L4D2_STARTUP_UNIT_PATH=/host/systemd-units/l4d2.service` | 启动项文件路径 |

原有环境变量通过 `docker inspect --format '{{range .Config.Env}}'` 导出到 `container.env` 再用 `--env-file` 传入，**密码不落日志、不经过中间文件明文打印**。

宿主机安装 `apply-startup.sh`、`run-maintenance.sh`、`repair-addons.sh`、`runtime-control.sh`、`diagnose-server.sh` 与 5 组 path/service unit；`l4d2-runtime.path`、`l4d2-maintenance.path`、`l4d2-repair.path`、`l4d2-diagnostics.path` 全部 `enable --now`。

### 22.3 本轮发现的缺陷

| 编号 | 缺陷 | 证据 | 修正 |
|---|---|---|---|
| R7 | `ExecStart` 指向包装脚本时端口恒为 0，健康检查误判"启动中" | 扫描结果 `spec.port=0`；`/runtime/status` 返回 `state=starting`、`port_listening=false`；实际 `ss` 显示 27015 在监听 | `SystemdProvider._live_port()`：spec.port 为 0 时读实际 srcds 进程 argv，先按 MainPID 精确匹配，再按 cgroup 里的 unit 名匹配子孙进程；`inspect()` 与 `_detect_scope()` 两处都调用 |
| R8 | rebase 解冲突时误删 `manager.Dockerfile` 里 `COPY ./manifest/runtime-agent` | `git diff` 显示该行被删 | 恢复；否则 `install.sh` 从 `/manager-runtime-agent` 提取 Agent 会失败 |

R7 是真实部署里很常见的形态：用户用 `start_server.sh` 之类的包装脚本做 tickrate 选择、RCV 密码写入、32 位库修复，`ExecStart` 里自然没有 `+hostport`。修复前这类部署接管后界面永远显示"启动中"。

### 22.4 验证结果

| 接口 | 结果 |
|---|---|
| `GET /platform` | 200，`agent_mode=bridge`、`runtime_management=true`、9 种 `runtime_kinds` |
| `POST /runtime/detect` | 200，候选 `l4d2.service`，`port=27015`（修复后），`confidence=0.95` |
| `POST /runtime/adopt` | 200，`id=main`、`spec.port=27015`、`origin=detected` |
| 重复 `POST /runtime/detect` | 200，`adoptable=false`、`reason=该服务端已被运行时 main 接管`（正确拒绝重复接管） |
| `GET /runtime/status?id=main` | 200，`state=running`、`port_listening=true`、`port_owners=[srcds_linux]`、`rcon_reachable=true` |
| `POST /runtime/action` restart | 200，`message=已重启: l4d2.service`、新 PID 459183 |
| 重启后端口 | 约 25 秒后 `srcds_linux` 重新监听 27015 |
| `POST /server-startup/get` | 200，`exec_start=/data/l4d2-server/start_server.sh`、`port=27015`、`working_directory=/data/l4d2-server` |
| 前端 bundle | `app-B1nd2q81.js`，与本地构建产物一致 |

Agent 单测：本地 31 个用例通过（新增 4 个端口兜底用例），宿主机同一份代码跑通。

### 22.5 已知限制

- **这台机器没有 SteamCMD**，`找 steamcmd.sh` 为空。服务端更新/检查更新功能不可用，`/server-maintenance/status` 的 `steam_directory` 落到 `/data`。需要先装 SteamCMD 才能用；当前 `appmanifest_222860.acf` 的 `buildid=23990100`。

---

## 23. 启动项保存与包装脚本参数解析修复（2026-11-23）

### 23.1 缺陷

用户反馈"保存启动项"报错：

```
读写启动项文件失败: open /host/systemd-units/l4d2.service: read-only file system
```

同页还有两个相关问题：
- 启动参数区显示"暂无启动参数"，同时红字提示"缺少 -game left4dead2"
- 服务端维护页提示 SteamCMD 不可执行（已在 §22 修复为可操作提示）

### 23.2 根因（实测证据）

| 现象 | 证据 | 结论 |
|---|---|---|
| 写 unit 失败 | 容器内 `touch /host/systemd-units/.writetest` → `Read-only file system`；`/proc/mounts` 显示 `ro,relatime` | 只读挂载，与 root 无关 |
| 容器身份 | `docker exec l4d2-manager id` → `uid=0(root)` | 不是权限问题 |
| 环境变量 | `L4D2_STARTUP_UNIT_PATH=/host/systemd-units/l4d2.service` | 旧代码无条件写该路径 |
| 参数为空 | `ExecStart=/data/l4d2-server/start_server.sh`（只有脚本路径） | 真实参数在脚本内部，unit 里读不到 |
| path unit 未生效 | `systemctl is-enabled l4d2-startup.path` → `disabled` | 保存后无人应用快照 |
| 应用脚本无法执行 | `/usr/bin/env: 'bash\r': No such file or directory` | `apply-startup.sh` 是 CRLF 行尾 |

### 23.3 代码修改

**`backend/logic/server_startup.go`**

| 函数 | 变更 |
|---|---|
| `serverStartupUnitPath()` | 只表示"部署的真实 unit 路径"，不再参与写入决策 |
| `serverStartupSnapshotPath()` | 新增：返回 `/control/l4d2.service`（Windows 为 `l4d2.bat`） |
| `serverStartupReadPath()` | 新增：真实 unit 与快照谁新读谁，宿主机手工改过 unit 时以 unit 为准 |
| `startupDirWritable()` | 新增：真正创建临时文件探测可写性，`Stat` 成功不代表能写 |
| `startupUnitWritablePath()` | 新增：可写就原地写，只读则退回快照；两者都不可写时给出带 `-v` 提示的错误 |
| `applyRunningProcess()` | 新增：`ExecStart` 是包装脚本时，从运行进程补齐真实参数与端口 |

**`backend/logic/process_commandline.go`（新增）**

- `HostProcRoot()`：优先 `L4D2_HOST_PROC_DIR`（容器为 `/host/proc`），否则 `/proc`
- `FindServiceProcess(unitName)`：按 cgroup 归属精确匹配；同 unit 下 `srcds_run` 与 `srcds_linux` 都命中时取参数最长者
- 无 cgroup 归属时候选必须唯一才采纳，避免多实例取错进程
- `resolveProcessExec()`：把 `./srcds_linux` 按进程 cwd 还原成绝对路径
- `isWrapperExecStart()`：可执行文件不以 `srcds` 开头即视为包装脚本

**`frontend/src/views/ServerStartup.vue`**

- 包装脚本场景解析运行进程命令行，而不是解析只有脚本路径的 `ExecStart`
- 过滤 `+map`：那是 `srcds_run` 运行时追加的当前地图，不属于启动参数
- 包装脚本场景改为信息提示，不再误报"缺少 -game left4dead2"
- 保存确认弹窗说明：保存后 unit 直接调用 `srcds`，脚本内的 tick 模板复制等逻辑不再执行

### 23.4 部署侧修正

- `manifest/systemd/*.sh` 与 `*.path` / `*.service` 全部转为 LF 行尾（原来 8 个文件是 CRLF，shebang 损坏）
- 宿主机 `l4d2-startup.path` 已 `enable --now`
- 容器补 `L4D2_HOST_PROC_DIR=/host/proc`

### 23.5 端到端验证

| 步骤 | 结果 |
|---|---|
| `POST /server-startup/get` | 200，`exec_start_is_wrapper=true`、`process_command=./srcds_linux -game left4dead2 -insecure -tickrate 100 -condebug +ip 0.0.0.0 +hostport 27015 +exec server.cfg +map c1m1_hotel`、`process_pid=459222`、`process_executable=/data/l4d2-server/srcds_linux` |
| `POST /server-startup/update`（写绝对路径） | 200，`reload_message=启动项已保存，宿主机 path unit 会自动重载` |
| 快照生成 | `/opt/l4d2-manager/control/l4d2.service` 1042 字节，含中文注释块与绝对路径 `ExecStart` |
| 真实 unit 未被面板直接改 | `mtime` 仅在 path unit 应用后变化，内容先备份到 `l4d2.service.bak` |
| `l4d2-startup.service` | `status=0/SUCCESS`，结果 JSON `status=success`、`daemon_reload=true` |
| `GET` 回读 | `reload_status=success`、`reload_message=systemd 服务定义已重载` |
| 恢复包装脚本后 | `reload_status=success`、`exec_start_is_wrapper=true`、参数仍从运行进程解析 |

单元测试 `TestUpdateServerStartupFallsBackToWritableSnapshot`、`TestServerStartupReadPathPrefersNewerSnapshot`、`TestFindServiceProcess*`、`TestIsWrapperExecStart` 全部通过（只读模拟用例在 root 与 Windows 下自动跳过）。

### 23.6 已知取舍

包装脚本 `start_server.sh` 做了三件事：复制 `server.cfg.<tick>tick`、写入 `rcon_password`、把 `bin/libstdc++.so.6` 改名为 `.so.6s`。实测三项当前均已处于目标状态（`server.cfg` 与 100tick 模板只差 rcon 密码一行），因此面板保存改成直接调用 `srcds` 行为等价。若用户仍希望保留脚本，可执行 `cp /root/l4d2-startup-backup-20261223/l4d2.service.orig /etc/systemd/system/l4d2.service && systemctl daemon-reload` 回退。

---

## 24. 服务器名多插件检测（2026-11-23）

### 24.1 需求

同一台服务器可能同时装多个服务器名插件（L4D2 中文服务器名、Neko ServerName、hostname 等）。
**不能按 smx 文件名识别**，必须按插件实际加载后的运行时名字判断，并且多个并存时要全部检测出来。

### 24.2 实现

`backend/logic/server_hostname.go`（新增）：

| 能力 | 实现 |
|---|---|
| 运行时生效值 | RCON `hostname`，正则 `"hostname"\s*=\s*"..."`，兼容裸值回显 |
| 插件识别 | RCON `sm plugins list` → 正则取引号内运行时插件名 → 关键词过滤 |
| 关键词 | `hostname`、`server name`、`servername`、`server_name`、`服名`、`服务器名`、`servertitle` |
| 配置来源 | 6 个候选文件按优先级探测，含纯文本、KeyValues、按端口嵌套三类格式 |
| 模板识别 | `\{[^}]*[A-Za-z][^}]*\}` 才算占位符，避免把 `[4特]` 误判 |
| 编码兼容 | UTF-8 优先，失败用 GBK 解码，去 BOM |

关键词用完整词组而非 `server`，避免把 `Server Message System` 误判成服务器名插件。

`ResolveHostname` 优先级：**RCON 生效值 > 非模板文件值 > 模板值**。
因为 `nekocustom.cfg` 里存的是 `CC偏纯净多特{servernum}服[...]` 这类模板，直接展示对用户没意义。

### 24.3 前端

`ServerInfo.vue` 分三块展示证据：运行中实际生效值、已加载的服务器名插件标签、配置文件列表（模板来源加橙色标记）。
标题旁的文件名从写死的 `l4d2_hostname.txt` 改成按检测结果动态显示。

### 24.4 实测结果（cc雨云）

```
hostname        = CC偏纯净多特155服[4特10秒][重启:0|路程:0%][计时:00m:03s]
hostname_source = rcon
plugins         = ['L4D2 中文服务器名', 'Neko ServerName']
source: hostname (SourceMod 服务器名) | configs/hostname/hostname.txt | template=False | raw=偏纯净三方多特tick100
source: Neko ServerName | data/nekocustom.cfg | template=True | raw=CC偏纯净多特{servernum}服[{specials}特{times}秒]...
```

两个服务器名插件都被识别出来，且模板文件被正确标记。

单测覆盖：RCON 两种回包格式、运行时插件名解析、关键词排除误判、模板识别、多来源探测、优先级选择、Neko 结构写入保留、GBK/BOM 解码。

---

## 25. 插件 cfg 中文注释与 README 补齐（2026-11-23）

### 25.1 cfg 注释翻译

远端 `cfg/sourcemod` 共 109 个文件、1973 行英文注释。先分类：

| 类型 | 去重条数 | 处理 |
|---|---|---|
| 描述行为的自然语言 | 184 | 翻译 |
| `ConVars for plugin "xxx"` 头 | 95 | 保留原文 |
| `Default:` / `Maximum:` / `Minimum:` | 195 | 保留原文 |
| `This file was auto-generated` | 11 | 保留原文 |
| `Requires: xxx.smx` | 4 | 保留原文 |

机器生成的元信息不翻译：它们是插件重新生成 cfg 时会被覆盖的产物，翻译没有意义。

写入格式保留原文，中文追加在后面：

```cfg
// Specifies how many maps to include in the vote.   ｜  指定投票中包含多少张地图。
sm_mapvote_include "5"
```

这样可追溯，且不会破坏 SourceMod 的注释解析。实测写入 192 行，涉及 29 个文件，备份在 `/root/cfg-translations-backup/<时间戳>/`。

运行时验证：通过 RCON 执行 `exec sourcemod/sourcemod.cfg`、`exec sourcemod/mapchooser.cfg`、`exec sourcemod/l4d2_skill_detect.cfg`、`exec sourcemod/l4dinfectedbots.cfg`，四条命令均无报错，插件数量保持 189 个不变。

### 25.2 插件 README 补齐

插件商店 187 个目录中，60 个原本有 README，127 个没有。

生成原则：**只用真实证据，不编造**。每份说明由以下可核实信息拼成：

1. 目录名里的 `必选-` / `可选-` 前缀
2. 目录名括号内的作者（排除纯版本号）
3. 插件目录内实际的 `.smx` 文件
4. gamedata 签名文件是否存在
5. cfg 里的 ConVar 与中文说明

关键设计：**配置不在插件商店目录里**。插件商店多数只放 smx，实际 cfg 由插件首次运行时写到游戏服 `cfg/sourcemod` 下，靠文件名对应（`l4d2_gift.smx` ↔ `l4d2_gift.cfg`）。所以生成器建立了 109 个文件名索引做跨目录关联，把配置说明补进 README。

匹配 ConVar 的正则必须允许大写字母（`l4d2_Kill_enabled_health`），否则会漏。被 cfg 折行截断的半句描述会被丢弃而不是展示，避免误导。

结果：187/187 全部有 README，其中 51 个含配置说明，21 个识别出作者。

### 25.3 脚本

全部在 `build/` 下（已 gitignore）：

| 脚本 | 用途 |
|---|---|
| `cfg_collect_comments.py` | 采集远端 cfg 英文注释 |
| `cfg_classify.py` | 分类，区分「值得翻译」与「机器生成」 |
| `cfg_translations.py` | 翻译词典（185 条） |
| `apply_cfg_translations.py` | 远端写入，保留原文追加中文 |
| `generate_plugin_readmes.py` | 生成 README，支持跨目录 cfg 关联 |

---

## 26. 切换为直调 srcds_linux（2026-11-23）

### 26.1 为什么要两步

直接照抄 `srcds_run` 的命令行会起不来，因为 `srcds_run` 做了两件 unit 里看不到的事：

| srcds_run 的行为 | 直调时必须补什么 |
|---|---|
| 第 11 行 `export LD_LIBRARY_PATH=".:bin:$LD_LIBRARY_PATH"` | systemd drop-in 注入 `Environment=LD_LIBRARY_PATH=.:bin` |
| 第 142-144 行自动补 `+map c1m1_hotel` | ExecStart 显式写 `+map c1m1_hotel` |

第一条容易被忽略：`ldd` 查不出缺失（这是链接期依赖），但运行时 `dlopen bin/engine_srv.so` 依赖这个路径。

第二条是实际踩到的坑：只补 `LD_LIBRARY_PATH` 时进程能起来、端口在监听，但 `sm plugins list` 返回 0，`meta version` 提示 `You must change the map to activate Metamod:Source`，`changelevel` 报 `Can't changelevel, not running server`——服务器停在 hibernating，Metamod 从未激活。

### 26.2 落地方式

```bash
# /etc/systemd/system/l4d2.service.d/10-ld-library-path.conf
[Service]
Environment=LD_LIBRARY_PATH=.:bin
```

```
ExecStart=/data/l4d2-server/srcds_linux -game left4dead2 -insecure -tickrate 100 \
  -condebug +ip 0.0.0.0 +hostport 27015 +exec server.cfg +map c1m1_hotel
```

启动项仍由面板写入快照，宿主机 `l4d2-startup.path` 校验后安装，链路不变。

### 26.3 验证结果

| 项 | 结果 |
|---|---|
| `systemctl is-active` | `active`，`NRestarts=0` |
| 进程 PPID | `1`（直接由 systemd 拉起，不再有 `srcds_run` 中间层） |
| `LD_LIBRARY_PATH` | `.:bin` 生效 |
| 端口 | `0.0.0.0:27015` UDP 监听 |
| 插件数 | **189**（与切换前一致） |
| 服务器名 | `CC偏纯净多特155服[4特10秒][重启:0\|路程:0%][计时:00m:06s]`，插件正常生效 |
| 启动项接口 | `reload_status=success`、`exec_start_is_wrapper=false` |

### 26.4 回滚

```bash
cp -a /root/l4d2-direct-switch-20261123/l4d2.service.before /etc/systemd/system/l4d2.service
rm -f /etc/systemd/system/l4d2.service.d/10-ld-library-path.conf
systemctl daemon-reload && systemctl restart l4d2.service
```

备份含 `l4d2.service.before`（原 unit）、`snapshot.before`（原快照）。
`start_server.sh` 原地保留，未删除。

---

## 27. 插件 README 补全命令与用法（2026-11-23）

### 27.1 问题

第一版 README 只有配置项，缺「怎么用」：玩家不知道按键和聊天命令。

### 27.2 数据来源（关键决策）

试过三条路，只有第三条可用：

| 方案 | 结果 |
|---|---|
| 从 smx 字符串池提取 `RegConsoleCmd` | **失败**：字符串池被编译器混淆，只有 `.code`/`.data` 段名和乱码 |
| 按目录名匹配运行时插件名 | **只能覆盖 41/91**：目录是中文（`击杀特感掉落礼盒`），运行时是英文（`L4D2 Gift`） |
| `sm plugins info <序号>` + `sm cmds <序号>` | **可用**：`Filename` 是两边唯一能对上的键 |

`sm plugins info` 返回：

```
Filename: l4d2_gift.smx
Title: L4D2 Gift (击杀特感几率掉落礼盒)
Author: Hitomi
Version: 1.2
URL: https://github.com/cy115/
```

`sm cmds` 返回命令表（表头 `[Name] [Type] [Help]`）：

```
sm_tgvote           console      打开特感投票菜单
sm_tgvoteadmin      admin        打开管理员投票控制菜单
```

### 27.3 采集结果

| 项 | 数量 |
|---|---|
| 插件总数 | 189 |
| 有 Filename | 189 |
| 有 Title（含中文说明） | 184 |
| 有 Author | 181 |
| 注册命令总数 | 270 |

### 27.4 README 结构

```markdown
# 击杀特感掉落礼盒

> 插件内显示名：L4D2 Gift (击杀特感几率掉落礼盒)

## 说明
- 版本：1.2
- 提供 15 个可配置项

## 命令与用法          ← 新增，玩家最需要
### 玩家命令
| 命令 | 聊天框输入 | 说明 |
| `sm_tgvote` | `!tgvote` | 打开特感投票菜单 |

### 管理员命令
| 命令 | 权限 | 说明 |
| `sm_tgvoteadmin` | admin | 打开管理员投票控制菜单 |

## 作者
- Hitomi
- 项目地址：https://github.com/cy115/

## 主要配置 / 配置文件
```

用户命令章节显式说明「在聊天框输入 `!命令名`」，因为 `sm_xxx` 是控制台命令，玩家不知道 `!xxx` 就是它的聊天框写法。

### 27.5 覆盖率

| 项 | 结果 |
|---|---|
| 有 README | 187 / 187 |
| 含命令章节 | **85**（原先 49，靠 Filename 匹配翻倍） |
| 含插件内显示名 | 162 |
| 识别出作者 | **175**（原先 67） |
| 含配置说明 | 86 |

`/plugins/readme` 接口验证返回完整 Markdown，面板可直接渲染。

### 27.6 脚本

| 脚本 | 用途 |
|---|---|
| `cc_collect_plugin_meta.py` | 采集 `sm plugins info` + `sm cmds`，产出 `plugin-meta.json` |
| `generate_plugin_readmes.py --all --apply` | 全量重生成，含命令章节 |

---

## 28. 插件说明自动化：上传即生成，启用后补全（2026-11-24）

### 28.1 要解决的问题

§25/§27 的 README 是靠一次性脚本补齐的，只对线上这 187 个插件有效。用户后续手动上传新插件时，面板仍然显示「此插件无说明文件」，还得人工再跑一遍脚本。

目标：把这件事变成面板自带能力。

### 28.2 设计：两阶段生成

| 阶段 | 触发点 | 能拿到什么 |
|---|---|---|
| 基础版 | `UploadPlugin` 落盘后 | smx 清单、cfg 配置项与中文注释、gamedata 签名、目录名里的作者 |
| 补全版 | `loadPluginRuntimeBatch` / `ReloadPlugin` 成功后 | 追加运行时命令表（`sm cmds`） |

选两阶段而不是一次性生成，原因很直接：**命令只有插件真正被 SourceMod 加载后才注册得到**，上传时服务器里还没有这个插件，`sm cmds` 一定查不到。

用户确认过这个取舍：「上传即生成基础版，启用后补全」。

### 28.3 覆盖保护

生成的文件首行写 `<!-- generated-by: l4d2-manager -->`。覆盖前先判断：

- 有标记 → 面板产物，允许覆盖
- 没有标记，但有旧版固定结尾「本说明由面板根据插件目录内的实际文件与配置自动整理」 → 也允许覆盖（§27 脚本产物）
- 都没有 → 作者或用户手写的文档，跳过并返回 `reason`

旧格式也要认，否则线上已有的 187 个 README 会被误判成人工文档，命令章节永远补不上。这一条有单测 `TestEnrichPluginReadmeUpdatesLegacyGeneratedReadme` 守着。

### 28.4 命令状态三分类（关键修正）

线上实测发现：`sm cmds` 有三种完全不同的返回，第一版把它们混为一谈，一律写「插件尚未启用」，误导用户。

| `sm cmds` 返回 | 含义 | README 写法 |
|---|---|---|
| `[SM] Listing commands for: xxx` + 命令表 | 已加载，有命令 | 正常命令章节 |
| `[SM] No commands found for: xxx` | **已加载，但不注册命令** | 「该插件已在运行，但没有注册任何控制台命令」 |
| `[SM] Plugin "xxx.smx" was not found.` | 未加载 | 带 pending 标记，提示启用后刷新 |

第二类是最容易误判的。L4D2 大量插件（`a2s fix`、`ai_damagefix` 这类修复插件）靠事件钩子生效，本来就不注册任何命令。把它说成「未启用」既不准确，也会让用户以为插件没生效。

证据样本：

```
# A2S查询修复（运行中）
sm plugins info l4d2_a2s_fix.smx
  Status: running
sm cmds l4d2_a2s_fix.smx
[SM] No commands found for: [L4D2] a2s fix

# 自选-Neko多特-NS7（未启用）
sm plugins info Neko_Specials_binhooks.smx
[SM] Plugin Neko_Specials_binhooks.smx is not loaded.
sm cmds Neko_Specials_binhooks.smx
[SM] Plugin "Neko_Specials_binhooks.smx" was not found.
```

对应实现是 `PluginCommandScan`（Loaded / NotLoaded / Unknown 三个计数）与 `buildPluginNoCommandSection` 的四分支。

### 28.5 增量规则

- `GeneratePluginReadme`：无条件生成/覆盖（带覆盖保护）
- `EnrichPluginReadme`：先比对内容，完全相同就返回 `reason=说明内容无变化`，不写盘

所以反复点「刷新说明」是幂等的，不会反复改文件时间戳。

### 28.6 新接口

```
POST /plugins/readme/refresh   （管理员）
body: { "name": "插件目录名" }
resp: {
  written: bool,
  has_commands: bool,
  command_count: int,
  convar_count: int,
  untranslated: int,
  reason: string,
  content: string,
  file_name: string
}
```

前端在插件详情弹窗右上角加了「刷新说明」按钮，按 `has_commands` / `written` / `reason` 给出不同提示。

### 28.7 翻译词典内嵌

§25 的翻译手段是在远端跑脚本改 cfg。这里把 185 条词典内嵌进二进制：

```
backend/resources/pluginreadme/cfg-translations.json   （25427 字节，185 条）
backend/resources/pluginreadme/resources.go            //go:embed + sync.Once 缓存
```

好处是不依赖运行时外部文件，容器重建后照样能用。词典没命中的注释保留英文原句，条数记进 `untranslated`，README 末尾提示「有 N 条配置注释暂无中文翻译」。

### 28.8 验证结果（cc雨云，2026-11-24）

全量刷新 187 个插件：

| 项 | 数量 |
|---|---|
| README 存在 | 187 / 187 |
| 面板生成（含标记） | 187 |
| 含运行时命令章节 | 79 |
| 已加载但无命令 | 87 |
| 待补全（未加载） | 21 |

二次全量刷新全部返回「说明内容无变化」，确认幂等。

### 28.9 测试

`backend/logic/plugin_readme_test.go` 共 16 个用例，覆盖：

- `sm cmds` 输出解析（有命令 / 无命令 / 空）
- 玩家命令必须给出 `!xxx` 聊天框写法
- cfg 解析：词典命中、已有中文、枚举项、截断半句过滤
- 目录名解析：中文作者、英文作者、说明性括号不臆造
- 生成：基础版落盘、用户自带 README 不覆盖
- 补全：命令章节补齐、旧格式 README 可刷新

另需同步 `plugins_test.go`：`LoadPlugin` / `ReloadPlugin` 现在会多发 `sm cmds`，RCON 调用序列断言要跟着改。

### 28.10 部署脚本

| 脚本 | 用途 |
|---|---|
| `cc_e2e_readme_refresh.py` | 端到端验证：登录、刷新、读回、落盘核对 |
| `cc_refresh_all_readmes.py` | 全量刷新 187 个插件并统计三状态分布 |
| `cc_probe_cmds_unloaded.py` | 采集三类 `sm cmds` 原始返回，确认分类正确 |
| `cc_spotcheck_readme.py` | 抽检三种状态的 README 文案 |
| `cc_find_skipped_readme.py` | 定位未写入的插件及原因 |

---

## 29. SteamCMD 自更新超时与 DNS 配置（2026-11-24）

### 29.1 现象

服务端维护页「检查更新」返回 `status=error`，`l4d2-check.log` 停在：

```
[  8%] Downloading update (2,146 of 25,754 KB)...
```

日志共 5427 行，其中 5420 行都是同一句进度重复，**没有任何 error/fail/retry 行**。

### 29.2 排查过程（按证据推进，不猜）

| 步骤 | 检查项 | 结果 | 结论 |
|---|---|---|---|
| 1 | `getent hosts steamcdn-a.akamaihd.net` | 解析到 2 个 IPv4 | DNS 正常 |
| 2 | `curl https://steamcdn-a.akamaihd.net/...` | `http_code=200`，`total=1.79s` | 网络通 |
| 3 | `resolvectl status` | DNS `223.5.5.5 / 223.6.6.6` | 阿里 DNS 正常 |
| 4 | `ip -6 addr` / `ip -6 route` | 无 IPv6 地址、无默认路由 | IPv6 不可用 |
| 5 | `curl -6` | `code=000`，0.6ms 失败 | **确认 IPv6 不通** |
| 6 | `getent ahostsv4 client-update.akamai.steamstatic.com` | 有 IPv4 结果 | 可回退 IPv4 |
| 7 | `/data/package` 内容 | 已有 26MB `steamcmd_bins_steamrt64.zip.vz` | **下载其实成功过** |
| 8 | 日志中 8% 前后的包大小 | 先是 `42,739 KB`，后是 `25,754 KB` | **SteamCMD 在换源重试** |

第 8 条是转折点：同一份日志里出现两种包体积，说明不是连接失败，而是 SteamCMD 换下载源后**从 0 重新下**，每次都在 8% 被 `timeout 1200` 杀掉。

### 29.3 根因

不是 DNS，不是 IPv6，是**超时值配得比慢速链路实际需要的时间短**。

实测速率约 1.7 KB/s（2MB / 1200 秒）。25MB 的包在这个速率下需要约 4 小时。1200 秒只能下 8%。

### 29.4 修复

`manifest/systemd/run-maintenance.sh`：

```diff
-STEAMCMD_SELF_UPDATE_TIMEOUT="${L4D2_STEAMCMD_SELF_UPDATE_TIMEOUT:-1200}"
+STEAMCMD_SELF_UPDATE_TIMEOUT="${L4D2_STEAMCMD_SELF_UPDATE_TIMEOUT:-3600}"
```

首次初始化允许等 1 小时，环境变量仍可覆盖。

### 29.5 DNS 加固（双保险）

不是根因，但慢速链路上多上游能避免拿到慢节点。写入
`/etc/systemd/resolved.conf.d/99-l4d2-dns.conf`：

```ini
[Resolve]
DNS=223.5.5.5 223.6.6.6 119.29.29.29 8.8.8.8
FallbackDNS=1.1.1.1 9.9.9.9
DNSSEC=no
```

注意：

- 不直接改 `/etc/resolv.conf`——它是 `systemd-resolved` 的软链接，改了会被覆盖
- `DNSSEC=no` 必须保留：部分 Steam CDN 域名签名链不完整，开了会解析失败

生效后 `resolvectl status` 显示 4 个上游，解析耗时 `0.77ms`。

### 29.6 验证（真实链路，不是 curl 模拟）

| 验证 | 方式 | 结果 |
|---|---|---|
| root 手动自更新 | `timeout 3600 ./steamcmd.sh +quit` | **15 秒内完成**，`Unloading Steam API...OK` |
| ubuntu 用户自更新 | `runuser -u ubuntu -- timeout 180 ./steamcmd.sh +quit` | **同样秒过**，日志 `Verifying installation...` |
| 面板触发 | `POST /server-maintenance/update/check` | 12 秒 `status=success`，`installed=23990100`，`latest=23990100`，`update_available=false` |
| systemd 直触发 | 写 `maintenance-request.json` + `systemctl start l4d2-maintenance` | `Result=success`，`ExecMainStatus=0`，build ID 一致 |

关键发现：**一旦自更新真的跑完一次，后续 `./steamcmd.sh +quit` 是秒退的**（`Verifying installation...` 阶段不需要重新下载）。所以 3600 秒只影响首次，之后每次都很快。

### 29.7 维护请求字段名（踩坑记录）

`maintenance-request.json` 的字段是 `steam_dir` / `install_dir`，**不是** `steam_directory` / `install_directory`：

```json
{
  "request_id": "uuid",
  "action": "check",
  "steam_dir": "/data",
  "install_dir": "/data/l4d2-server",
  "service_name": "l4d2.service"
}
```

写错字段会被 `read_request()` 判为「维护请求格式无效」，而且服务仍然 `Result=success`（因为业务失败统一 `exit 0`），容易误判成链路正常。

### 29.8 脚本

| 脚本 | 用途 |
|---|---|
| `cc_diag_steamcmd_net.py` | DNS、连通性、日志、结果文件一次收齐 |
| `cc_diag_ipv6.py` | IPv4/IPv6 解析与路由对比 |
| `cc_diag_steamcmd_stall.py` | 日志重复行分析、包体积变化、残留进程 |
| `cc_diag_steamcmd_user.py` | root 与 ubuntu 两用户对照实验 |
| `cc_run_steamcmd_selftest.py` | 后台跑自更新并分段观察进度 |
| `cc_deploy_maintenance_script.py` | 上传 + `bash -n` + 备份 + 安装 |
| `cc_configure_dns.py` | 写 drop-in、重启 resolved、验证解析与下载 |
| `cc_trigger_update_check.py` | HTTP 触发检查更新并轮询 |
| `cc_verify_maintenance_flow.py` | systemd 真实链路验证（绕过 HTTP） |

---

## 30. 插件说明补全：21 个 pending 的处理（2026-11-24）

### 30.1 起点

§28 全量刷新后，187 个插件里有 **21 个挂着 `pending: runtime-commands`**，说明里写着「启用插件后再刷新」。

逐个查证后发现，这 21 个根本不是同一类东西，笼统挂 pending 是错的。

### 30.2 四类真实形态（逐个取证）

| 分类 | 数量 | 判定依据 | 为什么不该挂 pending |
|---|---|---|---|
| SourceScramble 内存补丁 | 3 | 只有 `configs/sourcescramble/*.txt` + `gamedata/*.txt`，无 smx | 由 `sourcescramble_manager.smx` 启动时读配置改内存，本身不注册命令 |
| Metamod 扩展 | 1 | `addons/l4dtoolz.vdf` + `.so` + `.dll`，无 smx | Metamod 层级低于 SourceMod，不注册 SourceMod 命令 |
| 有源码、源码里确实无命令 | 8 | 有 `.smx` + `.sp`，`.sp` 里无 `RegConsoleCmd`/`RegAdminCmd` | 源码是静态事实，没有就是没有 |
| 无源码、插件未加载 | 5 | 有 `.smx`，无 `.sp`，`sm cmds` 返回 `was not found` | **这一类才该挂 pending** |

关键证据样本：

```
# SourceScramble 补丁
$ find 可选-修复类(修复HUD闪屏).../left4dead2
.../configs/sourcescramble/l4d2_ems_hud_patch.txt
.../gamedata/l4d2_ems_hud_sig.txt

# Metamod 扩展
$ find 自选-tick解锁扩展+多人破解扩展.../left4dead2/addons
l4dtoolz.vdf  l4dtoolz.so  l4dtoolz.dll

# 有源码但无命令
$ grep -E 'Reg(Console|Admin)Cmd' 自选-尸体直接消失.../l4d_ragdoll_fader.sp
（无输出）
```

### 30.3 三个行为修正

#### 修正一：按形态分类，不做命令查询

`classifyPluginKind` 新增四种形态：`sourcemod` / `scramble` / `metamod` / `empty`。只有 `sourcemod` 才会发 `sm cmds`——对补丁和扩展发查询，只会拿到 `not found` 并误挂 pending。

#### 修正二：SourceMod 扩展不冒充 Metamod

第一版把 `addons/**/*.so` 全归为 Metamod，导致带 `BinHooks.ext.so` 的 Neko 插件被写成「这是 Metamod 扩展」。

但 `addons/sourcemod/extensions/*.so` 是 **SourceMod 扩展**，和 smx 一起工作。现在按路径区分：

- `addons/sourcemod/extensions/` → SourceMod 扩展章节
- 其他位置（`addons/*.so`、`addons/metamod/bin/*.so`）→ Metamod 平台文件章节

顺带把标题从「Metamod 扩展」改成「Metamod 平台文件」，因为完整的 SourceMod 平台包会同时带 `metamod.vdf` 和几十个平台二进制，叫「扩展」不准确。

#### 修正三：源码兜底，补全未加载插件的命令

作者常常把 `.sp` 源码一起打包。源码里的 `RegConsoleCmd` / `RegAdminCmd` 是静态事实，可以据此补全命令表，不必等插件被启用。

解析规则：

```
RegConsoleCmd("sm_hp", Command_Reset, "生还者过关时自动满血或复活.")
RegAdminCmd("sm_limit", cmdSetLimit, ADMFLAG_RCON, "设置特感生成数量")
```

- 命令名取第一个字符串字面量
- `RegAdminCmd` 取 `ADMFLAG_XXX` 作为权限（`ADMFLAG_RCON` → `rcon`）
- 帮助文本取最后一个非权限字符串字面量
- 无帮助文本就留空，不编造

**必须在章节末尾标注来源差异**，否则读者会误以为命令已在服务器生效：

> 命令表来自插件包内的 `.sp` 源码（`RegConsoleCmd` / `RegAdminCmd`），不是运行时注册信息。插件启用并加载成功后，点「刷新说明」可换成运行时数据。

#### 修正四：源码确认无命令时不再挂 pending

这是 `sourceChecked` 参数的意义。三种「没有命令」要分开：

| 情况 | 文案 | pending |
|---|---|---|
| 已加载但无命令 | 「该插件已在运行，但没有注册任何控制台命令」 | 无 |
| 未加载 + 源码确认无命令 | 「插件包内附带的源码中没有 `RegConsoleCmd`…」 | 无 |
| 未加载 + 无源码 | 「插件当前未加载，SourceMod 查不到…」 | **有** |

第三种才保留 pending，因为「读不到」不等于「没有」。

### 30.4 附带修掉的解析 bug

| bug | 现象 | 修法 |
|---|---|---|
| 启动参数被当作者 | `(-tickrate 100 +sv_setmax 31)` 出现在「作者」列表 | 新增 `launchArgPattern`，`^[+-]` / `-tickrate` / `\+sv_` 一律跳过 |
| 效果说明被当作者 | `(无特效)` 出现在「作者」列表 | `descriptionHintPattern` 补 `无效`/`无特效`/`特效`/`客户端`/`服务端`/`测试`/`汉化`/`整合`/`优化`/`增强`/`修改`/`替换`/`自定义` |
| 「自选-」前缀不识别 | `自选-tick解锁扩展…` 拿不到「可选插件」提示 | 前缀解析同时认「可选」和「自选」，都归一成 `可选` |

### 30.5 最终结果

| 阶段 | pending 数 | 含命令插件数 |
|---|---|---|
| §28 初始 | 21 | 79 |
| 分类 + 扩展修正 | 17 | 79 |
| 源码兜底 | 13 | 85 |
| 源码确认无命令 | **5** | 85 |

剩余 5 个全是「无 `.sp` 源码 + 插件未加载」：

- 自选-Neko多特-NS7(himenekocn)
- 自选-Neko多特投票(需要原生投票前置)(himenekocn)
- 自选-友伤与反伤控制(v1.3)(可投票，需要原生投票库前置)(LaoYutang)
- 自选-肉鸽buff插件(LaoYutang)
- 自选-肉鸽前置随机章节插件(Yuzumi, LaoYutang)

这 5 个的 pending 是**准确状态**，不是缺陷。启用插件后点「刷新说明」即可补全。

全站分布：

| 项 | 数量 |
|---|---|
| README 总数 | 187 |
| 面板生成 | 187 |
| 含命令章节 | 183 |
| 运行时无命令（正常形态） | 85 |
| SourceScramble 补丁 | 3 |
| Metamod 纯扩展 | 1 |
| 源码确认无命令 | 8 |
| 待补全（未加载无源码） | 5 |

### 30.6 新增测试（共 8 个）

| 测试 | 覆盖点 |
|---|---|
| `TestParsePluginNameHintsSkipsLaunchArguments` | `-tickrate 100` 不算作者 |
| `TestParsePluginNameHintsSkipsUsageNotes` | 「游戏运行时添加无效」不算作者 |
| `TestParsePluginNameHintsSkipsEffectNotes` | 「无特效」不算作者 |
| `TestGeneratePluginReadmeForScramblePatch` | 补丁不查 RCON、不挂 pending、有专属章节 |
| `TestGeneratePluginReadmeForMetamodExtension` | 扩展不查 RCON、不挂 pending、启动参数不进作者 |
| `TestGeneratePluginReadmeForEmptyPlugin` | 空目录有合理文案 |
| `TestGeneratePluginReadmeSeparatesSourceModExtensions` | SourceMod 扩展不归 Metamod |
| `TestParseSourcePluginCommands` | 源码解析五种写法（含无帮助文本） |
| `TestGeneratePluginReadmeUsesSourceWhenUnloaded` | 未加载时用源码补全并标注来源 |
| `TestGeneratePluginReadmeConfirmsNoCommandsFromSource` | 源码确认无命令时不挂 pending |
| `TestGeneratePluginReadmeKeepsPendingWithoutSource` | 无源码时必须保留 pending |

### 30.7 脚本

| 脚本 | 用途 |
|---|---|
| `cc_collect_pending_plugins.py` | 采集 21 个 pending 的完整证据（状态/smx/运行时信息） |
| `cc_probe_enabled_no_smx.py` | 排查「enabled 但无 smx」的矛盾项 |
| `cc_dump_patch_files.py` | 读取 SourceScramble 配置与 Metamod 扩展内容 |
| `cc_probe_neko_misclass.py` | 定位 Neko 被误判 Metamod 的原因 |
| `cc_fix_pending_readmes.py` | 逐个刷新并统计 pending 变化 |
| `cc_spotcheck_new_readmes.py` | 抽检各类文案 |
| `cc_check_sp_sources.py` | 统计哪些 pending 插件带 `.sp` 源码 |
| `cc_count_sp_commands.py` | 统计源码里注册了多少命令 |
| `cc_probe_cmd_details.py` | 查看完整注册行（权限/说明参数） |
| `cc_verify_source_commands.py` | 验证源码命令表补全效果 |
| `cc_final_pending_check.py` | 最终确认剩余 5 个的真实状态 |




