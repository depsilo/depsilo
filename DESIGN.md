# Depsilo Design System

> Status: current implementation reference, updated 2026-07-10. The source of
> truth is `web/src/index.css`, `web/src/components/`, `web/src/portal/`, and
> `web/src/admin/`. When this document and code differ, update this document in
> the same change.

## Product Surfaces

| Surface | Routes | Purpose |
| --- | --- | --- |
| Portal | `/`, `/monitor` | Package-manager setup and public service health |
| Admin | `/admin/*` | Repeated operational work: cache, upstreams, logs, policy, users, settings |
| Setup | first-run gate | Port, storage, enabled ecosystems, and upstream configuration |

The Portal is not a marketing landing page. Quick Start is the first screen;
Monitor is the second. Admin is dense, quiet, and optimized for scanning and
repeated actions.

## Instrument Language

The active visual system is **Instrument**:

- Signal green communicates cache hits, healthy state, active navigation, and
  focus. It replaced the old purple palette.
- Amber means slow/degraded. Red means miss/danger/failure.
- Dark mode is the product default; light mode uses the same semantic roles.
- Surfaces are neutral gray/green-black, with restrained borders and shadows.
- Gradients are limited green sweeps retained for a few brand accents. Purple
  Aurora backgrounds are not part of the current design.

Do not use the old purple/OKLCH examples, `/status` route, shadcn components,
`CardV2`, or `MetricCardV2`. They belonged to an earlier design iteration.

## Token Source

Tokens live in `web/src/index.css`. Tailwind v4 exposes matching utilities via
`@theme`; runtime light/dark values are defined on `:root` and
`[data-theme="dark"]`.

### Core Roles

| Role | Current light value | Use |
| --- | --- | --- |
| `--bg-page` | `#F0F2F1` | Page background |
| `--bg-card` | `#FFFFFF` | Primary surface |
| `--bg-soft` | `#F1F3F2` | Inset/secondary surface |
| `--text` | `#14181A` | Primary text |
| `--text-muted` | `#586068` | Secondary text |
| `--brand` / `--hit` | `#0FA86F` | Active, hit, healthy, focus |
| `--btn-primary-bg` / `--btn` | `#0A8654` | Primary command |
| `--warn` / `--slow` | `#B5770E` | Slow/degraded |
| `--danger` | `#CF4444` | Miss/error/destructive |

Compatibility names such as `--brand` remain because existing components use
them. New code may prefer role names (`--hit`, `--btn`, `--surface`, `--line`),
but must not introduce hard-coded parallel palettes.

### Radius And Spacing

| Token | Value |
| --- | --- |
| `--spacing-1/2/3/4/6/8/12` | `4/8/12/16/24/32/48px` |
| `--radius-pill` | `4px` |
| `--radius-tag` | `6px` |
| `--radius-card` | `10px` |
| `--radius-shell` | `14px` |

Use the established token unless a fixed-format control has an explicit local
dimension. Avoid decorative nested cards and page sections styled as floating
cards.

## Typography And Icons

- UI: `Inter Variable` for Latin; Chinese falls through to the native
  `PingFang SC` / `HarmonyOS Sans SC` / `MiSans` / `Microsoft YaHei` stack.
- Display: `Inter Tight Variable`.
- Code/data: `JetBrains Mono Variable` with tabular numerals.
- Icons: tree-shakeable Lucide SVGs, wrapped by `components/Icon.tsx`. The
  wrapper preserves the existing Material-style string names at call sites.

Do not import a second general icon family or inline an SVG for a symbol already
present in `components/Icon.tsx`.
Metric values, versions, bytes, latency, and other changing numbers should use
the mono/tabular treatment to avoid layout movement.

## Shared Components

Reusable primitives live in `web/src/components/`:

- `Button`, `Input`, `Select`, `Segmented`, `Tabs`
- `Badge`, `StatusDot`, `Metric`, `SectionHeader`
- `Modal`, `DataTable`, `EmptyState`
- `Icon`, `EcosystemIcon`, `UpstreamCard`
- `ThemeToggle`, `LangToggle`, `Logo`

