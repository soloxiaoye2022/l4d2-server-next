"""Agent 单元测试（标准库 unittest，无第三方依赖）。

运行：在 manifest/runtime-agent 目录执行 `python -m unittest discover -s tests -v`
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from agentlib import common
from providers import systemd as systemd_provider


class SplitCommandTest(unittest.TestCase):
    def test_basic(self) -> None:
        self.assertEqual(
            common.split_command("./srcds_run -game left4dead2 +port 27015"),
            ["./srcds_run", "-game", "left4dead2", "+port", "27015"],
        )

    def test_quotes_and_escapes(self) -> None:
        self.assertEqual(
            common.split_command('"/opt/my games/srcds_run" -console'),
            ["/opt/my games/srcds_run", "-console"],
        )
        self.assertEqual(
            common.split_command("'/opt/my games/srcds_run'"), ["/opt/my games/srcds_run"]
        )
        self.assertEqual(
            common.split_command(r"/opt/my\ games/srcds_run"), ["/opt/my games/srcds_run"]
        )

    def test_rejects_broken(self) -> None:
        with self.assertRaises(common.AgentError):
            common.split_command('"./srcds_run')
        with self.assertRaises(common.AgentError):
            common.split_command("./srcds_run \\")


class PortAndQuoteTest(unittest.TestCase):
    def test_parse_port_variants(self) -> None:
        self.assertEqual(common.parse_port(["./srcds_run", "+port", "27015"]), 27015)
        self.assertEqual(common.parse_port(["./srcds_run", "-port", "27016"]), 27016)
        self.assertEqual(common.parse_port(["./srcds_run", "+port=27017"]), 27017)
        self.assertEqual(common.parse_port(["./srcds_run", "-game", "left4dead2"]), 0)

    def test_parse_port_hostport(self) -> None:
        # L4D2 服务端常用 +hostport，必须能识别，否则扫描出的端口恒为 0。
        self.assertEqual(common.parse_port(["./srcds_run", "+hostport", "12138"]), 12138)
        self.assertEqual(common.parse_port(["./srcds_run", "-hostport", "12139"]), 12139)
        self.assertEqual(common.parse_port(["./srcds_run", "+hostport=12140"]), 12140)

    def test_quote_shell_arg(self) -> None:
        self.assertEqual(common.quote_shell_arg("/opt/l4d2/srcds_run"), "/opt/l4d2/srcds_run")
        self.assertEqual(common.quote_shell_arg("/opt/my games"), "'/opt/my games'")
        self.assertEqual(common.quote_shell_arg("it's"), "'it'\\''s'")
        self.assertEqual(common.quote_shell_arg(""), "''")


class SafeHandleTest(unittest.TestCase):
    def test_accepts_valid(self) -> None:
        self.assertEqual(common.safe_handle("l4d2.service", "服务名"), "l4d2.service")

    def test_rejects_injection(self) -> None:
        for bad in ("l4d2; rm -rf /", "-bad", "a b", ""):
            with self.assertRaises(common.AgentError):
                common.safe_handle(bad, "服务名")


class AllowedRootTest(unittest.TestCase):
    def test_within_allowed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            allowed = Path(directory) / "data"
            target = allowed / "l4d2"
            target.mkdir(parents=True)
            with mock.patch.dict(os.environ, {"L4D2_ALLOWED_ROOTS": str(allowed)}):
                resolved = common.ensure_within_allowed(str(target))
                self.assertEqual(resolved, os.path.realpath(str(target)))
                with self.assertRaises(common.AgentError):
                    common.ensure_within_allowed("/etc")
                with self.assertRaises(common.AgentError):
                    common.ensure_within_allowed(str(allowed))
                with self.assertRaises(common.AgentError):
                    common.ensure_within_allowed("relative/path")


class SystemdUnitTest(unittest.TestCase):
    UNIT = """[Unit]
Description=L4D2 Server

