# Feature Backlog — 2026-05-20

> 走查现有实现后整理的候选功能清单。来源：CLAUDE.md §11 (2026-05-18 review) + 实际代码核验 + brainstorming。
> 按"商业价值 × 实现成本"排序，每项注明动机与落地切入点。后续挑中的条目会单独建 spec/plan。

---

## 一、现状回顾（截至 2026-05-20）

### 已落地能力

- **13 个生态代理**：pip / apt / npm / go / cargo / maven / gem / composer / nuget / conda / cran / helm + Docker Registry
- **基础设施**：singleflight + 流式 + stale-while-revalidate；priority/latency selector + 健康检查 + 单上游 HTTP 代理；local + S3 存储；SQLite/PostgreSQL（时间戳已统一 UTC）
- **Pro 模块**：`security` (OSV 扫描) / `sbom` (SPDX) / `audit` / `rules` (allow/deny) / `license`
- **前端**：12 个 admin 页 + 5 个 portal 页，含 Bandwidth Report、LiveStream、Monitor、Projects
- **CLI**：`status / daemon / manage / version`
- **测试**：13 个独立生态 Docker E2E (`make test-docker-<eco>`)，Docker Registry 单独 opt-in
- **i18n**：审计脚本 + Makefile lint 集成，451 key 双语对齐零漂移

### 已知小漏洞（2026-05-21 全部已修复）

- ~~CLAUDE.md §10 说 SBOM 是 CycloneDX，但 `internal/sbom` 实际生成 SPDX —— 文档需校正~~
  → 复核后发现 `internal/sbom/generator.go` 同时实现 `GenerateSPDX` + `GenerateCycloneDX`，错的是 `docs/specs/2026-04-28-functional-analysis.md` 第 103 行（仅写 "CycloneDX 格式"）。已改为 "SPDX 2.3 + CycloneDX 1.5（双格式 JSON，按 `?format=` 切换）"。
- ~~i18n audit 不校验 placeholder（`{count}` 在中英文是否对齐）~~
  → `scripts/i18n-audit.py` 增加第 5 类检查：`PLACEHOLDER MISMATCH`。扩展 `parse_locale` 同时回收 key→value，对 zh∩en 共有 key 比对 `{{var}}` 集合，差异时报告并退出非零。烟雾测试已通过（人为去掉一个 `{{count}}` → 立即报警，恢复后 clean）。
- ~~CONTRIBUTING.md 缺 "push 前跑 `make lint`" 提示~~
  → 新增 `## Before pushing` 段落，明确推送前跑 `make lint` + `make test-unit`，并解释 placeholder drift 这种"manual review 难发现"的场景为什么必须靠 lint 拦住。

---

## 二、新功能候选（按优先级分组）

### Tier S：差异化护城河（独家、复用现有架构）

#### S1. HuggingFace 模型镜像
- **痛点**：国内访问 hf.co 极慢，单模型 GB 级。轻量竞品空白。
- **复用**：现有 stream + cache + URL 重写框架；HuggingFace 用类 git-lfs 协议 + S3 下载链接。
- **切入点**：新增 `internal/adapter/huggingface/`，重写 LFS resolver 响应中的 download URL。
- **预估**：1 周，含 Docker E2E。
- **风险**：HF 协议变化频繁；模型动辄 50GB+，要验 storage 行为。

#### S2. OCI Artifact 通用代理（ORAS）
- **价值**：Helm chart、AI model、policy bundle、SBOM 都走 OCI。一次实现拿下长尾场景。
- **复用**：Docker Registry adapter 已实现 OCI 协议核心；扩展 manifest 类型即可。
- **切入点**：在 `internal/adapter/docker/` 基础上放宽 mediaType 白名单。

#### S3. Air-gapped 离线缓存导出 / 导入
- **痛点**：政企内网客户刚需，Nexus 做得很重。
- **形式**：`depsilo export-cache --since 7d --ecosystems pypi,npm` 打 tar；`depsilo import-cache file.tar`。
- **复用**：storage interface 已抽象，CLI 框架已就位。
- **切入点**：`cmd/depsilo` 新增 subcommand；元数据 manifest + 流式 tar writer。

#### S4. Peer 节点缓存同步
- **价值**：中心-边缘拓扑、多机房 HA、提前预热。Nexus Cluster 是付费版。
- **形式**：节点 A 周期 pull `B/api/v1/cache/sync?since=...` 取增量元数据，按需 fetch blob。
- **切入点**：新 `internal/sync/` 模块，复用 cache.Manager.Put。

---

### Tier A：商业转化（功能已做、就差临门一脚）

#### A1. `/pricing` 静态定价页
- **背景**：2026-05-18 review §3.5 明确指出。Pro 功能存在但没有公开入口 = 等于没做。
- **形式**：portal 加一个路由，列出 Free/Pro/Enterprise 三档对比表 + CTA 按钮。
- **预估**：半天。

#### A2. 14 天 Trial Key 自助激活
- **价值**：决策摩擦降到零。不留邮箱、点击即试。
- **切入点**：`license` 模块已有，加一个 "trial mode" 状态（带过期时间，写死单机器指纹）。
- **配套**：portal 上 "Start free trial" 按钮，点击生成本地 trial key。

