"""systemd provider：system 与 --user 两种作用域共用一份实现。"""

from __future__ import annotations

import os
import re
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    command_exists,
    find_game_processes,
    parse_port,
    read_text,
    run_command,
    safe_handle,
    split_command,
    tail_lines,
)
from providers.base import Provider

UNIT_DIR_SYSTEM = "/etc/systemd/system"
UNIT_DIR_USER = ".config/systemd/user"

EXEC_START = re.compile(r"^ExecStart\s*=\s*(?:[-@+!:]*)(.+)$", re.MULTILINE)
WORKING_DIRECTORY = re.compile(r"^WorkingDirectory\s*=\s*(?:[-+!]*)(.+)$", re.MULTILINE)
USER_LINE = re.compile(r"^User\s*=\s*(.+)$", re.MULTILINE)
RESTART_LINE = re.compile(r"^Restart\s*=\s*(.+)$", re.MULTILINE)


def _unit_scope_args(scope: str) -> list[str]:
    """返回 systemctl 的作用域参数。"""
    if scope == "user":
        return ["--user"]
    return []


def _unit_path(unit_name: str, scope: str) -> str:
    """计算 unit 文件路径。"""
    if scope == "user":
        home = os.path.expanduser("~")
        return os.path.join(home, UNIT_DIR_USER, unit_name)
    return os.path.join(UNIT_DIR_SYSTEM, unit_name)


