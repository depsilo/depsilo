.PHONY: build run run-pro dev stop test test-unit test-integration test-http test-all test-pypi test-apt test-e2e test-clean clean lint lint-i18n frontend help \
	test-docker-pypi test-docker-apt test-docker-npm test-docker-go test-docker-cargo \
	test-docker-maven test-docker-rubygems test-docker-composer test-docker-nuget \
	test-docker-conda test-docker-cran test-docker-helm test-docker-docker \
	test-docker-all test-docker \
	docker-build docker-run docker-stop docker-logs docker-shell docker-status docker-compose-up docker-compose-down docker-test \
	tray app-macos install-linux uninstall-linux autostart-linux unautostart-linux

# ─── 变量 ─────────────────────────────────────
APP        := depsilo
BIN        := bin/$(APP)
CONFIG     := config.toml
PID_FILE   := .server.pid
PORT       := 23333
TEST_DIR   := testground
HOST_IP    := $(shell ip -4 addr show docker0 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' || echo "172.17.0.1")

# ─── 版本注入 ─────────────────────────────────
# Match only semver-style tags (v0.2.3) to avoid descriptive tags like
# "portal-redesign-complete" polluting the version pill. Strip leading "v"
# so backend serves clean "0.2.3" — frontend formatVersion() re-prepends
# "v", avoiding "vv0.2.3" double-v.
VERSION    ?= $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X depsilo/internal/version.Version=$(VERSION) \
              -X depsilo/internal/version.Commit=$(COMMIT) \
              -X depsilo/internal/version.BuildDate=$(BUILD_DATE)

# ─── 构建 ─────────────────────────────────────
frontend:                       ## 构建前端
	cd web && npm run build

build: frontend                 ## 构建前端 + 编译后端（统一 CLI+Server 二进制）
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/depsilo

build-server: frontend          ## 构建前端 + 编译后端（纯服务器模式，用于桌面版）
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN)-server ./cmd/server

build-desktop: frontend         ## 构建前端 + 编译桌面版
	go build -tags "desktop,production" -ldflags "$(LDFLAGS)" -o bin/$(APP)-desktop ./cmd/server

tray: frontend                  ## 构建 menu-bar 应用二进制（macOS / Linux / Windows）
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP)-tray ./cmd/depsilo-tray

app-macos: tray                 ## 打 macOS .app bundle（必须在 macOS 上跑）
	@bash scripts/build-macos-app.sh "$(VERSION)"

install-linux: tray             ## Linux：安装 tray 二进制 + .desktop 到 ~/.local
	@bash scripts/install-linux.sh install

uninstall-linux:                ## Linux：卸载 tray
	@bash scripts/install-linux.sh uninstall

autostart-linux:                ## Linux：开启开机自启
	@bash scripts/install-linux.sh autostart-enable

unautostart-linux:              ## Linux：关闭开机自启
	@bash scripts/install-linux.sh autostart-disable

# ─── 运行 ─────────────────────────────────────
run: build                      ## 编译并前台运行
	DEPSILO_CONFIG=$(CONFIG) ./$(BIN) serve

run-pro: build                  ## 编译并前台运行（开启全部 Pro 功能）
	DEPSILO_DEV_PRO=1 DEPSILO_CONFIG=$(CONFIG) ./$(BIN) serve

dev: build stop                 ## 编译并后台运行（dev 模式）
	@echo ">>> starting $(APP) on :$(PORT) ..."
	@DEPSILO_CONFIG=$(CONFIG) ./$(BIN) serve > .dev.log 2>&1 & echo $$! > $(PID_FILE)
	@sleep 3
	@if curl -sf http://localhost:$(PORT)/health > /dev/null 2>&1; then \
		echo ">>> $(APP) running  pid=$$(cat $(PID_FILE))  http://localhost:$(PORT)"; \
	else \
		echo ">>> FAILED to start, check .dev.log"; cat .dev.log | tail -20; \
	fi

stop:                           ## 停止后台 dev 服务
	@if [ -f $(PID_FILE) ]; then \
		kill $$(cat $(PID_FILE)) 2>/dev/null || true; \
		rm -f $(PID_FILE); \
		echo ">>> stopped"; \
	fi

logs:                           ## 查看 dev 模式日志
	@tail -f .dev.log

# ─── CLI 管理 ────────────────────────────────
cli-status:                     ## 显示 Depsilo 状态
	@./$(BIN) status

cli-activate:                   ## 输出 Shell 环境变量配置
	@./$(BIN) activate

