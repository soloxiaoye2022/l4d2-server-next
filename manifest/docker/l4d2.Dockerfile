ARG VERSION=2026.11.04

FROM ubuntu:22.04

ARG VERSION
LABEL org.opencontainers.image.title="l4d2-pure"
LABEL org.opencontainers.image.description="Left 4 Dead 2 dedicated server image for the l4d2-manager-next panel"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.source="https://github.com/LaoYutang/l4d2-server-next"
LABEL org.opencontainers.image.licenses="AGPL-3.0"

EXPOSE 27015/tcp
EXPOSE 27015/udp
EXPOSE 27021/tcp
EXPOSE 27021/udp

# 环境准备
ENV DEBIAN_FRONTEND=noninteractive
RUN dpkg --add-architecture i386 && \
  apt-get update && \
  apt-get install -y \
  lib32gcc-s1 \
  lib32stdc++6 \
  libc6:i386 \
  libstdc++6:i386 \
  zlib1g:i386 \
  wget \
  vim \
  curl \
  ca-certificates && \
  apt-get clean && \
  rm -rf /var/lib/apt/lists/*
RUN mkdir -p /l4d2 /root/steamcmd

# 安装steamcmd并下载l4d2
WORKDIR /root/steamcmd
RUN wget https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz && \
  tar -zxvf steamcmd_linux.tar.gz && \
  rm steamcmd_linux.tar.gz
RUN for i in 1 2 3 4 5; do ./steamcmd.sh +@sSteamCmdForcePlatformType windows +force_install_dir /l4d2 +login anonymous +app_update 222860 validate +quit && break || sleep 15; done
RUN for i in 1 2 3; do ./steamcmd.sh +@sSteamCmdForcePlatformType linux +force_install_dir /l4d2 +login anonymous +app_update 222860 validate +quit && break || sleep 15; done

# 复制cfg文件
COPY ./manifest/docker/cfg/ /l4d2/left4dead2/cfg/

# 复制启动脚本
COPY ./manifest/docker/start.sh /start.sh
RUN sed -i 's/\r$//' /start.sh && chmod +x /start.sh

CMD ["/start.sh"]
