# 服务端维护

“服务端维护”位于“服务器设置”，集中处理求生服务端检查更新、立即更新和 L4D2 SourceTV 修复。启动项管理只负责启动命令和参数。

## 求生服务端更新

页面读取：

- 当前安装目录中的 `steamapps/appmanifest_222860.acf`。
- 当前 `buildid`。
- SteamCMD 匿名查询到的公共分支最新 `buildid`。

“检查更新”不会停止服务器，只执行 SteamCMD `app_info_print`。

“立即更新”会：

1. 确认当前在线玩家情况由管理员自行判断。
2. 停止 systemd 中的游戏服。
3. 以 `ubuntu` 用户运行 SteamCMD，避免 root 环境触发 L4D2 匿名更新平台识别问题。
4. 执行 `app_update 222860 validate`。
5. 如果 SteamCMD 返回 `Invalid platform`，按 LinuxGSM 的兼容流程先下载 Windows 平台文件，再回到 Linux 执行 `validate`。
6. 重新启动游戏服并确认 systemd 状态。
7. 写入更新日志和新的 buildid。

更新过程会中断在线玩家，页面会二次确认。

## SourceTV 修复

页面内置 `shqke/sourcetvsupport` 的 SourceMod 1.12 Linux/Windows 构建，固定到提交 `a19d90c`。一键安装会写入：

- `addons/sourcemod/extensions/sourcetvsupport.ext.2.l4d2.*`
- `addons/sourcemod/extensions/sourcetvsupport.autoload`
- `addons/sourcemod/gamedata/sourcetvsupport.txt`
- `addons/sourcetvsupport/bin/vsp_l4d2.*`

同时把以下参数写入启动项：

- `-hltv`
- `+tv_enable 1`
- `+tv_port 27021`

安装不会自动重启游戏服。需要重启后加载扩展。

该修复解决 L4D2 SourceTV 广播、录像、客户端断开崩溃、Steam 游戏服务器接口和端口冲突等问题，但上游仍明确说明 SourceTV 中继功能不完整，因此不提供 `tv_relay` 作为内置选择。

第三方文件、许可证、固定提交和 SHA-256 记录位于 `backend/resources/sourcetvsupport/`。

## 宿主机维护 helper

管理器容器不直接运行 Ubuntu 的 SteamCMD，而是写入 `/opt/l4d2-manager/control/maintenance-request.json`。宿主机安装：

- `l4d2-maintenance.path`
- `l4d2-maintenance.service`
- `/opt/l4d2-manager/run-maintenance.sh`

执行结果写入 `maintenance-result.json`，SteamCMD 日志写入 `l4d2-update.log`。
