.PHONY: build run dev stop test test-pypi test-apt test-clean clean lint frontend help

# ─── 变量 ─────────────────────────────────────
APP        := depsilo
BIN        := bin/$(APP)
CONFIG     := config.toml
PID_FILE   := .server.pid
PORT       := 23333
TEST_DIR   := testground

# ─── 构建 ─────────────────────────────────────
frontend:                       ## 构建前端
	@export NVM_DIR="$$HOME/.nvm" && . "$$NVM_DIR/nvm.sh" && cd web && npm run build

build: frontend                 ## 构建前端 + 编译后端
	go build -o $(BIN) ./cmd/server

# ─── 运行 ─────────────────────────────────────
run: build                      ## 编译并前台运行
	DEPSLIO_CONFIG=$(CONFIG) ./$(BIN)

dev: build stop                 ## 编译并后台运行（dev 模式）
	@echo ">>> starting $(APP) on :$(PORT) ..."
	@DEPSLIO_CONFIG=$(CONFIG) ./$(BIN) > .dev.log 2>&1 & echo $$! > $(PID_FILE)
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
test:                           ## 运行 Go 单元测试
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

test-clean:                     ## 清理测试环境
	rm -rf $(TEST_DIR)/.venv
	@echo ">>> test venv removed"

# ─── 清理 ─────────────────────────────────────
clean: stop                     ## 清理所有构建产物和缓存数据
	rm -rf bin/ data/ .dev.log $(PID_FILE)
	@echo ">>> clean done"

lint:                           ## 代码检查
	go vet ./...

# ─── 帮助 ─────────────────────────────────────
help:                           ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
