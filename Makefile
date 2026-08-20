.DEFAULT_GOAL := help

# ─── 变量 ─────────────────────────────────────
APP        := depsilo
BIN_DIR    := bin
BIN        := $(BIN_DIR)/$(APP)
DIST_DIR   := dist
WEB_DIR    := web
WEB_DIST   ?= $(WEB_DIR)/dist
WEB_DIST_INDEX := $(WEB_DIST)/index.html
CONFIG     := config.toml
DEV_JWT_SECRET ?= .dev-jwt-secret
PID_FILE   := .server.pid
DEV_LOG    := .dev.log
PORT       ?= 23333
DEV_URL    ?= http://localhost:$(PORT)
UI_HOST    ?= 127.0.0.1
UI_PORT    ?= 5173
TEST_DIR   := testground
NPM_RUN    := npm --prefix $(WEB_DIR) run
VITE_BIN   ?= $(WEB_DIR)/node_modules/.bin/vite
GO_PROD_PKGS := ./cmd/... ./internal/... ./web
GO_TEST_PKGS := $(GO_PROD_PKGS) ./tests/unit/... ./tests/mock/...
# Race detection is intentionally concentrated on packages that own goroutines,
# shared mutable state, streaming lifecycles, or background schedulers. The full
# non-race suite still covers every production and cross-module package.
GO_RACE_PKGS := \
	./internal/accesslog/... \
	./internal/adapter \
	./internal/adapter/huggingface/... \
	./internal/api/... \
	./internal/asyncruntime/... \
	./internal/audit/... \
	./internal/blocklist/... \
	./internal/cache/... \
	./internal/compilecache/... \
	./internal/config/... \
	./internal/db/... \
	./internal/license/... \
	./internal/middleware/... \
	./internal/notify/... \
	./internal/quarantine/... \
	./internal/security/... \
	./internal/server/... \
	./internal/trial/... \
	./internal/upstream/... \
	./internal/upstreamupdates/...

# ─── 版本注入 ─────────────────────────────────
# Match only semver-style tags (v0.2.3) to avoid descriptive tags like
# "portal-redesign-complete" polluting the version pill. Strip leading "v"
# so backend serves clean "0.2.3" — frontend formatVersion() re-prepends
# "v", avoiding "vv0.2.3" double-v.
VERSION    ?= $(shell value=$$(git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --always --dirty 2>/dev/null || true); printf '%s\n' "$${value:-dev}" | sed 's/^v//')
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X depsilo/internal/version.Version=$(VERSION) \
              -X depsilo/internal/version.Commit=$(COMMIT) \
              -X depsilo/internal/version.BuildDate=$(BUILD_DATE)
GO_BUILD   := CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)"

# ─── 初始化与构建 ─────────────────────────────
.PHONY: setup setup-ui version prepare-go frontend build build-dev-server build-server tray app-macos install-linux uninstall-linux autostart-linux unautostart-linux

setup:                          ## 安装 Go 与前端开发依赖
	go mod download
	npm --prefix $(WEB_DIR) ci

setup-ui: setup                 ## 安装依赖及 Playwright Chromium
	cd $(WEB_DIR) && npx --no-install playwright install chromium

version:                        ## 显示将注入二进制的构建信息
	@printf 'version=%s\ncommit=%s\nbuild_date=%s\n' "$(VERSION)" "$(COMMIT)" "$(BUILD_DATE)"

# Go embeds web/dist. Keep backend-only checks usable on a fresh clone without
# forcing a complete frontend build; `frontend` replaces this tiny placeholder.
prepare-go:                     # 内部：确保 Go embed 目录存在
	@if [ ! -f "$(WEB_DIST_INDEX)" ]; then \
		mkdir -p "$(WEB_DIST)"; \
		printf '%s\n' '<!doctype html><title>Depsilo test placeholder</title>' > "$(WEB_DIST_INDEX)"; \
		echo ">>> prepared $(WEB_DIST_INDEX) for Go embed"; \
	fi

frontend:                       # 内部：构建前端
	$(NPM_RUN) build

$(BIN_DIR):
	mkdir -p $@

build: frontend | $(BIN_DIR)    ## 构建前端 + 编译后端
	$(GO_BUILD) -o $(BIN) ./cmd/depsilo

