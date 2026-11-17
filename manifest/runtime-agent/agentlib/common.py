"""运行时 Agent 的公共工具：路径校验、进程查询、端口归属、命令执行。

设计约束（见 docs/operations/multi-runtime-architecture-20261117.md）：
1. 绝不拼接 shell 字符串，所有外部命令都用 argv 数组执行。
2. 用户输入只能落在白名单根目录内。
3. 任何失败都要返回结构化原因，而不是抛异常给面板。
"""

from __future__ import annotations

import json
import os
import re
import shutil
import socket
import subprocess
import time
from collections.abc import Iterable
from typing import Any

# pwd 仅存在于 POSIX。Agent 生产环境是 Linux，但单元测试要在 Windows 上跑，
# 因此这里做一次性兼容导入，调用处统一判空。
try:  # pragma: no cover - 平台分支
    import pwd
except ImportError:  # pragma: no cover - Windows
    pwd = None  # type: ignore[assignment]

AGENT_VERSION = "1.0.0"

# 默认允许操作的根目录，可用 L4D2_ALLOWED_ROOTS 覆盖（冒号分隔）。
DEFAULT_ALLOWED_ROOTS = ("/home", "/data", "/opt", "/srv", "/var/lib", "/root")

# 游戏进程名白名单，用于判定端口归属与进程扫描。
GAME_PROCESS_PATTERN = re.compile(r"^(srcds_linux|srcds_run|srcds\.exe|srcds_osx)$")

# 服务名 / 会话名 / 容器名统一字符集，防止注入到 argv 中的危险字符。
SAFE_HANDLE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.@:-]{0,127}$")

# 运行时类型枚举，必须与后端 backend/logic/runtime/kind.go 保持一致。
KINDS = (
    "systemd",
    "systemd-user",
    "docker",
    "screen",
    "tmux",
    "nohup",
    "bare",
    "mcsm",
    "lgsm",
)


class AgentError(Exception):
    """可预期的业务错误，会被转换成 status=error 的结果文件。"""


class UnsupportedError(AgentError):
    """当前 provider 不支持该动作。"""


def now_iso() -> str:
    """返回带时区的 RFC3339 时间戳。"""
    return time.strftime("%Y-%m-%dT%H:%M:%S%z", time.localtime())


def allowed_roots() -> list[str]:
    """读取允许写入的根目录白名单。

    分隔符用 os.pathsep：Linux 是 ":"，Windows 是 ";"。
    用 ":" 硬切会把 "C:\\path" 切断，导致 Windows 上永远匹配不上。
    """
    raw = os.environ.get("L4D2_ALLOWED_ROOTS", "")
    if raw.strip():
        return [item.strip() for item in raw.split(os.pathsep) if item.strip()]
    return list(DEFAULT_ALLOWED_ROOTS)


def ensure_within_allowed(path: str) -> str:
    """校验路径在允许的根目录内，返回规范化后的绝对路径。"""
    if not path:
        raise AgentError("路径不能为空")
    normalized = os.path.realpath(path)
    if not os.path.isabs(normalized):
        raise AgentError(f"路径必须是绝对路径: {path}")
    for root in allowed_roots():
        root_real = os.path.realpath(root)
        if normalized == root_real:
            raise AgentError(f"目标目录不能是白名单根目录本身: {root}")
        if normalized.startswith(root_real + os.sep):
            return normalized
    raise AgentError(f"目标目录不在允许范围内: {normalized}")


def safe_handle(value: str, label: str) -> str:
    """校验服务名、容器名、会话名等句柄。"""
    text = (value or "").strip()
    if not SAFE_HANDLE.match(text):
        raise AgentError(f"{label} 非法: {value}")
    return text


def run_command(
    argv: list[str],
    timeout: float = 20.0,
    check: bool = False,
    stdin_text: str | None = None,
) -> subprocess.CompletedProcess[str]:
    """执行命令并捕获输出，永不使用 shell=True。"""
    try:
        completed = subprocess.run(
            argv,
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
            input=stdin_text,
        )
    except FileNotFoundError as exc:
        raise AgentError(f"命令不存在: {argv[0]}") from exc
    except subprocess.TimeoutExpired as exc:
        raise AgentError(f"命令超时: {' '.join(argv[:3])}") from exc
    if check and completed.returncode != 0:
        message = (completed.stderr or completed.stdout or "").strip()
        raise AgentError(f"命令执行失败({completed.returncode}): {message[:400]}")
    return completed


def command_exists(name: str) -> bool:
    """判断命令是否存在于 PATH。"""
    return shutil.which(name) is not None


def read_text(path: str, limit: int = 512 * 1024) -> str:
    """读取文本文件，超过 limit 只读尾部。"""
    try:
        size = os.path.getsize(path)
        with open(path, "rb") as handle:
            if size > limit:
                handle.seek(size - limit)
            data = handle.read()
        return data.decode("utf-8", errors="replace")
    except OSError:
        return ""


