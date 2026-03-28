# testground — 本地代理测试目录

此目录用于通过 RepoCache 代理模拟安装依赖包，支持反复测试。

## 快速开始

```bash
# 启动服务（项目根目录）
make dev

# 安装包（首次自动创建 venv）
./pip_install.sh requests flask numpy

# 验证通过后，清空包再测
./pip_clean.sh
./pip_install.sh httpx pandas
```

## 脚本说明

### pip_install.sh — 安装包

```bash
./pip_install.sh [包名...]       # 通过代理安装，默认装 requests
./pip_install.sh requests flask  # 安装多个包
./pip_install.sh "numpy>=1.24"   # 支持版本约束
```

- 自动创建 `.venv`（如不存在）
- 代理地址默认 `http://localhost:23333`，可通过 `REPOCACHE_PORT` 环境变量修改

### pip_clean.sh — 清理环境

三种模式，适合不同测试场景：

```bash
./pip_clean.sh          # 卸载所有第三方包，保留 venv（最快，适合反复测试）
./pip_clean.sh --venv   # 删除整个 venv（下次 install 自动重建）
./pip_clean.sh --all    # 删除 venv + 服务端缓存数据（测试冷启动回源，需重启服务）
```

## 典型测试流程

### 测试缓存命中

```bash
./pip_install.sh requests    # 第一次：MISS，从上游下载
./pip_clean.sh               # 清空本地包
./pip_install.sh requests    # 第二次：HIT，从缓存读取（明显更快）
```

### 测试冷启动回源

```bash
./pip_clean.sh --all         # 清除所有缓存
make dev                     # 重启服务
./pip_install.sh flask       # 全部从上游重新下载
```

## Makefile 集成

也可以直接在项目根目录使用：

```bash
make test-pypi     # 一键测试（自动启动服务 + 安装 requests/flask）
make test-clean    # 清理测试 venv
```