build-dev-server: prepare-go | $(BIN_DIR)  # 内部：为 Vite 热更新仅编译后端
	$(GO_BUILD) -o $(BIN) ./cmd/depsilo

build-server: frontend | $(BIN_DIR)  # 兼容：构建纯服务器入口
	$(GO_BUILD) -o $(BIN)-server ./cmd/server

tray: frontend | $(BIN_DIR)     ## 构建桌面托盘应用
	go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP)-tray ./cmd/depsilo-tray

app-macos: tray                 ## 打 macOS .app bundle（必须在 macOS 上跑）
	@bash scripts/build-macos-app.sh "$(VERSION)"

LINUX_ACTION_install-linux    := install
LINUX_ACTION_uninstall-linux  := uninstall
LINUX_ACTION_autostart-linux  := autostart-enable
LINUX_ACTION_unautostart-linux := autostart-disable

install-linux: tray             ## Linux：安装桌面应用
uninstall-linux:                # 兼容：卸载 Linux 桌面应用
autostart-linux:                ## Linux：开启开机自启
unautostart-linux:              # 兼容：关闭开机自启
install-linux uninstall-linux autostart-linux unautostart-linux:
	@bash scripts/install-linux.sh $(LINUX_ACTION_$@)

# ─── 运行 ─────────────────────────────────────
.PHONY: run run-pro dev dev-ui stop logs

run: build                      ## 编译并前台运行（自动复用本地开发 JWT）
run-pro: build                  ## 编译并前台运行（开启全部 Pro 功能）
run-pro: RUN_ENV := DEPSILO_DEV_PRO=1
run run-pro:
	@$(RUN_ENV) DEPSILO_DEV_JWT_FILE="$(DEV_JWT_SECRET)" bash scripts/run-dev.sh "./$(BIN)" "$(CONFIG)"

dev: build stop                 ## 编译并后台运行（dev 模式）
	@DEPSILO_DEV_JWT_FILE="$(DEV_JWT_SECRET)" bash scripts/dev-service.sh start \
		"./$(BIN)" "$(CONFIG)" "$(PID_FILE)" "$(DEV_LOG)" "$(DEV_URL)" --port "$(PORT)"

dev-ui: build-dev-server        ## 同时运行后端与 Vite 热更新（Ctrl-C 一并停止）
	@bash scripts/dev-service.sh stop "./$(BIN)" "$(PID_FILE)"
	@DEPSILO_DEV_JWT_FILE="$(DEV_JWT_SECRET)" bash scripts/dev-service.sh start \
		"./$(BIN)" "$(CONFIG)" "$(PID_FILE)" "$(DEV_LOG)" "$(DEV_URL)" --port "$(PORT)"
	@bash scripts/dev-ui.sh scripts/dev-service.sh \
		"./$(BIN)" "$(PID_FILE)" "$(DEV_URL)" -- \
		"$(VITE_BIN)" "$(WEB_DIR)" --config "$(WEB_DIR)/vite.config.ts" \
		--host "$(UI_HOST)" --port "$(UI_PORT)" --strictPort

stop:                           ## 停止后台 dev 服务
	@bash scripts/dev-service.sh stop "./$(BIN)" "$(PID_FILE)"

logs:                           ## 查看 dev 模式日志
	@bash scripts/dev-service.sh logs "$(DEV_LOG)"

# ─── CLI 管理 ────────────────────────────────
.PHONY: cli-status cli-activate cli-warmup cli-flush cli-stop

CLI_ARGS_cli-status   := status
CLI_ARGS_cli-activate := activate
CLI_ARGS_cli-warmup   := warmup $(ECO) $(PKGS)
CLI_ARGS_cli-flush    := flush
CLI_ARGS_cli-stop     := stop

cli-status cli-activate cli-warmup cli-flush cli-stop:
	@./$(BIN) $(CLI_ARGS_$@)

# ─── SBOM ─────────────────────────────────────
.PHONY: sbom