[Service]
WorkingDirectory=/home/ubuntu/l4d2
ExecStart=/home/ubuntu/l4d2/srcds_run -game left4dead2 +port 27015
User=ubuntu
Restart=on-failure
"""

    def test_parse_unit(self) -> None:
        spec = systemd_provider.parse_unit(self.UNIT, "/etc/systemd/system/l4d2.service")
        self.assertEqual(spec["working_dir"], "/home/ubuntu/l4d2")
        self.assertEqual(spec["executable"], "/home/ubuntu/l4d2/srcds_run")
        self.assertEqual(spec["port"], 27015)
        self.assertEqual(spec["user"], "ubuntu")
        self.assertTrue(spec["auto_restart"])

    def test_parse_unit_without_exec_start(self) -> None:
        with self.assertRaises(common.AgentError):
            systemd_provider.parse_unit("[Service]\nUser=ubuntu\n", "/x/l4d2.service")

    def test_render_unit_quotes_spaces(self) -> None:
        spec = {
            "working_dir": "/home/ubuntu/my l4d2",
            "executable": "/home/ubuntu/my l4d2/srcds_run",
            "args": ["-game", "left4dead2", "+port", "27015"],
            "user": "ubuntu",
            "auto_restart": True,
        }
        unit = systemd_provider.render_unit("l4d2.service", spec)
        self.assertIn("WorkingDirectory=/home/ubuntu/my l4d2", unit)
        self.assertIn('ExecStart="/home/ubuntu/my l4d2/srcds_run"', unit)
        self.assertIn("User=ubuntu", unit)
        self.assertIn("Restart=on-failure", unit)

    def test_render_unit_rejects_missing_fields(self) -> None:
        with self.assertRaises(common.AgentError):
            systemd_provider.render_unit("l4d2.service", {"working_dir": "", "executable": ""})

    def test_looks_like_game_unit(self) -> None:
        self.assertTrue(systemd_provider._looks_like_game_unit("l4d2.service", ""))
        self.assertTrue(systemd_provider._looks_like_game_unit("srcds-main.service", ""))
        self.assertFalse(systemd_provider._looks_like_game_unit("nginx.service", "web server"))

    def test_rejects_panel_helper_units(self) -> None:
        # 面板自己的辅助 unit 名字里也带 l4d2，必须被排除，
        # 否则接管列表会出现一堆"可接管"的面板工具。
        for helper in (
            "l4d2-diagnostics.service",
            "l4d2-maintenance.service",
            "l4d2-repair.service",
            "l4d2-restart.service",
            "l4d2-startup.service",
            "l4d2-runtime.service",
        ):
            self.assertFalse(
                systemd_provider._looks_like_game_unit(helper, ""),
                f"{helper} 不应被识别为游戏服",
            )

    def test_wrapper_script_drops_port(self) -> None:
        # 真实部署常见 ExecStart 指向包装脚本，unit 里没有 +hostport，
        # parse_unit 只能得到 0，这就是需要 live port 兜底的原因。
        unit = """[Service]
