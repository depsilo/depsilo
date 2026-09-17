# Depsilo Visual Direction

> Stage B step 00. How Depsilo should look and feel, and — more usefully —
> what it should refuse to be. Principles are in
> [product-ui-principles.md](product-ui-principles.md); the values that
> implement this direction are in [design-tokens.md](design-tokens.md).

## 1. Working thesis

**An instrument panel, not a dashboard.**

A dependency proxy is measuring equipment. It sits on a request path, it
reports on traffic it did not generate, and it is read by someone who is
usually in the middle of something else. The interface should read like
calibrated equipment: quiet surfaces, hairline structure, one accent reserved
for action, and data that is dense without being cramped.

Nothing floats. Nothing glows. Depth comes from material contrast and keylines,
not from shadow, and no element is interesting enough to compete with the
number inside it.

## 2. The references, and what to take from each

These are studied for specific virtues, not for a look to copy.

| Reference | What to take | What not to take |
| --- | --- | --- |
| **Linear** | Discipline about a single accent, and the confidence to leave most of the screen neutral | Its density assumes a fast client and a large viewport |
| **Vercel** | Hairline structure and restraint in the product surfaces (as opposed to the marketing site) | The marketing register |
| **GitHub** | Dense, scannable tables and a status vocabulary that survives being read at a glance | Its breadth of ornament |
| **Cloudflare** | Dense operational data presented calmly, without alarm colouring everything | Card-per-metric composition |
| **Grafana** | Honest treatment of partial data, unknown series, and time ranges | Panel chrome, borders on everything |
| **Sentry** | Legible failure: an error always says what broke and what to do | Its information density in navigation |
| **Stripe Dashboard** | Numeric typography and the discipline of one primary action per view | Its decorative illustration |
| **Supabase** | Developer-tool directness; terminology at the surface | Its dark-first neon accents |
| **Railway / Resend** | Calm use of one accent against near-neutral surfaces | Their landing-page scale |

The common thread worth copying is *subtraction*: each of these is confident
enough to leave most of the interface neutral.

## 3. Composition

### 3.1 Structure from hairlines

Operational data reads better as one continuous surface divided by rules than
as a field of floating cards. Depsilo already has this instinct in the KPI
data rail, and Stage B generalises it:

| Pattern | Use for |
| --- | --- |
| **Data rail** — one surface, internally divided, no internal panels | KPI rows, summary strips, metric groups |
| **Section** — a titled region separated by a hairline or by spacing alone | Dashboard blocks, settings groups, log views |
| **Card** — a raised, bounded surface | Only genuinely discrete, movable objects: an Upstream, a connected project, an empty state |

The test: if two things would never be reordered independently, they are not
two cards.

### 3.2 Surfaces and elevation

Three levels, defined by contrast rather than by shadow:

1. **Canvas** — the page background.
2. **Surface** — the primary content plane (a rail, a table, a panel).
3. **Inset** — a recessed supporting region (an attention queue, a stale
   notice, a code block).

Elevation is reserved for things that genuinely sit above the page and can be
dismissed: dialogs, sheets, tooltips, toasts. A shadow on a static panel is a
misuse.

### 3.3 Borders

One hairline weight throughout. A border's job is to separate, so a border
between two neighbouring regions is usually duplicated — remove one. Borders do
not appear around both a container and its children.

### 3.4 Radius

Small and consistent. Controls and rows share a small radius; a single large
radius is reserved for the outermost dialog or sheet. Large radii on dense
controls make an operational UI look soft and toy-like.

## 4. Colour

### 4.1 One accent

Exactly one saturated brand colour, spent on:

- the primary command in a view,
- focus,
- the active navigation destination,
- the "hit" signal.

Everything else is neutral. An accent used in five places is decoration; used
in these five, it is navigation.

### 4.2 Status is a separate axis

