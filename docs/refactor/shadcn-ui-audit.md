# Depsilo UI audit — shadcn/ui + Base UI migration (Stage A, Phase 0)

> Status: historical evidence, not instructions. The inventory below was
> collected 2026-09-17 from the working tree at `master` / `23eb652`, before
> the migration began; every item in it has since been migrated, replaced, or
> deleted. It is kept as the record of what the legacy UI actually contained,
> which is what the migration was argued from.
>
> The executable plan that followed it, and the Stage B proposal after that,
> are in git history rather than the working tree, per
> [docs/README.md](../README.md). What the migration produced is in
> [DESIGN.md](../../DESIGN.md) and the [design set](../design/README.md).
>
> Scope: `web/` only. Backend, package protocols, and the Admin control-plane
> authority model are unchanged by this migration.

## 1. How the current UI is built

The frontend is a Vite + React 19 + TypeScript SPA served from an embedded Go
binary. It already runs Tailwind CSS v4 through `@tailwindcss/vite`, but it uses
Tailwind as a *utility vocabulary over a hand-authored CSS design system*, not
as a component library.

There are three parallel styling mechanisms:

1. **A 1,427-line `src/index.css`** holding `@font-face` blocks, a `@theme`
   block, three duplicated light/dark/system token cascades (`:root`,
   `[data-theme="dark"]`, `@media (prefers-color-scheme: dark)`), and ~60
   hand-written component/utility classes (`app-*`, `portal-*`, `admin-*`,
   `live-download-*`, `eco-*`, `manager-*`, `code-*`, …).
2. **A project-owned primitive layer** in `src/components/` (28 files,
   2,025 lines). Seven of these files wrap `@base-ui/react` directly.
3. **Inline `style={{ … var(--token) … }}` objects** — 462 occurrences across
   66 files. These bypass Tailwind entirely and re-implement colour, radius,
   spacing, and typography per call site.

Plus a fourth mechanism: a 177-line `src/admin/admin-shell.css` that redefines
the semantic tokens for `[data-admin-shell]` to produce a different Admin
palette from the shared Portal/Setup palette.

### Ownership seams today

| Layer | Location | Notes |
| --- | --- | --- |
| Generic primitives | `web/src/components/` | Mixed with product components — no separation |
| Admin composition | `web/src/admin/components/` | 12 components |
| Portal composition | `web/src/portal/components/` | 8 components |
| Tokens / global CSS | `web/src/index.css`, `web/src/admin/admin-shell.css` | Two competing token cascades |
| Direct Base UI | `web/src/components/{Modal,Drawer,Switch,Tabs,Toast,Tooltip}.tsx` | Inside the shared dir, not isolated |

There is no `components/ui` layer, no `components/app` layer, no
`components.json`, and no shadcn registry dependency.

### Size of the change surface

| Area | Files | Lines |
| --- | --- | --- |
| `web/src/components/` (shared) | 28 | 2,025 |
| `web/src/admin/` (shell, components, pages) | 32 | 12,066 |
| `web/src/portal/` | 11 | 1,918 |
| `web/src/setup/` | 3 | 950 |
| `web/src/routing/` + `hooks/` + `lib/` | 24 | ~1,600 |
| `web/src/index.css` + `admin-shell.css` | 2 | 1,604 |
| `web/e2e/` (Playwright) | 42 | 8,185 |
| `web/unit/` (Vitest) | 11 | ~600 |

## 2. UI primitive inventory

Every primitive the product actually uses, its current home, and its
disposition. "Usage" is the number of modules importing it.

