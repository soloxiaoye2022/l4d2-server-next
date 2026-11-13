
ARG VERSION=2026.11.04

# Stage 1: Build Frontend
FROM node:24-alpine AS web-builder
WORKDIR /app/frontend
COPY ./frontend/package.json ./frontend/package-lock.json ./
RUN npm ci
COPY ./frontend .
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.26.10-alpine AS builder
ARG VERSION
WORKDIR /app/backend
COPY ./backend/go.mod ./backend/go.sum ./
RUN go mod download
COPY ./backend .
RUN go build -ldflags "-X l4d2-manager-next/consts.Version=${VERSION}" -o /app/l4d2-manager

# Stage 3: Final Image
FROM docker:29.1.1-cli-alpine3.22
ARG VERSION
ARG APP_UID=1000
ARG APP_GID=1001
ARG DOCKER_GID=999
LABEL org.opencontainers.image.title="l4d2-manager-next"
LABEL org.opencontainers.image.description="Left 4 Dead 2 server manager with startup, maintenance, RCON, map, plugin and player-stat tools"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.source="https://github.com/LaoYutang/l4d2-server-next"
LABEL org.opencontainers.image.licenses="AGPL-3.0"
EXPOSE 27020
COPY --chown=${APP_UID}:${APP_GID} ./plugins /plugins
COPY ./manifest/systemd /manager-systemd
RUN chmod +x /manager-systemd/*.sh
RUN mkdir -p /data /control \
    && chown "${APP_UID}:${APP_GID}" /data /control
COPY ./backend/preset.yaml /
COPY --from=builder /app/l4d2-manager /
COPY --from=builder /app/backend/ip2region_v4.xdb /
COPY --from=builder /app/backend/ip2region_v6.xdb /
COPY --from=web-builder /app/backend/static /static
RUN addgroup -g "${APP_GID}" l4d2 \
    && adduser -D -H -u "${APP_UID}" -G l4d2 l4d2 \
    && docker_group="$(awk -F: -v gid="${DOCKER_GID}" '$3 == gid { print $1; exit }' /etc/group)" \
    && if [ -n "${docker_group}" ]; then addgroup l4d2 "${docker_group}"; else addgroup -g "${DOCKER_GID}" docker && addgroup l4d2 docker; fi
ENV HOME=/tmp \
    DOCKER_CONFIG=/tmp/.docker
USER l4d2
ENTRYPOINT ["/l4d2-manager"]
