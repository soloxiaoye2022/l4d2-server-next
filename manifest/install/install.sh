# 一键安装服务器与面板
set -e

# 检测是否有docker
if ! command -v docker &> /dev/null; then
  # 安装docker
  echo "Docker 未安装，正在安装..."
  if curl -s ipinfo.io | grep -q '"country": "CN"'; then
    export DOWNLOAD_URL="https://mirrors.tuna.tsinghua.edu.cn/docker-ce"
  fi
  curl -fsSL https://get.docker.com -o get-docker.sh 
  bash get-docker.sh 
  rm get-docker.sh
  echo "Docker 安装完成。"
fi

# 询问管理密码并确认
read -r -s -p "请设置管理密码: " admin_password
echo
read -r -s -p "请确认管理密码: " admin_password_confirm
echo

if [ "$admin_password" != "$admin_password_confirm" ]; then
    echo "两次输入的密码不一致，请重新运行"
    exit 1
fi

# 询问游戏端口，默认为27015
read -r -p "请输入游戏端口 (默认: 27015): " game_port
game_port=${game_port:-27015}

# 询问管理面板端口，默认为27020
read -r -p "请输入管理面板端口 (默认: 27020): " manager_port
manager_port=${manager_port:-27020}

# 询问 Steam API Key
echo "Steam API Key 用于查询玩家游戏时长，获取地址: https://steamcommunity.com/dev/apikey"
read -r -p "请输入 Steam API Key (可选，默认为空): " steam_api_key
steam_api_key=${steam_api_key:-""}

# 生成随机RCON密码
L4D2_RCON_PASSWORD=$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 16)

docker_gid=$(stat -c '%g' /var/run/docker.sock 2>/dev/null || echo 999)
mkdir -p /data/l4d2
# 运行时 Agent 的控制目录必须由面板用户（1000:1001）可写，
# 否则面板写不进 requests/，agent_mode 会退化为 disabled。
mkdir -p /opt/l4d2-manager/control
chown -R 1000:1001 /opt/l4d2-manager/control 2>/dev/null || true
# 写入docker-compose文件
cat > /data/l4d2/docker-compose.yaml << EOF
volumes:
  l4d2-data:
  l4d2-plugins:
  l4d2-manager-data:

networks:
  l4d2-network:

services:
  l4d2:
    image: laoyutang/l4d2-pure:latest
    container_name: l4d2
    restart: unless-stopped
    ports:
      - "$game_port:$game_port"
      - "$game_port:$game_port/udp"
    volumes:
      - l4d2-data:/l4d2/left4dead2
      - /etc/localtime:/etc/localtime:ro
      - /etc/timezone:/etc/timezone:ro
    networks:
      - l4d2-network
    security_opt:
      - seccomp:unconfined # Docker 29.3+ 默认 seccomp 与 32 位 srcds 不兼容，低版本可省略此项
    environment:
      - L4D2_TICK=100 # 30,60,100,128
      - L4D2_VAC=false # false: 添加-insecure, true: 不添加
      - L4D2_PORT=$game_port
      - L4D2_RCON_PASSWORD=$L4D2_RCON_PASSWORD
    logging:
      options:
        max-size: "50m" 
        max-file: "3"

  l4d2-manager:
    image: laoyutang/l4d2-manager-next:latest
    container_name: l4d2-manager
    restart: unless-stopped
    group_add:
      - "$docker_gid"
    ports:
      - "$manager_port:27020"
    volumes:
      - l4d2-data:/left4dead2
      - l4d2-plugins:/plugins
      - l4d2-manager-data:/data
      - /opt/l4d2-manager/control:/control
      - /var/run/docker.sock:/var/run/docker.sock
      - /proc:/host/proc:ro
      - /run/systemd/system:/host/systemd:ro
      - /etc/localtime:/etc/localtime:ro
      - /etc/timezone:/etc/timezone:ro
    environment:
      - L4D2_RESTART_BY_RCON=true
      - L4D2_MANAGER_PASSWORD=$admin_password
      - L4D2_RCON_PASSWORD=$L4D2_RCON_PASSWORD
      - L4D2_RCON_URL=l4d2:$game_port
      - L4D2_GAME_PATH=/left4dead2
      - L4D2_CONTROL_DIR=/control
      - L4D2_HOST_SYSTEMD_DIR=/host/systemd
      - STEAM_API_KEY=$steam_api_key
    networks:
      - l4d2-network
    logging:
      options:
        max-size: "50m" 
        max-file: "3"