class SystemdProvider(Provider):
    """通过 systemctl 管理 unit。"""

    def __init__(self, system_scope: bool = True) -> None:
        self.system_scope = system_scope
        self.kind = "systemd" if system_scope else "systemd-user"

    def _scope(self, target: dict[str, Any]) -> str:
        """确定作用域，默认取构造参数。"""
        scope = (target.get("scope") or "").strip()
        if scope in ("system", "user"):
            return scope
        return "system" if self.system_scope else "user"

    def _unit(self, target: dict[str, Any]) -> str:
        """取出并校验服务名。"""
        return safe_handle(target.get("unit_name", ""), "服务名")

    def _systemctl(self, scope: str, *args: str, timeout: float = 30.0) -> str:
        """执行 systemctl，返回 stdout。"""
        if not command_exists("systemctl"):
            raise AgentError("宿主机没有 systemctl")
        argv = ["systemctl", *_unit_scope_args(scope), *args]
        completed = run_command(argv, timeout=timeout)
        return (completed.stdout or "").strip()

    def _active_state(self, unit: str, scope: str) -> tuple[str, int]:
        """返回 (ActiveState, MainPID)。"""
        if not command_exists("systemctl"):
            raise AgentError("宿主机没有 systemctl")
        completed = run_command(
            [
                "systemctl",
                *_unit_scope_args(scope),
                "show",
                unit,
                "--property=ActiveState",
                "--property=MainPID",
                "--property=SubState",
            ],
            timeout=20,
        )
        state = ""
        pid = 0
        for line in (completed.stdout or "").splitlines():
            if line.startswith("ActiveState="):
                state = line.split("=", 1)[1].strip()
            elif line.startswith("MainPID="):
                value = line.split("=", 1)[1].strip()
                pid = int(value) if value.isdigit() else 0
        return state, pid

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        state, pid = self._active_state(unit, scope)
        port = int(spec.get("port") or 0)
        if port <= 0 and pid > 0:
            port = self._live_port(unit, pid)
        running = state == "active" and pid > 0
        log_tail = ""
        if pid > 0:
            log_tail = self._journal_tail(unit, scope, 40)
        return build_inspect(
            running=running,
            pid=pid,
            port=port,
            log_tail=log_tail,
            reason="" if running else f"ActiveState={state or 'unknown'}",
            state="running" if running else ("starting" if state == "activating" else ""),
        )

    def _live_port(self, unit: str, pid: int) -> int:
        """从运行中的进程命令行兜底解析端口。

        ExecStart 指向包装脚本（例如 start_server.sh）时，unit 文件里根本
        没有 +hostport，parse_unit 只能得到 0。这样健康检查会一直认为
        "端口未监听"，用户看到的是"启动中"，与事实相反。

        包装脚本在真实部署里很常见，所以这里读实际 srcds 进程的 argv：
        先按 MainPID 精确匹配，再按 cgroup 里的 unit 名匹配子孙进程。
        """
        for item in find_game_processes():
            if pid and item["pid"] == pid:
                return parse_port(item["argv"])
            if unit and unit in (item.get("cgroup") or ""):
                return parse_port(item["argv"])
        return 0

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        self._systemctl(scope, "start", unit, timeout=60)
        state, pid = self._active_state(unit, scope)
        return {"state": state, "pid": pid, "message": f"已提交启动请求: {unit}"}

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        self._systemctl(scope, "stop", unit, timeout=90)
        state, pid = self._active_state(unit, scope)
        return {"state": state, "pid": pid, "message": f"已停止: {unit}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        self._systemctl(scope, "restart", unit, timeout=90)
        state, pid = self._active_state(unit, scope)
        return {"state": state, "pid": pid, "message": f"已重启: {unit}"}

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        return {"lines": self._journal_lines(unit, scope, tail), "source": "journalctl"}

    def _journal_lines(self, unit: str, scope: str, tail: int) -> list[str]:
        """读取 journal 尾部。"""
        if not command_exists("journalctl"):
            return []
        argv = [
            "journalctl",
            *_unit_scope_args(scope),
            "-u",
            unit,
            "-n",
            str(tail),
            "--no-pager",
            "-o",
            "cat",
        ]
        completed = run_command(argv, timeout=20)
        return (completed.stdout or "").splitlines()

    def _journal_tail(self, unit: str, scope: str, tail: int) -> str:
        """读取 journal 尾部并拼成字符串。"""
        return "\n".join(self._journal_lines(unit, scope, tail))

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        path = (target.get("unit_path") or "").strip() or _unit_path(unit, scope)
        content = read_text(path, limit=256 * 1024)
        if not content:
            raise AgentError(f"无法读取 unit 文件: {path}")
        return parse_unit(content, path)

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        unit = self._unit(target)
        scope = self._scope(target)
        path = (target.get("unit_path") or "").strip() or _unit_path(unit, scope)
        content = render_unit(unit, spec)
        _write_unit(path, content)
        self._systemctl(scope, "daemon-reload", timeout=60)
        self._systemctl(scope, "enable", unit, timeout=60)
        return {"unit_path": path, "message": f"已写入 {unit}"}

    def detect(self) -> list[dict[str, Any]]:
        # 只让 systemd（system 作用域）负责扫描。
        #
        # 为什么：systemd-user provider 用的是同一批 list-units 数据源，
        # 只是 systemctl 多了 --user。如果两者都扫，同一个 unit 会出现两条
        # 内容几乎一样的候选，用户在接管列表里会看到"重复项"而不知道该点哪个。
        # user 作用域的 unit 目录不同，由 read_spec 按 scope 解析，不会漏。
        if not self.system_scope:
            return []
        candidates: list[dict[str, Any]] = []
        for scope in ("system", "user"):
            candidates.extend(self._detect_scope(scope))
        return candidates

    def _detect_scope(self, scope: str) -> list[dict[str, Any]]:
        """扫描指定作用域下所有与 L4D2 相关的 unit。"""
        if not command_exists("systemctl"):
            return []
        completed = run_command(
            [
                "systemctl",
                *_unit_scope_args(scope),
                "list-units",
                "--type=service",
                "--all",
                "--no-legend",
                "--no-pager",
                "--plain",
            ],
            timeout=30,
        )
        results: list[dict[str, Any]] = []
        for line in (completed.stdout or "").splitlines():
            fields = line.split()
            if not fields or not fields[0].endswith(".service"):
                continue
            unit = fields[0]
            active = "active" in line
            description = " ".join(fields[4:]) if len(fields) > 4 else ""
            if not _looks_like_game_unit(unit, description):
                continue
            spec = self._try_read_spec(unit, scope)
            if spec is None:
                continue
            state, pid = self._active_state(unit, scope)
            # ExecStart 是包装脚本时 spec.port 为 0，接管后健康检查会失准，
            # 所以扫描阶段就补上运行进程里的真实端口。
            if not spec.get("port") and pid > 0:
                live_port = self._live_port(unit, pid)
                if live_port:
                    spec["port"] = live_port
            results.append(
                {
                    "kind": self.kind,
                    "name": unit,
                    "running": active and state == "active",
                    "pid": pid,
                    "user": spec.get("user", ""),
                    "spec": spec,
                    "target": {
                        "unit_name": unit,
                        "unit_path": _unit_path(unit, scope),
                        "scope": scope,
                    },
                    "game_path": spec.get("working_dir", ""),
                    "port": spec.get("port", 0),
                    "confidence": 0.95,
                    "evidence": [
                        f"systemctl list-units 显示 {unit}",
                        f"unit 文件含 srcds 启动命令: {spec.get('executable', '')}",
                    ],
                }
            )
        return results

    def _try_read_spec(self, unit: str, scope: str) -> dict[str, Any] | None:
        """尝试解析 unit，失败返回 None 而不是中断整轮扫描。"""
        try:
            return self.read_spec({"unit_name": unit, "scope": scope})
        except AgentError:
            return None


def _looks_like_game_unit(unit: str, description: str) -> bool:
    """判断 unit 是否真的是游戏服，而不是面板自己的辅助脚本。

    必须两层过滤：
      1. 名字/描述含 l4d2 / srcds / left4dead；
      2. 排除面板自身的辅助 unit（diagnostics / maintenance / repair /
         restart / startup / runtime）。

    只做第一层会把 l4d2-diagnostics.service 这类一次性脚本当成游戏服，
    用户接管列表里会冒出 6 个"可接管的服务"，全是面板自己的工具。
    """
    haystack = f"{unit} {description}".lower()
    if not any(keyword in haystack for keyword in ("l4d2", "srcds", "left4dead")):
        return False
    stem = unit.removesuffix(".service").lower()
    for helper in PANEL_HELPER_UNITS:
        if stem == helper or stem.endswith("-" + helper) or stem.startswith(helper + "-"):
            return False
    return True


# 面板自身的一次性辅助 unit，永远不是游戏服。
PANEL_HELPER_UNITS = (
    "diagnostics",
    "maintenance",
    "repair",
    "restart",
    "startup",
    "runtime",
)


def parse_unit(content: str, path: str) -> dict[str, Any]:
    """解析 unit 文件为 LaunchSpec。"""
    exec_match = EXEC_START.search(content)
    if not exec_match:
        raise AgentError(f"unit 文件缺少 ExecStart: {path}")
    command = exec_match.group(1).strip()
    tokens = split_command(command)
    if not tokens:
        raise AgentError(f"ExecStart 为空: {path}")
    work_match = WORKING_DIRECTORY.search(content)
    working_dir = work_match.group(1).strip().strip('"') if work_match else ""
    user_match = USER_LINE.search(content)
    restart_match = RESTART_LINE.search(content)
    restart_value = restart_match.group(1).strip().lower() if restart_match else ""
    port = parse_port(tokens)
    return {
        "working_dir": working_dir,
        "executable": tokens[0],
        "args": tokens[1:],
        "user": user_match.group(1).strip() if user_match else "",
        "port": port,
        "auto_restart": restart_value not in ("", "no"),
        "log_path": "",
        "unit_path": path,
    }


def render_unit(unit: str, spec: dict[str, Any]) -> str:
    """把 LaunchSpec 渲染成 systemd unit。"""
    working_dir = (spec.get("working_dir") or "").strip()
    executable = (spec.get("executable") or "").strip()
    args = spec.get("args") or []
    if not working_dir or not executable:
        raise AgentError("启动规格缺少工作目录或可执行文件")
    command = " ".join([quote_unit_arg(executable), *[quote_unit_arg(item) for item in args]])
    lines = [
        "[Unit]",
        f"Description=L4D2 Dedicated Server ({unit})",
        "After=network-online.target",
        "Wants=network-online.target",
        "",
        "[Service]",
        "Type=simple",
        f"WorkingDirectory={working_dir}",
        f"ExecStart={command}",
        "Restart=on-failure",
        "RestartSec=5",
        "KillSignal=SIGTERM",
        "TimeoutStopSec=30",
    ]
    user = (spec.get("user") or "").strip()
    if user:
        lines.append(f"User={user}")
    for key, value in (spec.get("env") or {}).items():
        lines.append(f'Environment="{key}={value}"')
    lines.extend(["", "[Install]", "WantedBy=multi-user.target", ""])
    return "\n".join(lines)


def quote_unit_arg(value: str) -> str:
    """systemd 命令行里含空白的参数必须加引号。"""
    text = value or ""
    if not text:
        return '""'
    if any(char in text for char in ' \t"'):
        escaped = text.replace('"', '\\"')
        return f'"{escaped}"'
    return text


def _write_unit(path: str, content: str) -> None:
    """原子写入 unit 文件。"""
    from agentlib.common import write_json_atomic  # noqa: F401  (复用其原子写思想)

    directory = os.path.dirname(path)
    os.makedirs(directory, exist_ok=True)
    temporary = f"{path}.tmp-{os.getpid()}"
    with open(temporary, "w", encoding="utf-8") as handle:
        handle.write(content)
        handle.flush()
        os.fsync(handle.fileno())
    os.chmod(temporary, 0o644)
    os.replace(temporary, path)


def merge_process_evidence(candidates: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """用 srcds 进程补充 cgroup 证据，提升归属判断可信度。"""
    processes = find_game_processes()
    for candidate in candidates:
        pid = candidate.get("pid") or 0
        if pid <= 0:
            continue
        for process in processes:
            if process["pid"] != pid:
                continue
            cgroup = process.get("cgroup", "")
            if candidate["kind"].startswith("systemd"):
                unit = candidate["target"].get("unit_name", "")
                if unit and unit in cgroup:
                    candidate["confidence"] = min(1.0, candidate["confidence"] + 0.04)
                    candidate["evidence"].append(f"cgroup 证明进程归属 {unit}")
            break
    return candidates


def tail_from_journal(unit: str, scope: str, count: int) -> str:
    """供其他模块复用的 journal 读取。"""
    provider = SystemdProvider(system_scope=scope != "user")
    return "\n".join(tail_lines("\n".join(provider._journal_lines(unit, scope, count)), count))
