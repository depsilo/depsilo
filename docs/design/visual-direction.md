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

### 4.3 The action hue

**Superseded by the brand kit.** This section originally recommended keeping the
green family as the accent, because green is load-bearing in the product's
semantics: a cache hit is green, and Operators have muscle memory for it.

The delivered kit resolves that tension rather than ignoring it. Blue is the
brand and the action colour — an open repository boundary — and green stays
exactly where the product's meaning needs it: the cached module, and therefore
the cache-hit status. The two hues no longer compete for the same job, which is
the outcome the earlier recommendation was protecting.

The kit's blue also needs no tuning to pass the gates the old green needed:
`#2563EB` clears 5.2:1 as a fill with white text and 4.7:1 as a label on the
muted rail. See [design-tokens.md](design-tokens.md#22-brand-and-action).

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

**Resolved: the delivered brand kit supersedes both earlier marks.**

Stage A replaced the in-app mark with a neutral placeholder because the
migration brief made the logo discardable. `PRODUCT.md` meanwhile still named
the earlier **Dependency Shelf** mark canonical, and `web/public/favicon.svg`
carried it — so the browser tab and the app header disagreed for two steps.

The product owner has since delivered `depsilo-brand-kit`, and it is now the
single identity: an **open repository boundary around a cached dependency
module**, in Electric Blue `#3B82F6` / Deep Blue `#2563EB` with Cache Green
`#22C55E` / Deep Green `#16A34A`, tagged *Dependencies closer. Builds faster.*
The masters live in [docs/brand/](../brand/README.md), and the three places
that must move together are the favicon, `components/app/logo.tsx`, and the
desktop icon.

What that means for the design system, which is the part step 15 owns:

- **Blue is the action colour.** It replaces the green that Stage A carried
  through the token layer. `#2563EB` is the light theme's command fill and
  label colour; `#60A5FA` is the dark theme's, with navy text on it.
- **Green means cache, not "brand".** The kit gives the module its own green,
  and that is the same fact the status vocabulary already reports: a cache hit
  is `success`. The two meanings reinforce each other, so the status family
  stays where it was and simply draws its hue from the kit.
- **The neutrals are the kit's slate family**, with the kit's navy for the dark
  canvas. The old ramp was pure grey; a slate ladder is warmer against the blue
  and is what makes the mark look placed rather than pasted.
- **The wordmark is Inter 700–800**, which the product already ships. Lockups
  that carry the descriptor or the tagline are assets, not UI: a page header
  shows the mark and the name, never the tagline.

Two values deviate from the kit's literal hexes, both for contrast, both
documented in [design-tokens.md](design-tokens.md#2-colour): the secondary text
role takes the step below the kit's Slate, and the light status colours take
the step below the kit's headline green and red. The kit's values carry large
fills and the mark; these carry 12px text on a tint.

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
