# Docker Registry E2E is opt-in and is not part of `make test-e2e`.
#
# Verifying `docker pull` through Depsilo requires docker-in-docker (--privileged
# + /var/run/docker.sock or a nested dockerd). That's expensive in CI and
# environment-specific, so this target is opt-in.
#
# Run with:    make test-docker-docker
#
# What it does: spins up a privileged dind container, points it at Depsilo's
# /v2/ endpoint as an unsecured registry mirror, and runs `docker pull` of a
# tiny image. Success means Depsilo's docker adapter (token negotiation +
# manifest+blob fetch) is wired correctly end-to-end.

FROM docker:24-dind@sha256:9b17a9f25adf17b88d0a013b4f00160754adf4b07ccbe9986664a49886c2c98e

ARG DEPSILO_URL

# daemon.json points dockerd inside this container at Depsilo as a registry mirror
RUN registry_host="${DEPSILO_URL#*://}" \
    && mkdir -p /etc/docker \
    && printf '{\n  "registry-mirrors": ["%s"],\n  "insecure-registries": ["%s"]\n}\n' "${DEPSILO_URL}" "${registry_host}" > /etc/docker/daemon.json

# Smoke script: start dockerd, pull alpine, stop.
RUN printf '#!/bin/sh\nset -e\ndockerd >/var/log/dockerd.log 2>&1 &\nfor i in $(seq 1 30); do\n  docker info >/dev/null 2>&1 && break\n  sleep 1\ndone\ndocker info >/dev/null || (echo "dockerd failed to start"; tail /var/log/dockerd.log; exit 1)\ndocker pull alpine:3.18\ndocker image inspect alpine:3.18 >/dev/null\necho "docker: pull alpine OK"\n' > /smoke.sh && chmod +x /smoke.sh

CMD ["/smoke.sh"]