# Local SBOM generation mirrors what CI emits on a tag push. Useful for
# spot-checking what ships with a release, or for buyers asking "what's
# in the binary?" without waiting for the next release. Requires syft
# (https://github.com/anchore/syft) — installs are one-line.
sbom:                           # 维护者：生成 SBOM (CycloneDX + SPDX)
	@command -v syft >/dev/null 2>&1 || { echo "syft not installed. Install: curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /usr/local/bin"; exit 1; }
	@mkdir -p dist/sbom
	@VERSION="$(VERSION)"; \
		syft dir:. -o cyclonedx-json="dist/sbom/depsilo-$$VERSION-source.cdx.json" \
		           -o spdx-json="dist/sbom/depsilo-$$VERSION-source.spdx.json" \
		           --quiet; \
		echo "wrote dist/sbom/depsilo-$$VERSION-source.cdx.json"; \
		echo "wrote dist/sbom/depsilo-$$VERSION-source.spdx.json"

# ─── 测试 ─────────────────────────────────────
.PHONY: test test-full test-race test-integration test-ui test-ui-production test-compiler-cache

test: prepare-go                ## 快速 Go 测试（使用缓存，跳过慢速压力边界）
	go test -short $(GO_TEST_PKGS)

test-full: prepare-go           ## 完整 Go 测试（全包、非 race、不使用结果缓存）
	go test -count=1 $(GO_TEST_PKGS)

test-race: prepare-go           ## 对并发与生命周期高风险包运行 race detector
	go test -race -count=1 $(GO_RACE_PKGS)

test-integration: prepare-go    ## 运行集成测试（启动服务 + mock 上游）
	go test ./tests/integration/... -count=1 -timeout 300s -tags integration

test-ui:                        ## 快速浏览器冒烟测试（首次需安装 Playwright Chromium）
	$(NPM_RUN) test:ui:smoke

test-ui-production: build       ## 用 Go 嵌入的生产前端运行最小浏览器冒烟测试
	$(NPM_RUN) test:ui:production

test-compiler-cache:              ## 用官方 ccache + sccache 验证已运行的编译缓存
	@bash scripts/test-compiler-cache.sh

# ─── Docker E2E ──────────────────────────────
# Every fixture accepts one DEPSILO_URL build arg and derives its own route.
# Docker Registry remains opt-in because it additionally requires dind.
DOCKER_HOST_ALIAS       ?= host.docker.internal
DEPSILO_URL             ?= http://$(DOCKER_HOST_ALIAS):$(PORT)
DOCKER_E2E_BUILD_FLAGS  := --no-cache --progress=plain --add-host=$(DOCKER_HOST_ALIAS):host-gateway
TEST_DOCKER_ALL_ECOS    := pypi apt npm go cargo maven rubygems composer nuget conda cran alpine helm huggingface
TEST_DOCKER_ALL_TARGETS := $(addprefix test-docker-,$(TEST_DOCKER_ALL_ECOS))
TEST_DOCKER_ECOS        := $(TEST_DOCKER_ALL_ECOS) docker

.PHONY: test-e2e-server $(TEST_DOCKER_ALL_TARGETS) test-docker-docker test-e2e test-clean

test-e2e-server: build-dev-server stop  # 内部：仅启动真实客户端测试所需的后端
	@DEPSILO_DEV_JWT_FILE="$(DEV_JWT_SECRET)" bash scripts/dev-service.sh start \
		"./$(BIN)" "$(CONFIG)" "$(PID_FILE)" "$(DEV_LOG)" "$(DEV_URL)" --port "$(PORT)"

$(TEST_DOCKER_ALL_TARGETS): test-docker-%: test-e2e-server
	@echo "=== [$*] real-client E2E ==="
	docker build $(DOCKER_E2E_BUILD_FLAGS) --build-arg DEPSILO_URL=$(DEPSILO_URL) \
		-t depsilo-test-$* $(TEST_DIR)/docker-$*

test-docker-docker: test-e2e-server  # opt-in：Docker Registry dind E2E
	@echo "=== [docker] docker pull alpine (dind) ==="
	docker build $(DOCKER_E2E_BUILD_FLAGS) --build-arg DEPSILO_URL=$(DEPSILO_URL) \
		-t depsilo-test-docker $(TEST_DIR)/docker-docker
	docker run --rm --privileged --add-host=$(DOCKER_HOST_ALIAS):host-gateway depsilo-test-docker

test-e2e: $(TEST_DOCKER_ALL_TARGETS)  ## 运行 14 个真实客户端 E2E
	@echo ">>> ALL 14 ECOSYSTEMS PASSED"

