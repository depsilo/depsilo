# Depsilo Design System

> Status: current implementation reference, updated 2026-09-29. The source of
> truth is `web/src/index.css`, `web/src/components/ui/`,
> `web/src/components/`, `web/src/portal/`, and `web/src/admin/`. When this
> document and code differ, update this document in the same change.

## Product Surfaces

| Surface | Routes | Purpose |
| --- | --- | --- |
| Portal | `/`, `/monitor` | Package-manager setup and public service health |
| Admin | `/admin/*` | First-project connection plus repeated operational work: cache, upstreams, logs, policy, users, settings |
| Setup | first-run gate | Secure administrator creation with defaulted service configuration |

The Portal is not a marketing landing page. Quick Start is the first screen;
Monitor is the second. Admin is dense, quiet, and optimized for scanning and
repeated actions.

## Visual Language

The interface **is** the shadcn/ui default theme (neutral base). shadcn owns
surfaces, radii, control geometry, focus rings, and component behaviour; this
document records only what Depsilo adds or constrains.

- Surfaces are neutral, zero-chroma greys — pure white canvases in light mode,
  `oklch(0.145 0 0)` in dark — with 1px borders and one elevation system
  (Tailwind's `shadow-sm` / `shadow-md` / `shadow-lg`).
- The command colour is shadcn's neutral `--primary`: near-black in light mode
  and near-white in dark. Focus rings use `--ring`, never a brand hue.
- Colour carries state, not decoration. Green marks a cache hit, a healthy
  service, or a successful outcome; amber marks degraded or partially
  completed work; red marks a real failure, an explicit refusal, or a
  destructive action. A cache miss is a neutral result, and an unknown result
  means the outcome was not recorded.
- Dark mode is the product default. Both themes share one token set and
  nothing is themed per surface, so Admin, Portal, and Setup are one world.
- No ambient grain, gradient text, glow, cursor spotlight, or decorative
  sweep. The page sits on a plain canvas.

The brand mark is the one deliberate colour exception: the Dependency Shelf
stays green (see [Brand Mark](#brand-mark)).

Everything not listed in [Deviations From Upstream](#deviations-from-upstream)
follows shadcn/ui as shipped.

Two colour rules do not yet agree with each other and are open work: the trend
chart draws its series from `--brand` / `--danger` / `--warn-text`, so after
the neutral-primary change the "hits" series no longer carries the status
green this section promises. Series identity needs its own differentiated
ramp rather than reusing status or chrome tokens.

Do not reintroduce the Instrument palette (signal-green chrome, 0.5px
hairlines, BoardUI porcelain/charcoal Admin surfaces), the `/status` route,
`CardV2`, or `MetricCardV2`.

## Component Layer

shadcn/ui is the component library. It is a source distribution, not a runtime
dependency: the components are vendored under `web/src/components/ui/` and are
ours to edit.

- `web/components.json` holds the shadcn configuration. Style `base-nova`,
  base library `@base-ui/react`, icon library `lucide`. Add components with
  `npx shadcn@latest add <name>` from `web/`; keep the `base-nova` style so the
  vendored files stay comparable with upstream.
- `web/src/components/ui/` is the primitive layer: `button`, `input`,
  `textarea`, `label`, `native-select`, `select`, `badge`, `dialog`, `sheet`,
  `tabs`, `switch`, `tooltip`, `table`, `toggle`, `toggle-group`, `separator`,
  `sonner`. These files keep upstream's composition, class strings, and variant
  APIs; only the size ladder, the semantic badge variants, and the destructive
  button pair deviate, each for a reason recorded below.
- `web/src/components/` holds the app-facing adapters (`Button`, `Input`,
  `Modal`, `Drawer`, `Tabs`, `Toast`, ...). They keep Depsilo's prop names,
  i18n, and accessibility contracts so call sites do not track the component
  library. Compose UI from the adapters first and reach for `components/ui/`
  only when a screen needs a primitive the adapters do not expose.
- `web/src/lib/utils.ts` exports the `cn` helper every vendored component uses.

`tw-animate-css` supplies the enter/exit animations and `shadcn/tailwind.css`
supplies the state variants (`data-open`, `data-checked`, `data-active`,
`data-horizontal`, ...) that the vendored components reference.

## Deviations From Upstream

This is the single list. Anything not here follows shadcn/ui as shipped; each
entry below names the constraint that earns it.

Theme:

| Deviation | Earned by |
| --- | --- |
| Controls keep 36px (`h-9`) and 40px (`h-10` / icon) heights instead of upstream's 32/28px ladder | Admin's 40px touch-target contract, enforced by the accessibility suite |
| `--muted-foreground` is `oklch(0.52 0 0)`, one step darker than upstream's `oklch(0.556 0 0)` | Upstream clears 4.5:1 on white but only reaches 4.34:1 on `--muted`, and Admin puts secondary text on muted panels |
| Destructive buttons use the tinted danger pair, not solid red | Solid red against white text measures 2.9:1 in dark mode |
| The spacing scale is pinned in px (`4/8/12/16/24/32/48`) | The document sets a 13px root font size, so Tailwind's `rem`-derived steps would land on 3.25/6.5/9.75… instead of the grid the layout is drawn on |
| The type stack stays Inter / Inter Tight / JetBrains Mono | shadcn's default face is Geist, which has no CJK coverage; this product ships Chinese and English from one component tree |
| The brand mark keeps its green (`#0A8654` light, `#3DDC91` dark) | It is a confirmed asset (see [Brand Mark](#brand-mark)) — the one colour that is identity rather than role |

Components:

| Deviation | Earned by |
| --- | --- |
| `Select` is the platform `<select>` (shadcn's `native-select` recipe), not the Base UI listbox | Native keyboard, typeahead, and form semantics, plus the `combobox` + `<option>` contract operators and Playwright rely on. `components/ui/select` is vendored for option sets that need richer rows |
| Toasts render through Sonner's own markup | Tone rides on Sonner's `data-type` (`success` / `warning` / `error`) instead of the old `data-toast-tone`, and the live region is polite. Danger toasts are not assertive `role="alert"` elements; in-page failures use `InlineNotice` |
| `Dialog` keeps a scroll viewport and caps the popup height | Tall Admin forms stay reachable on short screens; upstream centres a fixed popup |
| `Sheet` keeps an explicit 320px rail width | The Admin navigation stays legible at 320px viewports, where upstream's `w-3/4` cannot fit the brand block plus the reserved close-button space |
| `Badge` keeps semantic variants (`success`, `warning`, `destructive`, `pro`, `neutral`) | They name product state, which upstream's generic set does not |
| `Table` does not ship upstream's scroll container | `TableViewport` owns the labelled, focusable region the accessibility contract depends on |
| `IconButton` holds a 41px box and the 40px target in an inline style | The icon contract is asserted by tests; folding it into the Button size ladder is open cleanup |

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

Tokens live in `web/src/index.css`. The shadcn semantic roles
(`--background`, `--foreground`, `--primary`, `--muted`, `--border`, `--ring`,
`--radius`, ...) are the source of truth and hold shadcn's default neutral
values; they are declared on `:root` and re-declared per theme on
`[data-theme="dark"]`, and `@theme inline` exposes them to Tailwind utilities.
The `@theme` block itself carries no colour — only the type stack and the
px-pinned spacing steps — so there is exactly one owner per colour.

The Instrument names this codebase grew up with (`--bg-page`, `--bg-card`,
`--text-muted`, `--border`, `--btn`, `--brand`, ...) remain as **compatibility
aliases** pointing at the shadcn roles, so existing `var(--…)` call sites did
not have to move. `--brand*` resolves to the neutral accent (selection, links,
active navigation); green survives only in `--ok*` and `--hit*`, where it is a
status and not chrome. The compatibility set is now only what something
actually reads — `--inverse`, `--on-inverse`, `--hit`, `--on-hit`, `--btn`,
`--btn-press`, `--btn-fg`, `--live`, `--grid`, `--r-tag`, `--r-card`; the
unread rest (`--bg`, `--surface`, `--inset`, `--hover`, `--line*`, `--fg*`,
`--slow*`, `--miss*`, `--glow`, `--hit-press`, `--hit-bg`, `--r-pill`) was
deleted rather than carried.

Do not give a role a literal colour that duplicates an existing token, and do
not add a second palette to `@theme`; point at the token instead.

### Core Roles

| Role | Current light value | Use |
| --- | --- | --- |
| `--background` / `--bg-page` | `oklch(1 0 0)` | Page canvas |
| `--card` / `--bg-card` | `oklch(1 0 0)` | Primary surface |
| `--muted` / `--bg-soft` | `oklch(0.97 0 0)` | Inset and secondary surface |
| `--foreground` / `--text` | `oklch(0.145 0 0)` | Primary text |
| `--muted-foreground` / `--text-muted` | `oklch(0.52 0 0)` | Secondary text |
| `--primary` / `--btn` | `oklch(0.205 0 0)` | Command button, selection |
| `--border` | `oklch(0.922 0 0)` | Hairlines and controls |
| `--ring` / `--focus-ring` | `oklch(0.708 0 0)` | Focus ring |
| `--ok` / `--ok-text` | `oklch(0.596 0.145 163.225)` | Hit, healthy, success |
| `--warn` / `--warn-text` | `oklch(0.666 0.179 58.318)` | Slow, degraded, partial |
| `--danger` / `--danger-text` | `oklch(0.577 0.245 27.325)` | Failure, refusal, destructive |
| `--logo-mark` | `#0A8654` | Brand mark only (dark: `#3DDC91`) |

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

New code should read the shadcn roles directly (`bg-card`,
`text-muted-foreground`, `border-border`) rather than the compatibility aliases,
and must not introduce a hard-coded parallel palette.

### Radius And Spacing

shadcn's scale is `--radius: 0.625rem` with `sm/md/lg/xl` at 6/8/10/14px.

| Token | Value | Use |
| --- | --- | --- |
| `--radius-sm` / `--r-sm` | `6px` | Tags, chips, inner segments |
| `--radius-md` / `--r-md` | `8px` | Buttons, inputs, selects, tabs |
| `--radius-lg` / `--r-lg` | `10px` | Grouped containers |
| `--radius-xl` / `--r-card` | `14px` | Cards, dialogs, sheets, panels |

Spacing stays on the 4px grid, pinned in px by the `--spacing-*` steps in
`@theme` (see [Deviations From Upstream](#deviations-from-upstream) for why).
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

The type stack is a deliberate carry-over from the previous world. shadcn's
own default is Geist, which has no CJK coverage; this product ships Chinese and
English from the same component tree, and Inter plus a native CJK fallback is
the closest obtainable match (`typography.fonts` may be revisited as one
change: swap `@fontsource-variable/inter*` for `@fontsource-variable/geist*`
and update the two `@font-face`/`--font-*` declarations).

Do not import a second general icon family or inline an SVG for a symbol already
present in `components/Icon.tsx`.
Metric values, versions, bytes, latency, and other changing numbers should use
the mono/tabular treatment to avoid layout movement.

## Shared Components

Reusable primitives live in `web/src/components/`, built on the vendored
shadcn/ui layer in `web/src/components/ui/`:

- `Button`, `Input`, `Select`, `Segmented`, `Tabs`
- `Badge`, `StatusDot`, `Metric`, `SectionHeader`
- `Modal`, `DataTable`, `EmptyState`
- `Icon`, `EcoadministrationIcon`, `UpstreamCard`
- `ThemeToggle`, `LangToggle`, `Logo`

Use these before adding a new primitive. Admin-specific composition belongs in
`web/src/admin/components/`; Portal-specific composition belongs in
`web/src/portal/components/`.

## Portal

The Portal header uses one 40px control geometry without pretending every
item has the same role. The copyable endpoint and service health share a quiet
information rail; language and appearance share a segmented preference rail;
Admin is the sole filled `--primary` navigation action. At narrow widths labels
collapse before essential controls disappear, and the document never scrolls
horizontally.

`QuickStart.tsx` contains:

1. A compact title and one-line orientation for choosing an ecosystem and
   package manager, copying the persistent configuration, and verifying it.
2. The primary setup surface, with `EcoadministrationCatalog` on the left and
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

Port, storage, enabled Ecoadministrations, and Upstreams retain working defaults and
are submitted even when their disclosure is closed. They live in one native
advanced-settings disclosure because the current Admin control plane cannot
activate an omitted Ecoadministration or edit port and storage after setup. Ecoadministration
selection uses one keyboard-operable pressed button per option; it never nests
a checkbox inside another interactive control. Upstream editors stack on
narrow screens and expand only for the Ecoadministration the Operator chooses to edit.

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

`/admin/connect` is the optional first-project loop: choose an Ecoadministration and its
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

The Admin canvas and its persistent workspace rail both come from the shared
neutral theme: the rail is `--muted` (`oklch(0.985 0 0)` light,
`oklch(0.205 0 0)` dark) against the page canvas. There is no Admin-only
palette any more — the scoped porcelain/charcoal override is gone, so Admin,
Portal, and Setup share one surface set. The current workspace receives a filled
selection on every route within it; the page-local tabs identify the current
destination. Language and appearance sit at the bottom of the rail, directly
below the navigation and above the **Instance management** link: adjacent flat
buttons separated by quiet spacing; do not wrap them in a tinted, bordered
preference card.

The top bar is a quiet utility layer: it contains the workspace/page
breadcrumb plus the navigation trigger on narrow screens. It never repeats
service status, renders the page `h1`, or carries preferences.
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
