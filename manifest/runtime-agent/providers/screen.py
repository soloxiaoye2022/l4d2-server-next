"""screen provider：通过 GNU screen 会话托管服务端。"""

from __future__ import annotations

import os
import re
import time
from typing import Any

from agentlib.common import (
    AgentError,
    command_exists,
    find_game_processes,
    parse_port,
    quote_shell_arg,
    read_pid_file,
    run_command,
    safe_handle,
    write_pid_file,
)
from providers.base import Provider

SCREEN_LIST = re.compile(r"^\s*(\d+)\.(\S+)\s+")


class ScreenProvider(Provider):
    kind = "screen"

    def _session(self, target: dict[str, Any]) -> str:
        return safe_handle(target.get("session_name", ""), "会话名")

    def _sessions(self) -> dict[str, bool]:
        """返回 {会话名: 是否 Attached}。"""
        if not command_exists("screen"):
            return {}
        completed = run_command(["screen", "-ls"], timeout=10)
        text = f"{completed.stdout}\n{completed.stderr}"
        sessions: dict[str, bool] = {}
        for line in text.splitlines():
            match = SCREEN_LIST.match(line)
            if not match:
                continue
            sessions[match.group(2)] = "Attached" in line
        return sessions

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        sessions = self._sessions()
        running = session in sessions
        pid = self._session_pid(session) if running else 0
        port = int(spec.get("port") or 0)
        from agentlib.common import build_inspect

        return build_inspect(
            running=running,
            pid=pid,
            port=port,
            log_tail=self._tail(spec, 20) if running else "",
            reason="" if running else "会话不存在",
            state="running" if running else "",
        )

    def _session_pid(self, session: str) -> int:
        """从 srcds 进程的 cgroup / 环境推断所属 screen 会话。"""
        for process in find_game_processes():
            if session in process.get("cgroup", ""):
                return int(process["pid"])
        return 0

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        if session in self._sessions():
            raise AgentError(f"screen 会话已存在: {session}")
        working_dir = (spec.get("working_dir") or "").strip()
        executable = (spec.get("executable") or "").strip()
        if not working_dir or not executable:
            raise AgentError("启动规格缺少工作目录或可执行文件")
        tokens = [executable, *[str(item) for item in (spec.get("args") or [])]]
        command = " ".join(quote_shell_arg(item) for item in tokens)
        argv = [
            "screen",
            "-dmS",
            session,
            "sh",
            "-c",
            f"cd {quote_shell_arg(working_dir)} && exec {command}",
        ]
        run_command(argv, timeout=30, check=True)
        time.sleep(1.0)
        return {"message": f"已在 screen 会话 {session} 中启动"}

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        if session not in self._sessions():
            return {"message": f"screen 会话不存在，视为已停止: {session}"}
        run_command(["screen", "-S", session, "-X", "quit"], timeout=20)
        deadline = time.time() + 20
        while time.time() < deadline:
            if session not in self._sessions():
                return {"message": f"已关闭 screen 会话 {session}"}
            time.sleep(0.3)
        # screen quit 无效时终止会话内的游戏进程。
        pid = self._session_pid(session)
        if pid > 0:
            from agentlib.common import kill_process

            kill_process(pid, "TERM")
        return {"message": f"已强制关闭 screen 会话 {session}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        self.stop(target, spec)
        return self.start(target, spec)

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        return {"lines": self._tail(spec, tail).splitlines(), "source": "log file"}

    def _tail(self, spec: dict[str, Any], count: int) -> str:
        from agentlib.common import read_text, tail_lines

        log_path = (spec.get("log_path") or "").strip()
        if not log_path:
            return ""
        if os.path.isdir(log_path):
            return ""
        return "\n".join(tail_lines(read_text(log_path), count))

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        session = self._session(target)
        pid = self._session_pid(session)
        if pid <= 0:
            raise AgentError(f"无法定位 screen 会话 {session} 内的游戏进程")
        return _spec_from_pid(pid)

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        # screen 没有配置文件，启动规格保存在注册表，启动时按 spec 重建会话。
        return {"message": "screen 启动规格保存在面板注册表"}

    def detect(self) -> list[dict[str, Any]]:
        results: list[dict[str, Any]] = []
        for session in self._sessions():
            pid = self._session_pid(session)
            if pid <= 0:
                continue
            try:
                spec = _spec_from_pid(pid)
            except AgentError:
                continue
            results.append(
                {
                    "kind": "screen",
                    "name": f"screen {session}",
                    "running": True,
                    "pid": pid,
                    "spec": spec,
                    "target": {"session_name": session},
                    "game_path": spec.get("working_dir", ""),
                    "port": spec.get("port", 0),
                    "confidence": 0.85,
                    "evidence": [
                        f"screen -ls 显示会话 {session}",
                        f"进程 {pid} 命令行包含 srcds",
                    ],
                }
            )
        return results


def _spec_from_pid(pid: int) -> dict[str, Any]:
    """从进程信息反推启动规格。"""
    from agentlib.common import proc_cmdline, proc_cwd, proc_user

    argv = proc_cmdline(pid)
    if not argv:
        raise AgentError(f"无法读取进程 {pid} 的命令行")
    return {
        "working_dir": proc_cwd(pid),
        "executable": argv[0],
        "args": argv[1:],
        "user": proc_user(pid),
        "port": parse_port(argv),
        "auto_restart": False,
        "log_path": "",
    }


def write_session_pid(path: str, pid: int) -> None:
    """暴露给其它模块写 pid 文件。"""
    write_pid_file(path, pid)


def read_session_pid(path: str) -> int:
    """暴露给其它模块读 pid 文件。"""
    return read_pid_file(path)


def command_tokens(spec: dict[str, Any]) -> list[str]:
    """把 spec 还原为 argv，供 nohup/bare 复用。"""
    tokens = [str(spec.get("executable") or "")]
    tokens.extend(str(item) for item in (spec.get("args") or []))
    return [item for item in tokens if item]


__all__ = ["ScreenProvider", "command_tokens", "read_session_pid", "write_session_pid"]
