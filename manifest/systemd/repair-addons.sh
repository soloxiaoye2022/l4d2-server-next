#!/usr/bin/env bash
# Quarantine broken addon VPKs, clean their references and optionally restart
# the game service. Runs as root via l4d2-repair.path / l4d2-repair.service.

set -uo pipefail

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
REQUEST_PATH="${CONTROL_DIR}/repair-request.json"
RESULT_PATH="${CONTROL_DIR}/repair-result.json"
ADDONS_DIR="${L4D2_ADDONS_DIR:-/home/ubuntu/steamcmd/l4d2/left4dead2/addons}"
SERVICE_NAME="${L4D2_STARTUP_SERVICE_NAME:-l4d2.service}"
GAME_PORT="${L4D2_GAME_PORT:-0}"

mkdir -p "$CONTROL_DIR" 2>/dev/null || true

exec python3 - "$REQUEST_PATH" "$RESULT_PATH" "$ADDONS_DIR" "$SERVICE_NAME" "$GAME_PORT" <<'PY'
import datetime
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time

request_path, result_path, addons_dir, service, game_port = sys.argv[1:6]
try:
    game_port = int(game_port)
except ValueError:
    game_port = 0

MAX_FILES = 20
REFERENCE_FILES = ("addonlist.txt", "maplist.txt")


def now_iso():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def stamp():
    return datetime.datetime.now().strftime("%Y%m%d-%H%M%S")


def run(command, timeout=10):
    try:
        completed = subprocess.run(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=timeout,
        )
        return (completed.stdout or "").strip()
    except Exception as error:  # noqa: BLE001
        return f"{' '.join(command)} 执行失败: {error}"


def write_result(payload):
    payload.setdefault("finished_at", now_iso())
    directory = os.path.dirname(result_path) or "."
    os.makedirs(directory, exist_ok=True)
    handle, temporary = tempfile.mkstemp(prefix=".repair-", dir=directory)
    with os.fdopen(handle, "w", encoding="utf-8") as output:
        json.dump(payload, output, ensure_ascii=False, indent=2)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    os.chmod(temporary, 0o644)
    os.replace(temporary, result_path)


def is_vpk(name):
    return name.lower().endswith(".vpk")


def vpk_problem(path):
    try:
        size = os.path.getsize(path)
    except OSError:
        return "无法读取", 0
    if size < 12:
        return "文件过短，没有 VPK 头", size
    try:
        with open(path, "rb") as handle:
            header = handle.read(28)
    except OSError:
        return "无法读取", size
    if len(header) < 12:
        return "文件过短，没有 VPK 头", size
    if int.from_bytes(header[0:4], "little") != 0x55AA1234:
        return "文件头不是 VPK 格式", size
    version = int.from_bytes(header[4:8], "little")
    tree_size = int.from_bytes(header[8:12], "little")
    header_size = 28 if version == 2 else 12
    if tree_size == 0:
        return "目录树为空（空 VPK）", size
    if size < header_size + tree_size:
        return "文件被截断，目录树不完整", size
    return "", size


def scan_broken():
    broken = []
    if not os.path.isdir(addons_dir):
        return broken
    try:
        names = sorted(os.listdir(addons_dir))
    except OSError:
        return broken
    for name in names:
        if not is_vpk(name):
            continue
        path = os.path.join(addons_dir, name)
        if not os.path.isfile(path):
            continue
        reason, size = vpk_problem(path)
        if reason:
            broken.append({"name": name, "size": size, "reason": reason})
        if len(broken) >= MAX_FILES:
            break
    return broken


def clean_references(quarantined_names, backup_stamp):
    cleaned = []
    for name in REFERENCE_FILES:
        path = os.path.join(addons_dir, name)
        if not os.path.isfile(path):
            continue
        try:
            with open(path, encoding="utf-8", errors="replace") as handle:
                lines = handle.readlines()
        except OSError:
            continue
        kept = []
        removed = 0
        for line in lines:
            if any(item in line for item in quarantined_names):
                removed += 1
                continue
            kept.append(line)
        if removed == 0:
            continue
        backup = f"{path}.bak-{backup_stamp}"
        try:
            shutil.copy2(path, backup)
            with open(path, "w", encoding="utf-8") as handle:
                handle.writelines(kept)
        except OSError as error:
            cleaned.append({"file": name, "removed": 0, "backup": "", "error": str(error)})
            continue
        cleaned.append({"file": name, "removed": removed, "backup": os.path.basename(backup)})
    return cleaned


def detect_port():
    if game_port > 0:
        return game_port
    for candidate in (
        f"/etc/systemd/system/{service}",
        "/control/l4d2.service",
    ):
        if not os.path.isfile(candidate):
            continue
        try:
            with open(candidate, encoding="utf-8", errors="replace") as handle:
                text = handle.read()
        except OSError:
            continue
        matches = re.findall(r"(?:\+hostport|\+port|-port)\s+(\d+)", text)
        if matches:
            return int(matches[-1])
    return 27015


def port_listening(port):
    output = run(["ss", "-lntup"], timeout=6)
    return bool(re.search(rf":{port}\b", output))


def port_owner_entries(port):
    output = run(["ss", "-lntup"], timeout=6)
    owners = []
    for line in output.splitlines():
        if not re.search(rf":{port}\b", line):
            continue
        for command, pid in re.findall(r'users:\(\("([^"]+)",pid=(\d+)', line):
            owners.append({"command": command, "pid": int(pid)})
    return owners


