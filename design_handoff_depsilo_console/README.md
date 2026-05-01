# Handoff: Depsilo Console — Visual System

## What this is

This bundle contains a **design reference** for the Depsilo cache-proxy console — built as a React + plain-CSS HTML prototype. **Do not copy the HTML directly.** Your job is to port the visual system (tokens, typography, utilities, component treatments) into the existing app codebase using the patterns it already uses.

**Target stack:** React 19 + TypeScript + Vite + **Tailwind CSS 4** + Base UI + i18next.
The Tailwind v4 `@theme` directive is the right home for tokens; the iridescent utilities below should live in a single CSS file imported once at app entry.

## Fidelity

**High-fidelity.** Colors, type sizes, weights, letter-spacing, gradients and component treatments are all final. Recreate pixel-perfectly using the existing component library and Tailwind utilities — but lift the visual *language* (tokens, eyebrow type, gradient accents, aurora rim, etc.) verbatim.

---

## TL;DR — what to do

1. **Add tokens** → paste `tokens.css` contents into your Tailwind v4 `@theme` block (or import the file). They expose CSS custom properties that Tailwind 4 also turns into utility classes (`text-text`, `bg-bg-soft`, etc.).
2. **Add utilities** → drop `utilities.css` into your global stylesheet. It defines `.eyebrow`, `.grad-text`, `.grad-text-aurora`, `.aurora-rim`, `.page-wash`, `.aurora-glow`, `.dot-halo`, `.grad-ring`, plus the existing `.dot-live` pulse and `.fade-up` entrance animation.
3. **Mount `<div className="page-wash" />`** as a sibling of your top-level layout (fixed, behind everything) — see "Layout shell" below.
4. **Update typography defaults** — body 13px / 1.5; h1 44/700 with `-0.04em` tracking; h2 26/700; h3 16/700; eyebrow 10/600 mono uppercase 0.12em.
5. **Audit components for the visual treatments** in the "Component treatments" section — the deltas from a stock card/badge/sparkline are small but they add up.

---

## Files in this bundle

| File | Purpose |
|---|---|
| `README.md` | This document |
| `tokens.css` | All CSS custom properties — paste into `@theme` |
| `utilities.css` | Iridescent utility classes — import globally |
| `reference/Depsilo Console v2.html` | The runnable prototype (open in browser) |
| `reference/styles-v2.css` | Original tokens + utilities (source of truth) |
| `reference/app-v2.jsx` | App shell — shows page-wash mount point |
| `reference/topnav.jsx` | Top nav reference |
| `reference/monitor.jsx` | Hero card, sparklines, mirror table reference |
| `reference/quickstart.jsx` | Quick-start screen reference |
| `reference/primitives.jsx` | Sparkline, StatusDot, Tag, CopyButton, Segmented, CodeBlock, Logo |
| `reference/_current.png` | Visual reference screenshot |

---

## Design tokens

### Surfaces — pure white

```
--bg-page:   #FFFFFF
--bg-panel:  #FFFFFF
--bg-card:   #FFFFFF
--bg-soft:   #F5F5F5     /* recessed regions: code blocks, inactive segmented */
--bg-hover:  rgba(0,0,0,0.035)
```

The page is **pure white**. Don't use off-white, warm paper, or cards-on-gray. Hierarchy comes from hairlines and color, not surface tone.

### Borders — neutral hairlines, always 0.5px

```
--border:        rgba(0,0,0,0.09)
--border-soft:   rgba(0,0,0,0.05)
--border-strong: rgba(0,0,0,0.18)
```

All separators and card outlines are `0.5px solid var(--border)`. On non-retina screens this rounds to 1px, which is fine. **Never** 1px borders by default — too heavy.

### Text — four-tier neutral grayscale

```
--text:        #0a0a0a   /* near-black, body and headings */
--text-muted:  #555555   /* body emphasis, secondary labels */
--text-soft:   #767676   /* descriptions, subtitles */
--text-subtle: #8a8a8a   /* timestamps, captions, placeholder */
```

