# Stage 1: Build frontend
FROM --platform=$BUILDPLATFORM node:22.23.2-alpine3.23@sha256:46825fbbd4e996a78b7a2cdc08d75e38a5a505bdab95dcda55605359bf124bc6 AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM --platform=$BUILDPLATFORM golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57 AS backend
WORKDIR /app

# Version stamped into the binary so `depsilo version` / the topbar version
# pill / Prometheus labels match the image tag. Pass at build time:
#   docker build --build-arg VERSION=X.Y.Z --build-arg COMMIT=<commit> -t ghcr.io/depsilo/depsilo:X.Y.Z .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist

# Unified cmd/depsilo entry — server mode when run with no args, otherwise
# dispatches to CLI subcommands (doctor / backup / restore / init-agent / ...).
# Lets `docker exec <container> depsilo doctor` work inside production
# deployments, which the self-test checklist relies on.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false \
    -ldflags="-s -w \
      -X depsilo/internal/version.Version=${VERSION} \
      -X depsilo/internal/version.Commit=${COMMIT} \
      -X depsilo/internal/version.BuildDate=${BUILD_DATE}" \
    -o depsilo ./cmd/depsilo

# Stage 3: Final image
FROM alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/depsilo /app/depsilo
RUN addgroup -S -g 10001 depsilo \
    && adduser -S -D -H -h /root -u 10001 -G depsilo depsilo \
    && mkdir -p /root/.depsilo /root/.local/share/depsilo /root/.config/depsilo \
    && chown root:10001 /root \
    && chmod 0710 /root \
    && chown -R 10001:10001 /root/.depsilo /root/.local /root/.config \
    && chmod 0555 /app/depsilo \
    && ln -s /app/depsilo /usr/local/bin/depsilo

# Preserve the historical /root/.depsilo state path for v0.9 upgrades while
# running the service itself as an unprivileged, fixed identity.
ENV HOME=/root
USER 10001:10001

# Binary installs default to loopback. A container must listen on every
# interface so publishing 23333 reaches the service from the host.
ENV DEPSILO_SERVER_HOST=0.0.0.0
EXPOSE 23333

# Keep container health independent from the host and orchestration layer.
# BusyBox wget is part of Alpine, so this adds no package or attack surface.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${DEPSILO_SERVER_PORT:-23333}/ready" || exit 1

# ENTRYPOINT (not CMD) so `docker run image doctor` works the same as
# `docker exec container depsilo doctor` — args get appended to the
# binary instead of replacing it. With CMD form, `docker run image version`
# tried to exec "version" as the entrypoint and failed.
ENTRYPOINT ["/app/depsilo"]
# Default arg = "serve" so `docker run image` starts the server. Override
# with `docker run image doctor` / `version` / etc. for one-shot CLI use.
# (Bare `./depsilo` with no args prints help by design — cmd/depsilo/main.go.)
CMD ["serve"]
