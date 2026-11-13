# L1 + L2：坏 VPK 自动隔离、引用清理、复检、热重载与处置记录

> 目标：把"启动诊断"从"只读取证"升级为"可安全处置"。L1 = 自动隔离 + 引用清理 + 复检；L2 = 一键"隔离并热重载"（默认，不踢人）/ "隔离并重启"（备选）；所有处置写入 `data/repair-history.jsonl`，面板可回看。
>
> 适用仓库：`/home/ubuntu/l4d2-build-nonroot-20261105`（无 .git）
> 线上目录：`/opt/l4d2-manager`（compose / control / 宿主机脚本 / data）
> 编写时间：2026-11-11

## 1. 设计原则

1. **处置只在宿主机做**。面板容器以 `1000:1001` 运行，`addons/` 属主是 `ubuntu:ubuntu`，容器内没有写权限；同时隔离、清理引用、重启服务都需要 root。因此沿用既有桥接模式：容器写请求文件 → 宿主机 path unit 触发 → 宿主机写结果文件 → 容器读结果。
2. **宿主机脚本自己判定坏包**，不信任容器传来的文件名清单。请求只携带 `request_id` 与 `action`，防注入。
3. **隔离不删除**。坏文件移动到 `addons/.quarantine-<时间戳>/`，随时可回滚。
4. **改动前留底**。`addonlist.txt` / `maplist.txt` 改动前备份为 `*.bak-<时间戳>`。
5. **范围受限**：只处理 `addons/` 顶层的 `*.vpk`，单次最多 20 个，不递归、不触碰 `pak01_*.vpk` 与 `*.vpk.corrupt-*`。
6. **优先热重载**：VPK 路径可以通过 RCON 热重载（默认命令 `update_addon_paths; mission_reload`，可在设置页修改），隔离后立即刷新 addon 路径，不重启、不踢人；重启只是兜底选项。
7. **全过程留痕**：每次处置追加一行 JSON 到 `data/repair-history.jsonl`（保留最近 500 行，读取按新→旧），面板诊断弹窗内可查看最近 20 条。

## 2. 数据流

```
前端「隔离并热重载」/「隔离并重启」
  → POST /server-startup/repair {action}
  → logic.RequestServerRepair：先自查 addons（无坏包直接 noop，但同样写记录）
  → 写 /opt/l4d2-manager/control/repair-request.json
  → l4d2-repair.path (PathChanged) 触发 l4d2-repair.service
  → /opt/l4d2-manager/repair-addons.sh
      ├─ 自行扫描 addons/*.vpk 判定坏包
      ├─ mv 到 addons/.quarantine-<ts>/
      ├─ 清理 addonlist.txt / maplist.txt 中的引用（先备份）
      ├─ 复检，产出 remaining[]
      └─ action=quarantine_and_restart 时 systemctl restart l4d2.service + 等端口
  → 写 /opt/l4d2-manager/control/repair-result.json
  → 容器轮询到 request_id 匹配的结果
      ├─ action=quarantine_and_reload：容器内执行 RCON 热重载，记录 reload 结果
      └─ 追加 data/repair-history.jsonl
  → 返回前端；重启模式进入 30s 恢复轮询，失败弹诊断
```

## 3. 文件清单

### 3.1 新增（宿主机侧，`manifest/systemd/`）

| 文件 | 作用 |
|---|---|
| `repair-addons.sh` | 隔离 / 清引用 / 复检 / 重启，root 执行，0755 |
| `l4d2-repair.path` | 监听 `control/repair-request.json` |
| `l4d2-repair.service` | oneshot，执行上面脚本 |

### 3.2 新增（后端）

| 文件 | 作用 |
|---|---|
| `backend/logic/server_repair.go` | 请求/结果结构、写请求、轮询结果、容器侧复检、热重载、处置记录读写 |
| `backend/logic/server_repair_test.go` | 单测：动作校验、noop、默认动作、热重载成功/失败、记录追加与排序 |
| `backend/controller/server_repair.go` | `POST /server-startup/repair`、`POST /server-startup/repair/history`，仅 admin |

### 3.3 修改

