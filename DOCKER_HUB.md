# L4D2 Server Next 增强版

改自 `LaoYutang/l4d2-server-next v1.15.3`。

原版功能挺全，但有些日常操作还是得登服务器改文件。这个版本把它们搬进了 Web 面板：改启动项、更新服务端、修 SourceTV、填 Steam API Key、清理玩家名，浏览器里点完就行。

## 镜像

| 镜像 | 用途 |
|---|---|
| `soloxiaoye2022/l4d2-manager-next` | Web 管理器，面板本体 |
| `soloxiaoye2022/l4d2-pure` | 游戏服，预装了 SteamCMD 和 L4D2 服务端 |

标签跟日期走：

- `2026.11.04`
- `latest`

## 改了什么

**启动项** —— 常用参数都配了中文说明和分类，可以直接从预设里挑，也能自己加。改完能看到完整的 `ExecStart` 长什么样。

**端口** —— 面板从启动项的 `+hostport`、`+port`、`-port` 里读游戏端口。换了端口不用再回头改面板配置，也不用重启管理器。

**服务端更新** —— 面板能查当前 buildid，也能触发 SteamCMD 更新。实际动作交给宿主机的 systemd helper 做：先停服、再更新、再拉起来，不会卡在半路。

**SourceTV** —— 内置 `shqke/sourcetvsupport` 的 SourceMod 1.12 文件，Linux 和 Windows 都有。一键装完会顺手配好 `-hltv`、`+tv_enable 1`、`+tv_port 27021`。

**Steam API Key** —— 在「系统管理 → 玩家统计设置」里填，存完立刻生效，不用重启。面板只回脱敏后的值。设了环境变量 `STEAM_API_KEY` 的话，以环境变量为准。

**玩家名** —— 把 `<#RGB>`、`<#RGBA>`、`<#RRGGBB>`、`<#RRGGBBAA>` 这类颜色标记，还有控制字符和零宽字符都滤掉。首页、在线列表、玩家统计里看到的都是干净名字。

**首页入口卡片** —— 显示当前真实端口，给一条能直接复制的 `connect IP:端口`，外加 `steam://connect/...` 一键进服。

**RCON** —— 原版多包响应会截断，长插件输出读不全。这里修好了，批量查状态也能拿到完整结果。

**按钮和窄屏** —— Ant Design 图标和文字对齐了，启动项页和维护页在手机宽度下不会再挤成一团。

## 启动诊断

服务端起不来的时候，面板能直接告诉你为什么。

点「启动诊断」，它会去宿主机抓 systemd 状态、journal 日志、端口占用和磁盘余量，然后按优先级给结论：坏 VPK、端口被别的进程占了、工作目录不对、启动文件丢了或没有执行权限、OOM、崩溃后自拉起、段错误、权限不足、磁盘满、插件加载失败……

坏 VPK 能直接处理。「隔离并热重载」把损坏的包挪到隔离目录，清掉 `addonlist`、`maplist` 里的引用，再用 RCON 让服务端重新加载地图，不用重启。想彻底重来就用「隔离并重启」。端口被占的话有「终止占用并重启」，先 SIGTERM 再 SIGKILL，只杀非 `srcds` 的进程。

每次处置都写进 `data/repair-history.jsonl`，面板的「处置记录」能看到最近 20 条，包括动了哪些文件、清了哪些引用、复检还剩几个坏包。

## 环境要求

- Docker Engine 或 Docker Desktop
- Docker Compose v2
- 要用启动项维护和服务端更新，宿主机得是带 systemd 的 Linux
- 至少留 20 GB 空间，游戏镜像本身就不小

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

然后打开：

```text
http://服务器IP:27020
```

用 `L4D2_MANAGER_PASSWORD` 登录。RCON 密码得和游戏服 `server.cfg` 里的 `rcon_password` 一致，不然连不上。

## 一键部署游戏服和管理器

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

`L4D2_GAME_PATH` 要指向真正的 `left4dead2` 目录，填游戏根目录不行。

