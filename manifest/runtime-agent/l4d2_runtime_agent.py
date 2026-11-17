#!/usr/bin/env python3
"""L4D2 面板宿主机运行时 Agent。

工作方式：面板把任务写成 JSON 落到 <control>/runtime/requests/<job>.json，
宿主 systemd path unit 触发本脚本，脚本执行后把结果写到
<control>/runtime/results/<job>.json。

安全边界：
1. 只接受白名单 action，绝不执行任意 shell 字符串。
2. 所有路径校验落在 L4D2_ALLOWED_ROOTS 白名单内。
3. 同一时刻只允许一个 job 在处理（systemd 串行 + 本地锁文件）。
"""

from __future__ import annotations

import json
import os
import sys
import time
import traceback
from typing import Any

# fcntl 仅存在于 POSIX。Agent 生产环境是 Linux，这里兼容导入便于在
# Windows 上跑单元测试；无 fcntl 时退化为"无锁"，只在测试环境发生。
try:  # pragma: no cover - 平台分支
    import fcntl
except ImportError:  # pragma: no cover - Windows
    fcntl = None  # type: ignore[assignment]

from agentlib.common import (
    AGENT_VERSION,
    AgentError,
    UnsupportedError,
    ensure_within_allowed,
    now_iso,
    read_json,
    write_json_atomic,
)
from providers.base import build_registry

CONTROL_DIR = os.environ.get("L4D2_CONTROL_DIR", "/control")
REQUEST_DIR = os.path.join(CONTROL_DIR, "runtime", "requests")
RESULT_DIR = os.path.join(CONTROL_DIR, "runtime", "results")
LOCK_PATH = os.path.join(CONTROL_DIR, "runtime", ".agent.lock")
PROCESSED_DIR = os.path.join(CONTROL_DIR, "runtime", "processed")

SUPPORTED_ACTIONS = (
    "probe",
    "inspect",
    "start",
    "stop",
    "restart",
    "logs",
    "detect",
    "export",
    "import",
    "migrate",
)


def ensure_directories() -> None:
    """确保请求/结果/归档目录存在。

    Agent 以 root 运行，而面板容器以 uid 1000 运行。目录必须对两者都可读写，
    否则面板写不进请求、也读不到结果，bridge 通道会永久失效。
    """
    for directory in (REQUEST_DIR, RESULT_DIR, PROCESSED_DIR):
        os.makedirs(directory, exist_ok=True)
        _chmod_best_effort(directory, 0o777)


def _chmod_best_effort(path: str, mode: int) -> None:
    """尽力调整权限；失败不致命（例如文件系统不支持）。"""
    try:
        os.chmod(path, mode)
    except OSError:
        pass


def acquire_lock() -> int | None:
    """获取全局文件锁，避免并发任务互相踩踏。返回 fd，失败返回 None。"""
    fd = os.open(LOCK_PATH, os.O_CREAT | os.O_RDWR, 0o666)
    _chmod_best_effort(LOCK_PATH, 0o666)
    if fcntl is None:
        return fd
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError:
        os.close(fd)
        return None
    return fd


def write_result(result: dict[str, Any]) -> None:
    """把结果写到 results/<job>.json。

    权限必须是 0666：结果由 root 写、面板（uid 1000）读。
    用 0600 会让面板永远读不到，表现为"等待 Agent 结果超时"。
    结果内容不含密钥（spec 里的密码是引用而非明文），可放宽读权限。
    """
    job_id = result.get("job_id") or f"unknown-{int(time.time() * 1000)}"
    path = os.path.join(RESULT_DIR, f"{job_id}.json")
    write_json_atomic(path, result, mode=0o666)


def archive_request(path: str) -> None:
    """处理完的请求移入 processed，保留 24 小时便于排查。"""
    try:
        target = os.path.join(PROCESSED_DIR, os.path.basename(path))
        os.replace(path, target)
    except OSError:
        try:
            os.remove(path)
        except OSError:
            pass


def prune_processed(max_age_seconds: float = 86400.0) -> None:
    """清理过期的已处理请求，避免目录无限增长。"""
    cutoff = time.time() - max_age_seconds
    try:
        entries = os.scandir(PROCESSED_DIR)
    except OSError:
        return
    for entry in entries:
        try:
            if entry.is_file() and entry.stat().st_mtime < cutoff:
                os.remove(entry.path)
        except OSError:
            continue


