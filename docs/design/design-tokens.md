# Depsilo Design Tokens

> Stage B step 00. The token contract that step 01 implements. Intent is in
> [visual-direction.md](visual-direction.md); principles are in
> [product-ui-principles.md](product-ui-principles.md).
>
> Stage A built the structure this document fills in: one `:root`, one `.dark`,
> and a `@theme inline` block in `web/src/index.css`. Nothing here changes that
> architecture — it changes the values and adds the roles below.

## 1. Architecture

Two tiers, no third:

| Tier | Purpose | Example |
| --- | --- | --- |
| **Primitive** | A raw value with no meaning | `--neutral-3`, `--green-600` |
| **Semantic** | A role the UI refers to | `--surface`, `--success`, `--row-height` |

Components only ever read semantic tokens. A primitive appearing in a component
is a defect: it means someone made a colour decision at a call site.

Rules carried from Stage A, which step 01 must not undo:

- One cascade. Light is `:root`, dark is `.dark`. No third copy of the palette.
- `@custom-variant dark` keyed on the root class, set before first paint.
- `@theme inline` exposes every semantic token as a Tailwind utility.
- **No negative letter spacing anywhere.** Enforced by `e2e/admin-axe.spec.ts`.
- **The root font size stays at the browser default.** The product's base text
  size lives on `body`. A non-default root size silently rescales every
  `rem`-based utility; Stage A found and removed exactly that trap.
- `prefers-reduced-motion` disables animations **by name**, globally.

## 2. Colour

### 2.1 Neutrals

A single neutral ramp drives canvas, surface, inset, borders, and text. The ramp
is tuned once, not per surface: every surface role is one step on the same
scale, so raising or lowering the whole interface is one edit.

| Role | Ladder position | Notes |
| --- | --- | --- |
| `--canvas` | furthest from text | The page |
| `--surface` | one step in | Rails, tables, panels |
| `--inset` | two steps in | Recessed supporting regions |
| `--hover` / `--active` | interaction steps | Distinguishable from `--surface` without relying on colour alone |
| `--border` | hairline | One weight |
| `--text` / `--text-muted` / `--text-faint` | three text weights | `faint` is for labels and metadata only, never for data a decision depends on |

Both themes use the same ladder with inverted direction. Dark is not "light with
the lights off": if the ladder inverts mechanically, dark mode reads as dimmed
paper. Dark gets its own tuned values at the same role positions.

### 2.2 Accent