如果用宿主机目录挂载，面板容器的 UID/GID 得和目录属主对上，否则面板写出来的文件会变成 root 所有：

```bash
export L4D2_MANAGER_UID="$(id -u)"
export L4D2_MANAGER_GID="$(id -g)"
export DOCKER_GID="$(stat -c '%g' /var/run/docker.sock)"
```

面板要通过 `/var/run/docker.sock` 控制游戏容器的话，还得把 socket 的组 ID 加进 `group_add`。之前已经被写成 root 的目录，用 `chown -R "$(id -u):$(id -g)" /path/to/left4dead2` 修回来。

## 启用启动项维护与服务端更新

管理器镜像的 `/manager-systemd` 里带着 helper 文件。Linux 宿主机要把它们装进 systemd：

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

启动项保存的是 `/opt/l4d2-manager/control/l4d2.service` 这份快照。`l4d2-startup.path` 发现它变了，会先校验，再装到 `/etc/systemd/system/l4d2.service`。

服务端更新不是容器直接跑的。容器只写一份请求，宿主机的 helper 收到后调 SteamCMD：

```text
/opt/l4d2-manager/control/maintenance-request.json
/opt/l4d2-manager/control/maintenance-result.json
/opt/l4d2-manager/control/l4d2-update.log
```

## RCON 端口从哪来

管理器保留 `L4D2_RCON_URL` 里的主机名，端口按这个顺序找：

1. `L4D2_RCON_PORT`
2. `L4D2_PORT`
3. 启动项里的 `+hostport`、`+port` 或 `-port`
4. `L4D2_RCON_URL` 自带的端口

启动项路径这两个变量也认：

- `L4D2_STARTUP_UNIT_PATH`
- `L4D2_SERVER_UNIT_FILE`

## Steam API Key

位置：

```text
系统管理 → 玩家统计设置 → Steam API 密钥
```

填 32 位十六进制 Key。没有的话去这儿申请：

```text
https://steamcommunity.com/dev/apikey
```

环境变量 `STEAM_API_KEY` 比面板里存的值优先。面板 API 只返回脱敏后的 Key。

## 玩家名过滤

管理器这边只做显示层清理，不动 SteamID、统计数据和封禁记录。要在游戏里也统一干净，得装插件包 `自选-玩家名字净化(v1.3.0)(Local)`，它会在游戏服侧拦掉带颜色标记的客户端名，再回写成看得见的名字。只部署管理器的话，游戏内 HUD 和聊天还是原样。

## SourceTV

在「服务端维护」页可以检测并安装内置的 `sourcetvsupport`。端口建议 `27021`，别和面板的 `27020` 撞。

装完要重启游戏服才会加载 SourceMod extension。上游目前仍把 SourceTV 中继标为不完整，这个镜像只提供直连观战端口和一键配好启动参数。

## 数据持久化

这几个目录留着：

- `/data` —— 授权、监控、玩家统计、审计、管理器配置
- `/plugins` —— 插件列表和插件文件
- `/control` —— 启动项快照、更新请求和结果
- `/left4dead2` —— 游戏服务端

升级镜像之前先备份。

## 更新

```bash
docker pull soloxiaoye2022/l4d2-manager-next:latest
docker compose up -d --force-recreate l4d2-manager
```

改完启动项，面板只会重载 systemd 定义，不会自动重启游戏服。要马上生效就点「重启游戏服」。

## 安全说明

- 示例里的密码全部换掉。
- RCON 密码、Steam API Key、面板密码别写进公开配置或仓库。
- `/var/run/docker.sock` 相当于宿主机高权限，只挂给信得过的容器。
- 面板默认 `27020`，别和 SourceTV `27021` 或游戏端口冲突。
- 对外暴露面板之前，最好套一层 HTTPS 反代加访问控制。

## 验证

```bash
docker version
docker compose config
docker logs --tail 200 l4d2-manager
curl -I http://127.0.0.1:27020
```

面板首页应该能看到：

- 服务器名称、地图、难度、游戏模式
- 真实的游戏端口
- `connect IP:端口`
- 在线列表里干净的名字
