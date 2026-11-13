#!/usr/bin/env bash

set -uo pipefail

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
SOURCE_UNIT="${CONTROL_DIR}/l4d2.service"
BACKUP_UNIT="${CONTROL_DIR}/l4d2.service.bak"
RESULT_PATH="${CONTROL_DIR}/l4d2-startup-result.json"
TARGET_UNIT="${L4D2_STARTUP_TARGET_PATH:-/etc/systemd/system/l4d2.service}"
SERVICE_NAME="${L4D2_STARTUP_SERVICE_NAME:-l4d2.service}"

write_result() {
  local status="$1"
  local message="$2"
  local checksum="${3:-}"
  local daemon_reload="${4:-false}"

  python3 - "$RESULT_PATH" "$status" "$message" "$checksum" "$daemon_reload" "$TARGET_UNIT" <<'PY'
import datetime
import json
import os
import sys

path, status, message, checksum, daemon_reload, target_path = sys.argv[1:]
payload = {
    "status": status,
    "message": message,
    "unit_sha256": checksum,
    "applied_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "target_path": target_path,
    "daemon_reload": daemon_reload == "true",
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

if [[ ! -r "$SOURCE_UNIT" ]]; then
  write_result "error" "未找到启动项快照" "" "false"
  exit 1
fi

checksum="$(sha256sum "$SOURCE_UNIT" | awk '{print $1}')"
verify_output=""
if ! verify_output="$(systemd-analyze verify "$SOURCE_UNIT" 2>&1)"; then
  write_result "error" "systemd 校验失败: ${verify_output:-未知错误}" "$checksum" "false"
  exit 1
fi

target_dir="$(dirname "$TARGET_UNIT")"
temporary_unit="${target_dir}/.${SERVICE_NAME}.manager-apply"
had_target=false
if [[ -f "$TARGET_UNIT" ]]; then
  had_target=true
  cp --preserve=mode,ownership -- "$TARGET_UNIT" "$BACKUP_UNIT"
fi

cleanup() {
  rm -f -- "$temporary_unit"
}
trap cleanup EXIT

if ! install -m 0644 -o root -g root -- "$SOURCE_UNIT" "$temporary_unit"; then
  write_result "error" "无法写入 systemd 临时启动项" "$checksum" "false"
  exit 1
fi

if ! mv -f -- "$temporary_unit" "$TARGET_UNIT"; then
  write_result "error" "无法替换 systemd 启动项" "$checksum" "false"
  exit 1
fi

if ! reload_output="$(systemctl daemon-reload 2>&1)"; then
  if [[ "$had_target" == true && -r "$BACKUP_UNIT" ]]; then
    install -m 0644 -o root -g root -- "$BACKUP_UNIT" "$TARGET_UNIT"
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
  write_result "error" "systemd daemon-reload 失败: ${reload_output:-未知错误}" "$checksum" "false"
  exit 1
fi

write_result "success" "systemd 服务定义已重载" "$checksum" "true"