| # | Current | Location | Backing | Usage | Replacement | Delete? | Risk | Phase |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | `Button` | `components/Button.tsx` | raw `<button>` + inline styles | 31 | `ui/button` (cva variants) | yes | M | 3A |
| 2 | `Input` | `components/Input.tsx` | raw `<input>` + `label`/`hint`/`error` | 14 | `ui/input` + `app/field` | yes | M | 4 |
| 3 | `Textarea` | `components/Textarea.tsx` | raw `<textarea>` | 2 | `ui/textarea` + `app/field` | yes | L | 4 |
| 4 | `Select` | `components/Select.tsx` | raw `<select>` | 14 | `ui/select` (Base UI) + `app/field` | yes | H | 4 |
| 5 | `Badge` | `components/Badge.tsx` | raw `<span>` + inline styles | 16 | `ui/badge` + `app/status-badge` | yes | L | 3A |
| 6 | `Switch` | `components/Switch.tsx` | `@base-ui/react/switch` | 1 | `ui/switch` | yes | M | 3B |
| 7 | `Tooltip` | `components/Tooltip.tsx` | `@base-ui/react/tooltip` | 1 | `ui/tooltip` | yes | M | 3A |
| 8 | `Tabs` | `components/Tabs.tsx` | `@base-ui/react/tabs` | 1 | `ui/tabs` | yes | M | 3B |
| 9 | `Modal` | `components/Modal.tsx` | `@base-ui/react/dialog` | 11 | `ui/dialog` | yes | H | 3C |
| 10 | `Drawer` | `components/Drawer.tsx` | `@base-ui/react/dialog` | 2 | `ui/sheet` | yes | H | 3C |
| 11 | `Toast` | `components/Toast.tsx` | `@base-ui/react/toast` | 9 | `ui/sonner`-equivalent toast over Base UI | yes | H | 3C |
| 12 | `DataTable` | `components/DataTable.tsx` | raw `<table>` | 5 | `ui/table` + `app/data-table` | yes | M | 7 |
| 13 | `TableViewport` | `components/TableViewport.tsx` | raw `<div>` | 7 | folded into `app/data-table` | yes | L | 7 |
| 14 | `Segmented` | `components/Segmented.tsx` | raw `<button>` | **0 — dead** | delete outright | yes | L | 9 |
| 15 | `IconButton` | `components/IconButton.tsx` | Tooltip + control | 12 | `ui/button` `size="icon"` + `ui/tooltip` | yes | M | 3A |
| 16 | `IconButtonControl` | `components/IconButtonControl.tsx` | raw `<button>` | 2 | `ui/button` `size="icon"` | yes | L | 3A |
| 17 | `Icon` | `components/Icon.tsx` | Lucide + Material-style name map | 35 | direct `lucide-react` imports | yes | M | 9 |
| 18 | `fieldFeedback` | `components/fieldFeedback.ts` | pure helper | 3 | folded into `app/field` | yes | L | 4 |

**Primitives that do not exist yet but the migration needs** (from the target
list in the brief): `Separator`, `Skeleton`, `Card`, `Checkbox`, `Radio`,
`Label`, `DropdownMenu`, `Popover`, `AlertDialog`, `Command`, `ScrollArea`,
`Sidebar`, `Table`, `Breadcrumb`, `Pagination`, `Empty`.

They are introduced only where an owning call site exists. There is no
requirement to add a primitive the product does not use.

### Product component inventory

These are Depsilo application components, not generic primitives. They belong in
`components/app/` (or the surface-local dirs) and are **kept**, rewritten on top
of `components/ui`.