#### A3. License 自助管理门户
- **背景**：当前 license 只能手动塞 key，没有续费 / 换机 / 升席位 UI。
- **形式**：admin 页加 License tab，展示当前 key、到期、绑定指纹、续费链接。

#### A4. Cost Calculator（"本月省了 ¥X"）
- **价值**：让 CFO 看见价值。bandwidth 数据已有。
- **形式**：admin Dashboard 加一个 metric card，配可编辑的"出口带宽单价"设置项。
- **预估**：半天前端 + 一个 settings 字段。

---

### Tier B：生产部署刚需（P1 review 已标但未做）

#### B1. Webhook 通知（Slack / 钉钉 / 企微 / Lark）
- **触发**：上游全挂、磁盘水位 > 阈值、安全告警命中、license 即将到期。
- **形式**：admin Settings 加 Webhook tab；payload 标准化为 JSON + 模板化文案。
- **切入点**：新 `internal/notify/` 模块，订阅事件总线。

#### B2. 备份 / 恢复 CLI
- **形式**：`depsilo backup --out backup.tar.gz`（仅 db + config，不含缓存）；`depsilo restore backup.tar.gz`。
- **价值**：运维必需。当前丢库就是丢全部审计/规则/账号。

#### B3. HA 集群部署文档
- **背景**：CLAUDE.md §11.6 P1 第二项。
- **要点**：PostgreSQL + S3 + 多实例 + 前置 LB；session 怎么共享、上游健康检查怎么避免重复探测。
- **形式**：docs/deploy-ha.md，配 docker-compose 样例。

#### B4. Helm Chart + Terraform Module
- **价值**：K8s/IaC 用户 5 行接入。
- **产出**：`deploy/helm/` + `deploy/terraform/` 两个独立子目录，独立 CI 发布。

---

### Tier C：扩展现有 Pro 模块（投入小、Pro 加价点）

#### C1. Typosquatting 检测
- **形式**：拦截 `nunpy` / `reuqests` / `lodahs` 等与 Top-N 包近似的包名。
- **算法**：Levenshtein ≤ 2 + 与 top packages 表对比。
- **复用**：现有 packagekey 提包名 + rules engine 拦截。

#### C2. 强制升级规则（version constraint）
- **形式**：rules engine 加一种 `block-version` 类型，如 `log4j < 2.17 → deny`。
- **切入点**：`internal/rules/engine.go` 加一种 matcher。

#### C3. License 合规扫描
- **形式**：SBOM 已生成 PURL，再加 SPDX license 字段，对比项目策略（如禁止 GPL）。
- **价值**：等保 / 出海合规刚需。
- **依赖**：需要可信 license 数据源（OSV 有部分，可考虑 ClearlyDefined）。

#### C4. 合规证据包导出
- **形式**：一键 zip：审计日志 CSV + SBOM + 用户/Token 清单 + Rules 配置快照。
- **场景**：给安全部门做 SOC2 / 等保 / ISO27001 准备材料。
- **Pro 加价点**：纯增值，开源版可禁用。

---

### Tier D：DevOps 集成 / 社区拉新

#### D1. OpenTelemetry tracing
- **形式**：cache → upstream → storage 链路 span。
- **接入**：`go.opentelemetry.io/otel`，环境变量配 OTLP endpoint。

#### D2. Grafana Dashboard + PrometheusRule
- **背景**：`/metrics` 已有但没人提供现成大盘。
- **产出**：`deploy/grafana/dashboard.json` + `deploy/prometheus/rules.yaml`。

#### D3. GitHub Action / GitLab CI 模板仓库
- **形式**：独立 repo `depsilo/setup-depsilo-action`，三行接入 CI 加速。
- **价值**：天然的 README hero example。

#### D4. mDNS / Service Discovery
- **场景**：局域网内开发者 `pip install` 自动找到 Depsilo，无需手动配 index-url。
- **形式**：bonjour/avahi 广播；配合 `depsilo-client` 小工具自动改 pip.conf。

---

### Tier E：长尾生态（按需）

- **Hex.pm**（Erlang/Elixir）
- **Pub.dev**（Dart/Flutter）
- **CocoaPods / Swift Package Manager**
- **GitHub Container Registry**（独立于 docker.io）
- **PyPI Mirror Mode**（完整 bandersnatch 风格离线镜像）

---

## 三、推荐打法（如果只挑三件）

| 顺序 | 功能 | 周期 | 理由 |
|---|---|---|---|
| 1 | **A1+A2 商业化闭环**（pricing + trial） | 1-2 天 | 把已经做完的 Pro 价值变现，零开发存量功能浪费 |
| 2 | **S1 HuggingFace + S3 离线导出** | 1-2 周 | 差异化最强；HuggingFace 是中国市场天然爆点；离线导出戳到 Nexus 痛处 |
| 3 | **B1 Webhook + B2 备份 CLI** | 3-5 天 | 生产可用性补完，让客户敢把 Depsilo 放到关键路径 |

之后再考虑：C 系列（扩展 Pro 模块） → D 系列（社区拉新）。

---

## 四、待决策

- **Wails 桌面版**：CLAUDE.md §11.6 列在"建议推迟"，本次复核维持该判断。
- **License Key 系统重构**：当前足够支撑 Tier A 商业化，先不动。
- **Docker Registry E2E 默认 opt-in**：等用户基数上来再投入做 dind CI。
