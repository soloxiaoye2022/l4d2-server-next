#!/usr/bin/env bash

set -uo pipefail

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
REQUEST_PATH="${CONTROL_DIR}/maintenance-request.json"
RESULT_PATH="${CONTROL_DIR}/maintenance-result.json"
UPDATE_LOG="${CONTROL_DIR}/l4d2-update.log"
LOCK_PATH="${L4D2_MAINTENANCE_LOCK:-/run/lock/l4d2-manager-maintenance.lock}"

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
  runuser -u ubuntu -- bash -c \
    'cd -- "$1" && shift && timeout 180 ./steamcmd.sh "$@"' \
    _ "$steam_dir" \
    +login anonymous \
    +app_info_update 1 \
    +app_info_print 222860 \
    +quit >"$output_file" 2>&1
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
  exit 1
}
IFS=$'\t' read -r request_id action steam_dir install_dir service_name <<<"$request_line"
manifest="${install_dir}/steamapps/appmanifest_222860.acf"
installed_build="$(read_build_id "$manifest" || true)"

write_result "$request_id" "$action" "running" "正在执行服务端维护任务" \
  "$installed_build" "" "false" ""

if [[ ! -x "${steam_dir}/steamcmd.sh" ]]; then
  write_result "$request_id" "$action" "error" "SteamCMD 不存在或不可执行" \
    "$installed_build" "" "false" ""
  exit 1
fi
if [[ "$install_dir" != "$steam_dir"/* ]]; then
  write_result "$request_id" "$action" "error" "游戏安装目录不在 SteamCMD 目录下" \
    "$installed_build" "" "false" ""
  exit 1
fi
if [[ "$(stat -c '%U' "$install_dir")" != "ubuntu" ]]; then
  chown -R ubuntu:ubuntu "$install_dir"
fi

if [[ "$action" == "check" ]]; then
  latest_build="$(query_latest_build "$steam_dir" "$UPDATE_LOG" || true)"
  if [[ -z "$latest_build" ]]; then
    tail_text="$(tail -n 80 "$UPDATE_LOG" 2>/dev/null || true)"
    write_result "$request_id" "$action" "error" "检查更新失败，请查看 SteamCMD 输出" \
      "$installed_build" "" "false" "$tail_text"
    exit 1
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
  exit 1
fi

if ! systemctl stop "$service_name"; then
  write_result "$request_id" "$action" "error" "停止游戏服失败" \
    "$installed_build" "" "false" ""
  exit 1
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
systemctl start "$service_name" || start_code=$?
if [[ $start_code -eq 0 ]]; then
  systemctl is-active --quiet "$service_name" || start_code=$?
fi

installed_after="$(read_build_id "$manifest" || true)"
tail_text="$(tail -n 120 "$UPDATE_LOG" 2>/dev/null || true)"
if [[ $update_code -ne 0 ]]; then
  write_result "$request_id" "$action" "error" "SteamCMD 更新失败，已尝试重新启动游戏服" \
    "$installed_after" "" "false" "$tail_text"
  exit 1
fi
if [[ $start_code -ne 0 ]]; then
  write_result "$request_id" "$action" "error" "更新完成，但游戏服重新启动失败" \
    "$installed_after" "" "false" "$tail_text"
  exit 1
fi

write_result "$request_id" "$action" "success" "求生服务端更新完成，游戏服已重新启动" \
  "$installed_after" "$installed_after" "false" "$tail_text"