Use these before adding a new primitive. Admin-specific composition belongs in
`web/src/admin/components/`; Portal-specific composition belongs in
`web/src/portal/components/`.

## Portal

`QuickStart.tsx` contains:

1. A compact three-step orientation: choose an ecosystem, copy the persistent
   package-manager configuration, then verify the request in Monitor.
2. The primary setup surface, with `EcosystemCatalog` on the left and
   `ConfigurePane` on the right for the selected technology stack.
3. A lower-priority optional-enhancements section containing the sole
   project-level AI integration path and the compiler-cache entry point.

The catalog recommends Python, Node.js, and Container first, remembers at most
three validated recent choices, searches ecosystem and manager names, and keeps
the complete 14-item catalog behind a native disclosure. `ConfigurePane`
defaults to the first supported manager (Python starts with pip); alternate
managers, one-off commands, and configuration paths use progressive
disclosure. Endpoint URLs derive from `window.location.origin`; Docker registry
mirrors use the service root, because Docker appends `/v2/` itself.

`Monitor.tsx` contains:

- upstream healthy/degraded/failed counts;
- seven-day hit rate and saved bytes when traffic exists;
- search/filter over grouped upstream cards;
- success rate, latency, and latency beats;
- explicit loading, error, successful-empty, stale, and search-empty states;
- one shared upstream status rule: unavailable is failed; an available
  upstream at or above 150 ms is degraded; other available upstreams are
  healthy;
- 30-second stats refresh and 60-second latency-series refresh.

There is no current Portal package-browser or live-event-stream page.

## Admin

`AdminApp.tsx` and `admin/components/MainLayout.tsx` provide the persistent
navigation shell. Admin pages use compact headings, stable table/control sizes,
clear empty/loading/error states, and explicit confirmation for destructive
commands.

The Dashboard is capped at 1440px inside the wider Admin outlet to keep
operational scanning distances reasonable. Its live service strip and recent
downloads precede the KPI/trend view; lower-priority package and bandwidth
summaries may share a two-column row only when each remains readable.

Current admin-specific components are:

- `MainLayout`
- `NowStrip`
- `TrendsCard`
- `WebhookTab`
- `ProRequiredCallout`

Shared metrics use `components/Metric.tsx`; upstream presentation uses
`components/UpstreamCard.tsx`. Do not reference removed `MetricCard`,
`UpstreamRow`, or `TopPackageChart` files.

### Shared Admin Primitives

Admin interactions are composed from the project-owned wrappers `Button`,
`IconButton`, `Input`, `Select`, `Textarea`, `Segmented`, `Tabs`, `Modal`,
`Toast`, `Tooltip`, `DataTable`, `TableViewport`, `EmptyState`, and
`QueryErrorState`. These wrappers own focus treatment, labels, keyboard
behavior, stable control dimensions, and semantic status feedback. Pages own
only their domain composition and query decisions.

Icon-only controls must use `IconButton`: a label and tooltip are mandatory,
and the interactive target is at least 40x40 CSS pixels. The same minimum
applies when an icon button is pending or disabled.

### Responsive Contract

| Width | Admin behavior |
| --- | --- |
| 320/390 | 16px page padding, single-column forms, horizontal Settings tabs, stacked section actions, two-column KPI grids |
| `sm` 640 | Forms may use two columns; ordinary toolbars may share a row and long action groups wrap |
| `md` 768 | Settings uses its 180px vertical tab rail; the top bar and extended status content use desktop composition |
| `lg` 1024 | The persistent 220px navigation sidebar appears; KPI grids may use four columns |
| `xl` 1280 | Analytics sections may use three columns; information density does not grow beyond this point |
| 1840+ | The Admin outlet is capped at 1840px and centered within the remaining main area |

The document viewport must never scroll horizontally. Wide tables scroll only
inside their focusable `TableViewport`.

### Control-Plane States