| Current | Location | Usage | Target | Notes |
| --- | --- | --- | --- | --- |
| `Metric` | `components/Metric.tsx` | 4 | `app/metric` | KPI label + tabular value + delta |
| `StatusDot` | `components/StatusDot.tsx` | 4 | `app/status-dot` | healthy/degraded/failed/unknown |
| `EmptyState` | `components/EmptyState.tsx` | 16 | `app/empty-state` | icon + title + hint + action |
| `ErrorState` (`QueryErrorState`) | `components/QueryErrorState.tsx` | 22 | `app/error-state` | message + retry |
| `SectionHeader` | `components/SectionHeader.tsx` | 12 | `app/section-header` | title + hint + action |
| `InlineNotice` | `components/InlineNotice.tsx` | 19 | `app/notice` | tone map incl. info |
| `EcosystemIcon` | `components/EcosystemIcon.tsx` | 17 | `app/ecosystem-icon` | simple-icons brand marks |
| `UpstreamCard` (`UpstreamGroupedPanel`) | `components/UpstreamCard.tsx` | 2 | `app/upstream-panel` | shared Portal + Admin |
| `Logo` | `components/Logo.tsx` | 4 | `app/logo` | placeholder wordmark until Stage B |
| `ThemeToggle` | `components/ThemeToggle.tsx` | 2 | `app/theme-toggle` | 3-way system/light/dark cycle |
| `LangToggle` | `components/LangToggle.tsx` | 2 | `app/language-toggle` | zh/en; keep i18n contract |
| `CodeBlock` | `portal/components/CodeBlock.tsx` | 2 | `portal/components/code-block` | keep, restyle |
| `CopyButton` | `portal/components/CopyButton.tsx` | 2 | `portal/components/copy-button` | keep, restyle |
| Admin shell (`MainLayout`) | `admin/components/MainLayout.tsx` | 1 | `admin/components/admin-shell` | 232px rail + topbar + drawer |
| `AdminPage` | `admin/components/AdminPage.tsx` | 19 | `admin/components/admin-page` | owns `h1`, description, actions, local nav |
| `AdminLocalNav` | `admin/components/AdminLocalNav.tsx` | 1 | folded into `admin-page` | route-manifest projection |
| `AdminPagination` | `admin/components/AdminPagination.tsx` | ~6 | `app/data-table-pagination` | becomes part of `app/data-table` |
| `ConfirmActionDialog` | `admin/components/ConfirmActionDialog.tsx` | ~12 | `app/confirm-dialog` | destructive confirmation |
| `NowStrip`, `TrendsCard`, `RecentDownloads`, `DashboardAttention`, `StaleDataNotice`, `ProRequiredCallout`, `WebhookTab` | `admin/components/` | 1 each | `admin/components/` | domain dashboards, keep |

## 3. Base UI usage

`@base-ui/react@^1.3.0` is a direct dependency and is imported by exactly six
files, all inside `src/components/`:

| File | Base UI namespace | Migrated into |
| --- | --- | --- |
| `components/Modal.tsx` | `dialog` | `components/ui/dialog.tsx` |
| `components/Drawer.tsx` | `dialog` | `components/ui/sheet.tsx` |
| `components/Switch.tsx` | `switch` | `components/ui/switch.tsx` |
| `components/Tabs.tsx` | `tabs` | `components/ui/tabs.tsx` |
| `components/Toast.tsx` | `toast` | `components/ui/toast.tsx` |
| `components/Tooltip.tsx` | `tooltip` | `components/ui/tooltip.tsx` |

No page or feature component imports Base UI directly today, so the "Page →
Base UI" prohibition is not yet violated. The migration must keep it that way:
after Stage A the only `@base-ui/react` imports live under `components/ui/`.

Shadcn's Base UI base adds further Base UI usage for
`select`, `checkbox`, `radio`, `popover`, `dropdown-menu`, `alert-dialog`,
`scroll-area`, `command`, and `sidebar` — all confined to `components/ui/`.

## 4. Legacy CSS and token inventory

### Global CSS

| File | Lines | Contents | Disposition |
| --- | --- | --- | --- |
| `src/index.css` | 1,427 | fonts, `@theme`, 3× token cascade, ~60 classes | reduce to imports + tokens + base |
| `src/admin/admin-shell.css` | 177 | Admin-only token override + local nav styles | delete |

### Token cascade duplication

The dark palette is written out **three times** (`[data-theme="dark"]`,
`@media (prefers-color-scheme: dark)` ×2 for `.page-wash` and the Admin shell).
This is the single largest structural defect in the current CSS.

### Legacy token usage census (`var(--…)`, top references)

