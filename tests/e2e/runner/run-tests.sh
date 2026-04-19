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
if pip3 install --quiet \
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
export GOINSECURE="*"
export GONOSUMCHECK="*"
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
