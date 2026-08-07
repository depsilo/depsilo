---
version: 1
slug: "web-src-admin-components-mainlayout-tsx"
primary_target: "web/src/admin/components/MainLayout.tsx"
related_targets: ["web/src/admin/pages/Dashboard.tsx","web/src/admin/routes.ts"]
---

# Admin surface brief

- **Scope and mode:** Depsilo authenticated Admin, Operate mode. Individual developers and small-team operators arrive to confirm current health, investigate history, and configure upstream sources without learning an enterprise artifact platform.
- **Job and outcome:** The first viewport answers whether the request path is healthy, why it is degraded, and what needs action. Five task workspaces replace seventeen equal-weight destinations: Overview, History, Sources & Cache, Supply-chain Governance, and System.
- **Direction:** **Dependency Flowline / 依赖流线控制台**. The overview makes the real request path—client ingress → Depsilo cache → upstreams—the dominant composition, with one continuous signal axis and a compact inset attention rail beside it. The page heading and content share the full default fluid canvas, capped at 1840px, and one left baseline. The pure-white light canvas is separated from a dedicated mint-porcelain workspace rail (`#F3F8F5`, hover `#EAF3EE`); dark mode keeps its existing rail appearance. Primary instruments remain open while supporting rails use surface contrast. Approved north-star: `.impeccable/mocks/admin-flowline.png`. Recent activity and one trend follow; full reports move into their expert workspaces.
- **Constraints:** Preserve Instrument tokens, pure-white light canvas, matte dark theme, bilingual copy, keyboard and WCAG behavior, existing APIs and permissions, legacy URL compatibility, and the database-vs-config authority distinction for upstreams. No gradients, glass, decorative cards, invented state, or backend rewrite.
- **Responsive behavior:** Desktop uses one 232px task sidebar and the default fluid Admin canvas capped at 1840px. Wide Dashboard rows use a fluid primary column plus an approximately 380px supporting rail. Multi-page workspaces default open and can be folded independently. Narrow screens retain the drawer with only the active workspace open by default, stack the flowline vertically, show status plus the first actionable item in the first viewport, and replace wide history tables with event rows.

## Implementation fidelity inventory

| Ingredient | Medium | Commitment |
| --- | --- | --- |
| Five-workspace navigation | Semantic links + independent disclosure buttons | Desktop workspaces default open; the drawer defaults to the active workspace; the light desktop rail uses `#F3F8F5` with `#EAF3EE` hover while dark mode remains unchanged; child destinations use indentation only, without connector lines or bullet dots; old routes remain addressable |
| Unified page heading | Shared React component | Title, description, refresh state, page actions, and Dashboard content share the full default fluid canvas and left baseline; desktop Overview does not repeat its name in the utility breadcrumb |
| Utility preferences | Existing language and appearance controls | Two adjacent flat buttons with quiet spacing; no tinted or bordered outer card |
| Dependency flowline | Semantic HTML + CSS | One continuous animated axis connects three existing-data stages; no redundant enclosing rules or rounded card silhouette; horizontal desktop, compact vertical rows on mobile |
| Attention rail | Existing query data + semantic list | Approximately 380px on wide screens; inset supporting surface, failures first, explicit destination actions, honest loading/stale states |
| Panel headers and runtime status | Semantic heading/status regions | Keep one concise visual row; generic helper copy is screen-reader context when needed, and runtime state is an inline dot plus text rather than a chip |
| 24-hour metrics | Shared Metric or semantic data blocks | Left aligned, 28–32px tabular values, one bare data rail with only the divisions needed to distinguish values rather than a card |
| Trend | Existing Recharts implementation | One open section without an enclosing keyline or rounded card silhouette; one chart only; metrics are unboxed text tabs; the time range is the sole segmented control, with an approximately 41px outer frame around 40px buttons; successful empty state collapses without a large void |
| Recent activity | Existing recent-download data | Inset supporting rail with at most three compact rows showing outcome, package, time, and history link |
| Quiet separators | Existing borders and semantic structure | Keep the top-bar divider, request axis, list-row and metadata separators, and necessary data divisions; remove repeated full-width rules that restate section boundaries |
| Themes and motion | Existing tokens + CSS | Green only for health/selection/action; the light rail's restrained mint tint is structural rather than decorative; dark mode remains unchanged; one restrained live-flow cue with reduced-motion fallback |