EOF

# ipinfo检测是否是国内环境，国内则增加compose中的镜像
if curl -s ipinfo.io | grep -q '"country": "CN"'; then
  # 询问是否使用镜像源
  read -r -p "检测到国内环境，是否使用国内镜像源？(Y/n, 默认: Y): " use_mirror
  use_mirror=${use_mirror:-y}
  if [[ "$use_mirror" =~ ^[Yy]$ ]]; then
    # 输入镜像源
    read -r -p "请输入国内镜像源地址 (默认: docker.cnb.cool): " mirror_url
    mirror_url=${mirror_url:-docker.cnb.cool}
    echo "正在配置compose文件使用国内镜像源..."
    sed -i "s|laoyutang/l4d2-pure:latest|$mirror_url/laoyutang/l4d2-pure:latest|" /data/l4d2/docker-compose.yaml
    sed -i "s|laoyutang/l4d2-manager-next:latest|$mirror_url/laoyutang/l4d2-manager-next:latest|" /data/l4d2/docker-compose.yaml
  fi
fi

# 启动docker-compose
cd /data/l4d2
docker compose up -d

# 增加软连接
ln -sf /var/lib/docker/volumes/l4d2_l4d2-data/_data/addons ./addons
ln -sf /var/lib/docker/volumes/l4d2_l4d2-data/_data/cfg ./cfg

# 安装宿主机运行时 Agent（可选，但只有装了才能接管 systemd/screen/tmux 等非 Docker 部署）
install_runtime_agent() {
  if ! command -v systemctl > /dev/null 2>&1; then
    echo "未检测到 systemd，跳过运行时 Agent 安装（面板将使用本地降级模式）。"
    return 0
  fi
  if ! command -v python3 > /dev/null 2>&1; then
    echo "未检测到 python3，跳过运行时 Agent 安装。"
    return 0
  fi

  local container="l4d2-manager-runtime-agent-extract"
  # 镜像名必须从运行中的管理器容器读取：用户可能选了国内镜像源，
  # 写死 laoyutang/... 会拿到不存在的镜像。
  local manager_image
  manager_image=$(docker inspect l4d2-manager --format '{{.Config.Image}}' 2> /dev/null)
  if [ -z "$manager_image" ]; then
    manager_image="laoyutang/l4d2-manager-next:latest"
  fi
  docker rm -f "$container" > /dev/null 2>&1 || true
  if ! docker create --name "$container" "$manager_image" > /dev/null 2>&1; then
    echo "无法从镜像提取运行时 Agent，请手动安装。"
    return 0
  fi
  mkdir -p /opt/l4d2-manager/runtime-agent
  docker cp "$container:/manager-runtime-agent/." /opt/l4d2-manager/runtime-agent/ > /dev/null 2>&1 || true
  docker cp "$container:/manager-systemd/l4d2-runtime.path" /etc/systemd/system/ > /dev/null 2>&1 || true
  docker cp "$container:/manager-systemd/l4d2-runtime.service" /etc/systemd/system/ > /dev/null 2>&1 || true
  # 维护/修复脚本要调这个分发器，缺了就会退回 systemctl（D4）。
  docker cp "$container:/manager-systemd/runtime-control.sh" /opt/l4d2-manager/ > /dev/null 2>&1 || true
  docker rm -f "$container" > /dev/null 2>&1 || true

  chmod +x /opt/l4d2-manager/runtime-agent/l4d2_runtime_agent.py 2>/dev/null || true
  chmod +x /opt/l4d2-manager/runtime-control.sh 2>/dev/null || true
  mkdir -p /opt/l4d2-manager/control/runtime/{requests,results,processed}
  chown -R 1000:1001 /opt/l4d2-manager/control 2>/dev/null || true

  systemctl daemon-reload
  systemctl enable --now l4d2-runtime.path
  echo "运行时 Agent 已安装：/opt/l4d2-manager/runtime-agent"
}

install_runtime_agent

# 输出提示信息
echo "L4D2 服务器和管理面板已安装并启动。"
