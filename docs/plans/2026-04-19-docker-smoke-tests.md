# Docker Smoke Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a Docker Compose-based E2E smoke test that verifies pip, npm, go, and apt work through the Depsilo proxy.

**Architecture:** Two containers — depsilo (built from project Dockerfile) and e2e-runner (ubuntu + pip/npm/go/apt) — connected via Docker Compose networking. Runner waits for depsilo health check, then executes install commands through the proxy.

**Tech Stack:** Docker, Docker Compose, Bash

---

## File Structure

| Action | File | Responsibility |
| ------ | ---- | -------------- |
| Create | `tests/e2e/config.e2e.toml` | Depsilo config with real upstreams, auth disabled |
| Create | `tests/e2e/runner/Dockerfile` | E2E runner image with pip, npm, go, apt |
| Create | `tests/e2e/runner/run-tests.sh` | Test script: wait → install → verify |
| Create | `tests/e2e/docker-compose.e2e.yml` | Orchestrate depsilo + e2e-runner |
| Modify | `Makefile` | Add `test-e2e` target |

---

### Task 1: Create E2E config

**Files:**
- Create: `tests/e2e/config.e2e.toml`

- [ ] **Step 1: Create the config file**

Create `tests/e2e/config.e2e.toml`:

```toml
[server]
host = "0.0.0.0"
port = 23333

[database]
driver = "sqlite"
dsn = "/app/data/depsilo.db"

[storage]
type = "local"
path = "/app/data/cache"

[cache]
max_size_gb = 5
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90

[auth]
enabled = false

[[pypi.upstreams]]
name = "tuna"
url = "https://pypi.tuna.tsinghua.edu.cn"
priority = 1

[[pypi.upstreams]]
name = "official"
url = "https://pypi.org"
priority = 2

[[npm.upstreams]]
name = "npmmirror"
url = "https://registry.npmmirror.com"
priority = 1

[[npm.upstreams]]
name = "official"
url = "https://registry.npmjs.org"
priority = 2

[[go.upstreams]]
name = "goproxy-cn"
url = "https://goproxy.cn"
priority = 1

[[go.upstreams]]
name = "official"
url = "https://proxy.golang.org"
priority = 2

[[apt.upstreams]]
name = "tuna"
url = "https://mirrors.tuna.tsinghua.edu.cn"
priority = 1

[[apt.upstreams]]
name = "official"
url = "http://archive.ubuntu.com"
priority = 2
```

- [ ] **Step 2: Commit**

```bash
git add tests/e2e/config.e2e.toml
git commit -m "test(e2e): add depsilo config for Docker smoke tests"
```

---

### Task 2: Create runner Dockerfile

**Files:**
- Create: `tests/e2e/runner/Dockerfile`

- [ ] **Step 1: Create the Dockerfile**

Create `tests/e2e/runner/Dockerfile`:

```dockerfile
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install base tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip \
    nodejs npm \
    golang-go \
    curl wget ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Go needs a writable GOPATH
ENV GOPATH=/tmp/gopath
ENV PATH=$GOPATH/bin:$PATH

WORKDIR /tests
COPY run-tests.sh /tests/run-tests.sh
RUN chmod +x /tests/run-tests.sh

ENTRYPOINT ["/tests/run-tests.sh"]
```

- [ ] **Step 2: Commit**

```bash
git add tests/e2e/runner/Dockerfile
git commit -m "test(e2e): add runner Dockerfile with pip/npm/go/apt"
```

---

### Task 3: Create test script

**Files:**
- Create: `tests/e2e/runner/run-tests.sh`

- [ ] **Step 1: Create the test script**

Create `tests/e2e/runner/run-tests.sh`:

```bash
#!/bin/bash
set -euo pipefail

DEPSILO_URL="${DEPSILO_URL:-http://depsilo:23333}"
PASSED=0
FAILED=0
RESULTS=""

# ─── Helpers ──────────────────────────────────
pass() {
  PASSED=$((PASSED + 1))
  RESULTS="${RESULTS}\n  ✅ $1"
  echo "✅ PASS: $1"
}

fail() {
  FAILED=$((FAILED + 1))
  RESULTS="${RESULTS}\n  ❌ $1: $2"
  echo "❌ FAIL: $1 — $2"
}

# ─── Wait for Depsilo ─────────────────────────
echo ""
echo "════════════════════════════════════════════"
echo "  Depsilo E2E Smoke Tests"
echo "════════════════════════════════════════════"
echo ""
echo "Waiting for depsilo at ${DEPSILO_URL}..."

for i in $(seq 1 30); do
  if curl -sf "${DEPSILO_URL}/health" > /dev/null 2>&1; then
    echo "Depsilo is ready."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "Depsilo did not become ready in 30s. Aborting."
    exit 1
  fi
  sleep 1
done

echo ""

# ─── Test 1: pip ──────────────────────────────
echo "── Test 1: pip install requests ──"
if pip3 install --quiet --break-system-packages \
    --index-url "${DEPSILO_URL}/pypi/simple/" \
    --trusted-host depsilo \
    requests 2>&1; then
  if python3 -c "import requests; print('requests', requests.__version__)" 2>&1; then
    pass "pip install requests"
  else
    fail "pip" "install succeeded but import failed"
  fi
else
  fail "pip" "pip install requests failed"
fi

echo ""

# ─── Test 2: npm ──────────────────────────────
echo "── Test 2: npm install lodash ──"
WORKDIR_NPM=$(mktemp -d)
cd "$WORKDIR_NPM"
npm init -y --silent > /dev/null 2>&1
if npm install --registry "${DEPSILO_URL}/npm/" lodash 2>&1; then
  if node -e "const _ = require('lodash'); console.log('lodash', _.VERSION)" 2>&1; then
    pass "npm install lodash"
  else
    fail "npm" "install succeeded but require failed"
  fi
else
  fail "npm" "npm install lodash failed"
fi
cd /tests

echo ""

# ─── Test 3: go ───────────────────────────────
echo "── Test 3: go get golang.org/x/text ──"
export GOPROXY="${DEPSILO_URL}/go,direct"
export GONOSUMDB="*"
export GOFLAGS="-insecure"
WORKDIR_GO=$(mktemp -d)
cd "$WORKDIR_GO"
go mod init e2etest > /dev/null 2>&1
if go get golang.org/x/text@latest 2>&1; then
  pass "go get golang.org/x/text"
else
  fail "go" "go get golang.org/x/text failed"
fi
cd /tests

echo ""

# ─── Test 4: apt ──────────────────────────────
echo "── Test 4: apt-get install jq ──"
# Configure apt to use depsilo proxy
cat > /etc/apt/sources.list << EOF
deb ${DEPSILO_URL}/apt/ubuntu jammy main restricted universe
deb ${DEPSILO_URL}/apt/ubuntu jammy-updates main restricted universe
deb ${DEPSILO_URL}/apt/ubuntu jammy-security main restricted universe
EOF

# Disable GPG verification for proxy (test environment only)
echo 'Acquire::AllowInsecureRepositories "true";' > /etc/apt/apt.conf.d/99insecure
echo 'APT::Get::AllowUnauthenticated "true";' >> /etc/apt/apt.conf.d/99insecure

if apt-get update -o Acquire::https::Verify-Peer=false 2>&1 | tail -3; then
  if apt-get install -y --allow-unauthenticated jq 2>&1 | tail -3; then
    if jq --version 2>&1; then
      pass "apt-get install jq"
    else
      fail "apt" "install succeeded but jq not found"
    fi
  else
    fail "apt" "apt-get install jq failed"
  fi
else
  fail "apt" "apt-get update failed"
fi

echo ""

# ─── Summary ──────────────────────────────────
echo "════════════════════════════════════════════"
echo "  Results: ${PASSED} passed, ${FAILED} failed"
echo -e "${RESULTS}"
echo "════════════════════════════════════════════"

if [ "$FAILED" -gt 0 ]; then
  exit 1
fi

exit 0
```