def handle_request(request: dict[str, Any]) -> dict[str, Any]:
    """执行一个任务并返回结果字典。"""
    action = str(request.get("action") or "").strip()
    job_id = str(request.get("job_id") or "")
    started = now_iso()

    if action not in SUPPORTED_ACTIONS:
        return {
            "job_id": job_id,
            "action": action,
            "status": "error",
            "message": f"不支持的动作: {action}",
            "started_at": started,
            "finished_at": now_iso(),
            "agent_version": AGENT_VERSION,
        }

    try:
        data = dispatch(action, request)
        return {
            "job_id": job_id,
            "action": action,
            "status": "success",
            "message": str(data.pop("message", "ok")),
            "data": data,
            "started_at": started,
            "finished_at": now_iso(),
            "agent_version": AGENT_VERSION,
        }
    except UnsupportedError as exc:
        return {
            "job_id": job_id,
            "action": action,
            "status": "unsupported",
            "message": str(exc),
            "started_at": started,
            "finished_at": now_iso(),
            "agent_version": AGENT_VERSION,
        }
    except AgentError as exc:
        return {
            "job_id": job_id,
            "action": action,
            "status": "error",
            "message": str(exc),
            "started_at": started,
            "finished_at": now_iso(),
            "agent_version": AGENT_VERSION,
        }
    except Exception as exc:  # noqa: BLE001 - 必须把未知异常也变成结构化结果
        return {
            "job_id": job_id,
            "action": action,
            "status": "error",
            "message": f"Agent 内部错误: {exc}",
            "data": {"traceback": traceback.format_exc(limit=8)},
            "started_at": started,
            "finished_at": now_iso(),
            "agent_version": AGENT_VERSION,
        }


def dispatch(action: str, request: dict[str, Any]) -> dict[str, Any]:
    """按 action 分发到 provider。"""
    registry = build_registry()

    if action == "probe":
        return {
            "message": "agent alive",
            "agent_version": AGENT_VERSION,
            "kinds": registry.kinds(),
            "allowed_roots": os.environ.get("L4D2_ALLOWED_ROOTS", ""),
        }

    if action == "detect":
        return detect_all(registry)

    kind = str(request.get("kind") or "").strip()
    target = request.get("target") or {}
    spec = request.get("spec") or {}
    if not kind:
        raise AgentError("缺少运行时类型 kind")
    provider = registry.get(kind)

    if action == "inspect":
        return provider.inspect(target, spec)
    if action == "start":
        validate_spec_paths(spec)
        return provider.start(target, spec)
    if action == "stop":
        return provider.stop(target, spec)
    if action == "restart":
        validate_spec_paths(spec)
        return provider.restart(target, spec)
    if action == "logs":
        tail = int((request.get("options") or {}).get("tail") or 200)
        return provider.logs(target, spec, tail)
    if action == "export":
        return provider.read_spec(target)
    if action == "import":
        validate_spec_paths(spec)
        return provider.write_spec(target, spec)
    if action == "migrate":
        return migrate(registry, request, provider)
    raise AgentError(f"未实现的动作: {action}")


def validate_spec_paths(spec: dict[str, Any]) -> None:
    """启动前校验工作目录与日志路径，防止写出白名单。"""
    working_dir = str(spec.get("working_dir") or "").strip()
    if working_dir:
        ensure_within_allowed(working_dir)
    log_path = str(spec.get("log_path") or "").strip()
    if log_path:
        ensure_within_allowed(os.path.dirname(log_path) or log_path)


def detect_all(registry: Any) -> dict[str, Any]:
    """汇总所有 provider 的扫描结果。"""
    candidates: list[dict[str, Any]] = []
    warnings: list[str] = []
    for kind in registry.kinds():
        provider = registry.get(kind)
        try:
            found = provider.detect()
        except AgentError as exc:
            warnings.append(f"{kind}: {exc}")
            continue
        except Exception as exc:  # noqa: BLE001 - 单个 provider 失败不能拖垮扫描
            warnings.append(f"{kind}: 扫描异常 {exc}")
            continue
        candidates.extend(found)
    return {
        "message": f"扫描完成，共 {len(candidates)} 个候选",
        "candidates": candidates,
        "warnings": warnings,
    }


