# Depsilo UI migration plan — shadcn/ui + Base UI (Stage A)

> Companion to [shadcn-ui-audit.md](shadcn-ui-audit.md). The audit holds the
> inventory and evidence; this document holds the executable plan.
>
> **Stage A is an architecture migration, not a redesign.** Visual quality is
> explicitly not a goal. The exit condition is a clean, single-source UI
> architecture with correct behaviour, accessibility, responsiveness, theming,
> and build health. Stage B is a separate, later engagement.

## Execution status

Updated 2026-09-17. Every phase below is either done or explicitly outstanding;
nothing is half-applied.

| Phase | Status | Evidence |
| --- | --- | --- |
| 0 Audit | done | `shadcn-ui-audit.md` |
| 1 Initialize shadcn/ui | done | `components.json`, `components/ui/`, `class-variance-authority` + `tw-animate-css` |
| 2 Neutral theme | done | `index.css` is one `:root` + one `.dark` + `@theme inline` + base layer |
| 3A Core primitives | done | `components/app/{button,icon-button,icon-button-control,badge,input,textarea,tooltip,field,notice,error-state,empty-state,section-header,metric,status-dot,logo}` |
| 3B Form controls | done | `components/app/{switch,tabs,checkbox,select}` |
| 3C Overlays | done | `components/app/{modal,drawer,toast}` over `ui/{dialog,sheet,toast}` |
| 4 Form unification | partly done | `Field` + `SettingRow`/`SettingSection` not yet extracted; pages still compose some rows locally |
| 5 Admin shell | done | `admin/components/{MainLayout,AdminPage}`; `admin-shell.css` deleted |
| 6 Admin pages | outstanding | pages compose `components/app` but still carry inline token-reading style objects |
| 7 Data tables | outstanding | `app/data-table` + `app/table-viewport` moved; not yet re-based on `ui/table` |
| 8 Portal + Setup | outstanding | Portal still owns `.portal-*` classes and inline token styles |
| 9 Legacy cleanup | partly done | legacy primitive layer deleted; `app/icon.tsx` still maps domain names onto Lucide |
| 10 CSS cleanup | partly done | the alias block and class rules are shrinking as owners migrate; they are not yet empty |
| 11 Docs + verification | done for what has landed | `DESIGN.md` rewritten; `make check` green |

The mechanical half of the token migration is complete: no
`utility-[var(--token)]` arbitrary values remain. What remains is the 316
inline `style` objects that read a token per property — those need per-component
restructuring, which is the work of phases 6 and 8.

## Recorded deviations

These are deliberate and were taken with the brief's own rules in hand. Each is
reversible and each is recorded rather than hidden.

| # | Brief item | Decision | Reasoning |
| --- | --- | --- | --- |
| D1 | Adopt shadcn `select` (Base UI) | Kept a native `<select>` styled on the semantic tokens | The product uses Select in dense filter bars and enum fields across 65 call sites. A scripted popup would trade OS typeahead, the platform picker on touch, and the `combobox` + value contract for styling the neutral Stage A theme does not need, and would rewrite ~30 spec interactions for no product gain. `ui/select.tsx` is therefore not added. |
| D2 | Adopt shadcn `sidebar` | Kept the Admin shell as a product component | The Admin rail owns a version pill, an instance-management link, a user footer, and a breadcrumb bar, and its geometry is asserted by `admin-shell.spec.ts`. shadcn's sidebar adds a cookie-persisted collapse state and its own mobile sheet — new behaviour, not a cleaner architecture. |
| D3 | Adopt `alert-dialog` | Confirmation dialogs use `ui/dialog` | The product's confirmations are already `role="dialog"` and a large part of the Playwright suite selects them that way. A second overlay primitive would be a behavioural change with no architectural gain. |
| D4 | Adopt `command`, `popover`, `dropdown-menu`, `scroll-area`, `radio` | Not added | No call site exists. Adding them would create unused surface the brief explicitly warns against. |
| D5 | Preserve the existing brand mark | `app/logo.tsx` is a neutral placeholder | The brief lists the current logo under what may be discarded and reserves identity for Stage B. |

