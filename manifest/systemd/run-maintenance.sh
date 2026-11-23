#!/usr/bin/env bash

set -uo pipefail

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
REQUEST_PATH="${CONTROL_DIR}/maintenance-request.json"
RESULT_PATH="${CONTROL_DIR}/maintenance-result.json"
UPDATE_LOG="${CONTROL_DIR}/l4d2-update.log"
# 检查更新单独一个日志，避免 SteamCMD 自更新的 "Downloading update" 写进
# l4d2-update.log，让用户误以为正在更新游戏本体。
CHECK_LOG="${CONTROL_DIR}/l4d2-check.log"
LOCK_PATH="${L4D2_MAINTENANCE_LOCK:-/run/lock/l4d2-manager-maintenance.lock}"
RUNTIME_CONTROL="${L4D2_RUNTIME_CONTROL:-${MANAGER_DIR}/runtime-control.sh}"
RUNTIME_SNAPSHOT="${CONTROL_DIR}/runtime-control.json"

# SteamCMD 客户端自更新与业务查询分别计时。
#
# 为什么必须分开：SteamCMD 首次启动或上游发布新客户端时会先下 ~36MB 自更新包，
# 慢速网络下要十几分钟。旧代码把 app_info 查询和自更新一起塞进 timeout 180，
# 结果每次都在 17% 左右被 kill，自更新永远完成不了、"检查更新"永远失败，
# 而且下次又从 0% 重新下载 —— 形成永久失败循环。
#
# 首次初始化放宽到 3600 秒：cc雨云 实测 1200 秒只下到 2MB（约 1.7 KB/s），
# 25MB 的包在这个速率下需要约 4 小时。慢速链路是常态，不能按快速链路设超时。
# 自更新完成后 ./steamcmd.sh +quit 会秒退，后续调用不会重复等待。
STEAMCMD_SELF_UPDATE_TIMEOUT="${L4D2_STEAMCMD_SELF_UPDATE_TIMEOUT:-3600}"
STEAMCMD_QUERY_TIMEOUT="${L4D2_STEAMCMD_QUERY_TIMEOUT:-180}"

# service_control 停止/启动游戏服。
#
# 修复 D4：优先走 runtime-control.sh（读面板写的运行时快照，交给 Agent 执行），
# 只有快照不存在时才回退 systemctl —— 也就是用户还没接管任何运行时的老部署。
service_control() {
  local action="$1"
  local service="$2"

  if [[ -x "$RUNTIME_CONTROL" && -r "$RUNTIME_SNAPSHOT" ]]; then
    local output code
    # 必须先执行再判 $?：写在 if 条件里的话 $? 拿到的是 if 的结果，不是退出码。
    output="$("$RUNTIME_CONTROL" "$action" "$RUNTIME_SNAPSHOT" 2>&1)"
    code=$?
    if [[ $code -eq 0 ]]; then
      return 0
    fi
    # 退出码 2 表示"无快照/无 Agent"，属于可回退场景；
    # 其他非零是真实执行失败，必须上抛，不能偷偷换成 systemctl。
    if [[ $code -ne 2 ]]; then
      echo "$output"
      return 1
    fi
  fi

  case "$action" in
    stop)
      systemctl stop "$service"
      ;;
    start)
      systemctl start "$service"
      ;;
    restart)
      systemctl restart "$service"
      ;;
    *)
      return 1
      ;;
  esac
}

