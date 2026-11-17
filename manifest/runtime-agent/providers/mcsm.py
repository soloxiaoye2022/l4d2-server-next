"""MCSManager provider：通过 MCSM 节点 API 管理实例。

凭据来自环境变量，不落盘、不回显：
  L4D2_MCSM_API_KEY  节点 API Key
  L4D2_MCSM_TIMEOUT  请求超时秒数（默认 15）
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.parse
import urllib.request
from typing import Any

from agentlib.common import AgentError, build_inspect, safe_handle
from providers.base import Provider


class MCSMProvider(Provider):
    kind = "mcsm"

    def _base_url(self, target: dict[str, Any]) -> str:
        url = (target.get("mcsm_node_url") or "").strip().rstrip("/")
        if not url.startswith("http://") and not url.startswith("https://"):
            raise AgentError("MCSManager 节点地址必须以 http:// 或 https:// 开头")
        return url

    def _instance(self, target: dict[str, Any]) -> str:
        return safe_handle(target.get("mcsm_instance", ""), "MCSM 实例 ID")

    def _request(
        self, method: str, url: str, payload: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        api_key = os.environ.get("L4D2_MCSM_API_KEY", "").strip()
        if not api_key:
            raise AgentError("未配置 L4D2_MCSM_API_KEY，无法调用 MCSManager API")
        timeout = float(os.environ.get("L4D2_MCSM_TIMEOUT", "15"))
        data = None
        headers = {
            "Content-Type": "application/json",
            "X-Requested-With": "XMLHttpRequest",
            "apikey": api_key,
        }
        if payload is not None:
            data = json.dumps(payload).encode("utf-8")
        request = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                body = response.read().decode("utf-8", errors="replace")
        except urllib.error.HTTPError as exc:
            raise AgentError(f"MCSManager 返回 HTTP {exc.code}") from exc
        except urllib.error.URLError as exc:
            raise AgentError(f"无法连接 MCSManager: {exc.reason}") from exc
        try:
            return json.loads(body)
        except json.JSONDecodeError as exc:
            raise AgentError("MCSManager 返回内容不是 JSON") from exc

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        base = self._base_url(target)
        instance = self._instance(target)
        payload = self._request(
            "GET",
            f"{base}/api/instance?uuid={urllib.parse.quote(instance)}&remote_uuid=&apikey=",
        )
        data = payload.get("data") or {}
        status = int(data.get("status") or 0)
        running = status == 3
        return build_inspect(
            running=running,
            pid=int(data.get("pid") or 0),
            port=int(spec.get("port") or 0),
            log_tail="",
            reason="" if running else f"MCSM 状态码 {status}",
            state="running" if running else "",
        )

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "start")

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "stop")

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return self._lifecycle(target, "restart")

    def _lifecycle(self, target: dict[str, Any], action: str) -> dict[str, Any]:
        base = self._base_url(target)
        instance = self._instance(target)
        payload = {"uuid": instance, "remote_uuid": "", "apikey": ""}
        result = self._request("POST", f"{base}/api/instance/{action}", payload)
        if result.get("status") not in (200, 0, None):
            raise AgentError(f"MCSM {action} 失败: {result.get('data')}")
        return {"message": f"MCSManager 实例已执行 {action}"}

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        base = self._base_url(target)
        instance = self._instance(target)
        payload = self._request(
            "GET",
            f"{base}/api/instance/outputlog?uuid={urllib.parse.quote(instance)}&apikey=",
        )
        text = str(payload.get("data") or "")
        lines = text.splitlines()
        return {"lines": lines[-tail:] if tail > 0 else lines, "source": "MCSManager 输出日志"}

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        raise AgentError("MCSManager 实例无法读取原始启动命令，请在面板内单独配置")

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        return {"message": "MCSManager 启动参数由实例自身管理，面板仅记录规格"}

    def detect(self) -> list[dict[str, Any]]:
        # 节点地址需要用户配置，无法从本机自动发现。
        return []