## Target stack

| Concern | Choice |
| --- | --- |
| Framework | React 19 + TypeScript + Vite (unchanged) |
| Styling | Tailwind CSS v4 via `@tailwindcss/vite` (unchanged) |
| Component layer | shadcn/ui, `base` base (Base UI) |
| Primitive base | `@base-ui/react` |
| Icons | `lucide-react`, imported directly |
| Class composition | `cn()` from `@/lib/utils` |
| Variants | `class-variance-authority` |

Explicitly **not** used: Radix UI, React Aria, MUI, Ant Design, Chakra,
Mantine, DaisyUI.

## Target architecture

```text
Page  (admin/pages/*, portal/pages/*, setup/*)
  ↓
Feature component  (admin/components/*, portal/components/*)
  ↓
Application component  (components/app/*)
  ↓
UI primitive  (components/ui/*)
  ↓
Base UI / native element
```

Rules enforced at the end of Stage A:

1. `components/ui/` contains only generic primitives. No product logic.
2. `components/app/` contains Depsilo-wide reusable components. No route data.
3. `@base-ui/react` is importable **only** from `components/ui/`.
4. No page imports `lucide-react`, `components/ui`, or Base UI transitively
   through a second styling system; there is exactly one button, one input, one
   dialog implementation in the repo.
5. `web/src/index.css` holds imports, tokens, the dark variant, base styles,
   and font setup. It holds no component styling.
6. One token cascade. Light is the `:root` default; `.dark` overrides it. The
   Admin surface no longer forks the palette.

## Directory layout after Stage A

```text
web/src/
  components/
    ui/          button input textarea select checkbox switch badge
                 dialog alert-dialog sheet popover dropdown-menu command
                 tooltip tabs table pagination card separator skeleton
                 scroll-area label sidebar breadcrumb field empty sonner
    app/         page-header page-section metric status-dot status-badge
                 empty-state error-state notice confirm-dialog
                 data-table data-table-toolbar data-table-pagination
                 data-table-column-header copy-button code-block
                 ecosystem-icon upstream-panel logo theme-toggle
                 language-toggle search-field
  admin/
    components/  admin-shell admin-page now-strip trends-card
                 recent-downloads dashboard-attention stale-data-notice
                 pro-required-callout webhook-tab
    pages/       (19 pages, unchanged route ownership)
  portal/
    components/  configure-pane ecosystem-catalog code-block copy-button
                 hero-ai-cta pytorch-index-notice compile-cache-intro
                 ecosystem-logo-wall
    pages/       quick-start monitor
  setup/         setup-gate setup-wizard
```

## Migration matrix

`Type`: P = primitive, A = application component, F = feature component,
S = shell/layout, C = CSS/token, D = dependency, T = test.