The four tiers are deliberate. Card titles use `--text`, descriptions use `--text-soft` (one step lighter than muted) so the title pops harder.

### Brand — plum-violet single color

```
--brand:        oklch(0.55 0.16 295)
--brand-strong: oklch(0.48 0.17 295)
--brand-soft:   oklch(0.55 0.16 295 / 0.09)   /* tinted backgrounds */
--brand-border: oklch(0.55 0.16 295 / 0.30)
--brand-text:   oklch(0.40 0.14 295)          /* darker, for text on white */
```

### Spectrum — for restrained iridescence

```
--spec-1: oklch(0.62 0.18 305)   /* plum */
--spec-2: oklch(0.66 0.15 260)   /* indigo */
--spec-3: oklch(0.72 0.12 210)   /* cyan-teal */
--spec-4: oklch(0.78 0.13 75)    /* warm amber */
```

These four colors compose the **aurora gradient** that's used very sparingly: hero number, page wash, sparkline strokes, logo fill, active tab indicator.

```
--grad-aurora: linear-gradient(115deg, --spec-1 0%, --spec-2 40%, --spec-3 75%, --spec-4 100%)
--grad-brand:  linear-gradient(120deg, oklch(0.50 0.18 305) 0%, oklch(0.58 0.16 275) 100%)
--grad-rim:    linear-gradient(90deg, transparent 0%, plum-translucent 25%, cyan-translucent 60%, transparent 100%)
```

**Rules for using gradients:**
- Never on a large background fill
- Only on data (sparkline strokes/areas), brand marks (logo, active indicator), or hero numbers/H1s
- The page-level wash is at 4–5% opacity — it's a *suggestion* of color, not a feature

### Semantics — muted, not alarmist

```
--ok:     oklch(0.58 0.12 155)   green — chroma intentionally low
--warn:   oklch(0.70 0.14 65)    amber
--danger: oklch(0.60 0.17 25)    red, also restrained
```

Each has matching `-fill`, `-border`, `-text` variants. See `tokens.css`.

### Typography

```
--font-sans: 'Inter', system-ui, -apple-system, 'Segoe UI', sans-serif
--font-mono: 'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, monospace
```

Load Inter at weights **400, 500, 600, 700** — body uses 400, key controls 500, eyebrow/badges/section heads 600, H1/H2 700. Without 600/700 the contrast collapses.

Load JetBrains Mono at **400, 500, 600**.

### Type scale (final, non-negotiable)

| Role | Size | Weight | Letter-spacing | Color |
|---|---|---|---|---|
| H1 (page title) | 44 | 700 | -0.04em | `--text` (or `.grad-text`) |
| H1 lede paragraph | 17 | 400 | -0.005em | `--text` (first half) → `--text-soft` (continuation) |
| H2 (section) | 26 | 700 | -0.03em | `--text` |
| H3 / panel head | 17 | 700 | -0.02em | `--text` |
| Body | 13 | 400 | 0 | `--text` |
| Description / subtitle | 12 | 400 | 0 | `--text-soft` |
| Caption / timestamp | 11 | 400 | 0 | `--text-subtle` |
| Eyebrow / data label | 10 | 600 | 0.12em uppercase | `--text-subtle`, mono |
| Big number (hero) | 76 | 600 | -0.06em | mono, gradient text |
| Big number (stat) | 32 | 600 | -0.04em | mono, plain |
| Tab (active) | 13 | 600 | -0.005em | `--text` |
| Tab (inactive) | 13 | 500 | 0 | `--text-soft` |
| Pill / badge | 11 | 600 | -0.01em (mono) / 0.005em (sans) | tone-dependent |

### Spacing — small, fixed

```
--s-1: 4   --s-2: 8   --s-3: 12   --s-4: 16   --s-6: 24   --s-8: 32   --s-12: 48
```