| 文件 | 改动 |
|---|---|
| `backend/main.go` | `serverStartup` 组新增 `/repair` 与 `/repair/history` 路由 |
| `frontend/src/services/api.ts` | 新增 `ServerRepairResult` 等类型、`repairServerAddons()`、`getServerRepairHistory()` |
| `frontend/src/components/ServerDiagnosticsModal.vue` | 坏 VPK 时显示「隔离并热重载」「隔离并重启」、展示上次处置与「处置记录」 |
| `frontend/src/views/Home.vue` | 处置确认、调用接口、隔离后刷新诊断与记录、重启后 30s 恢复轮询 |

## 4. 接口契约

`POST /server-startup/repair`，`Authorization: Bearer <管理员密码>`，body：

```json
{ "action": "quarantine" }
```

`action` 取值：`quarantine`（只隔离 + 复检）、`quarantine_and_reload`（隔离 + RCON 热重载，**推荐**）、`quarantine_and_restart`（隔离 + 重启，兜底）。缺省（请求体为空或不合法）为 `quarantine`，即**不会**重启服务。前端使用 JSON 提交，避免表单编码导致动作丢失。

响应（HTTP 200）：

```json
{
  "request_id": "…",
  "action": "quarantine_and_reload",
  "status": "ok",              // ok | partial | noop | timeout | error
  "message": "已隔离 1 个损坏 VPK，并清理了 addonlist/maplist 中的引用，已热重载（服务未重启）",
  "addons_dir": "/home/ubuntu/steamcmd/l4d2/left4dead2/addons",
  "quarantine_dir": "/home/ubuntu/steamcmd/l4d2/left4dead2/addons/.quarantine-20261111-034500",
  "quarantined": [{ "name": "3460315999.vpk", "size": 12, "reason": "目录树为空（空 VPK）" }],
  "cleaned_references": [{ "file": "addonlist.txt", "removed": 1, "backup": "addonlist.txt.bak-20261111-034500" }],
  "remaining": [],
  "verify_remaining": [],
  "reload": { "requested": true, "ok": true, "command": "update_addon_paths; mission_reload" },
  "restart": { "requested": false, "ok": false, "port": 12138, "port_listening": true },
  "timeout": false,
  "finished_at": "2026-11-11T03:45:12+00:00"
}
```

`status=noop` 表示面板自查 `addons/` 没有坏包，未提交宿主机任务（仍会写一条处置记录）。

`POST /server-startup/repair/history`，body `{ "limit": 20 }`（1–100），返回：

```json
{ "entries": [ { "id": "…", "at": "2026-11-11T12:45:12+08:00", "operator": "admin",
  "action": "quarantine_and_reload", "status": "ok", "message": "…",
  "quarantined": [ … ], "reload": { … }, "restart": { … }, "duration_ms": 1876 } ] }
```

记录落盘位置：容器内 `/data/repair-history.jsonl` = 宿主机 `/opt/l4d2-manager/data/repair-history.jsonl`，一行一条 JSON，最多保留 500 行。

## 5. 部署步骤

```bash
# 1) 安装宿主机脚本与 unit
sudo install -m 0755 -o root -g root manifest/systemd/repair-addons.sh /opt/l4d2-manager/repair-addons.sh
sudo install -m 0644 -o root -g root manifest/systemd/l4d2-repair.path \
  manifest/systemd/l4d2-repair.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now l4d2-repair.path
systemctl is-active l4d2-repair.path   # 期望 active

# 2) 重新构建镜像并重启面板
docker build -f Dockerfile.diagnostics -t l4d2-manager-next:diagnostics-20261111 .
sudo docker compose -f /opt/l4d2-manager/compose.yaml up -d --force-recreate l4d2-manager
```

## 6. 验收步骤

