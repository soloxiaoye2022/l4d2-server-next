"""tmux provider：通过 tmux 会话托管服务端。"""

from __future__ import annotations

import os
import time
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    command_exists,
    find_game_processes,
    quote_shell_arg,
    read_text,
    run_command,
    safe_handle,
    tail_lines,
)
from providers.base import Provider


class TmuxProvider(Provider):
    kind = "tmux"

    def _session(self, target: dict[str, Any]) -> str:
        return safe_handle(target.get("session_name", ""), "会话名")

    def _has_session(self, session: str) -> bool:
        if not command_exists("tmux"):
            return False
        completed = run_command(["tmux", "has-session", "-t", session], timeout=10)
        return completed.returncode == 0

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        running = self._has_session(session)
        pid = self._session_pid(session) if running else 0
        return build_inspect(
            running=running,
            pid=pid,
            port=int(spec.get("port") or 0),
            log_tail=self._tail(spec, 20) if running else "",
            reason="" if running else "会话不存在",
            state="running" if running else "",
        )

    def _session_pid(self, session: str) -> int:
        for process in find_game_processes():
            if session in process.get("cgroup", ""):
                return int(process["pid"])
        return 0

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        if self._has_session(session):
            raise AgentError(f"tmux 会话已存在: {session}")
        working_dir = (spec.get("working_dir") or "").strip()
        executable = (spec.get("executable") or "").strip()
        if not working_dir or not executable:
            raise AgentError("启动规格缺少工作目录或可执行文件")
        tokens = [executable, *[str(item) for item in (spec.get("args") or [])]]
        command = " ".join(quote_shell_arg(item) for item in tokens)
        run_command(
            [
                "tmux",
                "new-session",
                "-d",
                "-s",
                session,
                "-c",
                working_dir,
                command,
            ],
            timeout=30,
            check=True,
        )
        time.sleep(1.0)
        return {"message": f"已在 tmux 会话 {session} 中启动"}

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        if not self._has_session(session):
            return {"message": f"tmux 会话不存在，视为已停止: {session}"}
        run_command(["tmux", "kill-session", "-t", session], timeout=20)
        return {"message": f"已关闭 tmux 会话 {session}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        self.stop(target, spec)
        return self.start(target, spec)

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        return {"lines": self._tail(spec, tail).splitlines(), "source": "log file"}

    def _tail(self, spec: dict[str, Any], count: int) -> str:
        log_path = (spec.get("log_path") or "").strip()
        if not log_path or os.path.isdir(log_path):
            return ""
        return "\n".join(tail_lines(read_text(log_path), count))

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        pid = self._session_pid(session)
        if pid <= 0:
            raise AgentError(f"无法定位 tmux 会话 {session} 内的游戏进程")
        from providers.screen import _spec_from_pid

        return _spec_from_pid(pid)

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return {"message": "tmux 启动规格保存在面板注册表"}

    def detect(self) -> list[dict[str, Any]]:
        if not command_exists("tmux"):
            return []
        completed = run_command(
            ["tmux", "list-sessions", "-F", "#{session_name}"],
            timeout=15,
        )
        results: list[dict[str, Any]] = []
        for line in (completed.stdout or "").splitlines():
            session = line.strip()
            if not session:
                continue
            pid = self._session_pid(session)
            if pid <= 0:
                continue
            from providers.screen import _spec_from_pid

            try:
                spec = _spec_from_pid(pid)
            except AgentError:
                continue
            results.append(
                {
                    "kind": "tmux",
                    "name": f"tmux {session}",
                    "running": True,
                    "pid": pid,
                    "spec": spec,
                    "target": {"session_name": session},
                    "game_path": spec.get("working_dir", ""),
                    "port": spec.get("port", 0),
                    "confidence": 0.85,
                    "evidence": [
                        f"tmux list-sessions 显示会话 {session}",
                        f"进程 {pid} 命令行包含 srcds",
                    ],
                }
            )
        return results