### Radii — small, restrained

```
--r-pill: 4    /* tiny chips */
--r-tag:  6    /* badges, buttons, segmented control */
--r-card: 10   /* cards */
--r-shell:14   /* outer shells, modal */
```

### Elevation — none

`--shadow-card: none; --shadow-pop: none;` — flat. Hierarchy via hairlines + the aurora rim only.

---

## Layout shell

```jsx
<div className="min-h-screen relative">
  <div className="page-wash" />          {/* fixed, z-0, pointer-events-none */}
  <TopNav />
  <main className="max-w-[1240px] mx-auto px-7 pt-8 pb-16 relative z-10">
    {/* page content */}
  </main>
</div>
```

The `page-wash` div is **critical**. It's a fixed pseudo-background that adds two giant blurred radial gradients in the top corners (~5% opacity). That tiny color cast is what makes the white page feel premium instead of sterile. Without it the design is 30% less alive.

---

## Component treatments

### Hero card (the 94.2% on monitor page)

- `.aurora-rim` class on the card → adds a gradient hairline at the top (12px inset L/R)
- Big number: `JetBrains Mono / 76 / 600 / -0.06em / tabular-nums`, wrapped in `.grad-text-aurora` for plum→indigo→cyan gradient text
- Wrap the number row in a `.aurora-glow` element → adds a soft radial glow under the number
- Eyebrow above: `.eyebrow` class + a 6px `<StatusDot status="healthy" live />` inline

### Sparklines (gradient stroke + gradient area)

Two flavors — full-size hero (~110px tall) on the cache-rate card, and inline 24px sparklines in the stat strip and mirror rows.

Both use **two SVG `linearGradient` defs per instance** (unique IDs):
- One horizontal gradient for the **stroke** (two stops from the spectrum, picked by tone)
- One vertical gradient for the **area fill** (top color at ~18% opacity → fully transparent at bottom)

Stroke widths: hero = 1.4px, inline = 1.1px. The endpoint dot uses the second stroke stop color, with a translucent halo circle at 18% opacity 7px radius (hero) or 1.75px solid (inline).

Tone → stops:
- `ok`     → cyan (`--spec-3`) → green (`--ok`)
- `warn`   → amber-warn → spec-4
- `danger` → red-danger → plum (`--spec-1`)
- `brand`  → plum (`--spec-1`) → indigo (`--spec-2`)
- `neutral`→ two muted blue-grays (subtle but not flat)

See `reference/primitives.jsx` `Sparkline` component for the exact SVG structure.

### StatusDot

6px solid circle, color tied to status. The `live` variant adds the `dot-live` pulse animation (already in tokens). For more presence, wrap in `.dot-halo` to add a 3px-spread radial halo at 25% opacity using `currentColor`.

### Logo

20×20 rounded-rect fills with `linearGradient` (plum 0% → indigo 60% → cyan 100% diagonal), three concentric white circles inside (40% / 70% / 100% opacity from outer to center). The diagonal gradient is what makes it feel like a brand mark instead of a CSS box.

### Tabs (top nav)

Active state combines THREE signals:
1. Color: active = `--text`, inactive = `--text-soft`
2. Weight: active = 600, inactive = 500
3. Indicator: 1.5px-tall pill bar 15px below the text, `background: var(--grad-brand)` (plum→indigo gradient — not flat color)

Hover transitions only color & weight (120ms ease).

### Pills / badges

- Mono data pills (e.g. "+1.4 pts"): mono / 11 / 600 / -0.01em, 2px×6px padding, semantic `-fill` background, `-border` border, `-text` text
- Status pills in topnav: 11 / 600 / 0.005em sans, 4px×10px padding, white bg, `--border` hairline

### Segmented control

The active option gets:
- bg: `var(--bg-card)` (white) sitting on `var(--bg-soft)` track
- border: `0.5px var(--border)`
- text: `--text` weight 500
- *(optional escalation)* a 1.5px brand-gradient bottom rule like the tab indicator — only if you want extra brand presence