| Current | Location | Type | Replacement | Delete? | Risk | Phase |
| --- | --- | --- | --- | --- | --- | --- |
| `Button` | `components/Button.tsx` | P | `ui/button` (cva: default/secondary/ghost/destructive/outline) | yes | M | 3A |
| `IconButton` | `components/IconButton.tsx` | P | `ui/button` `size="icon"` + `ui/tooltip` | yes | M | 3A |
| `IconButtonControl` | `components/IconButtonControl.tsx` | P | `ui/button` `size="icon"` | yes | L | 3A |
| `Badge` | `components/Badge.tsx` | P | `ui/badge` + `app/status-badge` | yes | L | 3A |
| `Input` | `components/Input.tsx` | P | `ui/input` + `app/field` | yes | M | 3A |
| `Textarea` | `components/Textarea.tsx` | P | `ui/textarea` + `app/field` | yes | L | 3A |
| `Tooltip` | `components/Tooltip.tsx` | P | `ui/tooltip` (Base UI) | yes | M | 3A |
| `fieldFeedback` | `components/fieldFeedback.ts` | P | inside `app/field` | yes | L | 3A |
| `Segmented` | `components/Segmented.tsx` | P | — (dead code) | yes | L | 3A |
| `Switch` | `components/Switch.tsx` | P | `ui/switch` (Base UI) | yes | M | 3B |
| `Select` | `components/Select.tsx` | P | `ui/select` (Base UI) + `app/field` | yes | H | 3B |
| `Tabs` | `components/Tabs.tsx` | P | `ui/tabs` (Base UI) | yes | M | 3B |
| `Modal` | `components/Modal.tsx` | P | `ui/dialog` (Base UI) | yes | H | 3C |
| `Drawer` | `components/Drawer.tsx` | P | `ui/sheet` (Base UI) | yes | H | 3C |
| `Toast` | `components/Toast.tsx` | P | `ui/toast` over Base UI toast, `useToast` API preserved | yes | H | 3C |
| `DataTable` | `components/DataTable.tsx` | P | `ui/table` + `app/data-table` | yes | M | 7 |
| `TableViewport` | `components/TableViewport.tsx` | P | `app/data-table` viewport | yes | L | 7 |
| `Icon` | `components/Icon.tsx` | P | direct `lucide-react` imports | yes | M | 9 |
| `Metric` | `components/Metric.tsx` | A | `app/metric` | move | L | 3A |
| `StatusDot` | `components/StatusDot.tsx` | A | `app/status-dot` | move | L | 3A |
| `SectionHeader` | `components/SectionHeader.tsx` | A | `app/section-header` | move | L | 3A |
| `EmptyState` | `components/EmptyState.tsx` | A | `app/empty-state` | move | L | 3A |
| `QueryErrorState` | `components/QueryErrorState.tsx` | A | `app/error-state` | move | L | 3A |
| `InlineNotice` | `components/InlineNotice.tsx` | A | `app/notice` | move | L | 3A |
| `Logo` | `components/Logo.tsx` | A | `app/logo` (placeholder mark) | move | L | 3A |
| `ThemeToggle` | `components/ThemeToggle.tsx` | A | `app/theme-toggle` | move | M | 3A |
| `LangToggle` | `components/LangToggle.tsx` | A | `app/language-toggle` | move | M | 3A |
| `EcosystemIcon` | `components/EcosystemIcon.tsx` | A | `app/ecosystem-icon` | move | L | 3A |
| `UpstreamCard` | `components/UpstreamCard.tsx` | A | `app/upstream-panel` | move | M | 7 |
| `CodeBlock` | `portal/components/CodeBlock.tsx` | A | `portal/components/code-block` | move | M | 8 |
| `CopyButton` | `portal/components/CopyButton.tsx` | A | `portal/components/copy-button` | move | L | 8 |
| `MainLayout` | `admin/components/MainLayout.tsx` | S | `admin/components/admin-shell` + `ui/sidebar` | yes | H | 5 |
| `AdminPage` | `admin/components/AdminPage.tsx` | S | `admin/components/admin-page` | yes | M | 5 |
| `AdminLocalNav` | `admin/components/AdminLocalNav.tsx` | S | folded into `admin-page` | yes | L | 5 |
| `AdminPagination` | `admin/components/AdminPagination.tsx` | A | `app/data-table-pagination` | yes | M | 7 |
| `ConfirmActionDialog` | `admin/components/ConfirmActionDialog.tsx` | A | `app/confirm-dialog` on `ui/alert-dialog` | yes | M | 4 |
| `NowStrip` | `admin/components/NowStrip.tsx` | F | rewritten on tokens + `app/*` | keep | H | 6.8 |
| `TrendsCard` | `admin/components/TrendsCard.tsx` | F | rewritten on tokens + `app/*` | keep | H | 6.8 |
| `RecentDownloads` | `admin/components/RecentDownloads.tsx` | F | rewritten; `data-*` animation hooks | keep | H | 6.8 |
| `DashboardAttention` | `admin/components/DashboardAttention.tsx` | F | rewritten | keep | M | 6.8 |
| `StaleDataNotice` | `admin/components/StaleDataNotice.tsx` | F | rewritten | keep | L | 3A |
| `ProRequiredCallout` | `admin/components/ProRequiredCallout.tsx` | F | rewritten | keep | L | 6.3 |
| `WebhookTab` | `admin/components/WebhookTab.tsx` | F | rewritten | keep | M | 6.4 |
| 19 Admin pages | `admin/pages/*.tsx` | F | swept onto tokens + `app/*` | keep | H | 6 |
| 8 Portal components | `portal/components/*.tsx` | F | swept onto tokens + `app/*` | keep | H | 8 |
| `SetupWizard` / `SetupGate` | `setup/*.tsx` | F | swept; form contract preserved | keep | H | 8 |
| 57 hand-written classes | `index.css` | C | Tailwind utilities on semantic tokens; `data-*` where tests assert semantics | yes | H | 2, 10 |
| Legacy token cascade ×3 | `index.css` | C | one `:root` + one `.dark` | yes | H | 2 |
| `--brand*`, `--spec-*`, `--grad-*`, `--aurora*`, `--grid`, `--glow` | `index.css` | C | removed outright | yes | M | 2 |
| Admin palette fork | `admin/admin-shell.css` | C | removed; single token set | yes | M | 5 |
| `--code-*` block | `index.css` | C | `--card` / `--muted` / `--foreground` | yes | L | 10 |
| `--r-*`, `--radius-*` | `index.css` | C | `--radius` (+ shadcn derived radii) | yes | L | 2 |
| `.page-wash` overlay | `App.tsx` + `index.css` | C | removed; overlay hook becomes `data-app-overlay` | yes | M | 2 |
| `.live-download-*` animations | `index.css` + `RecentDownloads` | C | Tailwind keyframes + `data-live-flow` | yes | M | 6.8 |
| `.eco-tile`, `.manager-*`, `.code-*` | `index.css` + portal | C | Tailwind utilities | yes | M | 8 |
| `.portal-header-*` (22 classes) | `index.css` + `PortalApp` | C | Tailwind utilities | yes | H | 8 |
| `--btn` / `--btn-press` contract | `admin-contrast.spec.ts` | T | re-pointed to `--primary` / `--primary/90` | rewrite | M | 11 |
| `.page-wash` assertion | `light-theme-canvas.spec.ts`, `admin-shell.spec.ts` | T | `data-app-overlay` | rewrite | L | 11 |
| `.live-download-*` assertions | `admin-recent-downloads.spec.ts` | T | `data-live-flow` / `data-live-row` | rewrite | L | 11 |
| `DESIGN.md` Instrument contract | repo root | T | rewritten for the shadcn architecture | rewrite | M | 11 |
| `docs/development/change-map.md` UI row | docs | T | paths updated | edit | L | 11 |