write_result() {
  local request_id="$1"
  local action="$2"
  local status="$3"
  local message="$4"
  local installed_build="$5"
  local latest_build="$6"
  local update_available="$7"
  local log_tail="$8"

  python3 - "$RESULT_PATH" "$request_id" "$action" "$status" "$message" \
    "$installed_build" "$latest_build" "$update_available" "$log_tail" <<'PY'
import datetime
import json
import os
import sys

path, request_id, action, status, message, installed, latest, available, tail = sys.argv[1:]
payload = {
    "request_id": request_id,
    "action": action,
    "status": status,
    "message": message,
    "installed_build_id": installed,
    "latest_build_id": latest,
    "update_available": available == "true",
    "log_tail": tail,
    "finished_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
temporary = path + ".tmp"
with open(temporary, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, ensure_ascii=False)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
os.replace(temporary, path)
PY
}

read_request() {
  python3 - "$REQUEST_PATH" <<'PY'
import json
import re
import sys

request = json.load(open(sys.argv[1], encoding="utf-8"))
values = [
    request.get("request_id", ""),
    request.get("action", ""),
    request.get("steam_dir", ""),
    request.get("install_dir", ""),
    request.get("service_name", "l4d2.service"),
]
for value in values:
    if not isinstance(value, str) or "\n" in value or "\r" in value:
        raise SystemExit("invalid maintenance request")
for path in values[2:4]:
    if not path.startswith("/") or re.search(r"(^|/)\.\.(/|$)", path):
        raise SystemExit("maintenance path must be normalized and absolute")
if values[4] and not re.fullmatch(r"[A-Za-z0-9_.@:-]+\.service", values[4]):
    raise SystemExit("invalid service name")
print("\t".join(values))
PY
}

read_build_id() {
  local manifest="$1"
  if [[ ! -r "$manifest" ]]; then
    return 1
  fi
  sed -n 's/^[[:space:]]*"buildid"[[:space:]]*"\([0-9]*\)".*/\1/p' "$manifest" | head -1
}

query_latest_build() {
  local steam_dir="$1"
  local output_file="$2"
  # 先单独把 SteamCMD 客户端自更新跑完，再查 app_info。
  # 自更新超时不计入查询超时，失败也不直接判定整体失败 ——
  # 只要后续 app_info 能查到 buildid，本次 check 就是成功的。
  runuser -u ubuntu -- bash -c \
    'cd -- "$1" || exit 1; timeout "$2" ./steamcmd.sh +quit' \
    _ "$steam_dir" "$STEAMCMD_SELF_UPDATE_TIMEOUT" >"$output_file" 2>&1
  runuser -u ubuntu -- bash -c \
    'dir="$1"; limit="$2"; shift 2; cd -- "$dir" && timeout "$limit" ./steamcmd.sh "$@"' \
    _ "$steam_dir" "$STEAMCMD_QUERY_TIMEOUT" \
    +login anonymous \
    +app_info_update 1 \
    +app_info_print 222860 \
    +quit >>"$output_file" 2>&1
  local code=$?
  if [[ $code -ne 0 ]]; then
    return $code
  fi
  python3 - "$output_file" <<'PY'
import re
import sys

text = open(sys.argv[1], encoding="utf-8", errors="replace").read()
match = re.search(
    r'"branches"\s*\{[\s\S]*?"public"\s*\{[\s\S]*?"buildid"\s*"(\d+)"',
    text,
)
if not match:
    raise SystemExit("latest buildid not found")
print(match.group(1))
PY
}

if ! mkdir -p "$CONTROL_DIR"; then
  exit 1
fi
exec 9>"$LOCK_PATH"
if ! flock -n 9; then
  exit 0
fi

if [[ ! -r "$REQUEST_PATH" ]]; then
  exit 0
fi

request_line="$(read_request)" || {
  write_result "" "unknown" "error" "维护请求格式无效" "" "" "false" ""
  exit 0
}
IFS=$'\t' read -r request_id action steam_dir install_dir service_name <<<"$request_line"
manifest="${install_dir}/steamapps/appmanifest_222860.acf"
installed_build="$(read_build_id "$manifest" || true)"

write_result "$request_id" "$action" "running" "正在执行服务端维护任务" \
  "$installed_build" "" "false" ""

# 业务失败统一 exit 0：结论已经写进 maintenance-result.json，
# 面板只认结果文件。若这里 exit 1，systemd 会把 l4d2-maintenance.service
# 标成 failed，用户看到一片红却查不到真正原因（例如网络慢导致的 check 失败）。
if [[ ! -x "${steam_dir}/steamcmd.sh" ]]; then
  # 有人用 Steam 客户端下载游戏文件后手工搬运，宿主机上没有 SteamCMD。
  # 直接说清楚缺什么、装在哪，比只说"不可执行"有用。
  write_result "$request_id" "$action" "error" \
    "宿主机没有 SteamCMD（期望路径 ${steam_dir}/steamcmd.sh）。服务端更新需要它；只跑游戏不需要。安装方法：mkdir -p ${steam_dir} && cd ${steam_dir} && curl -sqL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz | tar zxf -" \
    "$installed_build" "" "false" ""
  exit 0
fi
if [[ "$install_dir" != "$steam_dir"/* ]]; then
  write_result "$request_id" "$action" "error" "游戏安装目录不在 SteamCMD 目录下" \
    "$installed_build" "" "false" ""
  exit 0
fi
if [[ "$(stat -c '%U' "$install_dir")" != "ubuntu" ]]; then
  chown -R ubuntu:ubuntu "$install_dir"
fi

if [[ "$action" == "check" ]]; then
  latest_build="$(query_latest_build "$steam_dir" "$CHECK_LOG" || true)"
  if [[ -z "$latest_build" ]]; then
    tail_text="$(tail -n 80 "$CHECK_LOG" 2>/dev/null || true)"
    write_result "$request_id" "$action" "error" "检查更新失败，请查看 SteamCMD 输出" \
      "$installed_build" "" "false" "$tail_text"
    exit 0
  fi
  update_available="false"
  if [[ -n "$installed_build" && "$installed_build" != "$latest_build" ]]; then
    update_available="true"
  fi
  write_result "$request_id" "$action" "success" "更新检查完成" \
    "$installed_build" "$latest_build" "$update_available" ""
  exit 0
fi

if [[ "$action" != "update" ]]; then
  write_result "$request_id" "$action" "error" "不支持的维护操作" \
    "$installed_build" "" "false" ""
  exit 0
fi

if ! service_control stop "$service_name"; then
  write_result "$request_id" "$action" "error" "停止游戏服失败" \
    "$installed_build" "" "false" ""
  exit 0
fi

update_code=0
(
  : >"$UPDATE_LOG"
  runuser -u ubuntu -- bash -c \
    'cd -- "$1" && shift && timeout 1800 ./steamcmd.sh "$@"' \
    _ "$steam_dir" \
    +force_install_dir "$install_dir" \
    +login anonymous \
    +app_update 222860 validate \
    +quit >>"$UPDATE_LOG" 2>&1
  linux_code=$?
  if [[ $linux_code -ne 0 ]] && grep -qi "Invalid platform" "$UPDATE_LOG"; then
    echo "SteamCMD reported Invalid platform; applying Windows platform workaround." >>"$UPDATE_LOG"
    runuser -u ubuntu -- bash -c \
      'cd -- "$1" && shift && timeout 1800 ./steamcmd.sh "$@"' \
      _ "$steam_dir" \
      +@sSteamCmdForcePlatformType windows \
      +force_install_dir "$install_dir" \
      +login anonymous \
      +app_update 222860 \
      +quit >>"$UPDATE_LOG" 2>&1
    windows_code=$?
    if [[ $windows_code -ne 0 ]]; then
      exit "$windows_code"
    fi
    runuser -u ubuntu -- bash -c \
      'cd -- "$1" && shift && timeout 1800 ./steamcmd.sh "$@"' \
      _ "$steam_dir" \
      +force_install_dir "$install_dir" \
      +login anonymous \
      +app_update 222860 validate \
      +quit >>"$UPDATE_LOG" 2>&1
    exit $?
  fi
  exit "$linux_code"
) >"$UPDATE_LOG" 2>&1 || update_code=$?

start_code=0
service_control start "$service_name" || start_code=$?
if [[ $start_code -eq 0 && -z "${L4D2_RUNTIME_CONTROL:-}" ]]; then
  # 只有回退到 systemctl 的老路径才需要额外确认 is-active。
  # 走运行时快照时，Agent 已经完成了健康检查。
  systemctl is-active --quiet "$service_name" || start_code=$?
fi

installed_after="$(read_build_id "$manifest" || true)"
tail_text="$(tail -n 120 "$UPDATE_LOG" 2>/dev/null || true)"
if [[ $update_code -ne 0 ]]; then
  write_result "$request_id" "$action" "error" "SteamCMD 更新失败，已尝试重新启动游戏服" \
    "$installed_after" "" "false" "$tail_text"
  exit 0
fi
if [[ $start_code -ne 0 ]]; then
  write_result "$request_id" "$action" "error" "更新完成，但游戏服重新启动失败" \
    "$installed_after" "" "false" "$tail_text"
  exit 0
fi

write_result "$request_id" "$action" "success" "求生服务端更新完成，游戏服已重新启动" \
  "$installed_after" "$installed_after" "false" "$tail_text"
