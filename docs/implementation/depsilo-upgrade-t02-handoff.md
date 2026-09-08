# 改造交接记录：T02 官网与默认文档

候选基线：主仓库 `9b1714254ad5e06aeb5cfdd7ea7b60873b6cc4d8`；官网仓库 `f46521c4acc8f7312fa95d6e22da36a5c4fafe38`  
记录日期：2026-09-08

状态：**IMPLEMENTED（独立官网仓库，待维护者发布）**。

官网源码位于 `/data/codelab/depsilo_workspace/depsilo-landingpage`，不是主仓库子目录。改造内容：

- `src/data/content.ts` 与稳定 Docker/CLI 入口改为 v0.9.2；Docker 新安装依赖状态卷和首次设置生成的绝对路径。
- 中英文首页的安全响应示例改为已支持的 `MALICIOUS_BLOCKED`，移除把 `QUARANTINED` 当作当前演示的表述；最小发布年龄明确为安全停用。
- 中英文供应链文档删除可直接启用最小发布时间的 v0.9.2 示例，改为启动拒绝与来源证明边界；恶意列表覆盖范围与当前确认身份一致。
- 中英文快速开始、客户端、Agent/MCP、部署和文档首页同步稳定版本标识；部署页保留 v0.9.0 仅作为旧 Compose 布局升级语境。
- 官网仓库 README 也同步安全停用说明和未来制品仓库方向。

验证（官网仓库）：

```text
npm run check   # 0 errors, 0 warnings, 0 hints
npm run build   # 16 pages built successfully
git diff --check
```

未执行线上部署、DNS/CDN 写入或发布；官网是否合并、发布以及发布时间由维护者决定。主仓库的 T02 交付不能替代官网线上状态核验。

建议官网 commit 标题：`docs: align website with v0.9.2 boundaries`