## Phase details

### Phase 1 — Initialize shadcn/ui

```bash
cd web
npx shadcn@latest info
npx shadcn@latest init --base base --template vite --css-variables --pointer --yes
```

Expected result: `components.json`, `src/lib/utils.ts` (existing `cn` retained,
`formatBytes`/`formatTime`/`formatVersion`/`formatDate` preserved),
`src/components/ui/*` seed, `src/index.css` rewritten by the CLI,
`class-variance-authority` + `tw-animate-css` added.

Exit gate: `npm run build` succeeds; `git diff` shows no lost helper.

### Phase 2 — Neutral theme, single cascade

1. Restore the three `@font-face` blocks (Latin subsets only).
2. Keep the CLI's `@theme inline` / `:root` / `.dark` structure. Set the base
   colour to `neutral`.
3. Add the tokens the product needs beyond the defaults: `--success`,
   `--warning`, `--info` (+ `-foreground`), `--sidebar*`, `--radius`.
4. Delete the legacy `:root`, `[data-theme="dark"]`, and duplicated
   `@media (prefers-color-scheme: dark)` cascades.
5. Add a **temporary, time-boxed alias block** mapping the legacy variable
   names still referenced (`--text`, `--text-soft`, `--bg-*`, `--brand*`,
   `--ok*`, `--warn*`, `--danger*`, `--btn*`, `--admin-*`, `--focus-ring`,
   `--r-*`, `--shadow-*`, `--code-*`) onto the new semantic tokens. This block
   is deleted in Phase 10. It exists so each phase can end green; it adds no
   logic.
