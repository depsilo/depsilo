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
| **Semantic** | A role the UI refers to | `--card`, `--success`, `--radius-control` |

Components only ever read semantic tokens. A primitive appearing in a component
is a defect: it means someone made a colour decision at a call site.

Rules carried from Stage A, which step 01 must not undo:

- One cascade. Light is `:root`, dark is `.dark`. No third copy of the palette.
- `@custom-variant dark` keyed on the root class, set before first paint.
- `@theme inline` exposes every semantic token as a Tailwind utility.
- **No letter spacing anywhere, in either direction.** Every visible element
  must resolve to `letter-spacing: 0`; do not reach for `tracking-*`. Enforced
  on every surface by `e2e/fixtures/a11y.ts`, which the Admin, Portal, and
  Setup specs share. The rule is stated as "no negative" in places because
  that is the direction the old display titles used; the check is stricter
  because positive tracking is a Latin-only device and a drift vector of its
  own.
- **The root font size stays at the browser default.** The product's base text
  size lives on `body`. A non-default root size silently rescales every
  `rem`-based utility; Stage A found and removed exactly that trap.
- `prefers-reduced-motion` disables animations **by name**, globally.

## 2. Colour

### 2.1 Neutrals

A single neutral ramp drives canvas, surface, inset, borders, and text. The ramp
is tuned once, not per surface: every surface role is one step on the same
scale, so raising or lowering the whole interface is one edit.

| Product role | Token | Ladder position |
| --- | --- | --- |
| Canvas | `--background` | furthest from text |
| Surface | `--card`, `--popover` | one step in |
| Inset | `--muted` | two steps in |
| Hover / selected surface | `--accent` | interaction step; distinguishable from `--surface` without relying on colour alone |
| Hairline | `--border`, `--input` | one weight |
| Text | `--foreground` | furthest from the canvas |
| Secondary text | `--muted-foreground` | mid-ramp |

**The names are shadcn's, and that is a decision.** The primitives in
`components/ui` are generated against them, so any other vocabulary would need
a translation layer that every future `shadcn add` would fight. Product
language ("canvas", "surface", "inset") is how the roles are *discussed*; the
table above is the mapping.

**Two text weights, not three.** A third, fainter weight cannot render text and
still clear 4.5:1, so it would be a contrast violation waiting to happen.
Disabled text is exempt from the requirement and uses opacity instead.

Both themes use the same ladder with inverted direction. Dark is not "light with
the lights off": if the ladder inverts mechanically, dark mode reads as dimmed
paper. Dark gets its own tuned values at the same role positions.

### 2.2 Brand and action