One accent family, spent only on primary commands, focus, active navigation,
and the hit signal (see [visual-direction.md](visual-direction.md#4-colour)).

| Role | Meaning |
| --- | --- |
| `--accent` | The accent at rest: primary command fill, active state |
| `--accent-strong` | Pressed / intensified |
| `--accent-surface` | A tinted area that is *about* the accent, not a status |
| `--accent-foreground` | Text on `--accent` |
| `--focus-ring` | Focus. May equal the accent but is a separate role so it can be tuned for contrast independently |

The family is kept from Stage A (`#0A8654` command, `#0FA86F` signal in light).
Step 01 re-verifies contrast; it does not go looking for a different hue.

### 2.3 Status

Four status roles, deliberately **not** derived from the accent hue:

| Role | Means | Never means |
| --- | --- | --- |
| `--success` | Observed success: a hit, a healthy service, completed work | "Everything is fine" in general |
| `--warning` | Degraded, slow, partial, stale, or an incomplete result | A failure |
| `--destructive` | A real failure, an explicit refusal, a destructive action | "Something is different" |
| `--neutral-status` | Recorded, but not an outcome: miss, unknown, not-observed | Anything at all about quality |

Each role has four members: the base colour, `-surface` (a tint over the
canvas), `-border`, and `-foreground` (text legible on both the canvas and its
own surface). Stage A's values are a verified baseline:

| Token | Value | Theme |
| --- | --- | --- |
| `--success` | `oklch(0.47 0.13 155)` | light |
| `--destructive` | `oklch(0.47 0.20 27)` | light |
| `--warning` | `oklch(0.47 0.12 70)` | light |
| `--neutral-status` | same as `--text-muted` | both |
| `--success-surface` | `color-mix(in oklab, var(--success) 14%, var(--canvas))` | light |
| `--warning-surface` | `color-mix(in oklab, var(--warning) 16%, var(--canvas))` | light |
| `--destructive-surface` | `color-mix(in oklab, var(--destructive) 14%, var(--canvas))` | light |

The lightness was chosen so each role clears **4.5:1 both as text on the canvas
and as text on its own surface**. Tuning hue or chroma is fine; dropping
lightness below this is not.

Chart series get a **separate five-step categorical ramp** (section 5), so a
chart never implies health and a status colour never has to double as a series.

## 3. Typography

### 3.1 Families

| Role | Face |
| --- | --- |
| UI (Latin) | one variable sans |
| UI (CJK) | platform stack, per locale |
| Numeric and machine text | one mono face with tabular figures |

Stage A's faces (Inter / JetBrains Mono with a platform CJK fallback) are
adequate and are kept. A second display face is not introduced: the product has
no headlines that need one, and the brief's own rules make tight display
tracking unavailable anyway.

### 3.2 Scale

| Token | Use | Constraint |
| --- | --- | --- |
| `--text-page-title` | The one `h1` per Admin page | One per page, never repeated as a section title |
| `--text-section` | Panel and section titles | Distinguishable from body text without relying on size alone |
| `--text-body` | Default reading size | 13px today |
| `--text-label` | Field labels, table headers, eyebrow copy | Never smaller than the smallest legible size for Chinese |
| `--text-metadata` | Timestamps, counts, secondary detail | Must stay legible: metadata carries decisions |

Line heights are unitless multipliers so they follow the text size:

- **Single-line operational rows: 1.4.** No inter-line relationship exists, so
  a tighter line box is safe and buys row padding.
- **Multi-line body copy: 1.6 minimum.** Chinese glyphs fill the em box, so
  tighter leading visibly crowds stacked lines.
- Line height is set on the **row and its text together**. Setting it only on
  the container lets icon buttons and badges inherit it and re-inflate the row.

### 3.3 Numeric

Numbers are a type scale, not a habit:

| Token | Use |
| --- | --- |
| `--text-metric` | A KPI value: the largest number on a page |
| `--text-metric-sm` | A secondary or in-rail metric |
| `--text-number` | A number inside a table cell |
| `--text-number-sm` | Latency, byte counts, inline statistics |

All four use the mono face with `font-variant-numeric: tabular-nums`. Values
that change — latency, sizes, counters — must not be able to move the layout
beside them, and a column of numbers must align by width alone.

## 4. Status System

This is Depsilo's most product-specific surface, so it is specified here rather
than left to component defaults: a generic success/warning/error triple cannot
express it.

### 4.1 The three outcome dimensions

A single request produces three **independent** results. Collapsing them into
one signal is the most likely design failure in this product.

| Dimension | Values | Normal | Attention |
| --- | --- | --- | --- |
| **Cache** | `hit`, `miss`, `unknown` | `hit` uses success | none — a miss and an unknown are neutral, because the request was still served |
| **Policy** | `allow`, `deny` | `allow` is neutral | `deny` uses destructive: the request was explicitly refused |
| **Delivery** | `upstream`, `completed`, `failed`, `cancelled`, `unknown` | `upstream` / `completed` are neutral | `failed` uses destructive; `cancelled` or partial uses warning |

A request can legitimately be a cache miss, policy-allowed, and delivered
successfully. That is three true facts and zero problems.

### 4.2 Health

| Value | Meaning |
| --- | --- |
| `healthy` | Reachable and within expectations *now* |
| `degraded` | Reachable but slow, or partially failing |
| `failed` | Unreachable, or an explicit failure |
| `unknown` | Not observed yet, or the observation itself failed |
| `stale` | A last-known value whose refresh failed — labelled as such |

`unknown` and `stale` are first-class. Rendering them as "no problem" is wrong;
rendering them as a failure is equally wrong.

### 4.3 One signal per row

In a dense list, **each row shows at most one coloured element**: the dominant
signal.

1. A destructive fact outranks everything (a refusal, a failure).
2. A warning outranks a success.
3. A success is shown only when nothing needs attention.
4. Neutral facts are shown as text, never as colour.

The full triple belongs in the row's detail view, where there is room to explain
it. A row showing three coloured chips has not decided what matters, and has
pushed that work onto the Operator.

### 4.4 Refusals explain themselves

A policy `deny` always carries what was refused, which rule decided, and what an
Operator can do next. Colour plus a one-word label is not an explanation.

## 5. Charts

In scope as **tokens and rules**, not as a charting rewrite.

| Token | Use |
| --- | --- |
| `--chart-1` … `--chart-5` | Categorical series ramp, distinct from the status axis |
| `--chart-grid` | The one grid weight |
| `--chart-axis-label` | Tick labels |

Rules:

- A series is identified by colour **and** by shape or label. The trend chart
  already distinguishes series by stroke pattern; that survives.
- The ramp must be distinguishable in greyscale and under the common forms of
  colour-vision deficiency.
- A partial series, a missing bucket, and a zero are three different things. A
  gap is drawn as a gap.
- An empty chart is an empty state, not an axis with no data.

## 6. Space, size, radius

| Token group | Contract |
| --- | --- |
| Spacing | One 4px base ladder. Page padding, section rhythm, and control padding all draw from it |
| `--row-height` | **40px.** One value for rows and controls, per [product-ui-principles.md](product-ui-principles.md#4-rhythm-one-row-not-three) |
| `--tap-target-min` | **40px.** The accessibility floor; never overridden downward |
| `--control-height` | 40px for standard controls; 32px only for controls that are not touch targets (inline chips) |
| `--radius-control` | Small, shared by rows, inputs, and buttons |
| `--radius-surface` | The outer radius for panels and dialogs |
| `--focus-ring-width` / `-offset` | Fixed, and identical in both themes |

## 7. Elevation and layering

| Level | Use |
| --- | --- |
| none | Canvas, surfaces, insets — almost everything |
| `--shadow-overlay` | Dialogs, sheets, popovers, tooltips |

One z-index scale, single source, no ad-hoc values:

| Layer | Value |
| --- | --- |
| base | 0 |
| topbar | 20 |
| sidebar | 30 |
| overlay | 50 |
| tooltip | 80 |
| toast | 90 |

## 8. Motion

| Token | Use |
| --- | --- |
| `--motion-fast` | Hover, focus, small state changes |
| `--motion-base` | Overlay entry and exit |
| `--motion-slow` | Reserved for a data entrance, not a page transition |
| `--ease-out` | Everything. No bounce, no overshoot |

Every animation is named, finite, and disabled by the global reduced-motion
switch. The suite asserts the disabled state by animation name, so an animation
added without a token fails the tests rather than quietly shipping.

## 9. Verification gates

Step 01 is not complete until, for both themes:

- every text/background pair in use clears 4.5:1, and every non-text pair that
  carries meaning (borders, focus rings, chart strokes) clears 3:1;
- each status role clears 4.5:1 **as text on the canvas** and **as text on its
  own surface** — these are different pairs and both are checked;
- the categorical chart ramp is distinguishable in greyscale;
- `e2e/admin-axe.spec.ts` passes on every Admin route;
- Portal and Setup converge on the same standard, which [PRODUCT.md](../../PRODUCT.md)
  records as unfinished today.
