# Depsilo Design System

> Status: current implementation reference, rewritten 2026-09-18 at the end of
> Stage B. The source of truth is `web/src/index.css`,
> `web/src/components/ui/`, `web/src/components/app/`, `web/src/admin/`, and
> `web/src/portal/`. When this document and the code disagree, fix the
> document in the same change.
>
> This document says what the system *is*: its layers, token roles, component
> inventory, states, and the invariants the suite encodes. It is also the
> contract a change is measured against.
>
> Which document is authoritative for what:
>
> | Fact | Home |
> | --- | --- |
> | The system as it stands (this document) | `DESIGN.md` |
> | Why the interface serves what it serves | [product-ui-principles.md](docs/design/product-ui-principles.md) |
> | Why it looks like this, and what it refuses | [visual-direction.md](docs/design/visual-direction.md) |
> | Every value: colour, type, space, radius, elevation, motion, charts, status | [design-tokens.md](docs/design/design-tokens.md) |
> | How components compose, and what must never be built | [component-guidelines.md](docs/design/component-guidelines.md) |
> | The mark and the palette it fixes | [docs/brand/](docs/brand/README.md) |
>
> [docs/design/README.md](docs/design/README.md) is the index to that set. The
> values live in `web/src/index.css`; a document that disagrees with it is the
> thing to correct.

## Product Surfaces

| Surface | Routes | Purpose |
| --- | --- | --- |
| Portal | `/`, `/monitor` | Package-manager setup and public service health |
| Admin | `/admin/*` | First-project connection plus repeated operational work: cache, upstreams, logs, policy, users, settings |
| Setup | first-run gate | Secure administrator creation with defaulted service configuration |