- [ ] **Step 2: Commit**

```bash
git add tests/e2e/runner/run-tests.sh
git commit -m "test(e2e): add smoke test script for pip/npm/go/apt"
```

---

### Task 4: Create Docker Compose file

**Files:**
- Create: `tests/e2e/docker-compose.e2e.yml`

- [ ] **Step 1: Create the compose file**

Create `tests/e2e/docker-compose.e2e.yml`:

```yaml
services:
  depsilo:
    build:
      context: ../..
      dockerfile: Dockerfile
    volumes:
      - ./config.e2e.toml:/app/config.toml:ro
    environment:
      - DEPSILO_CONFIG=/app/config.toml
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:23333/health"]
      interval: 2s
      timeout: 5s
      retries: 15

  e2e-runner:
    build:
      context: ./runner
    depends_on:
      depsilo:
        condition: service_healthy
    environment:
      - DEPSILO_URL=http://depsilo:23333
```

- [ ] **Step 2: Commit**

```bash
git add tests/e2e/docker-compose.e2e.yml
git commit -m "test(e2e): add Docker Compose for E2E smoke tests"
```

---

### Task 5: Add Makefile target and run

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add test-e2e target to Makefile**

Add the following after the `test-clean` target (around line 131), before the Docker section:

```makefile
test-e2e:                       ## Docker 冒烟测试（pip/npm/go/apt 端到端）
	docker compose -f tests/e2e/docker-compose.e2e.yml up --build --abort-on-container-exit --exit-code-from e2e-runner
	docker compose -f tests/e2e/docker-compose.e2e.yml down -v
```

Also add `test-e2e` to the `.PHONY` list on line 1.

- [ ] **Step 2: Run the E2E test**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && make test-e2e`

Expected: Both containers build, depsilo starts, runner executes 4 tests, prints summary with pass/fail for each. If all 4 pass, exit code 0. If any fail (likely apt due to GPG), note the failure and adjust the test script if needed.

- [ ] **Step 3: Fix any issues found**

Common issues to watch for:
- **apt GPG**: The test disables signature verification. If apt-get update still fails, check if the `apt/ubuntu` route in depsilo correctly proxies to the upstream.
- **npm SSL**: If npmmirror has SSL issues, the test will fall back to registry.npmjs.org via depsilo.
- **go sumdb**: `GONOSUMDB=*` disables checksum verification which might fail through proxy.
- **pip trusted-host**: Already set via `--trusted-host depsilo`.

If a test fails due to network/upstream issues (not depsilo bugs), make the script more resilient but don't hide real failures.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "test(e2e): add make test-e2e target for Docker smoke tests"
```

---

### Task 6: Final verification

- [ ] **Step 1: Run the full E2E suite from clean state**

Run: `cd /home/SENSETIME/ningxiangdong1/codelab/depsilo_workspace/depsilo && make test-e2e`

Expected output (approximate):
```
════════════════════════════════════════════
  Depsilo E2E Smoke Tests
════════════════════════════════════════════

Waiting for depsilo...
Depsilo is ready.

── Test 1: pip install requests ──
✅ PASS: pip install requests

── Test 2: npm install lodash ──
✅ PASS: npm install lodash

── Test 3: go get golang.org/x/text ──
✅ PASS: go get golang.org/x/text

── Test 4: apt-get install jq ──
✅ PASS: apt-get install jq

════════════════════════════════════════════
  Results: 4 passed, 0 failed
════════════════════════════════════════════
```

- [ ] **Step 2: Clean up**

Run: `docker compose -f tests/e2e/docker-compose.e2e.yml down -v --rmi local`

- [ ] **Step 3: Verify all files exist**

Run: `ls -la tests/e2e/ tests/e2e/runner/`

Expected:
```
tests/e2e/config.e2e.toml
tests/e2e/docker-compose.e2e.yml
tests/e2e/runner/Dockerfile
tests/e2e/runner/run-tests.sh
```