| Token | References | Disposition |
| --- | --- | --- |
| `--text-soft` | 199 | → `--muted-foreground` |
| `--text` | 185 | → `--foreground` |
| `--border` | 135 | → `--border` |
| `--text-muted` | 96 | → `--muted-foreground` |
| `--bg-soft` | 71 | → `--muted` |
| `--text-subtle` | 55 | → `--muted-foreground` |
| `--bg-card` | 48 | → `--card` |
| `--brand-text` | 42 | → `--primary` / `--accent-foreground` |
| `--warn-text` | 33 | → `--warning` |
| `--brand` | 31 | → `--primary` |
| `--danger-text` | 29 | → `--destructive` |
| `--brand-soft` | 27 | → `--accent` |
| `--bg-hover` | 27 | → `--accent` |
| `--danger` | 26 | → `--destructive` |
| `--ok-text` | 23 | → `--success` |
| `--border-soft` | 19 | → `--border` |
| `--border-strong` | 18 | → `--border` / `--input` |
| `--font-mono` | 18 | → `--font-mono` (keep) |
| `--danger-fill` | 14 | → `--destructive/10` |
| `--bg-page` | 11 | → `--background` |
| `--font-display` | 10 | → `--font-sans` (Stage B reintroduces display) |
| remainder | ≤12 each | per-token decision in Phase 10 |

Tokens that must **not** survive: `--brand*`, `--spec-1..4`, `--grad-*`,
`--aurora*`, `--page-wash`, `--grid`, `--glow`, `--admin-canvas`,
`--admin-rail*`, `--shadow-card/surface`, `--r-pill/tag/card/shell`,
`--btn-primary-*`, `--logo-mark`, `--code-*` (folded into `--muted`/`--card`).

### Hand-written classes referenced from TSX

57 distinct class names are referenced from components. All are legacy.

`admin-kpi-grid`, `admin-kpi-section`, `admin-page-destination(s)`,
`admin-page-navigation`, `admin-primary-panel`, `admin-secondary-panel`,
`app-button`, `app-dialog-backdrop/close/popup/title/viewport`,
`app-drawer-backdrop/close/popup/viewport`, `app-toast-*`, `app-tooltip-*`,
`code-block`, `code-copy-control`, `config-disclosure`, `disclosure-chevron`,
`eco-catalog`, `eco-tile`, `fade-up`, `fade-up-d1..d3`, `hit-extend`,
`manager-choice`, `manager-config-swap`, `manager-picker-viewport`, `num`,
`page-wash`, `portal-*` (22 names), `stripe-focus-ring`,
`dot-live`, `live-download-*`.

Disposition: every one is deleted. Surviving behaviour is re-expressed as
Tailwind utilities on shadcn tokens, or as a data attribute when the class
carried semantics that tests assert (see §7).

### Dead CSS

`--grad-text`, `--grad-text-aurora`, `--grad-ring`, `--aurora-glow`,
`--dot-halo`, `--chip`, `--chip-brand`, `--shell`, `--divider-h`,
`--row-hover`, `--skeleton`, `--animate-ping-health`, `--animate-fade-in`,
`--sv-reveal`, `--admin-primary-panel`, `--admin-secondary-panel` are defined
but referenced by no component. They are deleted in Phase 10.

## 5. Legacy components that should be deleted

| Component | Reason |
| --- | --- |
| `components/Segmented.tsx` | zero importers; dead since the Trends toolbar moved to pressed buttons |
| `components/Icon.tsx` | Material-style string-name indirection over Lucide; shadcn convention is direct `lucide-react` |
| `components/IconButtonControl.tsx` | split from `IconButton` only to avoid loading tooltip machinery; `ui/button` covers both |
| `components/TableViewport.tsx` | 21 lines owning one `overflow-x-auto` + `minWidth`; belongs inside the data-table component |
| `components/fieldFeedback.ts` | 5-line `aria-describedby` merge; belongs inside the field component |
| `components/DataTable.tsx` | replaced by `ui/table` + `app/data-table` |
| `components/Button|Input|Textarea|Select|Badge|Switch|Tooltip|Tabs|Modal|Drawer|Toast.tsx` | replaced by `components/ui/*` + `components/app/*` |
| `admin/admin-shell.css` | Admin-only token fork; the token layer becomes single-source |
| `admin/components/AdminLocalNav.tsx` | 37-line route projection; folds into the Admin page frame |

