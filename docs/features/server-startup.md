# 启动项管理

“启动项管理”供管理员编辑 systemd 游戏服单元的工作目录和 `ExecStart` 启动命令。它只处理服务定义，不会修改 `server.cfg` 中的游戏 CVar。

## 参数构建器

页面内置 61 项可用启动参数，按基础启动、网络连接、游戏玩法、安全与诊断、SourceTV、启动器控制和高级调试分类。每项都包含：

- 中文名称。
- 中文作用说明。
- 对应的 `-` / `+` 参数写法。
- 参数类型、默认值、候选值和取值范围。
- 推荐、谨慎、高级等风险标记。
- 插件依赖提示，例如 `+sv_setmax` 需要人数扩展插件。

可以搜索中文名、参数或说明，也可以直接选择一个或多个预设。预设只追加或更新对应参数，不删除现有自定义项。

页面同时提供“添加自定义启动项”和完整 `ExecStart` 文本编辑，未列入内置清单的参数仍可手动添加。

内置清单优先覆盖 L4D2 实际部署常用、可公开配置且不会暴露内部秘密的参数，不包含引擎隐藏调试开关，也不建议把 RCON 密码或服务器密码直接写进启动项，因为这些内容可能出现在 `systemctl status` 和其他进程信息中。

### 已核实的易混淆参数

| 参数 | 实际行为 | 页面处理 |
|------|----------|----------|
| `-usetcp` | 把引擎内部 `net_notcp` 从默认 `1` 改为 `0`，允许额外 TCP 监听/连接路径；常规 L4D2 联机仍依赖 UDP | 保留为高级实验项，并明确“不是纯 TCP” |
| `+sv_setmax` | L4DToolz 修改的引擎 `MaxClients` 槽位，不等于实际可玩人数 | 改名为“引擎客户端槽位上限”，另列 `sv_maxplayers` 实际人数 |
| `-consolelog` | Linux `srcds_run` 历史上支持，但要求 `bin/logger`；当前 SteamCMD L4D2 包不包含该程序 | 从内置选择中移除 |
| `-fork` | 当前 `srcds_run` 只解析和校验数字，没有后续执行逻辑 | 从内置选择中移除 |
| SourceTV | L4D2 原版广播和录像功能不完整，需要 `sourcetvsupport` 扩展 | 提供状态检测和一键安装 |

## 编辑范围

页面读取挂载到管理器容器内的 `l4d2.service` 快照，允许修改：

- `WorkingDirectory`：必须是 Linux 绝对路径。
- `ExecStart`：必须是绝对可执行文件路径，不能包含换行或空字符。

其他 systemd 配置（用户、重启策略、资源限制和依赖关系）保持不变。页面会根据 `+hostport`、`+port` 或 `-port` 解析游戏端口。

页面显示的中文说明会作为注释写入服务单元：

```ini
# L4D2 Manager startup options: begin
# 服务器端口：玩家连接和服务器通信使用的 UDP/TCP 端口 当前值：27015
# 游戏模式：指定服务器启动时的 L4D2 游戏模式 当前值：coop
# L4D2 Manager startup options: end
ExecStart=/path/to/srcds_run -game left4dead2 +hostport 27015 +mp_gamemode coop
```

## 保存与生效

保存时管理器先原子替换控制区中的服务单元快照。Linux 宿主机的 `l4d2-startup.path` 检测到变化后执行 `l4d2-startup.service`，校验单元、安装到 `/etc/systemd/system/l4d2.service` 并执行：

```sh
systemctl daemon-reload
```

保存不会自动重启游戏服。需要立即应用新启动项时，在页面中单独执行“重启游戏服”，或在宿主机手动重启对应服务。

SourceTV 修复和求生服务端检查更新、立即更新属于独立的[服务端维护](/features/server-maintenance)页面。启动项管理只保留 SourceTV 启动参数预设和参数编辑，不负责安装扩展或执行 SteamCMD。

## 默认路径

本仓库提供的 systemd 辅助单元使用以下路径：

| 用途 | 路径 |
|------|------|
| 管理器快照 | `/opt/l4d2-manager/control/l4d2.service` |
| 宿主机单元 | `/etc/systemd/system/l4d2.service` |
| 应用脚本 | `/opt/l4d2-manager/apply-startup.sh` |
| 应用结果 | `/opt/l4d2-manager/control/l4d2-startup-result.json` |

如部署路径不同，可通过 `L4D2_STARTUP_UNIT_PATH`、`L4D2_STARTUP_SERVICE_NAME` 和 `L4D2_STARTUP_RELOAD_CMD` 等环境变量调整管理器行为。

## 参数来源

内置参数说明综合了以下依据：

- 当前 SteamCMD L4D2 安装中的 `srcds_run -help` 行为。
- 实际 `engine_srv.so` 中包含的公开启动参数。
- `srcds_run` 中 `-autoupdate`、`-fork`、`-consolelog` 的真实执行路径。
- Valve Source Dedicated Server 命令行参数文档。
- L4D2 专用服务器社区配置中的常用 `+map`、`+maxplayers`、`+mp_gamemode`、`+z_difficulty`、`+sv_setmax` 等写法。
- `lakwsh/l4dtoolz` 对 `sv_setmax`、`sv_maxplayers` 和引擎槽位的说明。

Source 引擎内部仍存在大量未公开、已废弃或游戏不支持的开关，因此页面不承诺暴露所有内部字符串，而是提供安全可用的内置清单和自定义兜底。
