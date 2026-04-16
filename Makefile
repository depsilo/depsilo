.PHONY: build run run-pro dev stop test test-unit test-integration test-http test-all test-pypi test-apt test-clean clean lint frontend help \
	test-docker-pip test-docker-apt test-docker \
	docker-build docker-run docker-stop docker-logs docker-shell docker-status docker-compose-up docker-compose-down docker-test

# ─── 变量 ─────────────────────────────────────
APP        := depsilo
BIN        := bin/$(APP)
CONFIG     := config.toml
PID_FILE   := .server.pid
PORT       := 23333
TEST_DIR   := testground
HOST_IP    := $(shell ip -4 addr show docker0 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' || echo "172.17.0.1")

# ─── 构建 ─────────────────────────────────────
frontend:                       ## 构建前端
	@export NVM_DIR="$$HOME/.nvm" && . "$$NVM_DIR/nvm.sh" && cd web && npm run build

build: frontend                 ## 构建前端 + 编译后端（服务器模式）
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/server

build-desktop: frontend         ## 构建前端 + 编译桌面版
	go build -tags "desktop,production" -o bin/$(APP)-desktop ./cmd/server

# ─── 运行 ─────────────────────────────────────
run: build                      ## 编译并前台运行
	DEPSILO_CONFIG=$(CONFIG) ./$(BIN)

run-pro: build                  ## 编译并前台运行（开启全部 Pro 功能）
	DEPSILO_DEV_PRO=1 DEPSILO_CONFIG=$(CONFIG) ./$(BIN)

dev: build stop                 ## 编译并后台运行（dev 模式）
	@echo ">>> starting $(APP) on :$(PORT) ..."
	@DEPSILO_CONFIG=$(CONFIG) ./$(BIN) > .dev.log 2>&1 & echo $$! > $(PID_FILE)
	@sleep 2
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

test-docker-pip: dev            ## Docker 环境测试 pip 通过代理安装 opencv
	@echo ""
	@echo "=== Docker pip test (opencv via proxy) ==="
	docker build \
		--no-cache \
		--build-arg PIP_INDEX_URL=http://$(HOST_IP):$(PORT)/pypi/simple/ \
		--build-arg PIP_TRUSTED_HOST=$(HOST_IP) \
		--progress=plain \
		-t depsilo-test-pip \
		$(TEST_DIR)/docker-pip
	@echo ""
	@echo ">>> PASS: opencv installed successfully via proxy"

test-docker-apt: dev            ## Docker 环境测试 apt 通过代理安装包
	@echo ""
	@echo "=== Docker apt test (curl/wget/jq via proxy) ==="
	docker build \
		--no-cache \
		--build-arg APT_MIRROR=http://$(HOST_IP):$(PORT)/apt \
		--progress=plain \
		-t depsilo-test-apt \
		$(TEST_DIR)/docker-apt
	@echo ""
	@echo ">>> PASS: apt packages installed successfully via proxy"

test-docker: test-docker-pip test-docker-apt  ## 运行全部 Docker 代理测试

test-clean:                     ## 清理测试环境
	rm -rf $(TEST_DIR)/.venv
	-docker rmi depsilo-test-pip depsilo-test-apt 2>/dev/null
	@echo ">>> test env removed"

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

proxy-env:                      ## 打印 Docker build 可用的代理参数
	@echo "# ─── Depsilo Proxy for Docker Build ───"
	@echo "# Host IP (docker0): $(HOST_IP)"
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

# ─── 清理 ─────────────────────────────────────
clean: stop docker-stop         ## 清理所有构建产物、容器和缓存数据
	rm -rf bin/ data/ .dev.log $(PID_FILE)
	@echo ">>> clean done"

lint:                           ## 代码检查
	go vet ./...

# ─── 帮助 ─────────────────────────────────────
help:                           ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