## 6. Components that become shadcn primitives

Only these are added. Each is added when its owning call site moves, not in
bulk (`npx shadcn add --all` is prohibited).

| Batch | Components | Owner |
| --- | --- | --- |
| 3A (low risk) | `button`, `badge`, `input`, `textarea`, `separator`, `skeleton`, `card`, `tooltip`, `label` | `components/ui/` |
| 3B (form) | `checkbox`, `switch`, `select`, `tabs`, `dropdown-menu`, `popover` | `components/ui/` |
| 3C (overlay) | `dialog`, `alert-dialog`, `sheet`, `command`, `scroll-area` | `components/ui/` |
| 7 (data) | `table`, `pagination`, `empty` | `components/ui/` |
| 5 (shell) | `sidebar`, `breadcrumb` | `components/ui/` |

## 7. Components that stay as Depsilo application components

Everything in the "Product component inventory" table in §2, plus the Admin and
Portal surface components. They are **moved** to `components/app/`,
`admin/components/`, and `portal/components/`, and re-implemented on top of
`components/ui/` — preserving their public props so that the 2,700+ call sites
in pages do not have to change shape at the same time as the primitive layer.

This is deliberate: a single PR-shaped change that rewrote both the primitive
layer *and* every page would be unreviewable, and would make a red intermediate
state unavoidable. Moving the implementation while holding the interface is an
in-place migration, not a compatibility adapter — no legacy code survives
behind it.

## 8. Dependency inventory

| Dependency | Used by | Disposition |
| --- | --- | --- |
| `@base-ui/react` | 6 files | **keep** — becomes `components/ui` only |
| `lucide-react` | `components/Icon.tsx` | **keep** — becomes the direct icon API |
| `clsx` + `tailwind-merge` | `lib/utils.ts` | **keep** — `cn()` is already the shadcn convention |
| `tailwindcss` + `@tailwindcss/vite` | global | **keep** |
| `@tanstack/react-query` | 31 files | keep |
| `i18next`, `react-i18next` | 52 files | keep |
| `react-router` | 20 files | keep |
| `axios` | 6 files | keep |
| `recharts` | 3 files | keep (Dashboard trends) |
| `simple-icons` | `EcosystemIcon` | keep |
| `@fontsource-variable/inter`, `inter-tight`, `jetbrains-mono` | `index.css` | keep the Latin subsets; `inter-tight` becomes unused once `--font-display` goes, delete then |
| `class-variance-authority` | — | **add** (required by shadcn) |
| `tw-animate-css` | — | **add** (required by shadcn Tailwind v4 init) |

No dependency in `web/package.json` is unused today. Phase 9 re-runs this
census after the migration and removes anything the migration orphaned.

## 9. Page inventory