Inactive: transparent, `--text-muted`.

### Code block

Outer: `--bg-soft` background, `0.5px var(--border)`, 8px radius, `overflow: hidden`.
Header bar: 28px tall, `--bg-card` background, bottom hairline. File name in mono 11px `--text-muted`. Language label uses `.eyebrow`. Copy button on the right.
Body: padding 12×14px, mono 12px / 1.6 line-height, syntax tokens:
- `comment` → `--text-subtle` italic
- `var` (the `{VARIABLE}` placeholders) → `--brand` weight 500
- `str` (quoted strings) → `--ok-text`

### CopyButton

Minimal: ghost button with `0.5px var(--border)`, mono icon (clipboard), label "Copy" → "Copied" (with checkmark + `--ok-text`) for 1.4s after click. Hover deepens border + text. Don't add a filled variant.

### Banner (warning)

Inline horizontal strip at the top of monitor page when daemon is degraded. Tone-aware:
- `--warn-fill` background, `--warn-border` border, `--warn-text` text
- Right side: action button with `--bg-card` background, weight 500, dismiss × button at far right
- Mono text for service identifiers within the message

### Tweaks panel

Existing prototype uses the tweaks-panel.jsx starter. **Do not port this.** The Tweaks panel is a prototype-only affordance for live demoing knobs (accent color, daemon status, endpoint, etc.). Replace with proper settings UI in your app.

---

## Things to NOT carry over

- The `?v=N` cache-buster on the CSS link — that's a prototype-only hack
- The `tweaks-panel.jsx` and `data.jsx` files — prototype scaffolding
- The fade-up animation on every page (it conflicts with React Router transitions in real apps; reserve for first-paint or remove)
- The `.dot-pulse` keyframes specifically — fine, but check it doesn't fight your existing animation library

---

## Tailwind v4 integration notes

In Tailwind v4, the `@theme` directive turns CSS variables into utility classes automatically. Recommended structure for your app:

```css
/* app/globals.css */
@import "tailwindcss";

@theme {
  /* paste tokens.css :root variables here, prefixed with --color-* / --font-* / etc. */
  /* see tokens.css for the v4-renamed version */
}

/* utilities.css contents go here (or @import) */
```

After this, you can write `text-text`, `text-text-soft`, `bg-bg-soft`, `border-border`, `font-mono` etc. as Tailwind utilities, while the `.eyebrow` / `.grad-text` / `.aurora-rim` classes from `utilities.css` are available globally for the special treatments that don't compose well as utility chains.

For the gradients (`--grad-aurora`, `--grad-brand`, `--grad-rim`), keep them as CSS variables and reference via `bg-[var(--grad-brand)]` or use the prebuilt utility classes — Tailwind doesn't have first-class gradient tokens.

---

## Checklist for the migration

- [ ] Tokens added to `@theme` (or imported as `tokens.css`)
- [ ] `utilities.css` imported globally
- [ ] Inter loaded at 400/500/600/700 + JetBrains Mono at 400/500/600
- [ ] Body defaults set: 13px / 1.5 / `--text` / antialiased
- [ ] H1/H2/H3 base styles match the type scale table
- [ ] `<div className="page-wash" />` mounted at app root
- [ ] All Sparkline/chart instances re-rendered with gradient stroke + gradient area
- [ ] Logo re-rendered with diagonal gradient fill
- [ ] Active tab uses `--grad-brand` 1.5px indicator
- [ ] Hero numbers use mono + tabular-nums + grad-text where appropriate
- [ ] Cards use `0.5px solid var(--border)`, never shadows
- [ ] All uppercase labels use `.eyebrow` class
- [ ] Card titles use `--text`, descriptions use `--text-soft` (not `--text-muted`)
- [ ] Banner / pill / status colors all driven by semantic tokens (no hex literals)