6. Define one z-index scale: `base 0`, `topbar 20`, `sidebar 30`, `overlay 50`,
   `tooltip 80`, `toast 90`.
7. Keep `prefers-reduced-motion` handling.

Exit gate: light and dark both render; `npm run build`; `make test-ui -- admin-smoke`.

### Phase 3 — Primitive migration (3 batches)

For each primitive: add the shadcn component → find all legacy usages →
replace → fix TypeScript → verify interaction → delete the old implementation →
delete its old CSS → run the focused spec.

| Batch | Components | Focused proof |
| --- | --- | --- |
| 3A | button, badge, input, textarea, separator, skeleton, card, tooltip, label | `admin-forms`, `admin-dialog-actions` |
| 3B | checkbox, switch, select, tabs, dropdown-menu, popover | `admin-settings`, `admin-tables-actions` |
| 3C | dialog, alert-dialog, sheet, command, scroll-area | `admin-dialog-actions`, `admin-shell` |

Also create `components/app/*` for the moved application components listed in
the matrix, and update every import site mechanically.

Exit gate: `npm run type-check && npm run lint && npm run test:unit && make test-ui`.

### Phase 4 — Form unification

Build `app/field` (label + description + error + `aria-describedby` merge),
`app/setting-row`, `app/setting-section`, and `app/confirm-dialog`. Replace
every page-local `label` + control + hint + error composition.

Exit gate: `admin-forms.spec.ts`, `admin-settings.spec.ts`,
`admin-dialog-actions.spec.ts`.

### Phase 5 — Admin shell

Rebuild `MainLayout` on `ui/sidebar` + `ui/breadcrumb` + `ui/button` +
`ui/sheet`. Delete `admin-shell.css` and the Admin palette fork. Keep the
`data-admin-*` hooks the Playwright suite depends on.

Exit gate: `admin-shell.spec.ts`, `admin-axe.spec.ts`, `admin-page-layout.spec.ts`,
`admin-responsive-grids.spec.ts`.

### Phase 6 — Admin pages (8 sub-steps)

`Settings`+`License` → `Users`+`Login` → `Projects`+`ConnectProject` →
`Upstreams`+`UpstreamUpdates` → `Cache`+`Indexes`+`CompileCache` →
`Security`+`Quarantine`+`Rules` → `AccessLogs`+`AuditLogs` →
`Attention`+`Dashboard`+`Bandwidth`.

Each step: replace `style={{ color: 'var(--…)' }}` with Tailwind classes on
semantic tokens, replace page-local controls with `ui/*`/`app/*`, delete the
now-unused legacy CSS, and run that page's spec.

### Phase 7 — Data tables

`app/data-table`, `data-table-toolbar`, `data-table-pagination`,
`data-table-column-header`, `data-table-empty` on `ui/table` +
`ui/pagination`. Adopt in Requests/AccessLogs, AuditLogs, Upstreams,
UpstreamUpdates, Users, Quarantine, Rules.

Only abstractions that are actually reused are extracted. No universal
framework.

### Phase 8 — Portal and Setup

Portal shell → QuickStart (`ConfigurePane`, `EcosystemCatalog`, `CodeBlock`,
`CopyButton`) → Monitor (`UpstreamCard`/`app/upstream-panel`) → SetupWizard.

Portal keeps its own layout; `components/ui` and `components/app` are shared.

### Phase 9 — Legacy cleanup

Delete `components/*` legacy files, `admin-shell.css`, `Icon.tsx` (after the
direct-lucide migration), and run the dependency census.

Proof commands:

```bash
rg "@base-ui/react" web/src --glob '!web/src/components/ui/**'
rg "components/(Button|Input|Textarea|Select|Badge|Switch|Tooltip|Tabs|Modal|Drawer|Toast|DataTable|Icon|Segmented)" web/src
rg "node_modules" web/package.json
npm --prefix web ls --depth=0
```

