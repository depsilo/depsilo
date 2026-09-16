# Depsilo Design System

> Status: current implementation reference, updated 2026-09-16. The source of
> truth is `web/src/index.css`, `web/src/components/`, `web/src/portal/`, and
> `web/src/admin/`. When this document and code differ, update this document in
> the same change.
>
> **Decision of record, not yet in code:** React Aria Components replaces
> `@base-ui/react` as the interaction behaviour layer. Six wrappers still
> import Base UI — `Modal`, `Drawer`, `Tabs`, `Tooltip`, `Switch`, `Toast`.
> Until they are ported, their code wins over
> [Interaction Behaviour Layer](#interaction-behaviour-layer); everything else
> in this document is current.

## Product Surfaces

| Surface | Routes | Purpose |
| --- | --- | --- |
| Portal | `/`, `/monitor` | Package-manager setup and public service health |
| Admin | `/admin/*` | First-project connection plus repeated operational work: cache, upstreams, logs, policy, users, settings |
| Setup | first-run gate | Secure administrator creation with defaulted service configuration |

The Portal is not a marketing landing page. Quick Start is the first screen;
Monitor is the second. Admin is dense, quiet, and optimized for scanning and
repeated actions.

## Instrument Language

The active visual system is **Instrument**:

- Signal green communicates cache hits, healthy state, active navigation, and
  focus. It replaced the old purple palette.
- Amber means degraded or partially completed. Red means a real failure,
  explicit refusal, or destructive action. A cache miss is a normal neutral
  result; an unknown result means the outcome was not recorded.
- Dark mode is the product default; light mode uses the same semantic roles.
- Light mode uses a pure-white page canvas without ambient grain. Dark mode
  retains one subtle global grain layer mounted by `App`.
- Surfaces are neutral gray/green-black, with restrained borders and shadows.
- Restrained green sweeps may appear on rare decorative product surfaces, but
  never inside the Logo. Purple Aurora backgrounds are not part of the current
  design.

Admin surfaces additionally adopt the BoardUI free-tier surface treatment: the
light Admin canvas is `#FFFFFF` with a neutral `#F6F7F5` rail, and dark Admin
uses `#141915` with `#181F1A` navigation. These values are scoped to
`[data-admin-shell]`; Portal and Setup retain the shared Instrument tokens
above. Admin controls use 6px corners, 40px targets, and green is reserved for
commands, focus, health, and selection.

Do not use the old purple/OKLCH examples, `/status` route, shadcn components,
`CardV2`, or `MetricCardV2`. They belonged to an earlier design iteration.
BoardUI's registry uses the shadcn *item schema* as a distribution format; that
is a packaging detail and is not an exception to the shadcn component ban.

## Component Source And Licence Boundary

Depsilo vendors components from **BoardUI's free tier only**. This is a
licence boundary, not a quality preference: BoardUI free source is MIT, while
its Pro licence forbids publishing the source in a public repository, and this
repository is public. Read
[the BoardUI research record](docs/research/2026-09-16-boardui-design-reference.md)
before adding anything from that catalogue.

- **Pro source never enters this repository.** That covers the Pro chart cards,
  the composer and agent components, and the eight page templates. Not as a
  vendored file, not as a fixture, not as a copy-pasted fragment.
- **Components arrive as source, not as a dependency.** `npx boardui@latest add
  <name>` writes the component's `.tsx` files into `web/src/` and installs the
  npm packages that component needs. Depsilo owns and may modify those files;
  there is no package to upgrade.
- **Record provenance.** Keep the item name and installed version in a comment
  at the top of each adopted file, so a later `boardui add` can be diffed
  against what has been customised.

Adopted items are listed here. This table is the review surface for the
exceptions below; add to it in the same change that vendors a component.

| Item | Vendored files | Notes |
| --- | --- | --- |
| _(none — no BoardUI source is vendored yet)_ | | |

### Restyle rules for adopted source

Adopted files are restyled to Instrument before they are committed. BoardUI's
defaults are blue-accented and rounder than this system, and none of that
travels into Depsilo:

| BoardUI ships | Depsilo keeps |
| --- | --- |
| A blue `--color-accent-50…950` ramp | The Instrument greens, by role: `--btn` for commands, `--brand` for active and selected state, `--hit` for the cache-hit signal |
| `bg-button-primary` / `bg-button-danger` gradient fills | Flat `--btn` fill. No gradient on commands. |
| `--radius-2lg: 10px`, `--radius-2-5xl: 20px`, `--radius-notification-card: 10px` | The existing 4/6/10/14px ladder below |
| Tailwind-default spacing with arbitrary values | The established spacing tokens; no new arbitrary values |
| Composite `text-*` type utilities | Adopted — see Typography And Icons |

One BoardUI idea is adopted outright: **the command accent and the health
signal are separate tokens.** BoardUI re-tints every interactive surface from
one eleven-step accent ramp while charts and status chips keep their own hues.
Depsilo has the opposite arrangement today: in light mode `--hit` is a literal
alias of `--brand`, so the Admin shell's `--brand: #245443` override silently
re-points the cache-hit colour inside Admin, and `--hit` is also used as a plain
accent fill in Admin chrome rather than only as the hit signal. New work must
not deepen that coupling — use `--btn` for commands, `--hit` only for the
cache-hit signal, and treat a token that means both as a bug to fix rather than
a pattern to copy.

## Brand Mark

The canonical mark is **Dependency Shelf / 层仓栈**. Three dependency layers
shorten and step inward as they feed a continuous curved repository spine. The
layers represent direct and transitive dependencies converging on one indexed,
cached artifact store—the product name made visible as “dependencies in a
silo.” The silhouette deliberately avoids a letter, database cylinder, package
cube, network node, shield, lightning bolt, and other category-default motifs.

- `docs/brand/` is the master source. Web, favicon, desktop, and documentation
  assets must derive from it rather than forming separate identities.
- Use flat `#0A8654` for the mark on light backgrounds and `#3DDC91` on dark
  backgrounds. The paired wordmark follows the foreground (`#14181A` light,
  `#E9ECEE` dark). Do not add gradients, lightning shapes, shadows, glows, or
  an attached tagline.
- Application-tile assets are the contained exception: use a `#0A8654` field
  with the mark reversed in white. The favicon uses a theme-aware 16px optical
  master that keeps all three layers while thickening and grid-aligning them.
- The formal wordmark is always **Depsilo**. “依仓” may accompany it as
  localized copy, but is not a replacement wordmark.
- Preserve the mark's aspect ratio and at least one layer-height of clear space.
  Keep the three layers long-to-short, their left edges staggered, and the right
  repository spine continuous with a curved outer wall. The full mark is a
  filled construction on a 128-unit grid with 22-unit layers and 11-unit round
  ends; it does not use a stroke. The 16px optical master uses 24-unit layers.
  Verify every revision at 16, 24, 28, and 32px before judging it only at
  presentation size.
- Use the matching light/dark asset for its background. Theme-aware documents
  should provide both and fall back to the light-background asset.

## Token Source

Tokens live in `web/src/index.css`. Tailwind v4 exposes matching utilities via
`@theme`; runtime light/dark values are defined on `:root` and
`[data-theme="dark"]`.

### Core Roles

| Role | Current light value | Use |
| --- | --- | --- |
| `--bg-page` | `#FFFFFF` | Pure-white light-mode page background |
| `--admin-canvas` | `#FFFFFF` | Admin light-mode shell and main canvas |
| `--bg-card` | `#FFFFFF` | Primary surface |
| `--bg-soft` | `#F1F3F2` | Inset/secondary surface |
| `--text` | `#14181A` | Primary text |
| `--text-muted` | `#586068` | Secondary text |
| `--inverse` / `--on-inverse` | `#14181A` / `#FFFFFF` | Compact inverse tooltips and data details |
| `--brand` / `--hit` | `#0FA86F` | Active, hit, healthy, focus |
| `--btn-primary-bg` / `--btn` | `#0A8654` | Primary command |
| `--warn` / `--slow` | `#B5770E` | Slow/degraded/partial |
| `--danger` | `#CF4444` | Failure/refusal/destructive |

### State Semantics

Keep these dimensions separate when they appear together in Admin:

| Dimension | Normal result | Attention result |
| --- | --- | --- |
| Cache result | `hit` uses signal green; `miss` is neutral because the request was fetched; `unknown` is neutral and means the result was not recorded | Do not turn a miss or unknown into a failure |
| Policy result | `allow` uses the normal success treatment | `deny` is danger because the request was explicitly refused; an unknown decision remains unrecorded |
| Delivery result | `upstream` or `completed` uses the normal success treatment | `failed` is danger; `cancelled` or a partial outcome uses warning; unknown remains unrecorded |

Use warning for degraded or partial execution, and danger for real failures,
explicit refusals, and destructive commands. HTTP success only describes the
request transport; it does not make a cleanup operation complete when items
were skipped or failed.

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
  Depsilo-authored UI uses this wrapper and nothing else.
- BoardUI's free components carry `@remixicon/react` glyphs. Vendored source
  keeps the icons it ships with, because rewriting them would be undone by the
  next `boardui add`. That is a bounded exception to the single-family rule and
  it carries three review obligations:

  - A symbol that already exists in `components/Icon.tsx` is not re-drawn in
    Remix, and a Remix glyph is not added to `Icon.tsx`.
  - One control, row, toolbar, or table never mixes the two families. Where a
    vendored component sits directly beside Depsilo controls, the row resolves
    to one family; the vendored file is edited when that is the cheaper side.
  - Every vendored component that carries a second family is listed in the
    provenance table above, so the exception stays countable.

Do not import a second general icon family into Depsilo-authored code, and do
not inline an SVG for a symbol already present in `components/Icon.tsx`.
Metric values, versions, bytes, latency, and other changing numbers should use
the mono/tabular treatment to avoid layout movement.

## Shared Components

Reusable primitives live in `web/src/components/`:

- Actions and input: `Button`, `IconButton`, `IconButtonControl`, `Input`,
  `Textarea`, `Select`, `Segmented`, `Switch`
- Structure and navigation: `Tabs`, `Modal`, `Drawer`, `Toast`, `Tooltip`
- Content and state: `Badge`, `StatusDot`, `Metric`, `SectionHeader`,
  `InlineNotice`, `EmptyState`, `QueryErrorState`, `DataTable`,
  `TableViewport`, `UpstreamCard`
- Identity and preference: `Icon`, `EcosystemIcon`, `Logo`, `ThemeToggle`,
  `LangToggle`

Use these before adding a new primitive. Admin-specific composition belongs in
`web/src/admin/components/`; Portal-specific composition belongs in
`web/src/portal/components/`.

## Interaction Behaviour Layer

**React Aria Components** (`react-aria-components`, Apache-2.0) is the behaviour
and accessibility layer for every interactive primitive. It replaces
`@base-ui/react`, which is removed from `web/package.json` when the port lands.

Exactly one headless interaction library is a hard rule, not a preference.
BoardUI's free components are built on React Aria Components, so keeping Base
UI alongside adopted source would put two focus-management implementations and
two popover implementations inside a single dialog.

| Wrapper | React Aria Components |
| --- | --- |
| `components/Modal.tsx` | `Modal` + `Dialog` |
| `components/Drawer.tsx` | `Modal` + `Dialog`, drawer styling |
| `components/Tabs.tsx` | `Tabs` |
| `components/Tooltip.tsx` | `Tooltip` |
| `components/Switch.tsx` | `Switch` |
| `components/Toast.tsx` | `Toast` |

**Migration status: not started.** All six wrappers still import Base UI.
Port them together rather than one at a time — they share focus restoration,
escape handling, and outside-press dismissal, and a mixed state would be hard
to test honestly.

Behaviour is owned by these wrappers, not by pages. Focus trap and restore,
escape and outside-press dismissal, `aria-*` wiring, reduced-motion handling,
and stable control dimensions stay inside the component; pages own only domain
composition and query state.

## Portal

The Portal header uses one 40px control geometry without pretending every
item has the same role. The copyable endpoint and service health share a quiet
information rail; language and appearance share a segmented preference rail;
Admin is the sole brand-tinted navigation action. At narrow widths labels
collapse before essential controls disappear, and the document never scrolls
horizontally.

`QuickStart.tsx` contains:

1. A compact title and one-line orientation for choosing an ecosystem and
   package manager, copying the persistent configuration, and verifying it.
2. The primary setup surface, with `EcosystemCatalog` on the left and
   `ConfigurePane` on the right for the selected technology stack. This
   Precision Workbench is capped at 1440px rather than inheriting the wider
   Admin canvas.
3. A lower-priority optional-enhancements section containing the sole
   project-level AI integration path and the compiler-cache entry point.

The catalog remembers at most three validated recent choices as compact
shortcuts, searches ecosystem and manager names, and shows the complete
14-item catalog by default. Its white directory rail uses selection state, not
a large tinted slab, to establish hierarchy. `ConfigurePane` shows every
supported manager in one compact segmented rail and defaults to the first one
(Python starts with pip). The persistent configuration is the sole inverse
ink surface in the light workbench; one-off commands, test commands, and
configuration paths remain light and retain progressive disclosure. The test
command confirms client configuration through its own successful exit; request
records, cache results, and policy decisions belong to Admin and are not shown
in Portal. Endpoint URLs derive from `window.location.origin`; Docker registry
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

## Setup

Setup is a single-page security gate, not a product tour. Its primary task is
to verify the one-time bootstrap token when required, create the first
administrator, write the durable configuration, and restart the service. The
administrator fields and one completion command remain visible without an
introductory welcome step or progress tracker.

Port, storage, enabled Ecosystems, and Upstreams retain working defaults and
are submitted even when their disclosure is closed. They live in one native
advanced-settings disclosure because the current Admin control plane cannot
activate an omitted Ecosystem or edit port and storage after setup. Ecosystem
selection uses one keyboard-operable pressed button per option; it never nests
a checkbox inside another interactive control. Upstream editors stack on
narrow screens and expand only for the Ecosystem the Operator chooses to edit.

Language and appearance controls remain available before initialization.
Validation names the failed requirement next to the field and never relies on
a disabled command as the only explanation. Save failures preserve the form;
restart failures become a focused recovery state with the reconnect target and
a retry action.

After a fresh Setup creates the first administrator, the restarted service
continues to the authenticated `/admin/connect` route. A same-origin restart
may sign in with the credentials still held by the Setup form; a port or origin
change falls back to Login while preserving the destination. Bootstrap tokens
never become Admin or package credentials.

`/admin/connect` is the optional first-project loop: choose an Ecosystem and its
package manager, copy configuration generated from the browser-visible origin,
run a small dependency request when a safe client command exists, then observe
the request and an optional real cache hit. Python, Node.js, Rust, Java, and Go
are projected as the initial five choices from the same catalog used by Portal;
the full catalog remains available without a second support list. Wildcard bind
addresses are replaced with a client-usable localhost origin, while LAN hosts,
reverse-proxy hosts, ports, and HTTPS are preserved.

Verification uses an authenticated AuditLog cursor captured when the page
opens. It polls only the bounded event tail, pauses while the document is
hidden, and stops after a real `hit`; `miss`, policy `blocked`, and upstream
`error` are all handled requests. Continue writes `completed` or `skipped`, so
cache-hit confirmation never blocks Dashboard access. Missing onboarding state
means completed for upgrade compatibility; only fresh administrator creation
writes `not_started`.

## Admin

`AdminApp.tsx` and `admin/components/MainLayout.tsx` provide the persistent
navigation shell. Admin pages use compact headings, stable table/control sizes,
clear empty/loading/error states, and explicit confirmation for destructive
commands.

The **Dependency Flowline** shell organizes work into six task domains:
**Overview**, **Upstream Sources**, **Cache**, **Logs**, **Security**, and
**Projects**. The persistent sidebar is 232px wide. These six links remain
visible on desktop and in the mobile drawer, without child destinations or
disclosure controls. Page-level tabs own the individual destinations, so their
labels are not repeated in the sidebar.
Instance-wide Users, Settings, and License live under the bottom-left **Instance
management** link above the user information and share a separate local navigation row; they are not
project pages.
**Needs Attention** is integrated into Overview rather than presented as a primary navigation destination. Its legacy `/admin/attention`
URL remains reachable so bookmarks and direct links do not break, as do all
other established Admin URLs.

The light Admin canvas remains pure white, while its persistent workspace rail
uses a dedicated mint porcelain surface (`#F3F8F5`) and hover
(`#EAF3EE`). This near-white, brand-adjacent tint separates navigation from the
canvas without reusing the darker global inset surface. Dark mode retains its
existing rail and hover appearance. The current workspace receives a filled
selection on every route within it; the page-local tabs identify the current
destination. Language
and appearance remain adjacent in the utility bar, but each is a flat button
separated by quiet spacing; do not wrap them in a tinted, bordered preference
card.

The top bar is a quiet utility layer: it contains the workspace/page
breadcrumb, language, and appearance controls, plus the navigation trigger on
narrow screens. It never repeats service status or renders the page `h1`.
On desktop, Overview omits its redundant single-level breadcrumb because the
page heading already names the destination; nested pages retain the workspace
and page breadcrumb.
`AdminPage` owns the content title, optional description, page actions, and
readable/fluid width below that bar.

Each multi-page workspace exposes its canonical destinations as a compact
page-local navigation row below the heading. It is a projection of the route
manifest, so it must not create a second route registry or change deep links;
the sidebar remains the persistent workspace switcher. Overview contains the
Dashboard and Bandwidth Report. Upstreams and Projects each have one page and
render no local navigation row or empty divider.

| Sidebar destination | Local page navigation (in order) |
| --- | --- |
| Overview | Dashboard, Bandwidth Report |
| Upstreams | None; source configuration and health live on the same page |
| Cache | Artifacts, Index Cache, Compiler Cache |
| Logs | Access Logs, Metadata Refreshes, Audit Logs |
| Security | Vulnerability Intelligence, Quarantine & Blocking, Package Rules |
| Projects | None; project list opens the selected project's detail |
| Instance management (footer) | Users, Settings, License |

Security exposes only one row of page destinations. Inside Vulnerability
Intelligence, a labeled native select switches Overview, Vulnerabilities,
Suggested Rules, and Policies. Quarantine & Blocking similarly switches Events,
Approvals, and Malware blocklist with a labeled select. Only the selected view
is mounted. Existing `/admin/security?tab=...` URLs and browser Back continue to
select the intelligence view. Settings retains its own configuration tabs.

Policy runtime status is scoped to Overview and Security. Other
workspaces do not issue a policy-status request or reserve banner space; their
domain pages own any relevant inline status.

The Dashboard uses the Admin's default fluid canvas, capped at 1840px. Its page
heading and all Dashboard regions use the full same width and left baseline;
do not center a narrower content wrapper beneath a wider heading. On wide
screens, the main instrument column is fluid and the supporting rail remains
approximately 380px wide. Four left-aligned instruments lead the page—service
status, cache hit rate, healthy Upstreams, and requests in the last 24 hours—
followed by a compact queue for unhealthy Upstreams and cache-capacity pressure.
The live request path—**Client ingress → Depsilo cache → Upstreams**—then leads
the operational detail. The final row contains one multi-metric trend view and
at most three recent downloads; complete popular package, Upstream, and
bandwidth detail belongs in the Overview workspace’s Bandwidth Report
instead of being duplicated on Overview. Metrics with no observed data show an
honest unavailable marker rather than a fabricated zero.

Dashboard panel headers use one concise title/status row. Do not repeat generic
explanatory copy as a visible subtitle when the structure already communicates
the panel's purpose; keep useful context as screen-reader-only text when it is
needed for accessible naming. Runtime state is an inline semantic dot plus text,
not a filled badge or a second card inside the header.

The request path is the Admin's signature instrument: one continuous axis and
one moving signal segment connect all three stages. On narrow screens the same
axis turns vertical and each stage becomes a compact label/value row rather
than three stacked metric blocks. `NowStrip` and `TrendsCard` are open sections,
not bordered rectangles: do not add header and footer rules merely to frame
them, and do not use rounded card silhouettes, shadows, or nested card
surfaces. Keep only the top-bar divider, the request axis, list-row and metadata
separators, and necessary data-grid divisions; remove duplicate full-width
rules between a title and its content or between neighboring Dashboard regions.
The supporting attention and recent-download regions are quiet inset rails,
not primary cards. The four KPIs remain one bare, internally divided data rail.
Trend metrics use unboxed text tabs with an understated active underline. The
time range is the only segmented control in that toolbar: its outer frame stays
approximately 41px high around 40px buttons, without extra vertical padding.

On narrow screens the request path becomes a vertical flowline, followed by the
attention queue, so the first viewport communicates current service state and
the first actionable problem. Every Dashboard region owns an honest initial
loading, initial error, successful-empty, and cached-but-stale state. A failed
or incomplete refresh must never be presented as healthy or “all clear.”

Current admin-specific components are:

- `MainLayout`
- `AdminPage`
- `NowStrip`
- `DashboardAttention`
- `TrendsCard`
- `RecentDownloads`
- `WebhookTab`
- `ProRequiredCallout`
- `ConfirmActionDialog`

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

Programmatically focused route and setup status regions suppress the decorative
focus outline because they are announcement targets, not keyboard controls.
Their retry and navigation controls retain the standard visible focus ring.

Icon-only controls must use `IconButton`: a label and tooltip are mandatory,
and the interactive target is at least 40x40 CSS pixels. The same minimum
applies when an icon button is pending or disabled.

### Responsive Contract

| Width | Admin behavior |
| --- | --- |
| 320/390 | 16px page padding, single-column forms, horizontal Settings tabs, stacked section actions, two-column KPI grids; the Admin drawer shows six workspace links and scrolls only its navigation region; Dashboard uses a vertical request flow and keeps service state plus the first attention item in the first viewport |
| `sm` 640 | Forms may use two columns; ordinary toolbars may share a row and long action groups wrap |
| `md` 768 | Settings uses its 180px vertical tab rail; the Dashboard request flow becomes horizontal |
| `lg` 1024 | The persistent 232px workspace sidebar appears with six workspace links; KPI grids may use four columns |
| `xl` 1280 | Dashboard flow/attention and trend/recent-activity rows use a fluid main column plus an approximately 380px supporting rail; they stack when that rail would crowd the primary task |
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

Metadata Refreshes (`/admin/upstream-updates`) is stable episode history rather than a current-failures
dashboard. Operators can filter package, ecosystem, and result through
URL-backed server queries. Desktop uses a compact table; narrow screens use a
divided event list that keeps outcome and detail visible without horizontal
scrolling. The episode window displays both first and latest observation, while
latency is explicitly the latest observation's value. Auto-refresh runs only
while viewing the newest page; loading older pages pauses polling, and “Back to
latest” replaces history only after the newest-page request succeeds.

Access Logs and Audit Logs keep applied filters and pagination in canonical URL
parameters so links, reload, and browser navigation restore the same
investigation. Security does the same for its active intelligence view. Invalid values are
replaced with the default canonical form rather than retained as misleading
address-bar state. Export actions expose pending, success, and retryable failure
feedback.

Quarantine keeps dense tables on desktop and switches below 640px to direct
event, approval, and override lists. Each mobile row keeps the package,
version, reason, outcome, and primary action together without requiring
horizontal scrolling.

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

Destructive or access-changing actions identify the affected object and impact
in a confirmation dialog. Dialogs cannot be dismissed while their mutation is
pending; a failure stays in context and remains retryable. Multi-item security
policy changes use a shared draft and changed-only review, and cannot overlap
with an in-flight single-policy save.

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
make check
```

`make check` runs the fast Go and browser smoke layers plus frontend contracts.
Before merging broad UI changes, run `make verify` for the complete Playwright
suite. Accessibility coverage visits every Admin route once and uses a small set
of representative responsive, theme, and locale combinations; it also checks
API contracts, WCAG 2.1 A/AA rules, target sizes, layout caps, and Portal token
regressions. Keep screenshots failure-only; do not commit full-page pixel
snapshots.

The repository has unrelated historical lint debt. The manifest is the exact
Admin remediation scope: fix errors in those files without deriving a new list
from Git history or including unrelated Portal work.
