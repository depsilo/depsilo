#!/usr/bin/env bash
# 清理测试环境，支持三种模式：
#   ./pip_clean.sh          清空 venv 中所有已安装的第三方包（保留 venv）
#   ./pip_clean.sh --venv   删除整个 venv（下次 install 自动重建）
#   ./pip_clean.sh --all    删除 venv + 服务端缓存数据

set -e
cd "$(dirname "$0")"

VENV=".venv"

case "${1:-}" in
    --venv)
        echo ">>> 删除虚拟环境..."
        rm -rf "$VENV"
        echo ">>> done (下次 pip_install.sh 会自动重建)"
        ;;
    --all)
        echo ">>> 删除虚拟环境..."
        rm -rf "$VENV"
        echo ">>> 删除服务端缓存..."
        rm -rf ../data/cache ../data/repocache.db
        echo ">>> done (需重启服务: make dev)"
        ;;
    *)
        if [ ! -d "$VENV" ]; then
            echo ">>> venv 不存在，无需清理"
            exit 0
        fi
        echo ">>> 卸载所有第三方包..."
        PKGS=$(uv pip freeze --python "$VENV/bin/python" 2>/dev/null | grep -v '^\-e' || true)
        if [ -n "$PKGS" ]; then
            echo "$PKGS" | uv pip uninstall -r - --python "$VENV/bin/python"
            echo ">>> 已清空 $(echo "$PKGS" | wc -l) 个包"
        else
            echo ">>> 没有已安装的包"
        fi
        ;;
esac