def kill_port_owners(port):
    killed = []
    for owner in port_owner_entries(port):
        if owner["command"].lower().startswith("srcds"):
            continue
        try:
            os.kill(owner["pid"], signal.SIGTERM)
            killed.append(owner)
        except OSError:
            continue
    if killed:
        time.sleep(3)
        for owner in killed:
            try:
                os.kill(owner["pid"], signal.SIGKILL)
            except OSError:
                continue
    return killed


def restart_service(port):
    state = {"requested": True, "ok": False, "port": port, "port_listening": False}
    run(["systemctl", "restart", service], timeout=60)
    deadline = time.time() + 30
    while time.time() < deadline:
        if port_listening(port):
            state["ok"] = True
            state["port_listening"] = True
            break
        time.sleep(2)
    state["active_state"] = run(["systemctl", "is-active", service], timeout=5)
    if not state["ok"]:
        state["message"] = f"服务已重启，但 {port} 端口在 30 秒内仍未被监听"
    return state


try:
    with open(request_path, encoding="utf-8") as handle:
        request = json.load(handle)
except Exception as error:  # noqa: BLE001
    write_result({
        "request_id": "",
        "action": "",
        "status": "error",
        "message": f"处置请求无效: {error}",
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
    })
    sys.exit(0)

request_id = str(request.get("request_id", ""))
action = str(request.get("action", "quarantine")).strip() or "quarantine"
if action not in (
    "quarantine",
    "quarantine_and_reload",
    "quarantine_and_restart",
    "restart_service",
    "kill_port_and_restart",
):
    write_result({
        "request_id": request_id,
        "action": action,
        "status": "error",
        "message": "不支持的处置动作",
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
    })
    sys.exit(0)

if action in ("restart_service", "kill_port_and_restart"):
    port = detect_port()
    killed = kill_port_owners(port) if action == "kill_port_and_restart" else []
    restart_state = restart_service(port)
    message = f"服务已重启，端口 {port} "
    message += "已监听" if restart_state.get("ok") else "在 30 秒内仍未被监听"
    if killed:
        names = "、".join(f"{item['command']}(pid={item['pid']})" for item in killed)
        message = f"已终止占用端口的进程：{names}；" + message
    write_result({
        "request_id": request_id,
        "action": action,
        "status": "ok" if restart_state.get("ok") else "partial",
        "message": message,
        "addons_dir": addons_dir,
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
        "killed": killed,
        "restart": restart_state,
    })
    sys.exit(0)

if not os.path.isdir(addons_dir):
    write_result({
        "request_id": request_id,
        "action": action,
        "status": "error",
        "message": f"addons 目录不存在: {addons_dir}",
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
    })
    sys.exit(0)

broken = scan_broken()
if not broken:
    write_result({
        "request_id": request_id,
        "action": action,
        "status": "noop",
        "message": "addons 目录下没有发现损坏的 VPK",
        "addons_dir": addons_dir,
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
    })
    sys.exit(0)

backup_stamp = stamp()
quarantine_dir = os.path.join(addons_dir, f".quarantine-{backup_stamp}")
quarantined = []
try:
    os.makedirs(quarantine_dir, exist_ok=True)
except OSError as error:
    write_result({
        "request_id": request_id,
        "action": action,
        "status": "error",
        "message": f"创建隔离目录失败: {error}",
        "addons_dir": addons_dir,
        "quarantined": [],
        "cleaned_references": [],
        "remaining": [],
    })
    sys.exit(0)

for item in broken:
    source = os.path.join(addons_dir, item["name"])
    target = os.path.join(quarantine_dir, item["name"])
    try:
        shutil.move(source, target)
        quarantined.append(item)
    except OSError as error:
        item["reason"] = f"{item['reason']}；隔离失败: {error}"

cleaned = clean_references([item["name"] for item in quarantined], backup_stamp)
remaining = scan_broken()

restart_state = {
    "requested": action == "quarantine_and_restart",
    "ok": False,
    "port": detect_port(),
    "port_listening": port_listening(detect_port()),
}
if restart_state["requested"]:
    restart_state = restart_service(restart_state["port"])

if remaining:
    status = "partial"
    message = f"已隔离 {len(quarantined)} 个损坏 VPK，仍有 {len(remaining)} 个文件异常，请查看 remaining"
elif not quarantined:
    status = "error"
    message = "扫描到异常文件，但全部隔离失败，请检查 addons 目录权限"
elif restart_state["requested"] and not restart_state.get("ok"):
    status = "partial"
    message = f"已隔离 {len(quarantined)} 个损坏 VPK，但服务重启后端口未在 30 秒内监听"
else:
    status = "ok"
    message = f"已隔离 {len(quarantined)} 个损坏 VPK"
    if cleaned:
        message += "，并清理了 addonlist/maplist 中的引用"
    if restart_state["requested"]:
        message += f"，服务已重启，端口 {restart_state['port']} 已监听"

write_result({
    "request_id": request_id,
    "action": action,
    "status": status,
    "message": message,
    "addons_dir": addons_dir,
    "quarantine_dir": quarantine_dir if quarantined else "",
    "quarantined": quarantined,
    "cleaned_references": cleaned,
    "remaining": remaining,
    "restart": restart_state,
})
PY