def tail_lines(text: str, count: int) -> list[str]:
    """取文本最后 count 行。"""
    lines = text.splitlines()
    if count <= 0:
        return lines
    return lines[-count:]


def read_json(path: str) -> Any:
    """读取 JSON 文件，失败返回 None。"""
    try:
        with open(path, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except (OSError, json.JSONDecodeError):
        return None


def write_json_atomic(path: str, payload: Any, mode: int = 0o600) -> None:
    """原子写入 JSON：临时文件 + fsync + rename。"""
    directory = os.path.dirname(path) or "."
    os.makedirs(directory, exist_ok=True)
    temporary = os.path.join(directory, f".tmp-{os.getpid()}-{int(time.time() * 1000)}.json")
    with open(temporary, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2)
        handle.flush()
        os.fsync(handle.fileno())
    os.chmod(temporary, mode)
    os.replace(temporary, path)


def split_command(command: str) -> list[str]:
    """按 shell 风格分词，但不做变量展开与通配符。"""
    tokens: list[str] = []
    current: list[str] = []
    quote = ""
    escaped = False
    started = False

    for char in command:
        if escaped:
            current.append(char)
            escaped = False
            started = True
        elif char == "\\" and quote != "'":
            escaped = True
            started = True
        elif quote:
            if char == quote:
                quote = ""
            else:
                current.append(char)
            started = True
        elif char in ("'", '"'):
            quote = char
            started = True
        elif char.isspace():
            if started:
                tokens.append("".join(current))
                current = []
                started = False
        else:
            current.append(char)
            started = True
    if escaped:
        raise AgentError("启动命令以未完成的转义符结尾")
    if quote:
        raise AgentError("启动命令包含未闭合的引号")
    if started:
        tokens.append("".join(current))
    return tokens


def list_pids() -> Iterable[int]:
    """遍历 /proc 下的所有 PID。"""
    try:
        entries = os.listdir("/proc")
    except OSError:
        return []
    result: list[int] = []
    for entry in entries:
        if entry.isdigit():
            result.append(int(entry))
    return result


def proc_cmdline(pid: int) -> list[str]:
    """读取进程 argv 数组。"""
    try:
        with open(f"/proc/{pid}/cmdline", "rb") as handle:
            raw = handle.read()
    except OSError:
        return []
    if not raw:
        return []
    return [item.decode("utf-8", errors="replace") for item in raw.split(b"\x00") if item]


def proc_cwd(pid: int) -> str:
    """读取进程工作目录。"""
    try:
        return os.readlink(f"/proc/{pid}/cwd")
    except OSError:
        return ""


def proc_user(pid: int) -> str:
    """读取进程属主用户名。"""
    try:
        status = read_text(f"/proc/{pid}/status", limit=8192)
    except OSError:
        return ""
    for line in status.splitlines():
        if line.startswith("Uid:"):
            parts = line.split()
            if len(parts) >= 2 and parts[1].isdigit():
                if pwd is None:
                    return parts[1]
                try:
                    return pwd.getpwuid(int(parts[1])).pw_name
                except KeyError:
                    return parts[1]
    return ""


def proc_cgroup(pid: int) -> str:
    """读取进程 cgroup，用于证明它归谁管。"""
    return read_text(f"/proc/{pid}/cgroup", limit=16384)


def is_game_process(pid: int) -> bool:
    """判断进程是否 srcds 系列。"""
    argv = proc_cmdline(pid)
    if not argv:
        return False
    name = os.path.basename(argv[0])
    return bool(GAME_PROCESS_PATTERN.match(name))


def find_game_processes() -> list[dict[str, Any]]:
    """扫描全部 srcds 进程，返回可序列化摘要。"""
    result: list[dict[str, Any]] = []
    for pid in list_pids():
        if not is_game_process(pid):
            continue
        argv = proc_cmdline(pid)
        result.append(
            {
                "pid": pid,
                "argv": argv,
                "cwd": proc_cwd(pid),
                "user": proc_user(pid),
                "cgroup": proc_cgroup(pid),
            }
        )
    return result


def _parse_port_from_ss(line: str) -> tuple[int, str]:
    """从 ss 输出中解析本地端口与进程名。"""
    fields = line.split()
    local = ""
    for field in fields:
        if field.count(":") >= 1 and not field.startswith("users:"):
            local = field
            break
    port = 0
    if local:
        tail = local.rsplit(":", 1)[-1]
        if tail.isdigit():
            port = int(tail)
    command = ""
    match = re.search(r'users:\(\("([^"]+)"', line)
    if match:
        command = match.group(1)
    return port, command


def _parse_port_from_lsof(line: str) -> tuple[int, str]:
    """从 lsof 输出中解析端口与进程名。"""
    fields = line.split()
    port = 0
    command = fields[0] if fields else ""
    for field in fields:
        if field.startswith(":") and field[1:].rstrip("(LISTEN)").isdigit():
            port = int(field[1:].rstrip("(LISTEN)"))
            break
    return port, command


def listen_owners(port: int) -> list[dict[str, Any]]:
    """查询某端口上正在监听的进程列表。"""
    if port <= 0:
        return []
    owners: list[dict[str, Any]] = []
    if command_exists("ss"):
        completed = run_command(
            ["ss", "-H", "-tlnp", f"sport = :{port}"],
            timeout=10,
        )
        for line in completed.stdout.splitlines():
            parsed_port, command = _parse_port_from_ss(line)
            if parsed_port != port:
                continue
            pid = 0
            match = re.search(r"pid=(\d+)", line)
            if match:
                pid = int(match.group(1))
            owners.append({"command": command, "pid": pid})
    elif command_exists("lsof"):
        completed = run_command(
            ["lsof", "-nP", f"-iTCP:{port}", "-sTCP:LISTEN"],
            timeout=10,
        )
        for line in completed.stdout.splitlines()[1:]:
            parsed_port, command = _parse_port_from_lsof(line)
            if parsed_port != port:
                continue
            fields = line.split()
            pid = int(fields[1]) if len(fields) > 1 and fields[1].isdigit() else 0
            owners.append({"command": command, "pid": pid})
    return owners


def port_listening(port: int, host: str = "127.0.0.1") -> bool:
    """TCP 探测端口是否可连接。"""
    if port <= 0:
        return False
    try:
        with socket.create_connection((host, port), timeout=2.0):
            return True
    except OSError:
        return False


def read_pid_file(path: str) -> int:
    """读取 pid 文件，进程不存在时返回 0。"""
    text = read_text(path, limit=64).strip()
    if not text.isdigit():
        return 0
    pid = int(text)
    return pid if os.path.isdir(f"/proc/{pid}") else 0


def write_pid_file(path: str, pid: int) -> None:
    """写入 pid 文件。"""
    directory = os.path.dirname(path)
    if directory:
        os.makedirs(directory, exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(str(pid))
        handle.flush()
        os.fsync(handle.fileno())


def kill_process(pid: int, signal_name: str = "TERM", wait: float = 15.0) -> bool:
    """按信号终止进程并等待退出，超时后升级为 KILL。"""
    import signal as signal_module

    number = getattr(signal_module, f"SIG{signal_name.upper()}", signal_module.SIGTERM)
    try:
        os.kill(pid, number)
    except ProcessLookupError:
        return True
    except PermissionError as exc:
        raise AgentError(f"没有权限终止进程 {pid}") from exc

    deadline = time.time() + wait
    while time.time() < deadline:
        if not os.path.isdir(f"/proc/{pid}"):
            return True
        time.sleep(0.2)
    try:
        os.kill(pid, signal_module.SIGKILL)
    except ProcessLookupError:
        return True
    except PermissionError as exc:
        raise AgentError(f"没有权限强制终止进程 {pid}") from exc
    time.sleep(0.5)
    return not os.path.isdir(f"/proc/{pid}")


def build_inspect(
    running: bool,
    pid: int,
    port: int,
    log_tail: str,
    reason: str = "",
    state: str = "",
) -> dict[str, Any]:
    """组装 inspect 结果的标准结构。"""
    owners = listen_owners(port) if running else []
    listening = bool(owners) if port > 0 else False
    return {
        "state": state,
        "pid": pid,
        "process_running": running,
        "port_listening": listening,
        "port_owners": owners,
        "log_tail": log_tail,
        "reason": reason,
        "rcon_reachable": port_listening(port) if port > 0 else False,
    }


PORT_FLAGS = ("+port", "-port", "+hostport", "-hostport")


def parse_port(tokens: list[str]) -> int:
    """从启动参数中解析端口号。

    必须同时支持 +port 与 +hostport：L4D2 服务端常用 +hostport，
    只认 +port 会让扫描出来的候选端口恒为 0，后续迁移与端口校验全部失效。
    """
    for index, token in enumerate(tokens):
        if token in PORT_FLAGS and index + 1 < len(tokens):
            value = tokens[index + 1]
            if value.isdigit():
                return int(value)
        for flag in PORT_FLAGS:
            prefix = flag + "="
            if token.startswith(prefix):
                value = token[len(prefix) :]
                if value.isdigit():
                    return int(value)
    return 0


def quote_shell_arg(value: str) -> str:
    """把单个参数安全地包成 POSIX shell 单引号形式。"""
    text = value or ""
    if not text:
        return "''"
    if re.fullmatch(r"[A-Za-z0-9_@%+=:,./-]+", text):
        return text
    return "'" + text.replace("'", "'\\''") + "'"