| Surface | Route(s) | File | Lines | Complexity | Migration order |
| --- | --- | --- | --- | --- | --- |
| Admin → Settings | `/admin/settings` | `admin/pages/Settings.tsx` | 429 | medium | 6.1 |
| Admin → License | `/admin/license` | `admin/pages/License.tsx` | 443 | medium | 6.1 |
| Admin → Users | `/admin/users` | `admin/pages/Users.tsx` | 338 | medium | 6.2 |
| Admin → Login | `/admin/login` | `admin/pages/Login.tsx` | 152 | low | 6.2 |
| Admin → Projects | `/admin/projects` | `admin/pages/Projects.tsx` | 495 | medium | 6.3 |
| Admin → ConnectProject | `/admin/connect` | `admin/pages/ConnectProject.tsx` | 422 | medium | 6.3 |
| Admin → Upstreams | `/admin/upstreams` | `admin/pages/Upstreams.tsx` | 925 | high | 6.4 |
| Admin → UpstreamUpdates | `/admin/upstream-updates` | `admin/pages/UpstreamUpdates.tsx` | 571 | high | 6.4 |
| Admin → Cache | `/admin/cache` | `admin/pages/CacheManage.tsx` | 538 | high | 6.5 |
| Admin → Indexes | `/admin/indexes` | `admin/pages/CacheIndexes.tsx` | 297 | medium | 6.5 |
| Admin → Compiler Cache | `/admin/compile-cache` | `admin/pages/CompileCache.tsx` | 621 | high | 6.5 |
| Admin → Security | `/admin/security` | `admin/pages/Security.tsx` | 1,084 | very high | 6.6 |
| Admin → Quarantine | `/admin/quarantine` | `admin/pages/Quarantine.tsx` | 946 | very high | 6.6 |
| Admin → Rules | `/admin/rules` | `admin/pages/Rules.tsx` | 412 | high | 6.6 |
| Admin → Access Logs | `/admin/logs` | `admin/pages/AccessLogs.tsx` | 397 | high | 6.7 |
| Admin → Audit Logs | `/admin/audit` | `admin/pages/AuditLogs.tsx` | 366 | high | 6.7 |
| Admin → Attention | `/admin/attention` | `admin/pages/Attention.tsx` | 331 | medium | 6.8 |
| Admin → Dashboard | `/admin` | `admin/pages/Dashboard.tsx` | 310 | high | 6.8 |
| Admin → Bandwidth | `/admin/bandwidth` | `admin/pages/BandwidthReport.tsx` | 364 | high | 6.8 |
| Portal → QuickStart | `/` | `portal/pages/QuickStart.tsx` + 7 components | 1,306 | high | 8 |
| Portal → Monitor | `/monitor` | `portal/pages/Monitor.tsx` | 477 | high | 8 |
| Setup | first-run gate | `setup/SetupGate.tsx`, `setup/SetupWizard.tsx` | 832 | high | 8 |

## 10. Verification surface that encodes the current design

Four specs assert the *current* visual system rather than behaviour. They must
be re-pointed at the new contract, not deleted, so the behaviour they protect
survives.

| Spec | What it asserts today | Action |
| --- | --- | --- |
| `e2e/light-theme-canvas.spec.ts` | body is pure white; `--bg-page` is `#FFFFFF`; `.page-wash` grain opacity 0 / 0.07; dark body `rgb(11,13,15)` | rewrite as "light/dark resolve and the canvas has no decorative overlay"; drop literal hex and the `.page-wash` class |
| `e2e/admin-contrast.spec.ts` | `--btn` / `--btn-press` hover colour; `.recharts-*` classes | keep the hover-state behaviour; re-point to `--primary` / `--primary/90`; recharts assertions are library-owned, keep |
| `e2e/admin-recent-downloads.spec.ts` | `.live-download-flow/row/pulse` animation names | re-point to `data-*` hooks on the rewritten component |
| `e2e/admin-shell.spec.ts` | `#root > .page-wash` exists exactly once, never inside the Admin shell | re-point to `data-app-overlay` |

Everything else in the suite is already behaviour-based: `getByRole` ×636,
`getByText` ×173, `getByLabel` ×97, `data-*` hooks. That is the main reason
this migration is tractable at all.

### Repo documentation coupled to the current UI

| Document | Coupling | Action |
| --- | --- | --- |
| `DESIGN.md` | declares itself the current UI contract and the source of truth for tokens, shell, and components; explicitly says "Do not use … shadcn components" | must be rewritten in the same change as the code (root `AGENTS.md`: "When sources conflict, report the conflict") |
| `web/AGENTS.md` | points at `DESIGN.md` and `src/index.css` as truth | update the pointer |
| `docs/development/change-map.md` | "Shared UI primitive/theme → `web/src/components/`, `web/src/index.css`" | update paths |