cli-warmup:                     ## 预热缓存 (make cli-warmup ECO=pypi PKGS="requests numpy")
	@./$(BIN) warmup $(ECO) $(PKGS)

cli-flush:                      ## 清除过期缓存
	@./$(BIN) flush

cli-stop:                       ## 停止后台 daemon
	@./$(BIN) stop

# ─── 测试 ─────────────────────────────────────
test: test-unit                 ## 运行 Go 单元测试

test-unit:                      ## 运行单元测试
	go test ./tests/unit/... -v -count=1

test-integration:               ## 运行集成测试（启动服务 + mock 上游）
	go test ./tests/integration/... -v -count=1 -timeout 300s -tags integration

test-http:                      ## 运行集成测试（仅 HTTP 端点）
	go test ./tests/integration/... -v -count=1 -timeout 120s -tags integration \
		-run "Test[^_]+_(SimpleIndex|Metadata|Download|CacheHit|Release|Packages|ConfigJson|ModuleList|ArtifactDownload|Specs|PackagesJson|ServiceIndex|RepoData|IndexYaml)"

test-all:                       ## 运行全部测试（单元 + 集成）
	go test ./... -v -count=1

$(TEST_DIR)/.venv:              ## 初始化测试用 Python 虚拟环境（uv）
	@mkdir -p $(TEST_DIR)
	cd $(TEST_DIR) && uv venv .venv
	@echo ">>> venv created at $(TEST_DIR)/.venv"

test-pypi: $(TEST_DIR)/.venv dev  ## 通过代理安装 Python 包（端到端测试）
	@echo ""
	@echo "=== pip install requests through proxy ==="
	cd $(TEST_DIR) && uv pip install requests \
		--index-url http://localhost:$(PORT)/pypi/simple/ \
		--python .venv/bin/python
	@echo ""
	@echo "=== verify ==="
	$(TEST_DIR)/.venv/bin/python -c "import requests; print('requests', requests.__version__, '✓')"
	@echo ""
	@echo "=== pip install flask (multi-dep) ==="
	cd $(TEST_DIR) && uv pip install flask \
		--index-url http://localhost:$(PORT)/pypi/simple/ \
		--python .venv/bin/python
	$(TEST_DIR)/.venv/bin/python -c "import flask; print('flask', flask.__version__, '✓')"

test-apt: dev                   ## 通过代理获取 APT 元数据（端到端测试）
	@echo ""
	@echo "=== APT InRelease ==="
	@curl -sf -o /dev/null -w "HTTP %{http_code}, Size: %{size_download} bytes\n" \
		http://localhost:$(PORT)/apt/ubuntu/dists/jammy/InRelease \
		|| echo "FAIL (may need network)"
	@echo "=== APT Packages.gz ==="
	@curl -sf -o /dev/null -w "HTTP %{http_code}, Size: %{size_download} bytes\n" \
		http://localhost:$(PORT)/apt/ubuntu/dists/jammy/main/binary-amd64/Packages.gz \
		|| echo "FAIL (may need network)"
	@echo "=== APT cached files ==="
	@find data/cache/apt -type f 2>/dev/null | head -5 || echo "(no cache yet)"

# ─── Per-ecosystem Docker E2E ─────────────────
# Each target builds a tiny ecosystem-specific image whose RUN steps
# exercise the real client (pip / npm / mvn / etc.) against the running
# Depsilo. The Dockerfiles live under testground/docker-<eco>/.
#
# Layout note: each target depends on `dev` (background Depsilo).
# HOST_IP is the docker0 gateway so containers can reach the host.

DEPSILO_URL := http://$(HOST_IP):$(PORT)
DOCKER_BUILD_ARGS := --no-cache --progress=plain

test-docker-pypi: dev           ## E2E: pip install requests through proxy
	@echo "=== [pypi] pip install requests ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg PIP_INDEX_URL=$(DEPSILO_URL)/pypi/simple/ \
		--build-arg PIP_TRUSTED_HOST=$(HOST_IP) \
		-t depsilo-test-pypi $(TEST_DIR)/docker-pypi

test-docker-apt: dev            ## E2E: apt install curl/wget/jq through proxy
	@echo "=== [apt] apt install ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg APT_MIRROR=$(DEPSILO_URL)/apt \
		-t depsilo-test-apt $(TEST_DIR)/docker-apt

test-docker-npm: dev            ## E2E: npm install lodash through proxy
	@echo "=== [npm] npm install lodash ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg NPM_REGISTRY=$(DEPSILO_URL)/npm/ \
		-t depsilo-test-npm $(TEST_DIR)/docker-npm

