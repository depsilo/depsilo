# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build backend
FROM golang:alpine AS backend
WORKDIR /app

# Version stamped into the binary so `depsilo version` / the topbar version
# pill / Prometheus labels match the image tag. Pass at build time:
#   docker build --build-arg VERSION=0.8.0 --build-arg COMMIT=$(git rev-parse --short HEAD) -t depsilo/depsilo:0.8.0 .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist

# Unified cmd/depsilo entry — server mode when run with no args, otherwise
# dispatches to CLI subcommands (doctor / backup / restore / init-agent / ...).
# Lets `docker exec <container> /app/depsilo doctor` work inside production
# deployments, which the self-test checklist relies on.
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
      -X depsilo/internal/version.Version=${VERSION} \
      -X depsilo/internal/version.Commit=${COMMIT} \
      -X depsilo/internal/version.BuildDate=${BUILD_DATE}" \
    -o depsilo ./cmd/depsilo

# Stage 3: Final image
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=backend /app/depsilo .
EXPOSE 23333

# ENTRYPOINT (not CMD) so `docker run image doctor` works the same as
# `docker exec container /app/depsilo doctor` — args get appended to the
# binary instead of replacing it. With CMD form, `docker run image version`
# tried to exec "version" as the entrypoint and failed.
ENTRYPOINT ["./depsilo"]
# Default arg = "serve" so `docker run image` starts the server. Override
# with `docker run image doctor` / `version` / etc. for one-shot CLI use.
# (Bare `./depsilo` with no args prints help by design — cmd/depsilo/main.go.)
CMD ["serve"]
