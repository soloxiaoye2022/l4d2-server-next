"""LinuxGSM provider：通过 LinuxGSM 脚本管理服务端。

LinuxGSM 每个实例都有固定目录结构与脚本，例如：
  /home/lgsm/l4d2server/l4d2server  （脚本）
  /home/lgsm/l4d2server/serverfiles  （游戏目录）
"""

from __future__ import annotations

import os
import re
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    read_text,
    run_command,
    tail_lines,
)
from providers.base import Provider

LGSM_SCRIPT = re.compile(r"^[A-Za-z0-9_-]{1,64}$")


class LGSMProvider(Provider):
    kind = "lgsm"

    def _script(self, target: dict[str, Any]) -> str:
        path = (target.get("lgsm_script") or "").strip()
        if not path:
            raise AgentError("LinuxGSM 运行时缺少脚本路径")
        if not os.path.isabs(path):
            raise AgentError("LinuxGSM 脚本路径必须是绝对路径")
        if not os.path.isfile(path):
            raise AgentError(f"LinuxGSM 脚本不存在: {path}")
        if not LGSM_SCRIPT.match(os.path.basename(path)):
            raise AgentError("LinuxGSM 脚本名非法")
        return path

    def _server_dir(self, target: dict[str, Any]) -> str:
        return os.path.dirname(self._script(target))

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        script = self._script(target)
        completed = run_command([script, "details"], timeout=60)
        text = f"{completed.stdout}\n{completed.stderr}"
        running = "STATUS: RUNNING" in text.upper() or "ONLINE" in text.upper()
        pid = _parse_pid(text)
        return build_inspect(
            running=running,
            pid=pid,
            port=int(spec.get("port") or 0),
            log_tail="",
            reason="" if running else "LinuxGSM 报告未运行",
            state="running" if running else "",
        )

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "start")

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "stop")

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "restart")

    def _lifecycle(self, target: dict[str, Any], action: str) -> dict[str, Any]:
        script = self._script(target)
        completed = run_command([script, action], timeout=120)
        if completed.returncode != 0:
            message = (completed.stderr or completed.stdout or "").strip()
            raise AgentError(f"LinuxGSM {action} 失败: {message[:300]}")
        return {"message": f"LinuxGSM 已执行 {action}"}

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        log_dir = os.path.join(self._server_dir(target), "log")
        if not os.path.isdir(log_dir):
            return {"lines": [], "source": "LinuxGSM 日志目录不存在"}
        candidates = sorted(
            (os.path.join(log_dir, name) for name in os.listdir(log_dir)),
            key=lambda item: os.path.getmtime(item) if os.path.isfile(item) else 0,
            reverse=True,
        )
        for path in candidates:
            if os.path.isfile(path):
                return {
                    "lines": tail_lines(read_text(path), tail),
                    "source": path,
                }
        return {"lines": [], "source": "LinuxGSM 日志目录为空"}

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        server_dir = self._server_dir(target)
        config_path = os.path.join(server_dir, "lgsm", "config-l4d2server.cfg")
        content = read_text(config_path, limit=256 * 1024)
        if not content:
            raise AgentError(f"无法读取 LinuxGSM 配置: {config_path}")
        port = _parse_config_port(content)
        serverfiles = os.path.join(server_dir, "serverfiles")
        return {
            "working_dir": serverfiles,
            "executable": os.path.join(serverfiles, "srcds_run"),
            "args": [],
            "port": port,
            "auto_restart": True,
            "log_path": "",
        }

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return {"message": "LinuxGSM 启动参数由 cfg 管理，面板仅记录规格"}

    def detect(self) -> list[dict[str, Any]]:
        results: list[dict[str, Any]] = []
        for root in ("/home", "/opt", "/srv", "/data"):
            if not os.path.isdir(root):
                continue
            for entry in os.scandir(root):
                if not entry.is_dir():
                    continue
                script = _find_lgsm_script(entry.path)
                if not script:
                    continue
                try:
                    spec = self.read_spec({"lgsm_script": script})
                except AgentError:
                    continue
                results.append(
                    {
                        "kind": "lgsm",
                        "name": f"LinuxGSM · {os.path.basename(entry.path)}",
                        "running": _is_lgsm_running(script),
                        "pid": 0,
                        "spec": spec,
                        "target": {"lgsm_script": script},
                        "game_path": spec.get("working_dir", ""),
                        "port": spec.get("port", 0),
                        "confidence": 0.9,
                        "evidence": [
                            f"发现 LinuxGSM 脚本 {script}",
                            f"serverfiles 目录 {spec.get('working_dir', '')}",
                        ],
                    }
                )
        return results


def _find_lgsm_script(directory: str) -> str:
    """在实例目录下寻找可执行的 lgsm 脚本。"""
    try:
        entries = os.scandir(directory)
    except OSError:
        return ""
    for entry in entries:
        if not entry.is_file():
            continue
        if not os.access(entry.path, os.X_OK):
            continue
        name = entry.name
        if name.startswith(".") or "server" not in name:
            continue
        if os.path.isdir(os.path.join(directory, "lgsm")) and os.path.isdir(
            os.path.join(directory, "serverfiles")
        ):
            return entry.path
    return ""


def _is_lgsm_running(script: str) -> bool:
    completed = run_command([script, "details"], timeout=45)
    text = f"{completed.stdout}\n{completed.stderr}".upper()
    return "STATUS: RUNNING" in text or "ONLINE" in text


def _parse_pid(text: str) -> int:
    match = re.search(r"PID:\s*(\d+)", text, re.IGNORECASE)
    if match:
        return int(match.group(1))
    return 0


def _parse_config_port(content: str) -> int:
    match = re.search(r"^port\s*=\s*\"?(\d+)\"?", content, re.MULTILINE)
    if match:
        return int(match.group(1))
    return 0
