# L4D2 Server Next 群友服增强版

这是基于 `LaoYutang/l4d2-server-next v1.15.3` 的增强构建，包含完整 L4D2 管理面板和配套游戏服务端镜像。目标是让管理员直接在 Web 面板里完成启动项维护、服务端更新、SourceTV 修复、Steam API Key 配置和玩家名清理，而不需要频繁进入宿主机手改文件。

## 镜像

| 镜像 | 用途 |
|---|---|
| `soloxiaoye2022/l4d2-manager-next` | Web 管理器 |
| `soloxiaoye2022/l4d2-pure` | 已安装 SteamCMD 和 L4D2 专用服务端的游戏镜像 |

推荐标签：

- `2026.11.04`
- `latest`

## 相对原版的主要改动

- 启动项管理：内置 L4D2/Systemd 常用启动参数中文说明、分类、推荐预设、参数校验、自定义参数和完整 `ExecStart` 编辑。
- 动态端口：RCON 会从当前启动项中的 `+hostport`、`+port` 或 `-port` 自动读取游戏端口，修改端口后不需要同步修改面板配置或重启管理器。
- 服务端维护：面板可检查 L4D2 buildid、执行 SteamCMD 更新，并通过宿主机 systemd helper 安全停止、更新、重启游戏服。
- SourceTV 修复：内置 `shqke/sourcetvsupport` SourceMod 1.12 Linux/Windows 文件，一键安装、检测加载状态，并配置 `-hltv`、`+tv_enable 1`、`+tv_port 27021`。
- Steam API Key：管理员可在“系统管理 → 玩家统计设置”直接填写，保存后立即生效；面板只返回脱敏值，环境变量 `STEAM_API_KEY` 优先。
- 玩家名净化：过滤 `<#RGB>`、`<#RGBA>`、`<#RRGGBB>`、`<#RRGGBBAA>` 颜色标记、控制字符和常见零宽/双向控制符。首页、在线列表和玩家统计只显示有效字符。
- 服务器入口卡片：首页显示真实游戏端口、可复制的 `connect IP:端口` 命令和 `steam://connect/...` 一键进服。
- RCON 完整响应：修复多包响应截断，长插件输出和批量状态结果可完整读取。
- 按钮与移动端布局：统一 Ant Design 图标和文字垂直居中，并补齐启动项/维护页的窄屏布局。

## 环境要求

- Docker Engine 或 Docker Desktop
- Docker Compose v2
- 完整部署启动项维护功能时，需要 Linux systemd 宿主机
- 建议至少 20 GB 可用空间；L4D2 游戏镜像本身较大

## 最简部署：管理已有游戏服

```bash
mkdir -p /opt/l4d2-manager/{control,data,plugins}

docker run -d \
  --name l4d2-manager \
  --restart unless-stopped \
  --network host \
  -e L4D2_MANAGER_PORT=27020 \
  -e L4D2_MANAGER_PASSWORD='请替换为强密码' \
  -e L4D2_GAME_PATH=/left4dead2 \
  -e L4D2_PLUGIN_STORE_PATH=/plugins \
  -e L4D2_RCON_URL=127.0.0.1:27015 \
  -e L4D2_RCON_PASSWORD='请替换为RCON强密码' \
  -v /path/to/your/left4dead2:/left4dead2 \
  -v /opt/l4d2-manager/control:/control \
  -v /opt/l4d2-manager/data:/data \
  -v /opt/l4d2-manager/plugins:/plugins \
  -v /proc:/host/proc:ro \
  -v /var/run/docker.sock:/var/run/docker.sock \
  soloxiaoye2022/l4d2-manager-next:latest
```

打开：

```text
http://服务器IP:27020
```

使用 `L4D2_MANAGER_PASSWORD` 登录。RCON 密码必须与游戏服 `server.cfg` 中的 `rcon_password` 一致。

## 一键部署游戏服与管理器

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
      L4D2_RCON_PASSWORD: "请替换为RCON强密码"
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
      L4D2_MANAGER_PASSWORD: "请替换为面板强密码"
      L4D2_GAME_PATH: "/l4d2/left4dead2"
      L4D2_PLUGIN_STORE_PATH: "/plugins"
      L4D2_RCON_URL: "127.0.0.1:27015"
      L4D2_RCON_PASSWORD: "请替换为RCON强密码"
      L4D2_RESTART_CMD: "docker restart l4d2"
      L4D2_RESTART_BY_RCON: "false"
    volumes:
      - l4d2_game:/l4d2
      - l4d2_manager_control:/control
      - l4d2_manager_data:/data
      - l4d2_manager_plugins:/plugins
      - /proc:/host/proc:ro
      - /var/run/docker.sock:/var/run/docker.sock

volumes:
  l4d2_game:
  l4d2_manager_control:
  l4d2_manager_data:
  l4d2_manager_plugins:
```

`L4D2_GAME_PATH` 必须指向实际的 `left4dead2` 目录，而不是游戏根目录。

使用宿主机目录挂载时，面板容器必须使用与目录属主相同的 UID/GID，避免面板写入的文件变成 root 所有：

```bash
export L4D2_MANAGER_UID="$(id -u)"
export L4D2_MANAGER_GID="$(id -g)"
export DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
```

如果面板通过 `/var/run/docker.sock` 控制游戏容器，还必须把 Docker socket 的组 ID 加入 `group_add`。挂载目录可执行 `chown -R "$(id -u):$(id -g)" /path/to/left4dead2` 修正历史文件。

## 启用启动项与服务端更新

管理器镜像在 `/manager-systemd` 中附带 helper 文件。Linux 宿主机需要把它们安装到 systemd：

```bash
mkdir -p /opt/l4d2-manager/{control,data,plugins}

docker create --name l4d2-manager-helper soloxiaoye2022/l4d2-manager-next:latest
docker cp l4d2-manager-helper:/manager-systemd/apply-startup.sh /opt/l4d2-manager/
docker cp l4d2-manager-helper:/manager-systemd/run-maintenance.sh /opt/l4d2-manager/
docker cp l4d2-manager-helper:/manager-systemd/l4d2-startup.path /etc/systemd/system/
docker cp l4d2-manager-helper:/manager-systemd/l4d2-startup.service /etc/systemd/system/
docker cp l4d2-manager-helper:/manager-systemd/l4d2-maintenance.path /etc/systemd/system/
docker cp l4d2-manager-helper:/manager-systemd/l4d2-maintenance.service /etc/systemd/system/
docker rm l4d2-manager-helper

chmod +x /opt/l4d2-manager/apply-startup.sh /opt/l4d2-manager/run-maintenance.sh
systemctl daemon-reload
systemctl enable --now l4d2-startup.path l4d2-maintenance.path
```

启动项管理保存的是 `/opt/l4d2-manager/control/l4d2.service` 快照。`l4d2-startup.path` 检测变化后，会校验并安装到 `/etc/systemd/system/l4d2.service`。

服务端更新不会直接由容器执行，而是写入维护请求，由宿主机 helper 调用 SteamCMD：

```text
/opt/l4d2-manager/control/maintenance-request.json
/opt/l4d2-manager/control/maintenance-result.json
/opt/l4d2-manager/control/l4d2-update.log
```

## 支持的 RCON 端口来源

管理器保留 `L4D2_RCON_URL` 中的主机名，端口按以下优先级解析：

1. `L4D2_RCON_PORT`
2. `L4D2_PORT`
3. 启动项中的 `+hostport`、`+port` 或 `-port`
4. `L4D2_RCON_URL` 自带的端口

兼容以下启动项路径变量：

- `L4D2_STARTUP_UNIT_PATH`
- `L4D2_SERVER_UNIT_FILE`

## Steam API Key

管理员可在：

```text
系统管理 → 玩家统计设置 → Steam API 密钥
```

直接填写 32 位十六进制 Key。申请地址：

```text
https://steamcommunity.com/dev/apikey
```

环境变量 `STEAM_API_KEY` 优先级高于面板保存值。面板 API 只返回脱敏后的 Key。

## 玩家名过滤

管理器会显示干净名字，但不会修改 SteamID、统计数据或封禁记录。服务器插件包 `自选-玩家名字净化(v1.3.0)(Local)` 同时在游戏服侧拦截带颜色标记的客户端名，并回写为可见名字。只部署管理器时，游戏内 HUD/聊天仍需要安装该插件才能统一清理。

## SourceTV

在“服务端维护”页可检测并安装内置的 `sourcetvsupport`。推荐端口为 `27021`，避免与 Web 面板 `27020` 冲突。

安装后需要重启游戏服加载 SourceMod extension。上游仍将 SourceTV 中继标记为不完整，本镜像默认只提供直连观战端口和一键启动参数配置。

## 数据持久化

建议保留：

- `/data`：授权、监控、玩家统计、审计、管理器配置
- `/plugins`：插件列表与插件文件
- `/control`：启动项快照、更新请求和结果
- `/left4dead2`：游戏服务端

升级镜像前先备份这些目录。

## 更新

```bash
docker pull soloxiaoye2022/l4d2-manager-next:latest
docker compose up -d --force-recreate l4d2-manager
```

修改启动项后，面板只重载 systemd 定义，不会自动重启游戏服。需要立即应用时，使用“重启游戏服”。

## 安全说明

- 必须替换所有示例密码。
- 不要把 RCON 密码、Steam API Key、面板密码写进公开配置或仓库。
- `/var/run/docker.sock` 等价于宿主机高权限，只应挂载给可信的管理器容器。
- 面板默认使用 `27020`，不要与 SourceTV `27021` 或游戏端口冲突。
- 对外暴露管理器前建议加 HTTPS 反向代理和访问控制。

## 验证

```bash
docker version
docker compose config
docker logs --tail 200 l4d2-manager
curl -I http://127.0.0.1:27020
```

面板首页应显示：

- 服务器名称、地图、难度、游戏模式
- 正确游戏端口
- `connect IP:端口`
- 在线玩家列表中的干净名字
