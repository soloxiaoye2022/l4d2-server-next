FROM l4d2-manager-next:steam-key-final-20261104
ARG APP_UID=1000
ARG APP_GID=1001
ARG DOCKER_GID=999
RUN addgroup -g "${APP_GID}" l4d2 \
    && adduser -D -H -u "${APP_UID}" -G l4d2 l4d2 \
    && docker_group="$(awk -F: -v gid="${DOCKER_GID}" '$3 == gid { print $1; exit }' /etc/group)" \
    && if [ -n "${docker_group}" ]; then addgroup l4d2 "${docker_group}"; else addgroup -g "${DOCKER_GID}" docker && addgroup l4d2 docker; fi
ENV HOME=/tmp \
    DOCKER_CONFIG=/tmp/.docker
USER l4d2
