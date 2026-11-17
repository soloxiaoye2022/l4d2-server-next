"""nohup provider：nohup + 后台 + 重定向日志 + pid 文件。

这是"无守护进程但有启动规格"的中间形态：可以写入配置、可以启停，
但宿主机重启后不会自动恢复（Capabilities.Persistent=false）。
"""

from __future__ import annotations

import json
import os
import time
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    find_game_processes,
    kill_process,
    quote_shell_arg,
    read_pid_file,
    read_text,
    run_command,
    tail_lines,
    write_pid_file,
)
from providers.base import Provider


class NohupProvider(Provider):
    kind = "nohup"

    def _pid_file(self, target: dict[str, Any], spec: dict[str, Any]) -> str:
        explicit = (target.get("pid_file") or "").strip()
        if explicit:
            return explicit
        working_dir = (spec.get("working_dir") or "").strip()
        if not working_dir:
            raise AgentError("nohup 运行时需要工作目录或显式 pid 文件路径")
        return os.path.join(working_dir, "logs", "srcds.pid")

    def _log_path(self, spec: dict[str, Any]) -> str:
        explicit = (spec.get("log_path") or "").strip()
        if explicit:
            return explicit
        working_dir = (spec.get("working_dir") or "").strip()
        if not working_dir:
            return ""
        return os.path.join(working_dir, "logs", "srcds.log")

    def _running_pid(self, target: dict[str, Any], spec: dict[str, Any]) -> int:
        pid = read_pid_file(self._pid_file(target, spec))
        if pid > 0:
            return pid
        # pid 文件丢失时退化为按工作目录匹配 srcds 进程。
        working_dir = (spec.get("working_dir") or "").strip()
        if not working_dir:
            return 0
        for process in find_game_processes():
            if os.path.realpath(process.get("cwd", "")) == os.path.realpath(working_dir):
                return int(process["pid"])
        return 0

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        pid = self._running_pid(target, spec)
        return build_inspect(
            running=pid > 0,
            pid=pid,
            port=int(spec.get("port") or 0),
            log_tail=self._tail(spec, 20) if pid > 0 else "",
            reason="" if pid > 0 else "未找到游戏进程",
            state="running" if pid > 0 else "",
        )

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        if self._running_pid(target, spec) > 0:
            raise AgentError("nohup 运行时已在运行")
        working_dir = (spec.get("working_dir") or "").strip()
        executable = (spec.get("executable") or "").strip()
        if not working_dir or not executable:
            raise AgentError("启动规格缺少工作目录或可执行文件")
        if not os.path.isdir(working_dir):
            raise AgentError(f"工作目录不存在: {working_dir}")

        log_path = self._log_path(spec)
        pid_file = self._pid_file(target, spec)
        os.makedirs(os.path.dirname(pid_file), exist_ok=True)
        if log_path:
            os.makedirs(os.path.dirname(log_path), exist_ok=True)

        tokens = [executable, *[str(item) for item in (spec.get("args") or [])]]
        shell_command = "exec " + " ".join(quote_shell_arg(item) for item in tokens)
        if log_path:
            shell_command += f" >> {quote_shell_arg(log_path)} 2>&1"

        env = dict(os.environ)
        for key, value in (spec.get("env") or {}).items():
            env[str(key)] = str(value)

        completed = run_command(
            ["nohup", "sh", "-c", shell_command],
            timeout=20,
            check=False,
            cwd=working_dir,
        )
        # nohup 会立刻返回，真实 pid 必须从 pid 文件或进程扫描获得。
        deadline = time.time() + 8
        pid = 0
        while time.time() < deadline:
            pid = self._running_pid(target, spec)
            if pid > 0:
                break
            time.sleep(0.3)
        if pid <= 0:
            message = (completed.stderr or completed.stdout or "").strip()
            raise AgentError(f"启动后未找到游戏进程: {message[:300]}")
        write_pid_file(pid_file, pid)
        return {"pid": pid, "message": f"已以 nohup 启动，pid={pid}"}

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        pid = self._running_pid(target, spec)
        if pid <= 0:
            return {"message": "进程不存在，视为已停止"}
        signal_name = (spec.get("stop_signal") or "TERM").upper()
        kill_process(pid, signal_name)
        pid_file = self._pid_file(target, spec)
        if os.path.exists(pid_file):
            try:
                os.remove(pid_file)
            except OSError:
                pass
        return {"pid": pid, "message": f"已停止进程 {pid}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        self.stop(target, spec)
        return self.start(target, spec)

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        return {"lines": self._tail(spec, tail).splitlines(), "source": "log file"}

    def _tail(self, spec: dict[str, Any], count: int) -> str:
        log_path = self._log_path(spec)
        if not log_path or not os.path.isfile(log_path):
            return ""
        return "\n".join(tail_lines(read_text(log_path), count))

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        spec_file = (target.get("unit_path") or "").strip()
        if spec_file and os.path.isfile(spec_file):
            return _parse_spec_file(read_text(spec_file))
        pid = read_pid_file(self._pid_file(target, {}))
        if pid <= 0:
            raise AgentError("nohup 运行时没有可读取的启动规格")
        from providers.screen import _spec_from_pid

        return _spec_from_pid(pid)

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        """把启动规格写成可读的 JSON 快照，便于接管与迁移。"""
        spec_file = (target.get("unit_path") or "").strip()
        if not spec_file:
            working_dir = (spec.get("working_dir") or "").strip()
            if not working_dir:
                raise AgentError("nohup 运行时需要工作目录")
            spec_file = os.path.join(working_dir, "logs", "nohup-spec.json")
        directory = os.path.dirname(spec_file)
        if directory:
            os.makedirs(directory, exist_ok=True)
        from agentlib.common import write_json_atomic

        write_json_atomic(spec_file, spec, mode=0o600)
        return {"unit_path": spec_file, "message": "nohup 启动规格已保存"}

    def detect(self) -> list[dict[str, Any]]:
        """扫描有 pid 文件或命令行含 nohup 标记的 srcds 进程。"""
        results: list[dict[str, Any]] = []
        for process in find_game_processes():
            argv = process.get("argv") or []
            if not argv:
                continue
            working_dir = process.get("cwd", "")
            if not working_dir:
                continue
            pid = int(process["pid"])
            parent = _parent_command(pid)
            if "nohup" not in parent and not _has_pid_file(working_dir):
                continue
            from providers.screen import _spec_from_pid

            try:
                spec = _spec_from_pid(pid)
            except AgentError:
                continue
            results.append(
                {
                    "kind": "nohup",
                    "name": f"nohup · {os.path.basename(working_dir)}",
                    "running": True,
                    "pid": pid,
                    "user": process.get("user", ""),
                    "spec": spec,
                    "target": {"pid_file": os.path.join(working_dir, "logs", "srcds.pid")},
                    "game_path": working_dir,
                    "port": spec.get("port", 0),
                    "confidence": 0.7,
                    "evidence": [
                        f"进程 {pid} 的父进程为 nohup 或存在 pid 文件",
                        f"工作目录 {working_dir}",
                    ],
                }
            )
        return results


def _parent_command(pid: int) -> str:
    """读取父进程命令行，用于判断是否由 nohup 拉起。"""
    try:
        with open(f"/proc/{pid}/stat", "r", encoding="utf-8") as handle:
            fields = handle.read().split()
    except OSError:
        return ""
    if len(fields) < 4:
        return ""
    ppid = fields[3]
    try:
        with open(f"/proc/{ppid}/cmdline", "rb") as handle:
            return handle.read().decode("utf-8", errors="replace").replace("\x00", " ")
    except OSError:
        return ""


def _has_pid_file(working_dir: str) -> bool:
    return os.path.isfile(os.path.join(working_dir, "logs", "srcds.pid"))


def _parse_spec_file(content: str) -> dict[str, Any]:
    try:
        return json.loads(content)
    except json.JSONDecodeError as exc:
        raise AgentError("nohup 启动规格文件格式损坏") from exc


__all__ = ["NohupProvider"]