The Portal is not a marketing landing page. Quick Start is the first screen;
Monitor is the second. Admin is dense and optimised for scanning and repeated
actions. What each surface must prove in its first viewport is in
[product-ui-principles.md §5](docs/design/product-ui-principles.md#5-what-each-surface-must-prove).

## Architecture

```text
Page                admin/pages/*, portal/pages/*, setup/*
  ↓
Feature component   admin/components/*, portal/components/*
  ↓
Application component   components/app/*
  ↓
UI primitive        components/ui/*
  ↓
Base UI / native element
```

Rules:

1. `components/ui/` holds generic shadcn primitives only. No product logic.
2. `components/app/` holds Depsilo-wide reusable components. No route data.
3. `@base-ui/react` may be imported **only** from `components/ui/`. No other
   file in `src/` imports it.
4. Pages do not import `components/ui` or `@base-ui/react` directly. They
   compose feature and application components.
5. There is exactly one implementation of each control. A page-local button,
   input, dialog, or badge is a defect.
6. `index.css` holds imports, tokens, the dark variant, and base styles. It
   holds no component styling.

`components.json` records the shadcn configuration: `base-nova` style, Base UI
base, neutral base colour, CSS variables, `@/components/ui` alias, Lucide
icons. Add primitives with `npx shadcn@latest add <name>` and only when an
owning call site exists.

## Token Layer

Tokens live in `web/src/index.css` and are defined once:

| Block | Role |
| --- | --- |
| `:root` | Light values |
| `.dark` | Dark overrides |
| `@theme inline` | Exposes the tokens as Tailwind utilities (`bg-background`, `text-muted-foreground`, `border-border`, `rounded-lg`, …) |

`index.html` and `lib/theme.ts` put `light` or `dark` on `<html>` before first
paint, so `@custom-variant dark (&:is(.dark *))` is the single dark switch.
There is no third `prefers-color-scheme` copy of the palette.

### Roles

The full value table for every role — and the floors each pair must clear — is
[design-tokens.md §2](docs/design/design-tokens.md#2-colour). This table is the
map: what each role is *for*.

| Token | Meaning |
| --- | --- |
| `background` / `foreground` | Page canvas and primary text |
| `card` / `popover` | Raised surfaces |
| `primary` | Commands, links, focus, active navigation. The brand's blue |
| `secondary` / `muted` | Quiet fills and insets |
| `accent` | Hover and selected surfaces, tinted with the brand blue so "selected" never reads as "a shade darker" |
| `destructive` | Real failures, explicit refusals, destructive commands |
| `success` | Cache hits, healthy state, completed work. The brand kit's cache green family |
| `warning` | Degraded or partially completed work |
| `info` | Neutral operational signal (live activity) |
| `border` / `input` / `ring` | Keylines, control outlines, focus |
| `sidebar*` | The Admin navigation rail |
| `radius-control` / `radius-surface` | Two radii and no third: `rounded-sm`, `-md`, and `-lg` are the control radius; `-xl` is the surface radius |
| `shadow-surface` / `-card` / `-pop` | Three elevation levels. `-pop` is the only one used over content (tooltips, an ink code block) |
| `code-surface*` | The one deliberate dark surface in both themes: a command a reader is meant to paste |
| `chart-*` | Series, grid, and axis label colours. Charts are part of the system, not a page-local palette |
| Type scale | Nine roles (`micro` … `page-title`) plus the two metric sizes and the 16px `field` floor, each carrying its own line height. See [design-tokens.md](docs/design/design-tokens.md#3-typography) |

`success`, `warning`, and `info` exist because Depsilo reports cache, delivery,
and policy outcomes separately. A cache miss and a slow upstream are normal
results and must never render as `destructive`. See "State Semantics".

### Deliberate constraints

- **No letter spacing anywhere, in either direction.** CJK glyphs have no
  sidebearings to absorb it, and `e2e/fixtures/a11y.ts` asserts that every
  visible element resolves to zero letter spacing on every surface. Do not add
  `tracking-*` utilities.
- **The root font size is the browser default.** The product's base text size
  lives on `body`. Before the migration `html` carried `font-size: 13px` while
  some spacing tokens were pinned statically, so half the interface was scaled
  by 13/16 and half was not.
- **The document never scrolls horizontally.** Wide tables scroll inside their
  own focusable region.

### Admin vs Portal

Admin and Portal share one token set. They differ by layout and density, never
by a second palette. The Admin shell is not allowed to redefine the semantic
tokens.

## Typography And Icons

- UI: `Inter Variable` for Latin; Chinese falls through to
  `PingFang SC` / `HarmonyOS Sans SC` / `MiSans` / `Microsoft YaHei`.
- Code and data: `JetBrains Mono Variable` with tabular numerals.
- Icons: Lucide, imported directly from `lucide-react`. `components/app/icon.tsx`
  is only a sizing box for the places where the glyph is chosen at runtime (a
  tone map, a prop, a ternary), because a JSX element name cannot be an
  expression. It is not a name registry: there is no string vocabulary for
  icons, so a missing glyph is a type error rather than a runtime fallback.
- Metric values, versions, bytes, and latency use the mono/tabular treatment so
  changing digits cannot move layout.
- Nine type roles and four weights (400/500/600/700). A size or a weight that
  sits between two steps is drift: it reads as inconsistency rather than
  nuance. Sizes are px so a dense operator table cannot reflow when a reader
  changes their browser's base font size; the single exception is the 16px
  field floor that stops mobile Safari from zooming a focused input.

## Shared Components

`components/ui/` — generic primitives, shadcn-generated:

`button`, `badge`, `input`, `textarea`, `label`, `checkbox`, `switch`, `tabs`,
`separator`, `skeleton`, `card`, `tooltip`, `dialog`, `sheet`, `toast`.

`components/app/` — Depsilo application components:

`button`, `icon-button`, `icon-button-control`, `input`, `textarea`, `select`,
`checkbox`, `switch`, `tabs`, `field`, `badge`, `notice`, `error-state`,
`empty-state`, `section-header`, `metric`, `status-dot`, `modal`, `drawer`,
`tooltip`, `toast`, `confirm-dialog` (via `Modal`), `logo`, `theme-toggle`,
`language-toggle`, `ecosystem-icon`, `icon` (runtime-glyph sizing box),
`upstream-panel`, `data-table`, `table-viewport`, `code-block`, `copy-button`.

Use these before adding a primitive. Admin-specific composition belongs in
`admin/components/`; Portal-specific composition belongs in
`portal/components/`.

### Interface notes

- `Button` maps `primary`/`secondary`/`ghost`/`danger` and `sm`/`md` onto the
  shadcn variants. It does not default `type`, so a button inside a form keeps
  the browser's submit default.
- `Field` owns label + control + message composition and the
  `aria-describedby` merge (`lib/aria.ts`). An error replaces the hint rather
  than stacking a second line.
- `Input`, `Textarea`, and `Select` use 16px text below `md` so a focused field
  cannot trigger mobile zoom.
- `Select` is a **native** `<select>`. The product uses it for dense filter
  bars and enum fields, where OS typeahead, the platform picker on touch
  devices, and the `combobox` + value contract matter more than a styled
  popup. shadcn's Base UI select is deliberately not adopted.
- `IconButton` and `IconButtonControl` set `data-icon-button` and keep a
  40×40 target in every state, including pending. The shared contract in
  `e2e/fixtures/a11y.ts` measures both on every surface.
- `CodeBlock` holds its own copy action, because the action's accessible name
  has to name *that* block; `CopyButton` is the one copy control, with a
  labelled, an inline, and an icon-only presentation. Admin onboarding and the
  Portal share both, so a copied command cannot behave two ways.
- `Modal` keeps `closeDisabled` for mutations in flight: the dialog cannot be
  dismissed while a change is half-applied.
- The Admin shell is a product component, not shadcn's `sidebar` primitive; it
  keeps a 232px rail, a 48px topbar, and the `data-admin-*` hooks the
  Playwright suite asserts.
- `Logo` renders the brand kit's flat mark. It carries its own colours rather
  than `currentColor`: the kit fixes them, they read on both canvases, and a
  mark that inherits the surrounding text colour is a status icon wearing the
  brand's shape. The favicon and the desktop icon are the same geometry.

## State Semantics

The full vocabulary — the dimensions, the tones, severity, health, and the
one-signal-per-row rule — is
[design-tokens.md §4](docs/design/design-tokens.md#4-status-system). Keep these
dimensions separate when they appear together in Admin:

| Dimension | Normal result | Attention result |
| --- | --- | --- |
| Cache result | `hit` uses success; `miss` is neutral because the request was fetched; `unknown` is neutral and means the result was not recorded | Do not turn a miss or unknown into a failure |
| Policy result | `allow` uses the normal treatment | `deny` uses danger because the request was explicitly refused; an unknown decision stays unrecorded |
| Delivery result | `upstream` or `completed` uses the normal treatment | `failed` uses danger; `cancelled` or a partial outcome uses warning; unknown stays unrecorded |

An HTTP success only describes transport. It does not make a cleanup operation
complete when items were skipped or failed.

## Query-State Contract

| State | Required presentation |
| --- | --- |
| Initial pending | The owning region has `aria-busy="true"`; skeletons are hidden from assistive technology; no empty message is shown |
| Initial error | `app/error-state` names the failure and provides Retry |
| Successful empty response | `app/empty-state` is shown only after a successful response proves the collection is empty |
| Cached data plus refresh failure | Cached data stays visible with a stale notice |
| Permission denied | The denial is explicit; never represented as an empty collection |
| Mutation pending/error | Duplicate submission is disabled; the error stays inline and does not close the dialog or emit a success toast |

Independent sibling queries own independent pending, error, stale, and empty
boundaries. A failure in one panel must not erase successful data in another.

## Interaction Rules

- Icons identify familiar tools; text accompanies commands where the action is
  not self-evident.
- Every icon-only control needs an accessible label and a tooltip, and a
  minimum 40×40 target.
- Loading, empty, error, disabled, and permission-gated states are required.
- Controls keep stable dimensions as labels, counts, or loading states change.
- Focus stays visible in both themes. Programmatically focused route and setup
  regions suppress the decorative ring because they are announcement targets,
  not keyboard controls.
- Motion honours `prefers-reduced-motion`.
- Responsive tables scroll inside a named region; the document must never gain
  horizontal overflow at 320px.

## What Must Not Regress

The redesign changes how states look. It does not remove states, weaken an
invariant, or change behaviour the suite encodes. Each row names the spec that
holds it.

| Invariant | Enforced by |
| --- | --- |
| Five query states per data region, distinguishable | `admin-query-states.spec.ts` |
| A pending mutation cannot be abandoned | `admin-dialog-actions.spec.ts` |
| Every icon-only control is ≥40×40 in every state | `fixtures/a11y.ts`, asserted by the Admin, Portal, and Setup specs |
| No letter spacing, anywhere, in both locales | `fixtures/a11y.ts`, same three |
| No horizontal document scroll at 320px | `fixtures/a11y.ts`, responsive specs |
| Filters and pagination survive reload, Back, and deep links | log and settings workspace specs |
| The same health rule drives every health display | Portal Monitor and Admin Upstreams specs |
| Chinese and English stay in parity | `make lint-i18n` |
| An Admin route never falls through to the Portal SPA | routing specs |

## Verification

For frontend changes run:

```bash
make check
```

`make check` runs the fast Go and browser smoke layers plus frontend contracts.
Before merging broad UI changes run `make verify` for the complete Playwright
suite. Accessibility coverage visits every Admin route once, and the Portal,
Monitor, and first-run Setup through the same contract in
`e2e/fixtures/a11y.ts`, checking API contracts, WCAG 2.1 A/AA rules, target
sizes, layout caps, and token contrast. Keep screenshots failure-only; do not
commit full-page pixel snapshots.

Specs assert against semantic tokens rather than literal colours wherever the
token layer is expected to move. Do not reintroduce a hex value into a spec.

## Brand

The mark is an **open repository boundary around a cached dependency module**,
from the delivered brand kit. Its masters are in
[docs/brand/](docs/brand/README.md): Electric Blue `#3B82F6` / Deep Blue
`#2563EB` for the two shells, Cache Green `#22C55E` / Deep Green `#16A34A` for
the module, on the kit's Ink/Slate/Surface neutrals. The tagline is
*Dependencies closer. Builds faster.* and the descriptor is *Repository Cache
Control*; both are lockup assets, not UI copy.

Three files carry it, and they move together: `web/public/favicon.svg`,
`components/app/logo.tsx` (the flat variant, which the kit nominates for UI),
and `assets/macos/icon.svg` for the desktop bundle and the Linux tray.

In the token layer this makes **blue the action colour** and leaves **green
meaning cache**: the kit gives the module its own green, which is the same fact
the status vocabulary already reports for a hit. See
[visual-direction.md §8](docs/design/visual-direction.md#8-brand) for why the
two earlier marks were superseded.

## Stage Status

Both stages are complete: Stage A migrated the UI architecture to shadcn/ui on
Base UI without redesigning anything, and Stage B rebuilt the visual system on
top of it — tokens, typography, navigation, page layout, status vocabulary,
tables, forms, the Dashboard, the remaining Admin pages, the Portal and Setup,
and the brand kit.

There is no migration plan left to follow. The task-by-task plans and the
proposal that preceded them are in git history rather than the working tree,
per [docs/README.md](docs/README.md); what remains here is the system they
produced and the rules it must keep.

`index.css` is imports, tokens, the dark variant, a base layer, and the icon
box — no component styling. Pages still carry inline `style` objects where the
value is genuinely dynamic (a state-dependent colour, a chart prop, a computed
grid). Those read semantic tokens and are correct; they are not a second
styling system.