test-clean:                     ## 清理端到端测试环境
	@docker rmi $(addprefix depsilo-test-,$(TEST_DOCKER_ECOS)) >/dev/null 2>&1 || true
	@echo ">>> test env removed"

# ─── Docker ───────────────────────────────────
DOCKER_IMAGE := depsilo/depsilo
DOCKER_TAG   := local
DOCKER_NAME  := depsilo-local
COMPOSE      ?= docker compose

.PHONY: docker-build docker-run docker-stop docker-logs docker-shell docker-status docker-compose-up docker-compose-down docker-test proxy-env

docker-build:                   ## 本地构建 Docker 镜像
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo ">>> built $(DOCKER_IMAGE):$(DOCKER_TAG)"
	@docker images $(DOCKER_IMAGE):$(DOCKER_TAG) --format "    Size: {{.Size}}"

docker-run: docker-stop         ## 运行本地容器
	@mkdir -p data
	@test -f "$(CONFIG)" || { echo "missing $(CONFIG); create it before docker-run" >&2; exit 1; }
	@secret="$${DEPSILO_AUTH_JWT_SECRET:-}"; \
		if [ -z "$$secret" ] && [ -s "$(DEV_JWT_SECRET)" ]; then secret=$$(cat "$(DEV_JWT_SECRET)"); fi; \
		if [ -z "$$secret" ]; then echo "set DEPSILO_AUTH_JWT_SECRET or run make run once" >&2; exit 1; fi; \
		DEPSILO_AUTH_JWT_SECRET="$$secret" docker run -d \
			--name $(DOCKER_NAME) \
			-p $(PORT):23333 \
			-v $(CURDIR)/data:/app/data \
			-v $(abspath $(CONFIG)):/app/config.toml:ro \
			-e DEPSILO_CONFIG=/app/config.toml \
			-e DEPSILO_AUTH_JWT_SECRET \
			--restart unless-stopped \
			$(DOCKER_IMAGE):$(DOCKER_TAG)
	@sleep 2
	@if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
		echo ">>> $(DOCKER_NAME) running  http://localhost:$(PORT)"; \
	else \
		echo ">>> container started, waiting for ready..."; \
		sleep 3; \
		curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1 && \
			echo ">>> $(DOCKER_NAME) running  http://localhost:$(PORT)" || \
			{ echo ">>> FAILED — docker logs $(DOCKER_NAME)"; docker logs --tail 20 $(DOCKER_NAME); exit 1; }; \
	fi

docker-stop:                    ## 停止并删除本地容器
	@docker rm -f $(DOCKER_NAME) 2>/dev/null || true

docker-logs:                    # 兼容：查看容器日志
	docker logs -f $(DOCKER_NAME)

docker-shell:                   # 兼容：进入容器 shell
	docker exec -it $(DOCKER_NAME) sh

docker-status:                  # 兼容：查看容器状态
	@docker ps -f name=$(DOCKER_NAME) --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}\t{{.Size}}"
	@echo ""
	@curl -sf http://localhost:$(PORT)/health 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "health: unreachable"

docker-compose-up:              # 兼容：使用 Docker Compose 启动
	PORT=$(PORT) $(COMPOSE) up -d
	@echo ">>> running at http://localhost:$(PORT)"

docker-compose-down:            # 兼容：停止 Docker Compose
	$(COMPOSE) down

docker-test: docker-build docker-run  # 兼容：构建、启动并冒烟测试
	@echo ""
	@echo "=== Smoke Test ==="
	@echo -n "  health:   " && curl -sf http://localhost:$(PORT)/health | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('status','FAIL'))" 2>/dev/null || echo "FAIL"
	@echo -n "  pypi:     " && curl -sf -o /dev/null -w "%{http_code}" http://localhost:$(PORT)/pypi/simple/ && echo "" || echo "FAIL"
	@echo -n "  npm:      " && curl -sf -o /dev/null -w "%{http_code}" http://localhost:$(PORT)/npm/lodash 2>/dev/null && echo "" || echo "FAIL (may need upstream)"
	@echo -n "  go:       " && curl -sf -o /dev/null -w "%{http_code}" http://localhost:$(PORT)/go/github.com/gin-gonic/gin/@v/list 2>/dev/null && echo "" || echo "FAIL (may need upstream)"
	@echo -n "  maven:    " && curl -sf -o /dev/null -w "%{http_code}" http://localhost:$(PORT)/maven/ 2>/dev/null && echo "" || echo "FAIL (may need upstream)"
	@echo -n "  frontend: " && curl -sf -o /dev/null -w "%{http_code}" http://localhost:$(PORT)/ && echo "" || echo "FAIL"
	@echo ""
	@echo ">>> Done. Container still running. Use 'make docker-stop' to stop."