### Phase 10 — CSS cleanup

`index.css` ends as: Tailwind import, shadcn import, `@custom-variant dark`,
one `:root`, one `.dark`, `@theme inline`, `@layer base` (body, focus,
scrollbar, reduced motion), and the three `@font-face` blocks. The alias block
is deleted.

Proof:

```bash
rg "var\(--(bg|text|brand|ok|warn|danger|btn|admin|r-|shadow-|code-)" web/src
rg "(app-|portal-|admin-|live-download-|eco-tile|manager-|code-block|page-wash|stripe-focus-ring)" web/src
```

Both must return nothing (outside `data-*` attributes and test hooks).

### Phase 11 — Docs and verification

Rewrite `DESIGN.md` for the new architecture; update
`docs/development/change-map.md` UI paths and `web/AGENTS.md`.

```bash
make lint
make verify-web
make verify-build
make test-ui
make verify
```

## Verification plan

| Phase | Minimum checks |
| --- | --- |
| 1–2 | `npm run type-check`, `npm run lint`, `npm run build`, `make test-ui -- admin-smoke` |
| 3 | above + `npm run test:unit` + focused specs per batch |
| 4 | above + `admin-forms`, `admin-settings`, `admin-dialog-actions` |
| 5 | above + `admin-shell`, `admin-axe`, `admin-page-layout` |
| 6 | above + the owning spec per page, then full `make test-ui` |
| 7 | above + `admin-tables-actions` |
| 8 | above + `portal-*`, `auth-setup-state` |
| 9–10 | `make check` |
| 11 | `make verify` |

Cannot-run checks must be reported with the reason (network, Docker, or
missing client tooling).

## Commit strategy

Stage A commits, one phase per commit, never mixing migration and redesign:

```text
docs(ui): add shadcn migration audit and plan
refactor(ui): initialize shadcn ui
refactor(ui): establish semantic theme tokens
refactor(ui): migrate core primitives
refactor(ui): migrate form controls
refactor(ui): migrate overlay components
refactor(ui): unify form composition
refactor(ui): migrate admin shell
refactor(ui): migrate admin pages
refactor(ui): migrate data tables
refactor(ui): migrate portal and setup
refactor(ui): remove legacy ui system
refactor(ui): clean frontend dependencies
docs(ui): record the shadcn architecture
```

Stage B commits are separate and may not appear before Stage A is complete:

```text
design(ui): establish new design tokens
design(ui): redesign application shell
design(ui): redesign data tables
design(ui): redesign dashboard
design(ui): redesign admin pages
design(ui): redesign portal
design(brand): introduce new depsilo identity
```

## Conflict register

| # | Conflict | Resolution |
| --- | --- | --- |
| C1 | `DESIGN.md` says "Do not use … shadcn components" and declares Instrument the active visual administration. | This brief supersedes it. `DESIGN.md` is rewritten in Phase 11 to describe the shadcn architecture, and the Instrument section is replaced by a Stage A placeholder note. |
| C2 | `DESIGN.md` and `admin-shell.css` establish a separate Admin palette; the brief requires one semantic token set. | The Admin fork is deleted in Phase 5. Admin/Portal differ by layout and density, not by palette. |
| C3 | `e2e/light-theme-canvas.spec.ts` asserts `--bg-page === #FFFFFF` and a `.page-wash` grain layer; both are legacy visual artefacts the brief discards. | Rewritten in Phase 11 to assert theme resolution and the absence of decorative overlay, not literal hex. |
| C4 | `make check` includes `test-ui`, so the Playwright visual-contract specs are part of the normal gate. | Visual-contract specs are re-pointed in the same phase as the code they assert, so the gate never stays red across a phase boundary. |

## Explicit non-goals for Stage A

No new brand colour, logo, dashboard style, typography scale, special shadows,
gradients, or advanced visual effects. No page-level redesign. No new product
copy. No change to backend behaviour, routing, API contracts, or the Admin
control-plane authority model.