def migrate(registry: Any, request: dict[str, Any], source_provider: Any) -> dict[str, Any]:
    """迁移：停源 → 写目标配置 → 起目标 → 健康检查，失败回滚。"""
    options = request.get("options") or {}
    target_kind = str(options.get("target_kind") or "").strip()
    if not target_kind:
        raise AgentError("迁移缺少目标运行时类型")
    target_provider = registry.get(target_kind)
    target = options.get("target") or {}
    target_spec = options.get("target_spec") or {}
    source_target = request.get("target") or {}
    source_spec = request.get("spec") or {}

    validate_spec_paths(target_spec)

    steps: list[str] = []
    steps.append("准备目标运行时配置")
    target_provider.prepare_migration(target, target_spec)

    steps.append("停止源运行时")
    try:
        source_provider.stop(source_target, source_spec)
    except AgentError as exc:
        raise AgentError(f"停止源运行时失败，迁移中止: {exc}") from exc

    steps.append("启动目标运行时")
    try:
        target_provider.start(target, target_spec)
    except AgentError as exc:
        steps.append("目标启动失败，回滚源运行时")
        try:
            source_provider.start(source_target, source_spec)
            steps.append("源运行时已回滚")
        except AgentError as rollback_error:
            steps.append(f"源运行时回滚失败: {rollback_error}")
        raise AgentError(f"启动目标运行时失败: {exc}") from exc

    steps.append("健康检查")
    health = target_provider.inspect(target, target_spec)
    if not health.get("process_running"):
        steps.append("目标未运行，回滚")
        try:
            target_provider.stop(target, target_spec)
        except AgentError:
            pass
        try:
            source_provider.start(source_target, source_spec)
            steps.append("源运行时已回滚")
        except AgentError as rollback_error:
            steps.append(f"源运行时回滚失败: {rollback_error}")
        raise AgentError("迁移后目标运行时未启动成功，已尝试回滚")

    return {
        "message": "迁移完成",
        "steps": steps,
        "health": health,
    }


def process_pending() -> int:
    """处理 requests 目录下所有待处理任务。"""
    ensure_directories()
    prune_processed()
    try:
        names = sorted(os.listdir(REQUEST_DIR))
    except OSError:
        return 0
    pending = [name for name in names if name.endswith(".json") and not name.startswith(".")]
    if not pending:
        return 0

    lock = acquire_lock()
    if lock is None:
        # 已有实例在处理，交给它。
        return 0

    handled = 0
    try:
        for name in pending:
            path = os.path.join(REQUEST_DIR, name)
            request = read_json(path)
            if request is None:
                archive_request(path)
                continue
            result = handle_request(request)
            write_result(result)
            archive_request(path)
            handled += 1
    finally:
        try:
            if fcntl is not None:
                fcntl.flock(lock, fcntl.LOCK_UN)
        finally:
            os.close(lock)
    return handled


def run_inline(argv: list[str]) -> int:
    """同步执行单个动作，供宿主机 shell 脚本（维护、修复）复用。

    用法：
      python3 l4d2_runtime_agent.py --exec stop --kind systemd \
          --target '{"unit_name":"l4d2.service"}' --spec '{"port":27015}'

    结果 JSON 打到 stdout，成功退出码 0，失败 1。
    这样维护脚本不再硬编码 systemctl，也不用重复实现各 provider 的启停逻辑。
    """
    action = ""
    kind = ""
    target_raw = "{}"
    spec_raw = "{}"
    index = 0
    while index < len(argv):
        item = argv[index]
        if item == "--exec" and index + 1 < len(argv):
            action = argv[index + 1]
            index += 2
        elif item == "--kind" and index + 1 < len(argv):
            kind = argv[index + 1]
            index += 2
        elif item == "--target" and index + 1 < len(argv):
            target_raw = argv[index + 1]
            index += 2
        elif item == "--spec" and index + 1 < len(argv):
            spec_raw = argv[index + 1]
            index += 2
        else:
            index += 1

    if not action:
        print(json.dumps({"status": "error", "message": "缺少 --exec 动作"}, ensure_ascii=False))
        return 1
    try:
        target = json.loads(target_raw) if target_raw.strip() else {}
        spec = json.loads(spec_raw) if spec_raw.strip() else {}
    except json.JSONDecodeError as error:
        print(
            json.dumps(
                {"status": "error", "message": f"参数不是合法 JSON: {error}"}, ensure_ascii=False
            )
        )
        return 1

    result = handle_request(
        {
            "job_id": f"inline-{int(time.time() * 1000)}",
            "action": action,
            "kind": kind,
            "target": target,
            "spec": spec,
            "options": {},
            "requester": "host-script",
        }
    )
    print(json.dumps(result, ensure_ascii=False))
    return 0 if result.get("status") == "success" else 1


def main(argv: list[str]) -> int:
    """入口：支持 --once（处理一轮）、--watch（轮询）与 --exec（同步执行）。"""
    ensure_directories()
    if "--exec" in argv[1:]:
        return run_inline(argv[1:])
    mode = "once"
    for item in argv[1:]:
        if item in ("--once", "--watch"):
            mode = item[1:]
    if mode == "watch":
        interval = float(os.environ.get("L4D2_AGENT_INTERVAL", "2"))
        while True:
            try:
                process_pending()
            except Exception as exc:  # noqa: BLE001 - 守护循环不能退出
                print(f"[runtime-agent] 处理异常: {exc}", file=sys.stderr)
            time.sleep(interval)
    process_pending()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
