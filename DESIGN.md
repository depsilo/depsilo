# Depsilo Design System

> Status: current implementation reference, rewritten 2026-09-17 for the
> shadcn/ui + Base UI migration. The source of truth is `web/src/index.css`,
> `web/src/components/ui/`, `web/src/components/app/`, `web/src/admin/`, and
> `web/src/portal/`. When this document and the code disagree, fix the
> document in the same change.
>
> **The product is mid-migration.** Stage A replaces the UI architecture and
> deliberately does not design a new visual language. The theme below is a
> neutral, temporary base. Stage B owns brand, palette, typography, and page
> composition. Do not treat any colour or spacing value here as a product
> decision.

## Product Surfaces

| Surface | Routes | Purpose |
| --- | --- | --- |
| Portal | `/`, `/monitor` | Package-manager setup and public service health |
| Admin | `/admin/*` | First-project connection plus repeated operational work: cache, upstreams, logs, policy, users, settings |
| Setup | first-run gate | Secure administrator creation with defaulted service configuration |

The Portal is not a marketing landing page. Quick Start is the first screen;
Monitor is the second. Admin is dense and optimised for scanning and repeated
actions.

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

| Token | Meaning |
| --- | --- |
| `background` / `foreground` | Page canvas and primary text |
| `card` / `popover` | Raised surfaces |
| `primary` | Commands, links, focus, active navigation |
| `secondary` / `muted` / `accent` | Quiet fills and hover surfaces |
| `destructive` | Real failures, explicit refusals, destructive commands |
| `success` | Cache hits, healthy state, completed work |
| `warning` | Degraded or partially completed work |
| `info` | Neutral operational signal (live activity) |
| `border` / `input` / `ring` | Keylines, control outlines, focus |
| `sidebar*` | The Admin navigation rail |
| `radius` | Single radius seed; the scale derives from it |

`success`, `warning`, and `info` exist because Depsilo reports cache, delivery,
and policy outcomes separately. A cache miss and a slow upstream are normal
results and must never render as `destructive`. See "State Semantics".

### Deliberate constraints

- **No negative letter spacing anywhere.** CJK glyphs have no sidebearings to
  absorb it, and `e2e/admin-axe.spec.ts` asserts that every visible element
  resolves to zero letter spacing. Do not add `tracking-*` utilities.
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
`upstream-panel`, `data-table`, `table-viewport`.

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
  40×40 target in every state, including pending. `e2e/admin-axe.spec.ts`
  measures both.
- `Modal` keeps `closeDisabled` for mutations in flight: the dialog cannot be
  dismissed while a change is half-applied.
- The Admin shell is a product component, not shadcn's `sidebar` primitive; it
  keeps a 232px rail, a 48px topbar, and the `data-admin-*` hooks the
  Playwright suite asserts.
- `Logo` is a neutral placeholder. The brand mark is a Stage B decision.

## State Semantics

Keep these dimensions separate when they appear together in Admin:

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

## Verification

For frontend changes run:

```bash
make check
```

`make check` runs the fast Go and browser smoke layers plus frontend contracts.
Before merging broad UI changes run `make verify` for the complete Playwright
suite. Accessibility coverage visits every Admin route once and checks API
contracts, WCAG 2.1 A/AA rules, target sizes, layout caps, and Portal token
regressions. Keep screenshots failure-only; do not commit full-page pixel
snapshots.

Specs assert against semantic tokens rather than literal colours wherever the
token layer is expected to move. Do not reintroduce a hex value into a spec.

The repository has unrelated historical lint debt. The manifest is the exact
Admin remediation scope: fix errors in those files without deriving a new list
from Git history or including unrelated Portal work.

## Migration Status

See [docs/refactor/shadcn-ui-plan.md](docs/refactor/shadcn-ui-plan.md) for the
phase list and the audit that motivated it.

Stage B's visual direction, token contract, component guidelines, and page
order are written but **not implemented**: they live in
[docs/design/](docs/design/). This document describes what the code does today;
where the two disagree, the code is right and this document is the one to
correct.

Completed: the shadcn initialisation; the single-cascade semantic token layer;
the primitive migration (core, form controls, and overlays); the Admin shell;
the mechanical token-utility rewrite; the removal of every legacy token name;
the removal of every hand-written component class from `index.css`; the
Lucide icon migration; and the dependency census.

`index.css` is imports, tokens, the dark variant, a base layer, and the icon
box — no component styling.

Outstanding: the pages still contain ~50 inline `style` objects for values that
are genuinely dynamic (a state-dependent colour, a chart prop, an avatar
colour). Those read semantic tokens and are correct; they are not a second
styling system. A future pass may fold the static ones into utilities as each
page is redesigned.
