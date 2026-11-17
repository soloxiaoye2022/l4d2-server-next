#!/usr/bin/env bash
# 运行时控制分发器：宿主机脚本统一通过它启停游戏服。
#
# 为什么需要它：
#   旧版 run-maintenance.sh / repair-addons.sh 直接写死 systemctl，
#   接管 screen / tmux / nohup / docker 之后更新一次就会把服停死，
#   而且再也起不来（D4）。
#
# 现在的做法：
#   面板把"当前默认运行时"的 kind / target / spec 写到
#   <control>/runtime-control.json，本脚本只做两件事：
#     1. 读快照；
#     2. 调 runtime-agent 的 --exec 同步入口。
#   所有启停逻辑只有一份实现（Python providers），shell 不再重复。
#
# 用法： runtime-control.sh <start|stop|restart|inspect> [快照路径]
# 退出码： 0 成功；1 动作失败；2 无快照（调用方回退旧的 systemctl 路径）

set -uo pipefail

ACTION="${1:-}"
SPEC_PATH="${2:-}"

MANAGER_DIR="${L4D2_MANAGER_DIR:-/opt/l4d2-manager}"
CONTROL_DIR="${L4D2_CONTROL_DIR:-${MANAGER_DIR}/control}"
AGENT_PATH="${L4D2_RUNTIME_AGENT:-${MANAGER_DIR}/runtime-agent/l4d2_runtime_agent.py}"

if [[ -z "$ACTION" ]]; then
  echo "用法: runtime-control.sh <start|stop|restart|inspect> [快照路径]" >&2
  exit 1
fi
if [[ -z "$SPEC_PATH" ]]; then
  SPEC_PATH="${CONTROL_DIR}/runtime-control.json"
fi

# 没有快照说明用户还没接管任何运行时，调用方必须回退旧路径。
if [[ ! -r "$SPEC_PATH" ]]; then
  exit 2
fi
if [[ ! -r "$AGENT_PATH" ]]; then
  echo "运行时 Agent 未安装: $AGENT_PATH" >&2
  exit 2
fi

# 用 Python 解析快照，输出三行：kind / target / spec。
# 用换行分隔而不是空格，避免 JSON 里的空格被截断。
parsed="$(python3 - "$SPEC_PATH" <<'PY'
import json
import sys

try:
    with open(sys.argv[1], encoding="utf-8") as handle:
        snapshot = json.load(handle)
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"快照读取失败: {error}")

kind = str(snapshot.get("kind") or "").strip()
if not kind:
    raise SystemExit("快照缺少 kind")
print(kind)
print(json.dumps(snapshot.get("target") or {}, ensure_ascii=False))
print(json.dumps(snapshot.get("spec") or {}, ensure_ascii=False))
PY
)" || {
  echo "运行时快照无效" >&2
  exit 2
}

KIND="$(printf '%s\n' "$parsed" | sed -n '1p')"
TARGET="$(printf '%s\n' "$parsed" | sed -n '2p')"
SPEC="$(printf '%s\n' "$parsed" | sed -n '3p')"

if [[ -z "$KIND" ]]; then
  exit 2
fi

python3 "$AGENT_PATH" --exec "$ACTION" --kind "$KIND" --target "$TARGET" --spec "$SPEC"