```bash
# 造坏包（12 字节空 VPK）
printf '\x34\x12\xaa\x55\x01\x00\x00\x00\x00\x00\x00\x00' \
  | sudo tee /home/ubuntu/steamcmd/l4d2/left4dead2/addons/zz-test-empty.vpk >/dev/null

# L1：只隔离
curl -sS -X POST http://127.0.0.1:27020/server-startup/repair \
  -H "Authorization: Bearer $MANAGER_PASSWORD" \
  -H 'Content-Type: application/json' -d '{"action":"quarantine"}' | python3 -m json.tool
# 期望：status=ok，quarantined 含 zz-test-empty.vpk（12 B，目录树为空（空 VPK）），remaining 为空
# 期望：addons/ 下出现 .quarantine-<ts>/，原文件已不在顶层

# L2：隔离并热重载（推荐，不重启）
printf '\x34\x12\xaa\x55\x01\x00\x00\x00\x00\x00\x00\x00' \
  | sudo tee /home/ubuntu/steamcmd/l4d2/left4dead2/addons/zz-test-empty.vpk >/dev/null
curl -sS -X POST http://127.0.0.1:27020/server-startup/repair \
  -H "Authorization: Bearer $MANAGER_PASSWORD" \
  -H 'Content-Type: application/json' -d '{"action":"quarantine_and_reload"}' | python3 -m json.tool
# 期望：status=ok，reload.ok=true，restart.requested=false，MainPID 不变，端口持续监听

# L2：隔离并重启
printf '\x34\x12\xaa\x55\x01\x00\x00\x00\x00\x00\x00\x00' \
  | sudo tee /home/ubuntu/steamcmd/l4d2/left4dead2/addons/zz-test-empty.vpk >/dev/null
curl -sS -X POST http://127.0.0.1:27020/server-startup/repair \
  -H "Authorization: Bearer $MANAGER_PASSWORD" \
  -H 'Content-Type: application/json' -d '{"action":"quarantine_and_restart"}' | python3 -m json.tool
# 期望：restart.ok=true，port_listening=true，status=ok

# 处置记录
curl -sS -X POST http://127.0.0.1:27020/server-startup/repair/history \
  -H "Authorization: Bearer $MANAGER_PASSWORD" \
  -H 'Content-Type: application/json' -d '{"limit":5}' | python3 -m json.tool
# 期望：entries 按时间新→旧，含上面两次处置
sudo cat /opt/l4d2-manager/data/repair-history.jsonl | tail -3

# 再点一次诊断，期望不再出现「发现损坏的地图 VPK」
```

前端验收：管理员首页 → 启动诊断 → 若首条为「发现损坏的地图 VPK」，出现「隔离并热重载」「隔离并重启」；点热重载 → 隔离成功且服务不重启（MainPID 不变），展开「处置记录」能看到本次记录；点重启 → 30s 内恢复在线或自动重弹诊断。

## 7. 回滚

```bash
# 恢复被隔离的文件
mv /home/ubuntu/steamcmd/l4d2/left4dead2/addons/.quarantine-<ts>/<name>.vpk \
   /home/ubuntu/steamcmd/l4d2/left4dead2/addons/
# 恢复引用清单
cp -a /home/ubuntu/steamcmd/l4d2/left4dead2/addons/addonlist.txt.bak-<ts> \
   /home/ubuntu/steamcmd/l4d2/left4dead2/addons/addonlist.txt

# 停用处置能力（面板与诊断不受影响，接口返回「宿主机处置桥接未响应」）
sudo systemctl disable --now l4d2-repair.path
sudo rm -f /etc/systemd/system/l4d2-repair.path /etc/systemd/system/l4d2-repair.service
sudo systemctl daemon-reload
```

## 8. 已知边界（本次不做）

- 不做无限自动重试，不自动卸载插件、不改启动参数、不自动改权限。
- 插件冲突只列出最近一次启动以来加载失败的 `.smx`，处置仍建议人工移出。
- 地图名写错、第三方依赖缺失这类无稳定日志特征的故障，只落到「systemd 启动单元失败 / 端口未监听」兜底。

## 9. 全链路取证与诊断覆盖矩阵（2026-11-11）

### 9.1 取证方法

在真实服务器上按用例注入故障（每次注入后恢复 unit 并重启，游戏 0 人在线），逐一采集诊断接口返回并对照 journal 原文：

