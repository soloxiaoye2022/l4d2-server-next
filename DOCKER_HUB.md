# L4D2 Server Next 增强版

基于 [LaoYutang/l4d2-server-next](https://github.com/LaoYutang/l4d2-server-next) 的增强分支，把日常运维从 SSH 搬进浏览器：改启动项、更新服务端、修坏插件、接管已有服务端，都在面板里点。

## 镜像

| 镜像 | 用途 |
|---|---|
| `soloxiaoye2022/l4d2-manager-next` | 管理面板 |
| `soloxiaoye2022/l4d2-pure` | 游戏服，已装 SteamCMD 和 L4D2 服务端 |

标签：`latest`、`2026.11.17`（按日期）。

## 本次更新（2026.11.17）

### 运行时管理

以前面板只认 systemd 服务。如果你的服是 `screen`、`tmux`、`nohup` 拉起来的，或者跑在 MCSM、LGSM、另一个 Docker 容器里，面板基本没法管。

现在新增「运行时管理」页，支持 9 种运行时：`systemd`、`systemd-user`、`docker`、`screen`、`tmux`、`nohup`、`bare`、`mcsm`、`lgsm`。

- **自动扫描**：点一下扫描，面板会从宿主机找出所有 L4D2 服务端进程，自动过滤掉面板自己的辅助服务。
- **一键接管**：选一个候选，起个名字就纳管了。接管不改动正在跑的进程。
- **统一控制**：接管后启停、重启、看日志、看状态、跑诊断、更新服务端、修插件，全部走同一套链路。
- **迁移**：可以把 systemd 的服务迁到 screen，或者反过来。面板先给迁移计划，确认后才执行。
- **不强制**：老部署升级上来什么都不用动，行为和以前完全一样。只有你主动接管，才会切到新链路。

实现方式是面板写任务文件，宿主机上的 Python Agent 收到后执行。Agent 通过 systemd path unit 触发，不常驻，不占端口。

### 检查更新不再卡死

之前点「检查更新」经常失败。原因是 SteamCMD 客户端自更新（约 36 MB）和版本查询共用了一个 180 秒超时，慢速网络下自更新永远拉不完，每次都在 17% 被杀掉，下次又从 0% 开始，形成永久失败循环。

现在拆成两段：自更新 1200 秒、版本查询 180 秒，各自计时。检查日志也单独写 `l4d2-check.log`，不会再和游戏本体的更新日志混在一起让人误判。

### 参数一键选值

服务器配置页和启动项弹窗里，参数的可选值现在能直接点。点一下就写进命令行，不用手打。数值型参数给了常用值按钮，也可以自己填。互斥处理好了，不会出现既选预设又填自定义的重复。

### 其他修复

- RCON 多包响应截断：长插件输出现在能完整读取。
- 启动项端口：从 `+hostport`、`+port`、`-port` 里读，改端口不用同步改面板配置。
- 玩家名净化：滤掉 `<#RRGGBB>` 颜色标记、控制字符、零宽字符，只影响显示层。
- 窄屏和按钮对齐：维护页、启动项页在手机宽度下不再挤成一团。

## 快速开始

### 只装面板，管已有的游戏服

```bash
mkdir -p /opt/l4d2-manager/{control,data,plugins}

docker run -d \
  --name l4d2-manager \
  --restart unless-stopped \
  --network host \
  -e L4D2_MANAGER_PORT=27020 \
  -e L4D2_MANAGER_PASSWORD='换成强密码' \
  -e L4D2_GAME_PATH=/left4dead2 \
  -e L4D2_PLUGIN_STORE_PATH=/plugins \
  -e L4D2_RCON_URL=127.0.0.1:27015 \
  -e L4D2_RCON_PASSWORD='换成RCON密码' \
  -v /path/to/your/left4dead2:/left4dead2 \
  -v /opt/l4d2-manager/control:/control \
  -v /opt/l4d2-manager/data:/data \
  -v /opt/l4d2-manager/plugins:/plugins \
  -v /proc:/host/proc:ro \
  -v /run/systemd/system:/host/systemd:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  soloxiaoye2022/l4d2-manager-next:latest
```

打开 `http://服务器IP:27020`，用 `L4D2_MANAGER_PASSWORD` 登录。

`L4D2_RCON_PASSWORD` 必须和游戏服 `server.cfg` 里的 `rcon_password` 一致。

### 面板 + 游戏服一起装

```yaml
services:
  l4d2:
    image: soloxiaoye2022/l4d2-pure:latest
    container_name: l4d2
    restart: unless-stopped
    network_mode: host
    environment:
      L4D2_PORT: "27015"
      L4D2_TICK: "100"
      L4D2_VAC: "false"
      L4D2_RCON_PASSWORD: "换成RCON密码"
    volumes:
      - l4d2_game:/l4d2

  l4d2-manager:
    image: soloxiaoye2022/l4d2-manager-next:latest
    container_name: l4d2-manager
    restart: unless-stopped
    user: "${L4D2_MANAGER_UID:-1000}:${L4D2_MANAGER_GID:-1000}"
    group_add:
      - "${DOCKER_GID:-999}"
    network_mode: host
    depends_on:
      - l4d2
    environment:
      L4D2_MANAGER_PORT: "27020"
      L4D2_MANAGER_PASSWORD: "换成面板密码"
      L4D2_GAME_PATH: "/l4d2/left4dead2"
      L4D2_PLUGIN_STORE_PATH: "/plugins"
      L4D2_RCON_URL: "127.0.0.1:27015"
      L4D2_RCON_PASSWORD: "换成RCON密码"
      L4D2_RESTART_CMD: "docker restart l4d2"
      L4D2_RESTART_BY_RCON: "false"
    volumes:
      - l4d2_game:/l4d2
      - l4d2_manager_control:/control
      - l4d2_manager_data:/data
      - l4d2_manager_plugins:/plugins
      - /proc:/host/proc:ro
      - /run/systemd/system:/host/systemd:ro
      - /var/run/docker.sock:/var/run/docker.sock

volumes:
  l4d2_game:
  l4d2_manager_control:
  l4d2_manager_data:
  l4d2_manager_plugins:
```

`L4D2_GAME_PATH` 要指向 `left4dead2` 目录本身，不是游戏根目录。

用宿主机目录挂载时，容器 UID/GID 得和目录属主对上，否则面板写出来的文件会归 root：

```bash
export L4D2_MANAGER_UID="$(id -u)"
export L4D2_MANAGER_GID="$(id -g)"
export DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
```

已经写成 root 的目录用 `chown -R "$(id -u):$(id -g)" /path/to/left4dead2` 修回来。

## 启用运行时管理与服务端更新

这两个功能要在宿主机装 helper。面板镜像里已经带好文件，复制出去就行：

```bash
mkdir -p /opt/l4d2-manager/control/runtime/{requests,results,processed}

docker create --name l4d2-helper soloxiaoye2022/l4d2-manager-next:latest
docker cp l4d2-helper:/manager-systemd/apply-startup.sh /opt/l4d2-manager/
docker cp l4d2-helper:/manager-systemd/run-maintenance.sh /opt/l4d2-manager/
docker cp l4d2-helper:/manager-systemd/repair-addons.sh /opt/l4d2-manager/
docker cp l4d2-helper:/manager-systemd/runtime-control.sh /opt/l4d2-manager/
docker cp l4d2-helper:/manager-runtime-agent/. /opt/l4d2-manager/runtime-agent/
docker cp l4d2-helper:/manager-systemd/l4d2-startup.path /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-startup.service /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-maintenance.path /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-maintenance.service /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-repair.path /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-repair.service /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-runtime.path /etc/systemd/system/
docker cp l4d2-helper:/manager-systemd/l4d2-runtime.service /etc/systemd/system/
docker rm l4d2-helper

chmod +x /opt/l4d2-manager/*.sh /opt/l4d2-manager/runtime-agent/l4d2_runtime_agent.py
systemctl daemon-reload
systemctl enable --now l4d2-startup.path l4d2-maintenance.path l4d2-repair.path l4d2-runtime.path
```

需要 `python3`。没有 python3 的话运行时 Agent 装不了，面板会退回原来的 systemd 链路，其他功能不受影响。

工作方式：面板只写请求文件，helper 收到后执行，结果写回同一目录。

```text
/opt/l4d2-manager/control/maintenance-request.json
/opt/l4d2-manager/control/maintenance-result.json
/opt/l4d2-manager/control/l4d2-update.log
/opt/l4d2-manager/control/l4d2-check.log
/opt/l4d2-manager/control/runtime/requests/
```

## 环境变量

| 变量 | 说明 |
|---|---|
| `L4D2_MANAGER_PORT` | 面板端口，默认 `27020` |
| `L4D2_MANAGER_PASSWORD` | 管理员密码，生产必改 |
| `L4D2_GAME_PATH` | `left4dead2` 目录 |
| `L4D2_RCON_URL` | RCON 地址，如 `127.0.0.1:27015` |
| `L4D2_RCON_PASSWORD` | RCON 密码 |
| `L4D2_PLUGIN_STORE_PATH` | 插件库路径，默认 `/plugins` |
| `L4D2_RESTART_CMD` | 重启命令，如 `docker restart l4d2` |
| `L4D2_RESTART_BY_RCON` | 设为 `true` 时优先用 RCON 退出触发自拉起 |
| `L4D2_HOST_SYSTEMD_DIR` | 容器内看到的 systemd 目录，挂载 `/run/systemd/system` 时设为 `/host/systemd` |
| `L4D2_RUNTIME_USER` | 迁移/新建运行时默认用户，默认 `ubuntu` |
| `STEAM_API_KEY` | 玩家游戏时长查询用，可选 |

RCON 端口优先级：`L4D2_RCON_PORT` → `L4D2_PORT` → 启动项里的 `+hostport`/`+port`/`-port` → `L4D2_RCON_URL` 自带端口。

## 数据目录

| 路径 | 内容 |
|---|---|
| `/data` | 授权、监控、玩家统计、审计、面板配置 |
| `/plugins` | 插件列表和文件 |
| `/control` | 启动项快照、更新请求与结果、运行时任务 |
| `/left4dead2` | 游戏服务端 |

升级镜像前先备份这几个目录。

## 更新

```bash
docker pull soloxiaoye2022/l4d2-manager-next:latest
docker compose up -d --force-recreate l4d2-manager
```

改完启动项只重载 systemd 定义，不会自动重启游戏服。要立刻生效就点「重启游戏服」。

## 注意事项

- 示例里的密码全部换掉。RCON 密码、Steam API Key、面板密码别写进公开配置。
- `/var/run/docker.sock` 等于宿主机高权限，只挂给信得过的容器。
- 面板默认 27020，别和 SourceTV 的 27021 或游戏端口撞。
- 对外暴露面板前，套一层 HTTPS 反代加访问控制。
- Docker 29.3 以上跑 32 位 `srcds` 需要 `security_opt: seccomp:unconfined`。

## 排查

```bash
docker logs --tail 200 l4d2-manager
curl -I http://127.0.0.1:27020
systemctl status l4d2-runtime.path
```

面板里点「启动诊断」会抓 systemd 状态、journal 日志、端口占用、磁盘余量，然后按优先级给结论：坏 VPK、端口被占、工作目录不对、启动文件没执行权限、OOM、段错误、磁盘满、插件加载失败。坏 VPK 能直接「隔离并热重载」，不用重启。

---

文档：[l4d2-manage-docs.laoyutang.cn](https://l4d2-manage-docs.laoyutang.cn/)
源码：[github.com/LaoYutang/l4d2-server-next](https://github.com/LaoYutang/l4d2-server-next)
