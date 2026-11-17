"""bare provider：只接管已存在的裸进程，不负责启动。

裸进程没有守护，也没有启动规格。接管后能停、能看日志、能迁移出去，
但不能 start（否则会变成没人管的野进程）。
"""

from __future__ import annotations

import os
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    find_game_processes,
    kill_process,
    read_text,
    tail_lines,
)
from providers.base import Provider


class BareProvider(Provider):
    kind = "bare"

    def _pid(self, target: dict[str, Any]) -> int:
        value = target.get("pid")
        if isinstance(value, int) and value > 0:
            return value
        raw = str(target.get("pid_file") or "").strip()
        if raw.isdigit():
            return int(raw)
        return 0

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        pid = self._pid(target)
        running = pid > 0 and os.path.isdir(f"/proc/{pid}")
        return build_inspect(
            running=running,
            pid=pid,
            port=int(spec.get("port") or 0),
            log_tail=self._tail(spec, 20) if running else "",
            reason="" if running else "裸进程不存在",
            state="running" if running else "",
        )

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise AgentError("裸进程不支持启动，请迁移到 systemd / nohup / screen")

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        pid = self._pid(target)
        if pid <= 0:
            raise AgentError("未记录裸进程 PID，无法停止")
        kill_process(pid, (spec.get("stop_signal") or "TERM").upper())
        return {"pid": pid, "message": f"已停止裸进程 {pid}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise AgentError("裸进程不支持重启，请先迁移到受管运行时")

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        return {"lines": self._tail(spec, tail).splitlines(), "source": "log file"}

    def _tail(self, spec: dict[str, Any], count: int) -> str:
        log_path = (spec.get("log_path") or "").strip()
        if not log_path or not os.path.isfile(log_path):
            return ""
        return "\n".join(tail_lines(read_text(log_path), count))

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        pid = self._pid(target)
        if pid <= 0:
            raise AgentError("未记录裸进程 PID")
        from providers.screen import _spec_from_pid

        return _spec_from_pid(pid)

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise AgentError("裸进程不支持写入启动规格")

    def detect(self) -> list[dict[str, Any]]:
        """把所有没有归属的 srcds 进程作为 bare 候选上报。"""
        results: list[dict[str, Any]] = []
        for process in find_game_processes():
            cgroup = process.get("cgroup", "")
            if "system.slice" in cgroup or "docker" in cgroup:
                continue
            if ".scope" in cgroup and "session" not in cgroup:
                # systemd-run / user session scope 仍可能受管，降低置信度而不是丢弃。
                confidence = 0.4
            else:
                confidence = 0.6
            from providers.screen import _spec_from_pid

            try:
                spec = _spec_from_pid(int(process["pid"]))
            except AgentError:
                continue
            results.append(
                {
                    "kind": "bare",
                    "name": f"裸进程 · {os.path.basename(spec.get('working_dir', '')) or process['pid']}",
                    "running": True,
                    "pid": int(process["pid"]),
                    "user": process.get("user", ""),
                    "spec": spec,
                    "target": {"pid": int(process["pid"])},
                    "game_path": spec.get("working_dir", ""),
                    "port": spec.get("port", 0),
                    "confidence": confidence,
                    "evidence": [
                        f"进程 {process['pid']} 命令行包含 srcds",
                        f"cgroup: {cgroup.strip().splitlines()[0] if cgroup else '未知'}",
                    ],
                }
            )
        return results
