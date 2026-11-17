"""Docker provider：通过 docker CLI 管理容器。"""

from __future__ import annotations

import json
from typing import Any

from agentlib.common import (
    AgentError,
    build_inspect,
    command_exists,
    parse_port,
    run_command,
    safe_handle,
    split_command,
)
from providers.base import Provider


class DockerProvider(Provider):
    kind = "docker"

    def _container(self, target: dict[str, Any]) -> str:
        return safe_handle(target.get("container_name", ""), "容器名")

    def _docker(self, *args: str, timeout: float = 60.0) -> str:
        if not command_exists("docker"):
            raise AgentError("宿主机没有 docker")
        completed = run_command(["docker", *args], timeout=timeout)
        if completed.returncode != 0:
            message = (completed.stderr or completed.stdout or "").strip()
            raise AgentError(f"docker 命令失败: {message[:400]}")
        return (completed.stdout or "").strip()

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        container = self._container(target)
        raw = self._docker(
            "inspect",
            container,
            "--format",
            "{{json .State}}",
        )
        try:
            state = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise AgentError("docker inspect 输出无法解析") from exc
        running = bool(state.get("Running"))
        pid = int(state.get("Pid") or 0)
        port = int(spec.get("port") or 0)
        log_tail = self._tail(container, 40) if running else ""
        return build_inspect(
            running=running,
            pid=pid,
            port=port,
            log_tail=log_tail,
            reason="" if running else str(state.get("Status") or "stopped"),
            state="running" if running else "",
        )

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        container = self._container(target)
        self._docker("start", container, timeout=90)
        return {"message": f"容器已启动: {container}"}

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        container = self._container(target)
        self._docker("stop", container, timeout=120)
        return {"message": f"容器已停止: {container}"}

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        container = self._container(target)
        self._docker("restart", container, timeout=150)
        return {"message": f"容器已重启: {container}"}

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        container = self._container(target)
        output = self._tail(container, tail)
        return {"lines": output.splitlines(), "source": "docker logs"}

    def _tail(self, container: str, tail: int) -> str:
        return self._docker("logs", "--tail", str(tail), container, timeout=30)

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        container = self._container(target)
        raw = self._docker(
            "inspect",
            container,
            "--format",
            "{{json .Config.Cmd}}|{{json .Config.Entrypoint}}|{{json .Config.WorkingDir}}|{{json .Config.Env}}",
        )
        parts = raw.split("|")
        if len(parts) != 4:
            raise AgentError("docker inspect 输出格式异常")
        # Cmd / Entrypoint / Env 在容器里可能是 null，_load_json 会返回 fallback，
        # 但如果 JSON 本身是 "null" 也能解析成功，所以必须再兜一层类型检查。
        cmd = _as_list(_load_json(parts[0], []))
        entrypoint = _as_list(_load_json(parts[1], []))
        working_dir = _load_json(parts[2], "")
        env_list = _as_list(_load_json(parts[3], []))

        tokens = [*entrypoint, *cmd]
        if not tokens:
            raise AgentError("容器没有配置启动命令")
        env = {}
        for item in env_list:
            if isinstance(item, str) and "=" in item:
                key, value = item.split("=", 1)
                env[key] = value
        port = parse_port(tokens)
        return {
            "working_dir": working_dir if isinstance(working_dir, str) else "",
            "executable": tokens[0],
            "args": tokens[1:],
            "env": env,
            "port": port,
            "auto_restart": True,
            "log_path": "",
        }

    def detect(self) -> list[dict[str, Any]]:
        if not command_exists("docker"):
            return []
        raw = self._docker("ps", "-a", "--format", "{{json .}}", timeout=30)
        results: list[dict[str, Any]] = []
        for line in raw.splitlines():
            try:
                item = json.loads(line)
            except json.JSONDecodeError:
                continue
            name = item.get("Names") or ""
            image = (item.get("Image") or "").lower()
            if not _looks_like_game_container(name, image):
                continue
            try:
                spec = self.read_spec({"container_name": name})
            except AgentError:
                spec = {}
            results.append(
                {
                    "kind": "docker",
                    "name": f"容器 {name}",
                    "running": (item.get("State") or "").lower() == "running",
                    "pid": 0,
                    "spec": spec,
                    "target": {"container_name": name},
                    "game_path": spec.get("working_dir", ""),
                    "port": spec.get("port", 0),
                    "confidence": 0.9,
                    "evidence": [
                        f"docker ps 显示容器 {name}",
                        f"镜像 {image}",
                    ],
                }
            )
        return results


def _load_json(raw: str, fallback: Any) -> Any:
    try:
        return json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return fallback


def _as_list(value: Any) -> list[str]:
    """把可能为 None 的 JSON 字段强制成字符串列表。

    docker inspect 对未配置的 Cmd / Entrypoint / Env 会输出 null，
    json.loads 解析成 None 后再做 `[*entrypoint, *cmd]` 会抛
    TypeError，整个扫描就会以"扫描异常"告终。
    """
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str)]


def _looks_like_game_container(name: str, image: str) -> bool:
    """判断容器是否真的是游戏服，而不是面板自身。

    名字/镜像里带 l4d2 的容器很多，面板自己的 l4d2-manager 也在其中。
    必须排除管理面板本体，否则用户会在接管列表里看到"可接管：l4d2-manager"，
    真接管了就会把面板当游戏服停掉。
    """
    haystack = f"{name} {image}".lower()
    if not any(keyword in haystack for keyword in ("l4d2", "srcds", "left4dead")):
        return False
    for marker in PANEL_CONTAINER_MARKERS:
        if marker in haystack:
            return False
    return True


# 面板自身的容器标记，出现任意一个就排除。
PANEL_CONTAINER_MARKERS = ("l4d2-manager", "manager-next")


def parse_container_command(command: str) -> list[str]:
    """把容器命令字符串分词，供其他模块复用。"""
    return split_command(command)