| 用例 | 注入方式 | 修复前诊断 | 修复后诊断 |
|---|---|---|---|
| 12 字节空 VPK | 写入 `addons/zz-test-empty.vpk` | 已能报「发现损坏的地图 VPK」 | 保持 |
| 端口被占用（假健康） | 停服 → python 绑定 UDP 12138 → 启动 | **漏检**：端口显示已监听、无任何错误 | 「游戏端口被其他进程占用」，列出 `python3(pid=…)` |
| 工作目录不存在 | unit `WorkingDirectory=/tmp/zz-does-not-exist` | 只报「systemd 启动单元失败」 | 「工作目录不存在或不可访问」（匹配 `CHDIR` / `status=200/CHDIR`） |
| srcds 无执行权限 | `chmod 000 srcds_run` | 报「文件权限不足」，建议不精确 | 「启动文件不可执行」，带路径与 `chmod +x` 建议 |
| ExecStart 路径不存在 | 改成 `/tmp/zz-missing-srcds_run` | **误报**「文件权限不足」（读到上一次的日志） | 「启动文件缺失」，带缺失路径；不再误报权限 |
| 插件坏包 | 放入 4 字节 `zz_bad.smx` | **漏检** | 「插件加载失败」列出 `zz_bad.smx`；服务未监听时升级为 error |
| OOM | 未实机注入（避免打断服务），用合成 journal 单测覆盖 | 漏检 | 「内存不足，游戏进程被系统终止」 |
| 磁盘剩余不足 | 用诊断字段 + 单测覆盖（未真填满磁盘） | 漏检 | 「磁盘剩余空间不足」（< 1 GiB） |
| systemd 单元未加载 | `LoadState != loaded` | 漏检 | 「systemd 单元文件异常」 |
| 启动地图名错误 | 替换 `+map zz_no_such_map` | 服务端未产生稳定特征日志，未做规则 | 兜底为「systemd 启动单元失败 / 端口未监听」 |

### 9.2 两个关键修复

1. **时间窗污染**：原来所有规则都读最近 200 行 journal，上一次故障（甚至上一次实验）的 `Permission denied` 会串到后续诊断里。现在宿主机额外采集 `journal_recent`（最近 30 分钟、尾部 800 行），后端再截取**最后一次 `Starting …` 之后**的日志用于瞬态判断，历史事件不再误报。
2. **假健康**：srcds 绑定 12138 失败时会自动改用 12139，`ss` 仍显示 12138 被监听，但监听者是别的进程。宿主机解析 `ss` 的 `users:(("name",pid=…))` 输出 `port_owners`，后端发现监听者不是 `srcds*` 就直接报端口被占用。

### 9.3 一键处置能力（新增）

| 动作 | 适用场景 | 行为 | 风险 |
|---|---|---|---|
| `quarantine_and_reload` | 损坏 VPK | 隔离 + 清引用 + RCON 热重载，不重启 | 低 |
| `quarantine_and_restart` | 损坏 VPK 且热重载不生效 | 隔离 + 重启 | 踢人 |
| `restart_service` | 任意「起不来」兜底 | 仅 `systemctl restart` | 踢人 |
| `kill_port_and_restart` | 端口被非 srcds 占用 | 终止占用进程（SIGTERM→SIGKILL）→ 重启 → 等端口 | 踢人，可能杀掉无关进程，需二次确认 |
| `quarantine` | 只隔离不动服务 | 隔离 + 复检 | 低 |

前端：诊断弹窗在检测到端口冲突时显示「终止占用并重启」，底部常驻「重启服务」；两种都会写入处置记录。

### 9.4 实测验收（2026-11-11）

- 端口占用：诊断首条命中并列出 `python3(pid=228850)`；点「终止占用并重启」→ `POST /server-startup/repair` 200，5.27s，结果「已终止占用端口的进程：python3(pid=228850)；服务已重启，端口 12138 已监听」，MainPID 228866 → 229270，12138 回到 srcds。
- 工作目录 / 执行权限 / 路径缺失 / 插件坏包四类注入均命中对应提示，且互不串扰。
- 处置记录新增 `kill_port_and_restart` 条目，接口与弹窗均可回看。
- 单测新增 11 个诊断用例（端口归属、CHDIR、Exec 缺失/权限、上一轮日志隔离、OOM、插件、磁盘、unit、服务动作分类）全部通过；`gofmt` 无输出、`go vet` 通过、`go test ./...` 全绿。