WorkingDirectory=/data/l4d2-server
ExecStart=/data/l4d2-server/start_server.sh
User=root
"""
        spec = systemd_provider.parse_unit(unit, "/etc/systemd/system/l4d2.service")
        self.assertEqual(spec["port"], 0)

    def test_live_port_from_main_pid(self) -> None:
        provider = systemd_provider.SystemdProvider()
        processes = [
            {
                "pid": 456098,
                "argv": [
                    "/bin/sh",
                    "/data/l4d2-server/srcds_run",
                    "-game",
                    "left4dead2",
                    "+hostport",
                    "27015",
                ],
                "cgroup": "/system.slice/l4d2.service",
            }
        ]
        with mock.patch.object(systemd_provider, "find_game_processes", return_value=processes):
            self.assertEqual(provider._live_port("l4d2.service", 456098), 27015)

    def test_live_port_from_cgroup_child(self) -> None:
        # MainPID 是包装脚本时端口可能只在子进程上，按 cgroup 归属匹配。
        provider = systemd_provider.SystemdProvider()
        processes = [
            {
                "pid": 456124,
                "argv": ["./srcds_linux", "-game", "left4dead2", "+hostport", "27016"],
                "cgroup": "/system.slice/l4d2.service",
            }
        ]
        with mock.patch.object(systemd_provider, "find_game_processes", return_value=processes):
            self.assertEqual(provider._live_port("l4d2.service", 999), 27016)

    def test_live_port_returns_zero_without_match(self) -> None:
        provider = systemd_provider.SystemdProvider()
        with mock.patch.object(systemd_provider, "find_game_processes", return_value=[]):
            self.assertEqual(provider._live_port("l4d2.service", 1), 0)


class DockerDetectTest(unittest.TestCase):
    def test_rejects_panel_container(self) -> None:
        from providers import docker as docker_provider

        # 面板自身的容器镜像名带 l4d2，必须排除，
        # 否则用户可能把管理面板当游戏服接管并停掉。
        self.assertFalse(
            docker_provider._looks_like_game_container(
                "l4d2-manager", "l4d2-manager-next:runtime-20261117"
            )
        )
        self.assertFalse(
            docker_provider._looks_like_game_container(
                "l4d2-manager-prev-20261117", "l4d2-manager-next:webfix-20261117"
            )
        )

    def test_accepts_game_container(self) -> None:
        from providers import docker as docker_provider

        self.assertTrue(
            docker_provider._looks_like_game_container("l4d2-server", "some/l4d2:latest")
        )
        self.assertTrue(docker_provider._looks_like_game_container("srcds-main", "srcds:1.0"))

    def test_as_list_handles_null(self) -> None:
        from providers import docker as docker_provider

        # docker inspect 对未配置的字段输出 null，解析后是 None。
        self.assertEqual(docker_provider._as_list(None), [])
        self.assertEqual(docker_provider._as_list("not-a-list"), [])
        self.assertEqual(docker_provider._as_list(["a", 1, "b"]), ["a", "b"])


class AgentDispatchTest(unittest.TestCase):
    def test_unknown_action(self) -> None:
        from l4d2_runtime_agent import handle_request

        result = handle_request({"job_id": "job-1", "action": "rm-rf"})
        self.assertEqual(result["status"], "error")
        self.assertIn("不支持的动作", result["message"])

    def test_probe(self) -> None:
        from l4d2_runtime_agent import handle_request

        result = handle_request({"job_id": "job-2", "action": "probe"})
        self.assertEqual(result["status"], "success")
        self.assertEqual(result["data"]["agent_version"], common.AGENT_VERSION)
        self.assertIn("systemd", result["data"]["kinds"])

    def test_missing_kind(self) -> None:
        from l4d2_runtime_agent import handle_request

        result = handle_request({"job_id": "job-3", "action": "inspect"})
        self.assertEqual(result["status"], "error")
        self.assertIn("kind", result["message"])

    def test_unsupported_kind(self) -> None:
        from l4d2_runtime_agent import handle_request

        result = handle_request({"job_id": "job-4", "action": "inspect", "kind": "nope"})
        self.assertEqual(result["status"], "unsupported")


class InlineExecTest(unittest.TestCase):
    """验证宿主机脚本调用的 --exec 同步入口。"""

    def test_missing_action(self) -> None:
        import l4d2_runtime_agent

        self.assertEqual(l4d2_runtime_agent.run_inline([]), 1)

    def test_invalid_json(self) -> None:
        import l4d2_runtime_agent

        code = l4d2_runtime_agent.run_inline(
            ["--exec", "inspect", "--kind", "systemd", "--target", "{bad"]
        )
        self.assertEqual(code, 1)

    def test_probe_via_inline(self) -> None:
        import l4d2_runtime_agent

        self.assertEqual(l4d2_runtime_agent.run_inline(["--exec", "probe"]), 0)

    def test_unknown_kind_returns_nonzero(self) -> None:
        import l4d2_runtime_agent

        code = l4d2_runtime_agent.run_inline(["--exec", "inspect", "--kind", "nope"])
        self.assertEqual(code, 1)

    def test_systemd_stop_without_systemctl_fails_cleanly(self) -> None:
        import l4d2_runtime_agent

        # 当前测试机可能没有 systemctl，或没有该 unit。
        # 两种情况下都必须返回结构化失败，而不是抛异常。
        code = l4d2_runtime_agent.run_inline(
            [
                "--exec",
                "inspect",
                "--kind",
                "systemd",
                "--target",
                '{"unit_name":"l4d2-nonexistent.service"}',
                "--spec",
                '{"port":27015}',
            ]
        )
        self.assertIn(code, (0, 1))


if __name__ == "__main__":
    unittest.main()
