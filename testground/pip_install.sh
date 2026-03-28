#!/usr/bin/env bash
# 通过 RepoCache 代理安装 PyPI 包
# 用法: ./pip_install.sh [包名...]
# 示例: ./pip_install.sh requests flask numpy

set -e
cd "$(dirname "$0")"

PORT=${REPOCACHE_PORT:-23333}
INDEX_URL="http://localhost:${PORT}/pypi/simple/"
VENV=".venv"

# 确保 venv 存在
if [ ! -d "$VENV" ]; then
    echo ">>> 创建虚拟环境..."
    uv venv "$VENV"
fi

# 默认安装 requests
PACKAGES=("${@:-requests}")

echo ">>> 代理地址: $INDEX_URL"
echo ">>> 安装: ${PACKAGES[*]}"
echo ""

uv pip install "${PACKAGES[@]}" \
    --index-url "$INDEX_URL" \
    --python "$VENV/bin/python"

echo ""
echo ">>> 验证:"
for pkg in "${PACKAGES[@]}"; do
    # 去掉版本约束 (e.g. "requests>=2.0" → "requests")
    name="${pkg%%[>=<\!~]*}"
    "$VENV/bin/python" -c "import ${name//-/_}; print('  ${name} OK')" 2>/dev/null \
        || echo "  ${name} (installed, import skipped)"
done