Status colours are **not** derived from the brand hue. A healthy system must
never look like a branded system, and a failure must never look like a call to
action. See [design-tokens.md](design-tokens.md#4-status-system) — Depsilo has
an unusually specific status problem and it gets its own section there.

### 4.3 Keep the green

**Recommendation: keep the existing green family as the accent.**

The current values (`#0A8654` command, `#0FA86F` signal) already read as
technical infrastructure rather than marketing, and green is load-bearing in
this product's semantics — a cache hit is green, and Operators have muscle
memory for it. Replacing the hue would be novelty at the cost of legibility.

Stage B's colour work is therefore about *how the accent is spent and
governed*, not about picking a new one. If the tokens step reveals a genuine
contrast or CVD problem with the family, that is a reason to tune it, not a
reason to abandon it.

It did reveal one, and it was tuned: the mark's flat `#0A8654` clears 4.61:1 on
white but only 4.31:1 on the `--muted` rail, so using it as the UI action
colour would have failed on every label that sits on a rail. The UI action
green is therefore one step deeper than the mark, which never carries text.
See [design-tokens.md](design-tokens.md#22-brand-and-action).

## 5. Typography

- One UI face for the whole product. A separate display face is a marketing
  device; Depsilo has no headlines that need one.
- Numeric values are a first-class type: tabular figures, mono face, fixed
  widths, so `9 ms` → `1 240 ms` cannot move the column beside it.
- **No negative letter spacing.** This is enforced by the accessibility suite
  and is also correct for Chinese.
- Chinese is designed alongside Latin, not after it. The product is bilingual;
  a direction that only works in English is not a direction.

Details and the numeric scale are in
[design-tokens.md](design-tokens.md#3-typography).

## 6. Motion

Functional only:

- an entrance on a newly keyed live row,
- an overlay's entry and exit,
- nothing else.

No page-level fades, no scroll reveals, no hover theatre, no counters that
animate to their value. Every animation is named and is disabled by the global
`prefers-reduced-motion` switch, and the tests assert that.

## 7. Iconography

Lucide, one weight, one size per context. Icons identify; they do not decorate.
An icon-only control always has a label and a tooltip, and always reaches 40px.

## 8. Brand

**Open decision — do not design past it.**

Stage A replaced the in-app mark with a neutral placeholder, per the migration
brief, which lists the current logo under what may be discarded. But
[PRODUCT.md](../../PRODUCT.md) and [docs/brand/README.md](../brand/README.md)
declare the mark *canonical*, name it **Dependency Shelf / 层仓栈**, and
specify it in detail — three long-to-short staggered layers feeding a continuous
curved spine, flat colour, no gradients, no attached tagline — with
`docs/brand/` as the single master for every surface.

That is a live conflict between two sources of truth, and it should be resolved
before this step is implemented, because the answer changes the work:

- **If PRODUCT.md is current**, Stage B "brand refinement" means optical
  refinement, sizing, placement, and the light/dark pair — not a new identity.
  The in-app placeholder is a temporary divergence to be closed.
- **If the brief supersedes PRODUCT.md**, PRODUCT.md's Brand Commitments section
  must be updated in the same change, `docs/brand/` must be retired or
  rewritten, and the favicon, README, and release assets move with it.

There is a third state today that should not persist either way: the in-app
logo is a placeholder while `web/public/favicon.svg` still carries the real
mark, so the browser tab and the app header currently disagree.

## 9. Explicitly refused

Carried from the brief, kept here because a design direction is mostly a list
of refusals:

- giant gradients, glow, purple-to-blue anything;
- glassmorphism and frosted panels;
- floating cards, and cards nested inside cards;
- large border radii on dense controls;
- huge headings and oversized display type;
- excessive whitespace that pushes real data below the fold;
- random or decorative animation;
- decorative dashboards: charts and numbers that exist to fill a grid;
- illustration as a substitute for an empty state that should explain itself.

The failure mode this list guards against is the AI-SaaS template: an
interface that looks considered at a glance and communicates nothing on the
second look. Depsilo's whole value is what it refuses to serve, so its
interface should look like something that refuses things.
