#!/usr/bin/env bash

set -uo pipefail

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
REQUEST_PATH="${CONTROL_DIR}/diagnostics-request.json"
RESULT_PATH="${CONTROL_DIR}/diagnostics-result.json"

mkdir -p "$CONTROL_DIR" 2>/dev/null || true

exec python3 - "$REQUEST_PATH" "$RESULT_PATH" "$CONTROL_DIR" <<'PY'
import datetime
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

request_path, result_path, control_dir = sys.argv[1:4]
default_service = os.environ.get("L4D2_STARTUP_SERVICE_NAME", "l4d2.service")


def run(command, timeout=8):
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
    directory = os.path.dirname(result_path) or "."
    os.makedirs(directory, exist_ok=True)
    handle, temporary = tempfile.mkstemp(prefix=".diagnostics-", dir=directory)
    with os.fdopen(handle, "w", encoding="utf-8") as output:
        json.dump(payload, output, ensure_ascii=False)
        output.write("\n")
        output.flush()
        os.fsync(output.fileno())
    os.chmod(temporary, 0o644)
    os.replace(temporary, result_path)


try:
    with open(request_path, encoding="utf-8") as handle:
        request = json.load(handle)
except Exception as error:  # noqa: BLE001
    write_result({
        "available": False,
        "message": f"诊断请求无效: {error}",
        "request_id": "",
    })
    sys.exit(0)

request_id = str(request.get("request_id", ""))
service = str(request.get("service_name", default_service)).strip() or default_service
if not re.fullmatch(r"[A-Za-z0-9_.@:-]+\.service", service):
    write_result({
        "available": False,
        "message": "服务名不合法",
        "request_id": request_id,
    })
    sys.exit(0)

properties_output = run([
    "systemctl", "show", service,
    "-p", "LoadState",
    "-p", "ActiveState",
    "-p", "SubState",
    "-p", "UnitFileState",
    "-p", "MainPID",
    "-p", "ExecMainStatus",
    "-p", "Result",
    "-p", "NRestarts",
    "-p", "ActiveEnterTimestamp",
])
properties = {}
for line in properties_output.splitlines():
    if "=" in line:
        key, value = line.split("=", 1)
        properties[key] = value

unit_candidates = [
    os.environ.get("L4D2_STARTUP_UNIT_PATH", ""),
    f"/etc/systemd/system/{service}",
    os.path.join(control_dir, "l4d2.service"),
]
unit_path = ""
unit_modified_at = ""
port = int(os.environ.get("L4D2_GAME_PORT", "0") or 0)
for candidate in unit_candidates:
    if not candidate or not os.path.isfile(candidate):
        continue
    if not unit_path:
        unit_path = candidate
        unit_modified_at = datetime.datetime.fromtimestamp(
            os.path.getmtime(candidate), datetime.timezone.utc
        ).isoformat()
    if port <= 0:
        try:
            with open(candidate, encoding="utf-8", errors="replace") as handle:
                text = handle.read()
            matches = re.findall(r"(?:\+hostport|\+port|-port)\s+(\d+)", text)
            if matches:
                port = int(matches[-1])
        except OSError:
            pass
if port <= 0:
    port = 27015

ss_output = run(["ss", "-lntup"], timeout=6)
listeners = [
    line.strip()
    for line in ss_output.splitlines()
    if re.search(rf":{port}\b", line)
]
port_listening = bool(listeners)

port_owners = []
for line in listeners:
    for command, pid in re.findall(r'users:\(\("([^"]+)",pid=(\d+)', line):
        port_owners.append({"command": command, "pid": int(pid)})

process_output = run(["pgrep", "-a", "-f", "[s]rcds"], timeout=5)
processes = [
    line.strip()
    for line in process_output.splitlines()
    if line.strip() and "pgrep" not in line
][:10]

status_text = run(["systemctl", "status", service, "--no-pager", "-l", "-n", "0"], timeout=10)
journal_output = run(["journalctl", "-u", service, "-n", "200", "--no-pager", "-o", "short-iso"], timeout=12)
journal = [line.rstrip() for line in journal_output.splitlines()]
journal = [line[:4000] for line in journal]
recent_output = run(
    ["journalctl", "-u", service, "--since", "-30 min", "--no-pager", "-o", "short-iso"],
    timeout=12,
)
# --since with -n returns the FIRST N lines in the window, not the newest, so
# slice the tail here to keep the most recent 800 lines after filtering.
journal_recent = [line.rstrip()[:4000] for line in recent_output.splitlines()][-800:]

disk_free_bytes = 0
disk_total_bytes = 0
for target in (os.environ.get("L4D2_ADDONS_DIR", ""), "/home/ubuntu/steamcmd/l4d2", "/"):
    if not target or not os.path.isdir(target):
        continue
    try:
        usage = shutil.disk_usage(target)
    except OSError:
        continue
    disk_free_bytes = usage.free
    disk_total_bytes = usage.total
    break

write_result({
    "available": True,
    "request_id": request_id,
    "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "service_name": service,
    "unit_path": unit_path,
    "unit_modified_at": unit_modified_at,
    "load_state": properties.get("LoadState", ""),
    "active_state": properties.get("ActiveState", ""),
    "sub_state": properties.get("SubState", ""),
    "unit_file_state": properties.get("UnitFileState", ""),
    "main_pid": properties.get("MainPID", ""),
    "exec_main_status": properties.get("ExecMainStatus", ""),
    "result": properties.get("Result", ""),
    "n_restarts": properties.get("NRestarts", ""),
    "active_enter_timestamp": properties.get("ActiveEnterTimestamp", ""),
    "port": port,
    "port_listening": port_listening,
    "listeners": listeners,
    "port_owners": port_owners,
    "processes": processes,
    "status_text": status_text,
    "journal": journal,
    "journal_recent": journal_recent,
    "disk_free_bytes": disk_free_bytes,
    "disk_total_bytes": disk_total_bytes,
})
PY
