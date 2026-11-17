# cvar 描述校正记录（2026-11-17）

## 一、问题来源

用户对照第三方 `server.cfg` 注释与面板 cvar 描述，指出两处明显不符：

```cfg
//禁止匹配（1:开启，0：关闭）
sm_cvar sv_force_unreserved "1"
//移除大厅
sm_cvar sv_cookie "0"

//绕过SteamID验证
sv_steam_bypass 1
```

结论：用户的理解是对的，面板原来的描述是错的。因此对全部 47 条 cvar 重新做了一次逐条核实。

## 二、核实方法

Source 引擎对每个 ConVar 都保存了一段内置帮助文本，`help <cvar>` 会原样输出，这是比任何第三方 wiki 都权威的一手资料。本次全部描述都以它为准。

采集方式：通过面板自身的 RCON 接口（`POST /rcon`，`Authorization: Bearer <password>`）执行 `help <cvar>`，从真实运行中的服务器取回原文，再对插件类变量用插件源码/README 交叉验证实际行为。

采样时间：2026-11-17，目标服务器 101.43.105.208:27015。

## 三、核心结论：三条被误解的变量

| 变量 | 原面板描述（错） | 引擎原文 | 正确含义 |
| --- | --- | --- | --- |
| `sv_steam_bypass` | 「Steam 大厅/人数绕过」 | `Bypass steam validation` | 跳过 SteamID 官方校验，与大厅、人数无关 |
| `sv_force_unreserved` | 「强制使用未预留槽位」 | `Disallow lobby reservation` | 禁止大厅预留；副作用是大厅匹配不到本服 |
| `sv_cookie` | 目录中缺失 | `Lobby reservation cookie` | 大厅预留凭据编号，由引擎写入，不该手改 |

用户注释的「禁止匹配」「移除大厅」在语义方向上是对的，只是措辞不够精确；真正错的是面板描述。

## 四、全部修正清单

### 默认值与引擎边界类（写错会让用户以为参数无效）

| 变量 | 原值 | 实测值 |
| --- | --- | --- |
| `z_difficulty` | `Impossible` | `Normal` |
| `sv_consistency` | `0` | `1` |
| `sb_all_bot_game` | `1` | `0` |
| `sv_maxplayers` range.min | `1` | `-1` |
| `sv_maxrate` range.max | `1000000` | `30000`（引擎硬上限，超出被截断） |
| `sv_mincmdrate` range.min | `10` | `0` |
| `l4d2_afk_kick` | `2` | 插件默认 1，服务器当前 2 |
| `l4d2_afk_player_kick_time` | `600` | 插件默认 300，服务器当前 600 |
| `l4d2_afk_player_kick_time_admin` | `1200` | 插件默认 500，服务器当前 1200 |
| `l4d2_tank_health` | `1000` | 插件默认 2500，服务器当前 1000 |
| `l4d2_witch_health` | `250` | 插件默认 200，服务器当前 250 |
| `l4d2_witch_Switch` | `0` | 插件默认 1，服务器当前 0 |
| `l4d2_vote_menuTime` | `25` | 插件默认 25，服务器当前 20 |

默认值统一改为「插件/引擎默认 X；当前服务器为 Y」的写法，避免把服务器现值误当默认值。

### 语义错误类（会导致误用）

| 变量 | 问题 |
| --- | --- |
| `sv_infinite_ammo` | 原描述把 1 和 2 的语义写反。实测 1 = 弹匣与备弹都不减、无需换弹；2 = 备弹不减但仍需换弹 |
| `allow_all_bot_survivor_team` | 漏掉原文后半句「Human players are still required for the game to be active」，原描述暗示可零真人跑图，实际不行 |
| `l4d2_tank_minimum` / `l4d2_witch_minimum` | 漏掉「四名生还者或以内时」限定，名字里的 minimum 不是保底值而是四人及以内基础血量 |
| `survivor_revive_health` | 未说明是临时生命（虚血），会被误读成永久血量 |
| `sv_maxcmdrate` | 未说明只有 `sv_mincmdrate > 0` 时才生效 |
| `sv_visiblemaxplayers` | 未说明只改列表显示，不改变实际可进人数 |
| `sv_lan` | 未说明同时禁止心跳、身份校验和公网网段 |
| `l4d2_afk_kick` | 未说明它是总开关，设 0 会连 `!timer` 指令一起失效 |

### 补充项

- 新增 `sv_cookie` 条目（原目录完全缺失）
- 所有条目补 `verifiedOnServer: true`
- 引擎原文统一以「引擎原文：」前缀保留英文，中文解释放在其后

## 五、UI 层配套改动

`CvarRangeInfo` 新增可选字段 `hardLimit?: boolean`。当范围来自服务器 help 实测的硬性边界（超出会被引擎截断或拒绝）时置为 true，`CvarDefinitionDetails.vue` 会额外显示「（引擎硬边界，超出会被截断或拒绝）」。

目前标记的两项：
- `sv_maxplayers`：`min. -1.000000 max. 31.000000`
- `sv_maxrate`：`min. 0.000000 max. 30000.000000`

其余变量的范围是经验建议值，不标 hardLimit，避免用户以为填超了会报错。

## 六、验证过程与结果

1. **静态校验**（`check_cvars.mjs`）：47 条条目，唯一性、必填字段、`verifiedOnServer` 标记、实测默认值一致性、引擎边界抽查全部通过
2. **前端构建**：`npm run build` 通过，无 TS 报错
3. **远端部署**：增量镜像 `l4d2-manager-next:webfix-20261117`，仅替换 `/static`，二进制与容器配置保持不变
4. **产物断言**：容器内 `chunk-Ci78MXs8.js` 含全部新描述文本，旧错误描述残留 0 处
5. **浏览器渲染**：真实登录面板，`sv_steam_bypass`、`sv_force_unreserved` 在服务器配置页正确显示新描述；`sv_cookie` 在参数库检索到完整条目；控制台无报错，接口无 4xx/5xx
6. **健康检查**：`/` 返回 200，`/rcon` 返回 200

## 七、部署事故与恢复

首次部署脚本顺序错误：先 `docker rm -f l4d2-manager`，后构建镜像，且 Dockerfile 在解包前上传被 `rm -rf` 清掉，导致构建失败、容器已删除、服务短暂中断。

恢复措施：
1. 用远端已有的 `l4d2-manager-next:merge1160-20261117` 按原 env/binds/network/user 完整重建容器，服务恢复
2. 修正脚本顺序：解包 → `chown` 归还属主 → 上传 Dockerfile → 构建 → 校验产物 → 才删旧容器
3. 构建失败时脚本直接 return 1，不触碰现有容器

后续所有部署脚本必须遵循「新镜像验证通过后才替换旧容器」的顺序。

## 八、遗留说明

- 回滚容器 `l4d2-manager-old-20261117` 已不存在（在事故恢复阶段被清理），当前运行的是 `l4d2-manager-next:webfix-20261117`
- 描述文本仅代表 2026-11-17 这次 help 实测的引擎与插件版本；若后续升级引擎或插件，应重新采集一次