`DESIGN.md` currently states *"Do not use the old purple/OKLCH examples,
`/status` route, shadcn components, `CardV2`, or `MetricCardV2`."* This brief
supersedes that sentence. The conflict is recorded here and resolved by the
`DESIGN.md` rewrite at the end of Stage A.

## 11. Risks

| # | Risk | Severity | Mitigation |
| --- | --- | --- | --- |
| R1 | `index.css` is a single 1,427-line file; shadcn `init` wants to own it | High | Let shadcn write a fresh `index.css`, then re-add the `@font-face` block and a **time-boxed** alias block for legacy variable names still in use. Aliases are deleted in Phase 10 — they are not an adapter layer, they are the migration window. |
| R2 | 462 inline `var(--…)` style objects across 66 files | High | Migrate per surface in Phases 5–8; a `rg "var\(--"` census after each phase is the exit condition. |
| R3 | Admin intentionally forked the palette via `admin-shell.css` | Medium | Delete the fork; a single semantic token set drives both surfaces in Stage A. If Admin needs density differences, they come from *layout* utilities, not a second palette. |
| R4 | `e2e/admin-axe.spec.ts` asserts **zero** non-zero `letter-spacing` on every visible element under `#root`, and `documentElement.scrollWidth === viewport` | High | Never emit `tracking-*` utilities; shadcn defaults such as `tracking-tight` on card titles must be stripped for this repo. Keep the assertion, keep the invariant. |
| R5 | `e2e/admin-axe.spec.ts` asserts every `[data-icon-button]` is ≥40×40 | High | The rewritten icon-button helper must keep `size-10` (40px) and set `data-icon-button`. |
| R6 | `admin-contrast.spec.ts` requires the primary button hover colour to equal `--primary` at rest → `--primary/90` on hover, with `filter: none` | Medium | Encode that mapping in `ui/button` and keep the token names. |
| R7 | 4,700 lines of Admin page code references `--btn`, `--brand-*`, `--ok-*`, `--warn-*`, `--danger-*` | High | The alias block (R1) keeps them resolvable during the sweep; the Phase 10 census proves they are gone. |
| R8 | shadcn `select`/`popover`/`dropdown-menu` use Base UI portals with their own z-index and focus handling; the Admin shell has a fixed topbar (`z-20`) and sidebar (`z-30`), dialogs `z-50`, tooltips `z-80`, toasts `z-90` | Medium | Establish a single z-index scale in Phase 2 and verify with the existing `admin-dialog-actions` / `admin-shell` specs. |
| R9 | `SetupWizard` submits a live form and must not lose focus or field state during migration | Medium | Migrate Setup last (Phase 8); `auth-setup-state.spec.ts` (221 lines) is the guard. |
| R10 | Bundle budget is enforced (`initial ≤ 650 kB`, `entry ≤ 450 kB`, `chunk ≤ 500 kB`) | Medium | Base UI is already a dependency. Adding `cva` + `tw-animate-css` is small; the real risk is losing the 4 lazy seams (`PortalApp-`, `SetupWizard-`, `AdminApp-`, `AdminShell-`). Keep them. |
| R11 | `admin-shell.spec.ts`, `portal-redesign.spec.ts` and friends assert layout geometry | Medium | Run the full `make test-ui` after each phase, not just at the end. |
| R12 | Two locales with 1,644 keys each; every new user-visible string needs both | Low | No new copy is introduced in Stage A beyond placeholder text already keyed. `make lint-i18n` after each phase. |

## 12. What happened next

This audit fed a phased plan: initialize shadcn/ui, establish one neutral token
layer, migrate the primitives in three batches by risk, unify forms, rebuild the
Admin shell, then the pages, the tables, the Portal and Setup, and finish with
the legacy and CSS cleanup. The shell moved before the pages because every
page's layout is expressed relative to it, and Setup moved last because it owns
a live multi-step submission.

All of it shipped. The plan itself is in git history rather than the working
tree, per [docs/README.md](../README.md); the architecture it produced is
described in [DESIGN.md](../../DESIGN.md).