One brand family, spent only on primary commands, focus, active navigation, and
the hit signal (see [visual-direction.md](visual-direction.md#4-colour)).

| Role | Token | Meaning |
| --- | --- | --- |
| Action | `--primary` | Primary command fill, active navigation, brand label text |
| On action | `--primary-foreground` | Text on `--primary` |
| Focus | `--ring` | Focus. A separate role so it can be tuned for contrast independently |

**`--accent` is not the brand.** shadcn's `--accent` is a hover/selected
*surface*; using the word for both is the collision this section exists to
prevent.

The family is kept from the pre-migration palette, with one correction that
implementation forced:

| Token | Value (light) | Why |
| --- | --- | --- |
| `--brand-action` | `oklch(0.52 0.125 158.2)` | One step deeper than the mark |
| `--brand-focus` | `oklch(0.569 0.126 160.2)` | ≥3:1 on every surface it can outline |
| `--brand-action-dark` | `oklch(0.796 0.169 157.7)` | The dark counterpart |

`docs/brand/` fixes the flat mark at `#0A8654`. The UI's action green is
deliberately one step deeper, because the mark never carries text while the
action colour is used as a *label* on tinted rails as well as a command fill —
and `#0A8654` clears 4.61:1 on white but only 4.31:1 on `--muted`. Adopting the
mark's value directly would have failed on every rail in the product.

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

`--neutral-status` is the fourth role. It exists so a cache miss and an
unrecorded result have somewhere to live that is neither success nor failure.

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

Nine roles, and nothing else. Before step 02 the app layer used seventeen
arbitrary sizes and twelve arbitrary font weights, which is drift rather than a
system.

| Token | Size | Role |
| --- | --- | --- |
| `--text-micro` | 10px | Eyebrow copy, table headers, dense metadata |
| `--text-meta` | 11px | Badges, nav labels, secondary metadata |
| `--text-label` | 12px | Field labels, table cells, secondary copy |
| `--text-body` | 13px | The default reading size |
| `--text-title` | 16px | Panel and dialog titles |
| `--text-subhead` | 18px | Section headings |
| `--text-page-title` | 26px | The one `h1` per page |
| `--text-metric-sm` | 22px | An in-rail measured value |
| `--text-metric` | 30px | The largest value on a page |

Sizes are **px, deliberately**: a dense operator table should not reflow
because a reader raised their browser's base font size. The one exception is
`--text-field` (16px), which is not a type choice at all — it is the floor
below which a focused input makes mobile Safari zoom.

Two landmines are worth knowing before adding a token here:

1. **`text-*` is a shared prefix.** A size token whose name matches a
   `--color-*` token loses: `--text-input` collided with shadcn's
   `--color-input`, so `text-input` silently painted text with a translucent
   outline colour instead of setting a size. It is called `--text-field` for
   that reason. Check the `--color-*` list before naming a size.
2. **tailwind-merge must be told.** It only knows Tailwind's built-in sizes, so
   an unregistered `text-<value>` is treated as a *colour* — which silently
   dropped a button's `text-primary-foreground` and let the label inherit the
   container's colour. Every token above is listed in
   `web/src/lib/utils.ts`, and `unit/cn.test.ts` pins the behaviour.

| Token | Use | Constraint |
| --- | --- | --- |
| `--text-page-title` | The one `h1` per Admin page | One per page, never repeated as a section title |
| `--text-section` | Panel and section titles | Distinguishable from body text without relying on size alone |
| `--text-body` | Default reading size | 13px today |
| `--text-label` | Field labels, table headers, eyebrow copy | Never smaller than the smallest legible size for Chinese |
| `--text-metadata` | Timestamps, counts, secondary detail | Must stay legible: metadata carries decisions |

Line heights are unitless multipliers so they follow the text size. Each type
token carries its own, and a `leading-*` utility at a call site overrides it,
because Tailwind emits line-height utilities after font-size ones:

- **Single-line operational rows: 1.4.** No inter-line relationship exists, so
  a tighter line box is safe and buys row padding.
- **Multi-line body copy: 1.6 minimum.** Chinese glyphs fill the em box, so
  tighter leading visibly crowds stacked lines.
- Line height is set on the **row and its text together**. Setting it only on
  the container lets icon buttons and badges inherit it and re-inflate the row.

### 3.3 Numeric

**Numeric is a treatment, not a second size scale.** A number in a table cell
is ordinary body or label text that happens to use the mono face with
`font-variant-numeric: tabular-nums`; giving it its own size tokens produced
nothing but duplicates of the text scale.

The two exceptions are genuinely distinct sizes and are the only numeric
tokens: `--text-metric` (the largest value on a page) and `--text-metric-sm`
(an in-rail value).

The requirement is what matters: values that change — latency, sizes, counters
— must not be able to move the layout beside them, and a column of numbers must
align by width alone.

### 3.4 Weight

Four weights, and nothing between them: normal (400), medium (500), semibold
(600), bold (700).

Step 02 collapsed twelve arbitrary weights into these. Anything between two
steps rounds to the nearer one — 550 became medium, 680 became semibold —
because a weight that exists at 650 in one place and 680 in another is drift
that reads as inconsistency rather than nuance.

### 3.5 Where `components/ui` keeps its own sizes

The generated primitives still use Tailwind's named sizes (`text-sm`,
`text-xs`). They are left alone on purpose: the application layer overrides
their type at every call site, and editing generated files is a tax on every
future `shadcn add`. The product's type is decided in `components/app` and the
surfaces, not in the primitives.

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

### 4.1a Definition versus outcome

The same words appear in two different jobs, and they are coloured differently
on purpose:

- A **rule's action** (`allow` / `deny`) is a *definition* — what the rule
  says. Two distinguishable kinds, so they are distinguished: allow reads as
  success, deny as destructive. Scanned in a rules table, that is the point.
- A **request's decision** (`allow` / `deny`) is an *outcome*. An allow is the
  normal case and stays neutral; a deny is an explicit refusal and reads as
  destructive. Colouring every allowed request green would paint every row of
  every log.

### 4.1b Vocabulary lives in code

`web/src/lib/status.ts` is the authority, and `unit/status.test.ts` pins it.
A surface picks a *dimension* and asks for its tone; it never chooses a colour:

```ts
<Badge variant={policyTone('deny')}>{t('audit.blocked')}</Badge>
```

This exists because the same fact was previously coloured differently on
different surfaces — a cache miss was `warning` in the dashboard feed and
neutral in the logs — and because a refusal was rendering as `warning`, which
says "degraded" about a decision the Operator deliberately made.

Tones are named after the token they read, so the vocabulary, the CSS, and
`components/ui` all say **`destructive`** for the same thing. Before this, the
app layer said `danger` in one component and `error` in another while the
tokens said `destructive`.

### 4.1c Severity is a fourth dimension

Vulnerability severity is not an outcome. A critical finding and a failed
request are different kinds of fact, and giving them the same colour by
accident would make a security page and an operations page mean the same thing.

| Severity | Tone |
| --- | --- |
| `critical` | destructive |
| `high` | warning |
| `medium`, `low`, `unknown` | neutral |

A finding is never a success. `low` previously rendered green, which said the
opposite of what it meant.

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

**Which palette a chart uses depends on what its series are.**

- A chart whose series are **outcomes** uses the status axis. The Dashboard's
  activity trend plots cache results, and the product has already taught the
  Operator that a hit is green; recolouring hits blue to satisfy a categorical
  ramp would trade a learned association for tidiness.
- A chart whose series are **categories** uses the ramp: per-ecosystem traffic,
  per-upstream share. None of these is better or worse than another, and the
  ramp says so while the status axis would not.

Rules:

- **Every series clears 3:1 against the plot surface.** This is the floor for a
  graphic that carries meaning, and it is the only ramp property the token gate
  asserts.
- **Greyscale legibility is not a property of the ramp, and cannot be.** Five
  hues that all clear 3:1 on white converge on one lightness, so a ramp cannot
  be both contrast-safe and luminance-separated. Series are therefore
  distinguished by **shape and label as well as colour** — the trend chart
  already separates series by stroke pattern, and that is what carries the
  requirement.
- A partial series, a missing bucket, and a zero are three different things. A
  gap is drawn as a gap.
- An empty chart is an empty state, not an axis with no data.

## 6. Space, size, radius

| Token group | Contract |
| --- | --- |
| Spacing | One 4px base ladder — the Tailwind step itself (`min-h-10` is 40px), not a second scale on top of it. Arbitrary `[Npx]` values are drift and have been collapsed to the ladder |
| Row height | **40px**, the ladder's `10`. One value for rows and controls, per [product-ui-principles.md](product-ui-principles.md#4-rhythm-one-row-not-three) |
| Tap target floor | **40px.** The accessibility floor, never overridden downward, and asserted on `[data-icon-button]` and the segmented controls by `e2e/admin-axe.spec.ts` and the responsive-grid specs |
| Control height | **36px**, the ladder's `9`, for fields and standard buttons. The 40px floor is a *target* floor — it applies to icon-only controls and to row rhythm, not to a text field, and a field that is 40px tall inside a 40px row has no breathing room |
| `--radius-control` | **`6px`.** Rows, inputs, buttons, chips, insets, code blocks, skeletons — anything a reader points at or reads as one unit |
| `--radius-surface` | **`10px`.** The outermost frame: dialogs, sheets, panels, and table wrappers |
| `--focus-ring-width` / `-offset` | Fixed, and identical in both themes |

The two radii are the only two in the stylesheet. `--radius-sm`, `--radius-md`,
and `--radius-lg` all resolve to `--radius-control`, `--radius-xl` and
`--radius-2xl` resolve to `--radius-surface`, and a call site that writes a
third value is making a decision the design has already made.

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

`web/e2e/token-contrast.spec.ts` implements this gate and runs in the smoke
set, so a token edit that breaks a pair fails `make check` rather than
production:

- every text/background pair in use clears **4.5:1**, including each status
  role as text on the canvas *and* on its own tinted surface — those are
  different pairs and the tint is the stricter one;
- the focus ring and every chart series clear **3:1** against the surfaces they
  can appear on;
- colours are resolved by painting them and reading the pixels back, so the
  check works for any colour space or `color-mix` expression and pins no
  literal value.

Hairline borders are deliberately **not** in the gate. WCAG 1.4.11 applies to
information required to identify a control, which a separator is not; a
hairline that clears 3:1 against its surface would render as a heavy rule and
would make a dense table look like a grid.

Two further gates:

- `e2e/admin-axe.spec.ts` passes on every Admin route, in both themes and both
  locales;
- Portal and Setup pass the same contract — same helper, same floors — through
  `e2e/portal-axe.spec.ts`, which is what [PRODUCT.md](../../PRODUCT.md) listed
  as unfinished before step 14.