test-docker-go: dev             ## E2E: go get golang.org/x/text through proxy
	@echo "=== [go] go get x/text ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg GOPROXY=$(DEPSILO_URL)/go \
		-t depsilo-test-go $(TEST_DIR)/docker-go

test-docker-cargo: dev          ## E2E: cargo fetch serde through proxy
	@echo "=== [cargo] cargo fetch serde ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg CARGO_REGISTRY=$(DEPSILO_URL)/crates/ \
		-t depsilo-test-cargo $(TEST_DIR)/docker-cargo

test-docker-maven: dev          ## E2E: mvn fetch guava through proxy
	@echo "=== [maven] mvn dependency:get guava ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg MAVEN_MIRROR=$(DEPSILO_URL)/maven \
		-t depsilo-test-maven $(TEST_DIR)/docker-maven

test-docker-rubygems: dev       ## E2E: gem install rake through proxy
	@echo "=== [rubygems] gem install rake ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg RUBYGEMS_MIRROR=$(DEPSILO_URL)/rubygems \
		-t depsilo-test-rubygems $(TEST_DIR)/docker-rubygems

test-docker-composer: dev       ## E2E: composer require monolog through proxy
	@echo "=== [composer] composer require monolog ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg COMPOSER_MIRROR=$(DEPSILO_URL)/composer \
		-t depsilo-test-composer $(TEST_DIR)/docker-composer

test-docker-nuget: dev          ## E2E: dotnet add Newtonsoft.Json through proxy
	@echo "=== [nuget] dotnet add Newtonsoft.Json ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg NUGET_INDEX=$(DEPSILO_URL)/nuget/v3/index.json \
		-t depsilo-test-nuget $(TEST_DIR)/docker-nuget

test-docker-conda: dev          ## E2E: conda install requests through proxy
	@echo "=== [conda] conda install requests ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg CONDA_CHANNEL=$(DEPSILO_URL)/conda \
		-t depsilo-test-conda $(TEST_DIR)/docker-conda

test-docker-cran: dev           ## E2E: R install.packages('jsonlite') through proxy
	@echo "=== [cran] R install.packages('jsonlite') ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg CRAN_MIRROR=$(DEPSILO_URL)/cran \
		-t depsilo-test-cran $(TEST_DIR)/docker-cran

test-docker-helm: dev           ## E2E: helm repo add + index.yaml fetch through proxy
	@echo "=== [helm] helm repo add + update ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg HELM_REPO_URL=$(DEPSILO_URL)/helm \
		-t depsilo-test-helm $(TEST_DIR)/docker-helm

test-docker-huggingface: dev    ## E2E: huggingface-cli download bert-tiny through proxy
	@echo "=== [huggingface] huggingface-cli download prajjwal1/bert-tiny ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg HF_ENDPOINT=$(DEPSILO_URL)/huggingface \
		-t depsilo-test-huggingface $(TEST_DIR)/docker-huggingface

test-docker-docker: dev         ## E2E: docker pull alpine through proxy (dind, opt-in)
	@echo "=== [docker] docker pull alpine (dind) ==="
	docker build $(DOCKER_BUILD_ARGS) \
		--build-arg DOCKER_REGISTRY_HOST=$(HOST_IP):$(PORT) \
		-t depsilo-test-docker $(TEST_DIR)/docker-docker
	docker run --rm --privileged depsilo-test-docker

# Thirteen ecosystems by default. Docker registry is opt-in (requires dind) — run
# `make test-docker-docker` separately when needed.
TEST_DOCKER_ALL_ECOS    := pypi apt npm go cargo maven rubygems composer nuget conda cran helm huggingface
TEST_DOCKER_ALL_TARGETS := $(addprefix test-docker-,$(TEST_DOCKER_ALL_ECOS))

# Listed as plain prerequisites (not a $(MAKE) loop) so that GNU Make's
# "phony-target-runs-once-per-invocation" guarantee lets `dev` build the
# server exactly once, even though every test-docker-<eco> declares dev
# as its own prerequisite. Behaviour: fail-fast (first failure aborts);
# add `-k` (`make -k test-docker-all`) to keep going through failures.
test-docker-all: $(TEST_DOCKER_ALL_TARGETS)  ## E2E: all 13 non-docker ecosystems (fail-fast; -k to keep-going)
	@echo ""
	@echo ">>> ALL 13 ECOSYSTEMS PASSED"

test-docker: test-docker-all    ## Alias for test-docker-all

test-e2e: test-docker-all       ## Alias: end-to-end test all 13 non-docker ecosystems