proxy-env: cli-activate          # 兼容别名：打印代理环境变量

# ─── 发布 ─────────────────────────────────────
.PHONY: release-dry-run release-check

release-dry-run: frontend        ## 本地模拟 goreleaser 发布（不推送）
	goreleaser release --snapshot --clean --skip=publish

release-check:                   ## 检查 goreleaser 配置是否正确
	goreleaser check

# ─── 清理 ─────────────────────────────────────
.PHONY: clean clean-all lint lint-go lint-web lint-i18n check verify verify-modules verify-build verify-web verify-ui security verify-scripts verify-installer help

clean: stop docker-stop         ## 清理构建产物（保留运行数据与开发 JWT）
	rm -rf $(BIN_DIR) $(DIST_DIR) $(WEB_DIST) $(DEV_LOG) $(PID_FILE)
	@echo ">>> clean done"

clean-all: clean                ## 额外删除本地运行数据与开发 JWT（不可恢复）
	rm -rf data $(DEV_JWT_SECRET) $(DEV_JWT_SECRET).tmp.*
	@echo ">>> local runtime data removed"

lint: lint-go lint-web lint-i18n  ## Go/前端/i18n 静态检查

lint-go: prepare-go             # 内部：检查 gofmt + go vet
	@unformatted="$$(git ls-files --cached --others --exclude-standard -z -- '*.go' | \
		xargs -0 sh -c 'for file do [ ! -f "$$file" ] || gofmt -l "$$file"; done' sh)"; \
		test -z "$$unformatted" || { echo "gofmt required:"; echo "$$unformatted"; exit 1; }
	go vet $(GO_TEST_PKGS)

lint-web:                       # 内部：检查前端 ESLint
	$(NPM_RUN) lint

lint-i18n:                      ## 检查 zh.ts/en.ts 与 t() 调用是否一致
	@python3 scripts/i18n-audit.py

verify-web:                     # 内部：前端类型、浏览器契约与生产构建
	$(NPM_RUN) type-check:e2e
	$(NPM_RUN) test:unit
	$(NPM_RUN) build
	$(NPM_RUN) check:bundle

verify-modules:                 # 内部：校验 Go 模块完整性与 tidy 状态
	go mod verify
	go mod tidy -diff

verify-build: prepare-go        # 内部：编译正式 CLI 入口
	@output="$$(mktemp)"; trap 'rm -f "$$output"' 0; $(GO_BUILD) -o "$$output" ./cmd/depsilo

verify-ui:                      # 内部：运行完整 Playwright 浏览器测试
	$(NPM_RUN) test:ui

security: prepare-go            ## 扫描 Go 与全部前端依赖漏洞（需要联网）
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 $(GO_PROD_PKGS)
	npm --prefix $(WEB_DIR) audit --audit-level=moderate

verify-scripts:                 # 内部：安装、开发与发布脚本回归测试
	bash -n install.sh scripts/*.sh
	bash scripts/test-install-checksum.sh
	bash scripts/test-makefile.sh
	bash scripts/test-make-dev-run.sh
	bash scripts/test-dev-service.sh
	bash scripts/test-dev-ui.sh
	node scripts/test-vite-proxy-routes.mjs
	bash scripts/test-release-tag.sh
	bash scripts/test-release-workflow.sh

verify-installer: verify-scripts # 兼容别名

check: lint test verify-web verify-build test-ui  ## 日常提交前快速检查

verify: lint verify-modules test-full test-race test-integration verify-web verify-build verify-ui verify-scripts  ## 完整离线验证

# These aggregate targets share web/dist between Vite and Go's embed package.
# Keep their prerequisites serialized even when a caller passes `make -j`.
.NOTPARALLEL: check verify

# ─── 帮助 ─────────────────────────────────────
help:                           ## 显示帮助
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-21s\033[0m %s\n", $$1, $$2}'
