"""Provider 基类与注册表。

每个 provider 负责一种运行时类型的完整生命周期：
inspect / start / stop / restart / logs / read_spec / write_spec / detect。
Agent 只做参数校验与结果封装，具体实现全部下沉到 provider。
"""

from __future__ import annotations

from typing import Any

from agentlib.common import AgentError, UnsupportedError


class Provider:
    """所有 provider 的父类，默认实现统一抛"不支持"。"""

    kind = ""

    def inspect(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持状态查询")

    def start(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持启动")

    def stop(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持停止")

    def restart(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持重启")

    def logs(self, target: dict[str, Any], spec: dict[str, Any], tail: int) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持日志读取")

    def read_spec(self, target: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持读取启动配置")

    def write_spec(self, target: dict[str, Any], spec: dict[str, Any]) -> dict[str, Any]:
        raise UnsupportedError(f"{self.kind} 不支持写入启动配置")

    def detect(self) -> list[dict[str, Any]]:
        return []

    def prepare_migration(
        self,
        target: dict[str, Any],
        spec: dict[str, Any],
    ) -> dict[str, Any]:
        """迁移到本 provider 前的准备工作，默认写配置。"""
        return self.write_spec(target, spec)


class Registry:
    """provider 注册表，按 kind 分发。"""

    def __init__(self) -> None:
        self._providers: dict[str, Provider] = {}

    def register(self, provider: Provider) -> None:
        if not provider.kind:
            raise AgentError("provider 必须声明 kind")
        self._providers[provider.kind] = provider

    def get(self, kind: str) -> Provider:
        provider = self._providers.get(kind)
        if provider is None:
            raise UnsupportedError(f"未知运行时类型: {kind}")
        return provider

    def kinds(self) -> list[str]:
        return sorted(self._providers)


def build_registry() -> Registry:
    """构建全部 provider。延迟导入避免循环依赖。"""
    from providers.bare import BareProvider
    from providers.docker import DockerProvider
    from providers.lgsm import LGSMProvider
    from providers.mcsm import MCSMProvider
    from providers.nohup import NohupProvider
    from providers.screen import ScreenProvider
    from providers.systemd import SystemdProvider
    from providers.tmux import TmuxProvider

    registry = Registry()
    registry.register(SystemdProvider(system_scope=True))
    registry.register(SystemdProvider(system_scope=False))
    registry.register(DockerProvider())
    registry.register(ScreenProvider())
    registry.register(TmuxProvider())
    registry.register(NohupProvider())
    registry.register(BareProvider())
    registry.register(MCSMProvider())
    registry.register(LGSMProvider())
    return registry