test-clean:                     ## 清理测试环境（venv + Docker e2e images）
	rm -rf $(TEST_DIR)/.venv
	-for eco in $(TEST_DOCKER_ALL_ECOS) docker; do \
		docker rmi depsilo-test-$$eco 2>/dev/null || true; \
	done
	@echo ">>> test env removed (13 ecos + docker registry)"

# ─── Docker ───────────────────────────────────
DOCKER_IMAGE := depsilo/depsilo
DOCKER_TAG   := local
DOCKER_NAME  := depsilo-local

docker-build:                   ## 本地构建 Docker 镜像
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .
	@echo ">>> built $(DOCKER_IMAGE):$(DOCKER_TAG)"
	@docker images $(DOCKER_IMAGE):$(DOCKER_TAG) --format "    Size: {{.Size}}"

docker-run: docker-stop         ## 构建并运行本地容器
	@mkdir -p data
	@test -f config.toml || cp config.example.toml config.toml
	docker run -d \
		--name $(DOCKER_NAME) \
		-p $(PORT):23333 \
		-v $(PWD)/data:/app/data \
		-v $(PWD)/config.toml:/app/config.toml:ro \
		-e DEPSILO_CONFIG=/app/config.toml \
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
			echo ">>> FAILED — check: docker logs $(DOCKER_NAME)"; \
	fi

docker-stop:                    ## 停止并删除本地容器
	@docker rm -f $(DOCKER_NAME) 2>/dev/null || true

docker-logs:                    ## 查看容器日志
	docker logs -f $(DOCKER_NAME)

docker-shell:                   ## 进入容器 shell
	docker exec -it $(DOCKER_NAME) sh

docker-status:                  ## 查看容器状态
	@docker ps -f name=$(DOCKER_NAME) --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}\t{{.Size}}"
	@echo ""
	@curl -sf http://localhost:$(PORT)/health 2>/dev/null | python3 -m json.tool 2>/dev/null || echo "health: unreachable"

docker-compose-up:              ## 使用 docker-compose 构建并启动
	docker-compose up -d --build
	@echo ">>> running at http://localhost:$(PORT)"

docker-compose-down:            ## 停止 docker-compose 服务
	docker-compose down

docker-test: docker-build docker-run  ## 构建镜像 + 启动 + 跑冒烟测试
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

proxy-env:                      ## 打印可用的代理配置（推荐使用 depsilo activate）
	@echo "# ─── Depsilo Proxy Configuration ───"
	@echo "# Better: eval \"$$(./$(BIN) activate)\""
	@echo ""
	@echo "# pip (Python):"
	@echo "  --build-arg PIP_INDEX_URL=http://$(HOST_IP):$(PORT)/pypi/simple/"
	@echo "  --build-arg PIP_TRUSTED_HOST=$(HOST_IP)"
	@echo ""
	@echo "# npm (Node.js):"
	@echo "  --build-arg NPM_CONFIG_REGISTRY=http://$(HOST_IP):$(PORT)/npm/"
	@echo ""
	@echo "# Go modules:"
	@echo "  --build-arg GOPROXY=http://$(HOST_IP):$(PORT)/go,direct"
	@echo ""
	@echo "# Maven:"
	@echo '  --build-arg MAVEN_OPTS="-Dmaven.repo.remote=http://$(HOST_IP):$(PORT)/maven/"'
	@echo ""
	@echo "# Composer (PHP):"
	@echo "  --build-arg COMPOSER_MIRROR=http://$(HOST_IP):$(PORT)/composer/"
	@echo ""
	@echo "# Full example:"
	@echo "  docker build \\"
	@echo "    --build-arg PIP_INDEX_URL=http://$(HOST_IP):$(PORT)/pypi/simple/ \\"
	@echo "    --build-arg PIP_TRUSTED_HOST=$(HOST_IP) \\"
	@echo "    -t myapp ."

# ─── 发布 ─────────────────────────────────────
release-dry-run: frontend        ## 本地模拟 goreleaser 发布（不推送）
	goreleaser release --snapshot --clean --skip=publish

release-check: frontend          ## 检查 goreleaser 配置是否正确
	goreleaser check

# ─── 清理 ─────────────────────────────────────
clean: stop docker-stop         ## 清理所有构建产物、容器和缓存数据
	rm -rf bin/ data/ .dev.log $(PID_FILE)
	@echo ">>> clean done"

lint: lint-i18n                 ## 代码检查 (go vet + i18n audit)
	go vet ./...

lint-i18n:                      ## 检查 zh.ts/en.ts 与 t() 调用是否一致
	@python3 scripts/i18n-audit.py

# ─── 帮助 ─────────────────────────────────────
help:                           ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