Settings treats `config.toml` as the configured authority. It displays the
configured and effective values, environment override source, fields applied
immediately, fields waiting for restart, and fields blocked by an environment
override. A successful HTTP response alone is not presented as "applied".

Ordinary ecosystem Upstreams are database-authoritative after first-run seed.
Create, update, delete, and manual check responses reflect the live Registry
snapshot; Docker remains configuration-authoritative and outside this CRUD
surface. Mutation controls stay disabled and dimensionally stable while their
request is pending, and row-local failures preserve the current data and form.

The Upstreams page is an operational inventory before it is a chart: operators
can search names, ecosystems, URLs, and proxies, then filter by the shared
healthy/degraded/failed rule. Large ecosystem groups expand into an adaptive
multi-column list while small groups retain the compact tiled layout. “Check
All” runs at most four requests concurrently, exposes progress and partial
request failures, and reports the same three health states used by filters.
Pending create, update, and delete dialogs cannot be dismissed until their
request completes.

Upstream Updates is stable episode history rather than a current-failures
dashboard. Operators can filter package, ecosystem, and result through
URL-backed server queries. Desktop uses a compact table; narrow screens use a
divided event list that keeps outcome and detail visible without horizontal
scrolling. The episode window displays both first and latest observation, while
latency is explicitly the latest observation's value. Auto-refresh runs only
while viewing the newest page; loading older pages pauses polling, and “Back to
latest” replaces history only after the newest-page request succeeds.

### Query-State Contract

| State | Required presentation |
| --- | --- |
| Initial pending | The owning region has `aria-busy="true"`; skeletons are hidden from assistive technology; no empty message is shown |
| Initial error | `QueryErrorState` names the failure and provides Retry |
| Successful empty response | `EmptyState` is shown only after a successful response proves the collection is empty |
| Cached data plus refresh failure | Cached data remains visible with a stale/degraded notice |
| Permission denied | The denial is explicit; it is never represented as an empty collection |
| Mutation pending/error | Duplicate submission is disabled; an error remains inline and does not close the dialog or emit a success toast |

Independent sibling queries own independent pending, error, stale, and empty
boundaries. A failure in one panel must not erase successful data in another.

### Control-Plane Authority

| Resource | Authority | Runtime application |
| --- | --- | --- |
| Settings | `config.toml` | Log level applies immediately; Cache/Auth changes require restart |
| Ordinary active Upstreams | Database | Registry atomically updates the next proxy request |
| Docker registries and extra indexes | `config.toml` | Restart-managed; absent from Admin Upstream CRUD |
| Users, tokens, rules, Webhooks, security policy | Database | Existing handler-specific runtime behavior |

The complete permission, response, persistence, and operator verification
contract is documented in [Admin Control Plane](docs/admin-control-plane.md).

## Interaction Rules

- Icons identify familiar tools; text accompanies commands where the action is
  not self-evident.
- Every icon-only control needs an accessible label and tooltip.
- Loading, empty, error, disabled, and permission-gated states are required.
- Controls must retain stable dimensions as labels, counts, or loading states
  change.
- Focus must remain visible in both themes.
- Motion must honor `prefers-reduced-motion`.
- Do not use negative letter spacing for new UI; existing legacy CSS can be
  migrated when touched.

## Verification

For frontend changes run:

```bash
cd web
npm run type-check
npm run type-check:e2e
npm run build
npm run test:e2e
xargs -r npx eslint < admin-remediation-eslint-files.txt
cd ..
python3 scripts/i18n-audit.py
```

The Playwright suite verifies the 13 Admin routes across the responsive,
light/dark, and Chinese/English matrix; it also checks API contracts, WCAG 2.1
A/AA rules, 40x40 icon targets, zero non-normal letter spacing, the 1840px wide
layout cap, and Portal token regressions. Keep screenshots failure-only; do not
commit full-page pixel snapshots.

The repository has unrelated historical lint debt. The manifest is the exact
Admin remediation scope: fix errors in those files without deriving a new list
from Git history or including unrelated Portal work.
