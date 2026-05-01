# Design System Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current Stripe-inspired design system with the new plum-violet token system (iridescent utilities, pure-white surfaces, flat cards, aurora gradients) across the entire Portal and Admin UI.

**Architecture:** Single CSS foundation (`index.css`) is fully replaced with new tokens + utilities; all component inline styles are updated from Stripe var names to new names; two new primitive components (`Sparkline`, `StatusDot`) are added; existing `Logo`, `Badge`, `Tabs`, `Card`, `MetricCard`, `CodeBlock`, `PortalApp` topnav, and `MainLayout` sidebar are each updated in one self-contained pass; a final sweep converts remaining Stripe var references in admin/portal pages.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind CSS v4, i18next — no new deps required.

---

## Var Name Mapping (reference for all tasks)

| Old (Stripe) | New (token system) |
|---|---|
| `var(--heading)` | `var(--text)` |
| `var(--label)` | `var(--text-muted)` |
| `var(--body)` | `var(--text-soft)` |
| `var(--surface)` | `var(--bg-card)` |
| `var(--surface-low)` | `var(--bg-soft)` |
| `var(--surface-container)` | `var(--bg-soft)` |
| `var(--bg)` | `var(--bg-page)` |
| `var(--stripe-purple)` | `var(--brand)` |
| `var(--on-primary)` | `#ffffff` |
| `var(--success)` | `var(--ok)` |
| `var(--success-text)` | `var(--ok-text)` |
| `var(--error)` | `var(--danger)` |
| `var(--error-container)` | `var(--danger-fill)` |
| `var(--lemon)` | `var(--warn-text)` |
| `box-shadow: var(--shadow-*)` | remove entirely |
| `border: '1px solid var(--border)'` | `border: '0.5px solid var(--border)'` |
| TW class `text-on-surface-variant` | inline `style={{ color: 'var(--text-soft)' }}` |
| TW class `text-heading` | inline `style={{ color: 'var(--text)' }}` |
| TW class `bg-surface-container` | inline `style={{ background: 'var(--bg-soft)' }}` |
| TW class `hover:bg-surface-container` | onMouseEnter/Leave inline |

---

## Task 1: Replace `index.css` — CSS Foundation

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Overwrite `index.css` with new token system**

Replace the entire file contents with:

```css
@import "tailwindcss";
@import "@fontsource-variable/inter";
@import "@fontsource-variable/jetbrains-mono";
@import "@fontsource/noto-sans-sc/400.css";
@import "@fontsource/noto-sans-sc/500.css";
@import "@fontsource/noto-sans-sc/700.css";
@import "material-symbols/outlined.css";

/* ── Tailwind v4 @theme — turns CSS vars into utility classes ── */
@theme {
  --color-bg-page:   #FFFFFF;
  --color-bg-panel:  #FFFFFF;
  --color-bg-card:   #FFFFFF;
  --color-bg-soft:   #F5F5F5;
  --color-bg-hover:  rgba(0, 0, 0, 0.035);

  --color-border:        rgba(0, 0, 0, 0.09);
  --color-border-soft:   rgba(0, 0, 0, 0.05);
  --color-border-strong: rgba(0, 0, 0, 0.18);

  --color-text:        #0a0a0a;
  --color-text-muted:  #555555;
  --color-text-soft:   #767676;
  --color-text-subtle: #8a8a8a;

  --color-brand:        oklch(0.55 0.16 295);
  --color-brand-strong: oklch(0.48 0.17 295);
  --color-brand-soft:   oklch(0.55 0.16 295 / 0.09);
  --color-brand-border: oklch(0.55 0.16 295 / 0.30);
  --color-brand-text:   oklch(0.40 0.14 295);

  --color-spec-1: oklch(0.62 0.18 305);
  --color-spec-2: oklch(0.66 0.15 260);
  --color-spec-3: oklch(0.72 0.12 210);
  --color-spec-4: oklch(0.78 0.13 75);

  --color-ok:          oklch(0.58 0.12 155);
  --color-ok-fill:     oklch(0.58 0.12 155 / 0.10);
  --color-ok-border:   oklch(0.58 0.12 155 / 0.28);
  --color-ok-text:     oklch(0.40 0.10 155);

  --color-warn:        oklch(0.70 0.14 65);
  --color-warn-fill:   oklch(0.70 0.14 65 / 0.12);
  --color-warn-border: oklch(0.70 0.14 65 / 0.30);
  --color-warn-text:   oklch(0.45 0.12 55);

  --color-danger:        oklch(0.60 0.17 25);
  --color-danger-fill:   oklch(0.60 0.17 25 / 0.10);
  --color-danger-border: oklch(0.60 0.17 25 / 0.30);
  --color-danger-text:   oklch(0.42 0.15 25);

  /* Keep Chinese fallbacks for i18n support */
  --font-sans: 'Inter Variable', 'Noto Sans SC', 'PingFang SC', 'Microsoft YaHei', sans-serif;
  --font-mono: 'JetBrains Mono Variable', ui-monospace, monospace;

  --spacing-1:  4px;
  --spacing-2:  8px;
  --spacing-3:  12px;
  --spacing-4:  16px;
  --spacing-6:  24px;
  --spacing-8:  32px;
  --spacing-12: 48px;

  --radius-pill:  4px;
  --radius-tag:   6px;
  --radius-card:  10px;
  --radius-shell: 14px;
}

/* ── :root mirror — keeps var(--text) etc. working in inline styles ── */
:root {
  --bg-page:   #FFFFFF;
  --bg-panel:  #FFFFFF;
  --bg-card:   #FFFFFF;
  --bg-soft:   #F5F5F5;
  --bg-hover:  rgba(0, 0, 0, 0.035);

  --border:        rgba(0, 0, 0, 0.09);
  --border-soft:   rgba(0, 0, 0, 0.05);
  --border-strong: rgba(0, 0, 0, 0.18);

  --text:        #0a0a0a;
  --text-muted:  #555555;
  --text-soft:   #767676;
  --text-subtle: #8a8a8a;

  --brand:        oklch(0.55 0.16 295);
  --brand-strong: oklch(0.48 0.17 295);
  --brand-soft:   oklch(0.55 0.16 295 / 0.09);
  --brand-border: oklch(0.55 0.16 295 / 0.30);
  --brand-text:   oklch(0.40 0.14 295);

  --spec-1: oklch(0.62 0.18 305);
  --spec-2: oklch(0.66 0.15 260);
  --spec-3: oklch(0.72 0.12 210);
  --spec-4: oklch(0.78 0.13 75);

  --grad-aurora: linear-gradient(
    115deg,
    var(--spec-1) 0%,
    var(--spec-2) 40%,
    var(--spec-3) 75%,
    var(--spec-4) 100%
  );
  --grad-brand: linear-gradient(
    120deg,
    oklch(0.50 0.18 305) 0%,
    oklch(0.58 0.16 275) 100%
  );
  --grad-rim: linear-gradient(
    90deg,
    transparent 0%,
    oklch(0.55 0.16 295 / 0.45) 25%,
    oklch(0.66 0.15 230 / 0.45) 60%,
    transparent 100%
  );

  --ok:          oklch(0.58 0.12 155);
  --ok-fill:     oklch(0.58 0.12 155 / 0.10);
  --ok-border:   oklch(0.58 0.12 155 / 0.28);
  --ok-text:     oklch(0.40 0.10 155);

  --warn:        oklch(0.70 0.14 65);
  --warn-fill:   oklch(0.70 0.14 65 / 0.12);
  --warn-border: oklch(0.70 0.14 65 / 0.30);
  --warn-text:   oklch(0.45 0.12 55);

  --danger:        oklch(0.60 0.17 25);
  --danger-fill:   oklch(0.60 0.17 25 / 0.10);
  --danger-border: oklch(0.60 0.17 25 / 0.30);
  --danger-text:   oklch(0.42 0.15 25);

  --r-pill: 4px;
  --r-tag:  6px;
  --r-card: 10px;
  --r-shell: 14px;

  --s-1: 4px; --s-2: 8px; --s-3: 12px; --s-4: 16px;
  --s-6: 24px; --s-8: 32px; --s-12: 48px;

  --shadow-card: none;
  --shadow-pop:  none;
}

/* ── Body defaults ── */
html, body {
  background: var(--bg-page);
  color: var(--text);
  font-family: var(--font-sans);
  font-size: 13px;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  font-feature-settings: 'cv11', 'ss01', 'ss03';
}
body { margin: 0; }
* { box-sizing: border-box; }

h1, h2, h3, h4 {
  color: var(--text);
  font-weight: 700;
  letter-spacing: -0.025em;
  line-height: 1.1;
  margin: 0;
}
h1 { font-size: 44px; letter-spacing: -0.04em; line-height: 1.02; }
h2 { font-size: 26px; letter-spacing: -0.03em; }
h3 { font-size: 17px; letter-spacing: -0.02em; line-height: 1.2; }
h4 { font-size: 13px; font-weight: 600; letter-spacing: 0; line-height: 1.3; }

::selection { background: var(--brand-soft); }

.num, .mono, [data-tabular] {
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
}

/* ── Material Symbols (unchanged) ── */
.icon {
  font-family: 'Material Symbols Outlined';
  font-weight: normal;
  font-style: normal;
  font-size: 20px;
  line-height: 1;
  letter-spacing: normal;
  text-transform: none;
  display: inline-block;
  white-space: nowrap;
  word-wrap: normal;
  direction: ltr;
  font-feature-settings: 'liga';
  -webkit-font-smoothing: antialiased;
}
.icon-sm { font-size: 18px; }
.icon-lg { font-size: 24px; }

/* ── Iridescent utilities ── */

.eyebrow {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-subtle);
}

.grad-text {
  background: var(--grad-brand);
  -webkit-background-clip: text;
          background-clip: text;
  -webkit-text-fill-color: transparent;
          color: transparent;
}

.grad-text-aurora {
  background: var(--grad-aurora);
  -webkit-background-clip: text;
          background-clip: text;
  -webkit-text-fill-color: transparent;
          color: transparent;
}

.aurora-rim {
  position: relative;
}
.aurora-rim::before {
  content: '';
  position: absolute;
  top: 0; left: 12px; right: 12px;
  height: 1px;
  background: var(--grad-rim);
  opacity: 0.9;
  pointer-events: none;
  border-radius: 1px;
}

.page-wash {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 0;
  background:
    radial-gradient(900px 360px at 12% -10%, oklch(0.62 0.18 305 / 0.05), transparent 60%),
    radial-gradient(720px 320px at 88% -8%,  oklch(0.72 0.12 210 / 0.045), transparent 60%);
}

.grad-ring {
  position: relative;
  background: var(--bg-card);
  border-radius: inherit;
}
.grad-ring::before {
  content: '';
  position: absolute;
  inset: 0;
  padding: 1px;
  border-radius: inherit;
  background: var(--grad-aurora);
  -webkit-mask: linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0);
          mask: linear-gradient(#000 0 0) content-box, linear-gradient(#000 0 0);
  -webkit-mask-composite: xor;
          mask-composite: exclude;
  pointer-events: none;
}

.aurora-glow {
  position: relative;
}
.aurora-glow::after {
  content: '';
  position: absolute;
  left: -8%; right: -8%;
  top: 30%; bottom: -20%;
  background: radial-gradient(60% 80% at 50% 50%, oklch(0.62 0.18 305 / 0.10), transparent 70%);
  filter: blur(24px);
  z-index: -1;
  pointer-events: none;
}

.dot-halo {
  position: relative;
}
.dot-halo::before {
  content: '';
  position: absolute;
  inset: -3px;
  border-radius: 50%;
  background: radial-gradient(circle, currentColor 0%, transparent 70%);
  opacity: 0.25;
  pointer-events: none;
}

@keyframes dotPulse {
  0%, 100% { box-shadow: 0 0 0 0 currentColor; }
  50%       { box-shadow: 0 0 0 4px transparent; }
}
.dot-live {
  animation: dotPulse 1.6s ease-in-out infinite;
}

.card {
  background: var(--bg-card);
  border: 0.5px solid var(--border);
  border-radius: var(--r-card);
}

.shell {
  background: var(--bg-card);
  border: 0.5px solid var(--border);
  border-radius: var(--r-shell);
}

.divider-h {
  height: 0.5px;
  background: var(--border);
}

.row-hover { transition: background 140ms ease; }
.row-hover:hover { background: var(--bg-hover); }

@keyframes fadeUp {
  from { opacity: 0; transform: translateY(4px); }
  to   { opacity: 1; transform: translateY(0); }
}
.fade-up { animation: fadeUp 220ms ease both; }

@keyframes skeletonPulse {
  0%, 100% { opacity: 0.5; }
  50%       { opacity: 0.85; }
}
.skeleton {
  background: var(--bg-soft);
  border-radius: 4px;
  animation: skeletonPulse 1.4s ease-in-out infinite;
}

/* Keep existing animate-* helpers for pages that use them */
@keyframes ping-health {
  0%   { transform: scale(1); opacity: 1; }
  75%  { transform: scale(2.2); opacity: 0; }
  100% { transform: scale(2.2); opacity: 0; }
}
@layer utilities {
  .animate-ping-health { animation: ping-health 1.5s cubic-bezier(0,0,0.2,1) infinite; }
}

@keyframes fade-in-down {
  from { opacity: 0; transform: translateY(-8px); }
  to   { opacity: 1; transform: translateY(0); }
}
@layer utilities {
  .animate-fade-in { animation: fade-in-down 0.3s ease-out; }
}

::-webkit-scrollbar { width: 10px; height: 10px; }
::-webkit-scrollbar-thumb {
  background: var(--border-strong);
  border-radius: 4px;
  border: 2px solid var(--bg-page);
}
::-webkit-scrollbar-track { background: transparent; }
```

- [ ] **Step 2: Verify build compiles**

```bash
cd web && npm run build 2>&1 | tail -20
```

Expected: build succeeds (there will be TS errors from Stripe var refs in other files — those are handled in later tasks; Vite CSS should compile cleanly).

- [ ] **Step 3: Commit**

```bash
git add web/src/index.css
git commit -m "feat(ui): replace Stripe design system with new token system"
```

---

## Task 2: Root Layout — `page-wash` + `ThemeToggle` stub

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/ThemeToggle.tsx`

- [ ] **Step 1: Add `page-wash` div to `App.tsx`**

In `AppRoutes()`, wrap the `<Routes>` return in a relative shell and insert `page-wash`:

```tsx
// Change the final return in AppRoutes() from:
return (
  <Routes>
    <Route path="/admin/*" element={<AdminApp />} />
    <Route path="/*" element={<PortalApp />} />
  </Routes>
)

// To:
return (
  <div className="relative min-h-screen">
    <div className="page-wash" />
    <Routes>
      <Route path="/admin/*" element={<AdminApp />} />
      <Route path="/*" element={<PortalApp />} />
    </Routes>
  </div>
)
```

Also fix the loading state color (line ~27):
```tsx
// Before:
<div style={{ color: 'var(--body)' }}>Loading...</div>
// After:
<div style={{ color: 'var(--text-soft)' }}>Loading...</div>
```

- [ ] **Step 2: Stub `ThemeToggle.tsx`**

Replace the entire file with a non-functional light-only button:

```tsx
import Icon from './Icon'

/** Light-only stub. Dark mode CSS is not implemented; button is kept for future use. */
export default function ThemeToggle() {
  return (
    <button
      disabled
      className="bg-transparent cursor-not-allowed rounded-[4px] p-1.5"
      style={{
        color: 'var(--text-subtle)',
        border: '0.5px solid var(--border)',
      }}
      aria-label="Theme: light (dark mode not yet available)"
      title="Dark mode coming soon"
    >
      <Icon name="light_mode" size="sm" />
    </button>
  )
}
```

- [ ] **Step 3: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep -E "error TS" | head -20
```

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/components/ThemeToggle.tsx
git commit -m "feat(ui): add page-wash, stub ThemeToggle for light-only mode"
```

---

## Task 3: Logo — Inline SVG with Aurora Gradient

**Files:**
- Modify: `web/src/components/Logo.tsx`

- [ ] **Step 1: Replace `Logo.tsx` with inline SVG**

The old file imported an external SVG file. Replace it with an inline SVG that renders a rounded-rect with diagonal plum→indigo→cyan gradient fill and three concentric white circles. `useId()` prevents gradient ID collisions when multiple instances exist.

```tsx
import { useId } from 'react'

interface LogoProps {
  className?: string
  height?: number
}

export default function Logo({ className = '', height = 28 }: LogoProps) {
  const uid = useId().replace(/:/g, '')
  const gradId = `logo-grad-${uid}`

  return (
    <svg
      width={height}
      height={height}
      viewBox="0 0 20 20"
      fill="none"
      className={className}
      aria-label="Depsilo"
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%"   stopColor="oklch(0.50 0.20 305)" />
          <stop offset="60%"  stopColor="oklch(0.56 0.16 270)" />
          <stop offset="100%" stopColor="oklch(0.66 0.13 220)" />
        </linearGradient>
      </defs>
      <rect x="1" y="1" width="18" height="18" rx="5" fill={`url(#${gradId})`} />
      <circle cx="10" cy="10" r="5.5" fill="none" stroke="#fff" strokeWidth="1.2" opacity="0.4" />
      <circle cx="10" cy="10" r="3.2" fill="none" stroke="#fff" strokeWidth="1.2" opacity="0.7" />
      <circle cx="10" cy="10" r="1.2" fill="#fff" />
    </svg>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "Logo" | head -10
```

Expected: no errors relating to Logo.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Logo.tsx
git commit -m "feat(ui): replace Logo img with inline SVG aurora gradient mark"
```

---

## Task 4: New `StatusDot` Component

**Files:**
- Create: `web/src/components/StatusDot.tsx`

- [ ] **Step 1: Create `StatusDot.tsx`**

```tsx
interface StatusDotProps {
  /** 'healthy' → green, 'degraded' → amber, 'failed' → red, other → subtle */
  status: 'healthy' | 'degraded' | 'failed' | string
  size?: number
  /** Adds the dotPulse animation (defined in utilities CSS) */
  live?: boolean
}

export default function StatusDot({ status, size = 6, live = false }: StatusDotProps) {
  const color =
    status === 'healthy'  ? 'var(--ok)' :
    status === 'degraded' ? 'var(--warn)' :
    status === 'failed'   ? 'var(--danger)' :
                            'var(--text-subtle)'

  return (
    <span
      className={live ? 'dot-live' : ''}
      style={{
        display: 'inline-block',
        width: size,
        height: size,
        borderRadius: '50%',
        background: color,
        color, // used by dotPulse box-shadow keyframe via currentColor
        flexShrink: 0,
      }}
    />
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "StatusDot" | head -10
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/StatusDot.tsx
git commit -m "feat(ui): add StatusDot primitive (ok/warn/danger + live pulse)"
```

---

## Task 5: New `Sparkline` Component

**Files:**
- Create: `web/src/components/Sparkline.tsx`

- [ ] **Step 1: Create `Sparkline.tsx`**

```tsx
import { useMemo } from 'react'

type Tone = 'ok' | 'warn' | 'danger' | 'brand' | 'neutral'

interface SparklineProps {
  values: number[]
  width?: number
  height?: number
  tone?: Tone
  showDot?: boolean
}

const STROKE_STOPS: Record<Tone, [string, string]> = {
  ok:      ['var(--spec-3)', 'var(--ok)'],
  warn:    ['var(--warn)',   'var(--spec-4)'],
  danger:  ['var(--danger)', 'var(--spec-1)'],
  brand:   ['var(--spec-1)', 'var(--spec-2)'],
  neutral: ['oklch(0.78 0.04 260)', 'oklch(0.70 0.05 240)'],
}

const AREA_STOPS: Record<Tone, [string, string]> = {
  ok:      ['oklch(0.72 0.12 210 / 0.18)', 'oklch(0.58 0.12 155 / 0)'],
  warn:    ['oklch(0.70 0.14 65  / 0.18)', 'oklch(0.78 0.13 75  / 0)'],
  danger:  ['oklch(0.60 0.17 25  / 0.18)', 'oklch(0.62 0.18 305 / 0)'],
  brand:   ['oklch(0.62 0.18 305 / 0.20)', 'oklch(0.66 0.15 260 / 0)'],
  neutral: ['oklch(0.7  0.04 260 / 0.10)', 'oklch(0.7  0.04 260 / 0)'],
}

export default function Sparkline({
  values,
  width = 120,
  height = 24,
  tone = 'neutral',
  showDot = true,
}: SparklineProps) {
  // Stable unique IDs per instance (avoid SVG gradient ID collisions)
  const gradId = useMemo(() => `spark-${Math.random().toString(36).slice(2, 8)}`, [])
  const areaGradId = `${gradId}-a`

  if (!values || values.length < 2) return null

  const n = values.length
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const padY = 2
  const usableH = height - padY * 2

  const pts = values.map((v, i) => [
    (i / (n - 1)) * (width - 1) + 0.5,
    padY + (1 - (v - min) / range) * usableH,
  ])

  const linePath = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${p[0]},${p[1]}`).join(' ')
  const areaPath = `${linePath} L${width - 0.5},${height} L0.5,${height} Z`
  const last = pts[pts.length - 1]
  const stroke = STROKE_STOPS[tone]
  const area   = AREA_STOPS[tone]

  return (
    <svg
      width={width}
      height={height}
      style={{ display: 'block', overflow: 'visible' }}
    >
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%"   stopColor={stroke[0]} />
          <stop offset="100%" stopColor={stroke[1]} />
        </linearGradient>
        <linearGradient id={areaGradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%"   stopColor={area[0]} />
          <stop offset="100%" stopColor={area[1]} />
        </linearGradient>
      </defs>
      <path d={areaPath} fill={`url(#${areaGradId})`} />
      <path
        d={linePath}
        fill="none"
        stroke={`url(#${gradId})`}
        strokeWidth="1.1"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      {showDot && (
        <circle cx={last[0]} cy={last[1]} r="1.75" fill={stroke[1]} />
      )}
    </svg>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "Sparkline" | head -10
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Sparkline.tsx
git commit -m "feat(ui): add Sparkline primitive (gradient stroke + area fill, 5 tones)"
```

---

## Task 6: `Badge` — Semantic Token Remap

**Files:**
- Modify: `web/src/components/Badge.tsx`

- [ ] **Step 1: Replace `Badge.tsx`**

```tsx
import { type ReactNode } from 'react'

type BadgeVariant = 'default' | 'success' | 'error' | 'warning' | 'pro' | 'ecosystem'

interface BadgeV2Props {
  variant?: BadgeVariant
  children: ReactNode
  className?: string
}

const variantStyles: Record<BadgeVariant, { bg: string; color: string; border: string }> = {
  default:   { bg: 'var(--bg-soft)',      color: 'var(--text-muted)',  border: 'var(--border)' },
  ecosystem: { bg: 'var(--bg-soft)',      color: 'var(--text)',        border: 'var(--border)' },
  success:   { bg: 'var(--ok-fill)',      color: 'var(--ok-text)',     border: 'var(--ok-border)' },
  error:     { bg: 'var(--danger-fill)',  color: 'var(--danger-text)', border: 'var(--danger-border)' },
  warning:   { bg: 'var(--warn-fill)',    color: 'var(--warn-text)',   border: 'var(--warn-border)' },
  pro:       { bg: 'var(--grad-aurora)',  color: '#ffffff',            border: 'transparent' },
}

export default function BadgeV2({
  variant = 'default',
  children,
  className = '',
}: BadgeV2Props) {
  const s = variantStyles[variant]
  const isPro = variant === 'pro'

  return (
    <span
      className={`inline-flex items-center whitespace-nowrap ${className}`}
      style={{
        padding: '2px 6px',
        fontSize: 11,
        fontWeight: 600,
        letterSpacing: '0.005em',
        borderRadius: 'var(--r-tag)',
        background: isPro ? 'var(--grad-aurora)' : s.bg,
        color: s.color,
        border: s.border !== 'transparent' ? `0.5px solid ${s.border}` : 'none',
      }}
    >
      {children}
    </span>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "Badge" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Badge.tsx
git commit -m "feat(ui): remap Badge variants to semantic tokens (ok/warn/danger/brand)"
```

---

## Task 7: `Tabs` — `--grad-brand` Active Indicator

**Files:**
- Modify: `web/src/components/Tabs.tsx`

- [ ] **Step 1: Replace `Tabs.tsx`**

The active indicator changes from a `borderBottom` on the button to an absolute-positioned `<span>` with `background: var(--grad-brand)`. The container hairline stays.

```tsx
interface TabItem {
  key: string
  label: string
  icon?: React.ReactNode
}

interface TabsV2Props {
  items: TabItem[]
  active: string
  onChange: (key: string) => void
}

export default function TabsV2({ items, active, onChange }: TabsV2Props) {
  return (
    <div className="flex gap-0" style={{ borderBottom: '0.5px solid var(--border)' }}>
      {items.map((tab) => {
        const isActive = active === tab.key
        return (
          <button
            key={tab.key}
            onClick={() => onChange(tab.key)}
            className="relative flex items-center gap-2 bg-transparent border-none cursor-pointer"
            style={{
              padding: '6px 16px',
              fontSize: 13,
              fontWeight: isActive ? 600 : 500,
              letterSpacing: isActive ? '-0.005em' : '0',
              color: isActive ? 'var(--text)' : 'var(--text-soft)',
              transition: 'color 120ms ease',
            }}
          >
            {tab.icon}
            {tab.label}
            {isActive && (
              <span
                style={{
                  position: 'absolute',
                  left: 10,
                  right: 10,
                  bottom: -1,
                  height: 1.5,
                  background: 'var(--grad-brand)',
                  borderRadius: 1,
                }}
              />
            )}
          </button>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "Tabs" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Tabs.tsx
git commit -m "feat(ui): update Tabs to grad-brand active indicator (1.5px gradient pill)"
```

---

## Task 8: `Card` — Flat, `0.5px` Border

**Files:**
- Modify: `web/src/components/Card.tsx`

- [ ] **Step 1: Replace `Card.tsx`**

Remove shadows entirely. `elevated` prop is kept in the interface for backward compat but has no visual effect.

```tsx
import { type ReactNode } from 'react'

interface CardV2Props {
  children?: ReactNode
  className?: string
  /** Retained for API compat; visually no-op (flat design has no elevation) */
  elevated?: boolean
  noPad?: boolean
}

export default function CardV2({
  children,
  className = '',
  noPad = false,
}: CardV2Props) {
  return (
    <div
      className={`${noPad ? '' : 'p-5'} ${className}`}
      style={{
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-card)',
      }}
    >
      {children}
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "Card" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/Card.tsx
git commit -m "feat(ui): flatten Card (0.5px border, no shadow, --r-card radius)"
```

---

## Task 9: `MetricCard` — New Token System + Sparkline Slot

**Files:**
- Modify: `web/src/components/MetricCard.tsx`

- [ ] **Step 1: Replace `MetricCard.tsx`**

Adds optional `sparkline` slot. Label uses `.eyebrow` class. Number: mono 32/600/-0.04em.

```tsx
import { type ReactNode } from 'react'

interface MetricCardV2Props {
  label: string
  value: string
  icon?: ReactNode
  change?: number | null
  /** Optional Sparkline element rendered bottom-right */
  sparkline?: ReactNode
}

export default function MetricCardV2({ label, value, icon, change, sparkline }: MetricCardV2Props) {
  return (
    <div
      className="p-4"
      style={{
        background: 'var(--bg-card)',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-card)',
      }}
    >
      <div className="flex items-center justify-between mb-2">
        <span className="eyebrow">{label}</span>
        {icon && <span style={{ color: 'var(--text-subtle)' }}>{icon}</span>}
      </div>
      <div className="flex items-baseline justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span
            className="font-mono tabular-nums"
            style={{
              fontSize: 32,
              fontWeight: 600,
              letterSpacing: '-0.04em',
              color: 'var(--text)',
              lineHeight: 1,
            }}
          >
            {value}
          </span>
          {typeof change === 'number' && (
            <span
              className="font-mono tabular-nums text-[11px]"
              style={{ color: change >= 0 ? 'var(--ok-text)' : 'var(--danger-text)' }}
            >
              {change >= 0 ? '+' : ''}{change.toFixed(1)}%
            </span>
          )}
        </div>
        {sparkline && <div className="shrink-0">{sparkline}</div>}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "MetricCard" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/MetricCard.tsx
git commit -m "feat(ui): update MetricCard to new tokens, eyebrow label, sparkline slot"
```

---

## Task 10: `CodeBlock` (Portal) — Light Theme

**Files:**
- Modify: `web/src/portal/components/CodeBlock.tsx`

- [ ] **Step 1: Replace `CodeBlock.tsx`**

Migrates from dark navy theme to light: `--bg-soft` outer, `--bg-card` header, lightweight SVG copy button (no Material Icon dependency for the icon itself), inline syntax highlighting for `{VAR}`, `"strings"`, and `# comments`.

```tsx
import { useState, useCallback, useMemo } from 'react'

interface CodeBlockV2Props {
  filename?: string
  code: string
  language?: string
}

type TokType = 'plain' | 'comment' | 'var' | 'str'

function tokenize(text: string): { type: TokType; value: string }[] {
  const out: { type: TokType; value: string }[] = []
  const push = (type: TokType, value: string) => out.push({ type, value })

  text.split('\n').forEach((line, li, arr) => {
    const cm = line.match(/^(\s*)(#|\/\/|;)(.*)$/)
    if (cm) {
      push('plain', cm[1])
      push('comment', cm[2] + cm[3])
    } else {
      let j = 0
      let buf = ''
      while (j < line.length) {
        const ch = line[j]
        if (ch === '{' && /[A-Z]/.test(line[j + 1] ?? '')) {
          const end = line.indexOf('}', j)
          if (end > -1) {
            if (buf) { push('plain', buf); buf = '' }
            push('var', line.slice(j, end + 1))
            j = end + 1
            continue
          }
        }
        if (ch === '"' || ch === "'") {
          let end = j + 1
          while (end < line.length && line[end] !== ch) end++
          if (line[end] === ch) {
            if (buf) { push('plain', buf); buf = '' }
            push('str', line.slice(j, end + 1))
            j = end + 1
            continue
          }
        }
        buf += ch
        j++
      }
      if (buf) push('plain', buf)
    }
    if (li < arr.length - 1) push('plain', '\n')
  })
  return out
}

export default function CodeBlockV2({ filename, code, language }: CodeBlockV2Props) {
  const [copied, setCopied] = useState(false)
  const tokens = useMemo(() => tokenize(code), [code])

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1400)
    })
  }, [code])

  return (
    <div
      className="overflow-hidden"
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        fontFamily: 'var(--font-mono)',
      }}
    >
      {/* Header bar */}
      <div
        className="flex items-center justify-between"
        style={{
          height: 28,
          padding: '0 8px 0 12px',
          background: 'var(--bg-card)',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <div className="flex items-center gap-2 min-w-0">
          {filename && (
            <span
              className="truncate"
              style={{ fontSize: 11, color: 'var(--text-muted)', letterSpacing: '-0.01em' }}
            >
              {filename}
            </span>
          )}
          {language && <span className="eyebrow">{language}</span>}
        </div>
        <button
          onClick={handleCopy}
          className="inline-flex items-center gap-1.5 cursor-pointer transition-all duration-150"
          style={{
            padding: '3px 8px',
            fontSize: 11,
            fontWeight: 500,
            background: 'transparent',
            border: '0.5px solid var(--border)',
            borderRadius: 'var(--r-tag)',
            color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
          }}
          onMouseEnter={(e) => {
            e.currentTarget.style.borderColor = 'var(--border-strong)'
            e.currentTarget.style.color = copied ? 'var(--ok-text)' : 'var(--text)'
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.borderColor = 'var(--border)'
            e.currentTarget.style.color = copied ? 'var(--ok-text)' : 'var(--text-muted)'
          }}
        >
          <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
            {copied ? (
              <path d="M2.5 6.2L4.7 8.5L9.5 3.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
            ) : (
              <>
                <rect x="3.5" y="3.5" width="6" height="6" rx="1" stroke="currentColor" strokeWidth="1" />
                <path d="M2 7.5V2.5C2 2.22 2.22 2 2.5 2H7.5" stroke="currentColor" strokeWidth="1" strokeLinecap="round" />
              </>
            )}
          </svg>
          <span>{copied ? 'Copied' : 'Copy'}</span>
        </button>
      </div>

      {/* Code body */}
      <pre
        style={{
          margin: 0,
          padding: '12px 14px',
          fontSize: 12,
          lineHeight: 1.6,
          color: 'var(--text)',
          overflowX: 'auto',
          whiteSpace: 'pre',
        }}
      >
        {tokens.map((t, i) => {
          if (t.type === 'comment') return <span key={i} style={{ color: 'var(--text-subtle)', fontStyle: 'italic' }}>{t.value}</span>
          if (t.type === 'var')     return <span key={i} style={{ color: 'var(--brand)', fontWeight: 500 }}>{t.value}</span>
          if (t.type === 'str')     return <span key={i} style={{ color: 'var(--ok-text)' }}>{t.value}</span>
          return <span key={i}>{t.value}</span>
        })}
      </pre>
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "CodeBlock" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/portal/components/CodeBlock.tsx
git commit -m "feat(ui): migrate CodeBlock to light theme (bg-soft, syntax tokens, ghost copy btn)"
```

---

## Task 11: Portal TopNav (`PortalApp.tsx`) — Full Rework

**Files:**
- Modify: `web/src/portal/PortalApp.tsx`

- [ ] **Step 1: Replace the entire `PortalApp.tsx`**

Key changes vs current: 52px height, new frosted-glass bg using `--bg-page`, `0.5px` border, logo with version chip, nav with gradient active indicator via children render-prop, status pill with `StatusDot`, `LangToggle` updated inline.

```tsx
import { Routes, Route, NavLink, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import StatusDot from '@/components/StatusDot'
import QuickStartV2 from '@/portal/pages/QuickStart'
import MonitorV2 from '@/portal/pages/Monitor'

export default function PortalAppV2() {
  const { t } = useTranslation()
  const { data } = useQuery<{ service: { status: string } }>({
    queryKey: ['stats-status'],
    queryFn: async () => { const res = await statsApi.getStats(); return res.data },
    refetchInterval: 30000,
  })

  const isHealthy = data?.service?.status === 'healthy'

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-page)' }}>
      <header
        className="fixed top-0 inset-x-0 z-50"
        style={{
          height: 52,
          background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
          backdropFilter: 'saturate(180%) blur(8px)',
          WebkitBackdropFilter: 'saturate(180%) blur(8px)',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <div
          className="relative h-full mx-auto flex items-center"
          style={{ maxWidth: 1240, padding: '0 28px', gap: 24 }}
        >
          {/* Logo + wordmark + version */}
          <Link to="/" className="flex items-center gap-2 no-underline shrink-0">
            <Logo height={20} />
            <span style={{ fontSize: 15, fontWeight: 700, letterSpacing: '-0.025em', color: 'var(--text)' }}>
              depsilo
            </span>
            <span
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 10,
                color: 'var(--text-subtle)',
                padding: '1px 5px',
                border: '0.5px solid var(--border)',
                borderRadius: 'var(--r-pill)',
                marginLeft: 2,
              }}
            >
              v0.1
            </span>
          </Link>

          {/* Nav */}
          <nav className="flex items-center gap-0.5" style={{ marginLeft: 8 }}>
            {[
              { to: '/', end: true, label: t('portal.quickStart') },
              { to: '/monitor', end: false, label: t('portal.monitor') },
            ].map(({ to, end, label }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className="relative bg-transparent border-none cursor-pointer no-underline"
                style={({ isActive }) => ({
                  display: 'inline-block',
                  padding: '6px 10px',
                  fontSize: 13,
                  fontWeight: isActive ? 600 : 500,
                  letterSpacing: isActive ? '-0.005em' : '0',
                  color: isActive ? 'var(--text)' : 'var(--text-soft)',
                  transition: 'color 120ms ease',
                  borderRadius: 6,
                })}
              >
                {({ isActive }) => (
                  <>
                    {label}
                    {isActive && (
                      <span
                        style={{
                          position: 'absolute',
                          left: 10, right: 10, bottom: -15,
                          height: 1.5,
                          background: 'var(--grad-brand)',
                          borderRadius: 1,
                        }}
                      />
                    )}
                  </>
                )}
              </NavLink>
            ))}
          </nav>

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* Status pill */}
          {data && (
            <div
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 6,
                padding: '4px 10px 4px 8px',
                fontSize: 11,
                fontWeight: 600,
                letterSpacing: '0.005em',
                color: 'var(--text)',
                background: 'var(--bg-card)',
                border: '0.5px solid var(--border)',
                borderRadius: 999,
                whiteSpace: 'nowrap',
              }}
            >
              <StatusDot status={isHealthy ? 'healthy' : 'failed'} live={isHealthy} size={6} />
              <span>{isHealthy ? t('portal.online') : t('portal.offline')}</span>
            </div>
          )}

          <LangToggle />
          <ThemeToggle />

          {/* Admin link */}
          <Link
            to="/admin"
            className="no-underline"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '4px 10px 4px 8px',
              fontSize: 12,
              fontWeight: 500,
              color: 'var(--text)',
              background: 'var(--bg-card)',
              border: '0.5px solid var(--border)',
              borderRadius: 999,
            }}
          >
            <span
              style={{
                width: 22, height: 22,
                borderRadius: '50%',
                background: 'var(--brand-soft)',
                border: '0.5px solid var(--brand-border)',
                color: 'var(--brand)',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                fontSize: 10,
                fontWeight: 500,
                fontFamily: 'var(--font-mono)',
              }}
            >
              AD
            </span>
            <span>{t('portal.adminPanel')}</span>
          </Link>
        </div>
      </header>

      <main
        className="relative z-10"
        style={{
          paddingTop: 52,
          maxWidth: 1240,
          margin: '0 auto',
          padding: '52px 28px 64px',
        }}
      >
        <Routes>
          <Route index element={<QuickStartV2 />} />
          <Route path="monitor" element={<MonitorV2 />} />
        </Routes>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "PortalApp\|portal/PortalApp" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/portal/PortalApp.tsx
git commit -m "feat(ui): rework Portal topnav (52px, grad-brand indicator, status pill, new tokens)"
```

---

## Task 12: Admin `MainLayout` — Sidebar + Topbar

**Files:**
- Modify: `web/src/admin/components/MainLayout.tsx`

- [ ] **Step 1: Replace `MainLayout.tsx`**

Key changes: white sidebar (`--bg-card`), `0.5px` borders, `.eyebrow` section labels, active nav item uses `--brand-soft` bg + `2px solid var(--brand)` left border, user avatar uses brand colors, topbar uses `--text` title at 14px/600, health pill uses `StatusDot`.

```tsx
import { NavLink, Outlet, useNavigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import Logo from '@/components/Logo'
import LangToggle from '@/components/LangToggle'
import ThemeToggle from '@/components/ThemeToggle'
import StatusDot from '@/components/StatusDot'
import BadgeV2 from '@/components/Badge'
import { authApi } from '@/lib/api'

interface NavItem {
  label: string
  to: string
  icon: string
  end?: boolean
  pro?: boolean
}

function SidebarNavItem({ item }: { item: NavItem }) {
  return (
    <NavLink
      to={item.to}
      end={item.end}
      className="flex items-center gap-3 py-2 text-[13px] no-underline relative transition-colors duration-150"
      style={({ isActive }) => ({
        padding: '7px 20px',
        fontWeight: isActive ? 600 : 500,
        color: isActive ? 'var(--text)' : 'var(--text-soft)',
        background: isActive ? 'var(--brand-soft)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--brand)' : '2px solid transparent',
      })}
    >
      <Icon name={item.icon} size="sm" />
      <span className="flex-1">{item.label}</span>
      {item.pro && <BadgeV2 variant="pro">Pro</BadgeV2>}
    </NavLink>
  )
}

export default function MainLayoutV2() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation()
  const user = JSON.parse(localStorage.getItem('user') || '{"username":"admin","role":"admin"}')

  const monitorItems: NavItem[] = [
    { label: t('nav.dashboard'),   to: '/admin',           icon: 'dashboard',     end: true },
    { label: t('bandwidth.title'), to: '/admin/bandwidth', icon: 'bar_chart' },
    { label: t('nav.accessLogs'),  to: '/admin/logs',      icon: 'receipt_long' },
    { label: t('nav.auditLogs'),   to: '/admin/audit',     icon: 'policy',        pro: true },
  ]

  const manageItems: NavItem[] = [
    { label: t('nav.cacheManage'), to: '/admin/cache',      icon: 'storage' },
    { label: t('nav.upstreams'),   to: '/admin/upstreams',  icon: 'cloud_sync' },
    { label: t('nav.userManage'),  to: '/admin/users',      icon: 'group' },
    { label: t('nav.rules'),       to: '/admin/rules',      icon: 'shield',       pro: true },
    { label: t('nav.security'),    to: '/admin/security',   icon: 'security',     pro: true },
    { label: t('nav.projects'),    to: '/admin/projects',   icon: 'folder_managed', pro: true },
    { label: t('nav.settings'),    to: '/admin/settings',   icon: 'settings' },
  ]

  const pageTitles: Record<string, string> = {
    '/admin':            t('nav.dashboard'),
    '/admin/bandwidth':  t('bandwidth.title'),
    '/admin/logs':       t('nav.accessLogs'),
    '/admin/audit':      t('nav.auditLogs'),
    '/admin/cache':      t('nav.cacheManage'),
    '/admin/upstreams':  t('nav.upstreams'),
    '/admin/users':      t('nav.userManage'),
    '/admin/rules':      t('nav.rules'),
    '/admin/security':   t('nav.security'),
    '/admin/projects':   t('nav.projects'),
    '/admin/settings':   t('nav.settings'),
  }

  const pageTitle = pageTitles[location.pathname] || t('nav.dashboard')

  const handleLogout = async () => {
    try { await authApi.logout() } catch { /* ignore */ }
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/admin/login', { replace: true })
  }

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-page)' }}>
      {/* Sidebar — 240px */}
      <aside
        className="fixed left-0 top-0 z-30 h-screen w-[240px] flex flex-col"
        style={{ background: 'var(--bg-card)', borderRight: '0.5px solid var(--border)' }}
      >
        {/* Logo row */}
        <div className="px-5 py-5 flex items-center gap-2">
          <Logo height={20} />
          <span style={{ fontSize: 15, fontWeight: 700, letterSpacing: '-0.025em', color: 'var(--text)' }}>
            depsilo
          </span>
          <span
            className="ml-auto"
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--text-subtle)',
              padding: '1px 5px',
              border: '0.5px solid var(--border)',
              borderRadius: 'var(--r-pill)',
            }}
          >
            v0.1
          </span>
        </div>

        {/* Nav */}
        <nav className="flex-1 overflow-y-auto py-2">
          <p className="eyebrow px-5 mb-1 mt-1">{t('nav.monitor')}</p>
          {monitorItems.map((item) => <SidebarNavItem key={item.to} item={item} />)}

          <p className="eyebrow px-5 mb-1 mt-5">{t('nav.manage')}</p>
          {manageItems.map((item) => <SidebarNavItem key={item.to} item={item} />)}
        </nav>

        {/* User area */}
        <div style={{ borderTop: '0.5px solid var(--border)' }} className="px-3 py-3">
          <div
            className="flex items-center gap-2.5 rounded-[6px] px-2 py-2 group cursor-default transition-colors duration-150"
            onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--bg-hover)' }}
            onMouseLeave={(e) => { e.currentTarget.style.background = '' }}
          >
            <div
              className="flex h-7 w-7 items-center justify-center rounded-[6px] text-[11px] font-[500] shrink-0"
              style={{
                background: 'var(--brand-soft)',
                border: '0.5px solid var(--brand-border)',
                color: 'var(--brand)',
                fontFamily: 'var(--font-mono)',
              }}
            >
              {user.username?.[0]?.toUpperCase() ?? 'A'}
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-[13px] font-[500] truncate leading-tight" style={{ color: 'var(--text)' }}>
                {user.username}
              </p>
              <p className="text-[10px] leading-tight mt-0.5" style={{ color: 'var(--text-subtle)' }}>
                {user.role === 'admin' ? t('nav.admin') : t('nav.readonly')}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="bg-transparent opacity-0 group-hover:opacity-100 cursor-pointer transition-all duration-150 p-1 rounded-[4px]"
              style={{ color: 'var(--text-soft)' }}
              title={t('nav.logout')}
              onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--text)' }}
              onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-soft)' }}
            >
              <Icon name="logout" size="sm" />
            </button>
          </div>
        </div>
      </aside>

      {/* Top bar */}
      <header
        className="fixed top-0 left-[240px] right-0 h-[52px] z-40 flex items-center justify-between px-8"
        style={{
          background: 'color-mix(in oklab, var(--bg-page) 88%, transparent)',
          backdropFilter: 'saturate(180%) blur(8px)',
          WebkitBackdropFilter: 'saturate(180%) blur(8px)',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <h1 style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', margin: 0 }}>
          {pageTitle}
        </h1>
        <div className="flex items-center gap-3">
          <LangToggle />
          <ThemeToggle />
          <div
            className="inline-flex items-center gap-1.5"
            style={{ fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-soft)' }}
          >
            <StatusDot status="healthy" live size={6} />
            <span>Healthy</span>
          </div>
        </div>
      </header>

      {/* Main content */}
      <main
        className="ml-[240px] min-h-[calc(100vh-52px)]"
        style={{ marginTop: 52, padding: 32, background: 'var(--bg-page)' }}
      >
        <Outlet />
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify TS**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "MainLayout" | head -10
```

- [ ] **Step 3: Commit**

```bash
git add web/src/admin/components/MainLayout.tsx
git commit -m "feat(ui): rework Admin MainLayout sidebar + topbar (new tokens, eyebrow labels, StatusDot)"
```

---

## Task 13: `LangToggle` — Token Update

**Files:**
- Modify: `web/src/components/LangToggle.tsx`

- [ ] **Step 1: Replace `LangToggle.tsx`**

Remove Tailwind classes that referenced the old `@theme` vars. Use inline styles with new tokens.

```tsx
import { useTranslation } from 'react-i18next'

export default function LangToggle() {
  const { i18n } = useTranslation()
  const isZh = i18n.language === 'zh'

  function toggle() {
    const next = isZh ? 'en' : 'zh'
    i18n.changeLanguage(next)
    localStorage.setItem('lang', next)
  }

  return (
    <button
      onClick={toggle}
      className="bg-transparent cursor-pointer transition-colors duration-150"
      style={{
        fontSize: 11,
        fontWeight: 500,
        fontFamily: 'var(--font-mono)',
        color: 'var(--text-muted)',
        padding: '4px 8px',
        border: '0.5px solid var(--border)',
        borderRadius: 'var(--r-tag)',
      }}
      onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--text)'; e.currentTarget.style.borderColor = 'var(--border-strong)' }}
      onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-muted)'; e.currentTarget.style.borderColor = 'var(--border)' }}
      title={isZh ? 'Switch to English' : '切换到中文'}
    >
      {isZh ? 'EN' : '中'}
    </button>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/LangToggle.tsx
git commit -m "feat(ui): update LangToggle to new token vars"
```

---

## Task 14: Sweep — Admin & Portal Pages

**Files:**
- Modify: `web/src/admin/pages/Dashboard.tsx`
- Modify: `web/src/admin/pages/Upstreams.tsx`
- Modify: `web/src/admin/pages/AccessLogs.tsx`
- Modify: `web/src/admin/pages/CacheManage.tsx`
- Modify: `web/src/admin/pages/Users.tsx`
- Modify: `web/src/admin/pages/Settings.tsx`
- Modify: `web/src/admin/pages/BandwidthReport.tsx`
- Modify: `web/src/admin/pages/AuditLogs.tsx`
- Modify: `web/src/admin/pages/Login.tsx`
- Modify: `web/src/admin/pages/Rules.tsx`
- Modify: `web/src/admin/pages/Security.tsx`
- Modify: `web/src/admin/pages/Projects.tsx`
- Modify: `web/src/components/UpstreamCard.tsx`
- Modify: `web/src/components/Button.tsx`
- Modify: `web/src/components/Input.tsx`
- Modify: `web/src/components/Select.tsx`
- Modify: `web/src/components/Modal.tsx`
- Modify: `web/src/components/DataTable.tsx`
- Modify: `web/src/components/EmptyState.tsx`
- Modify: `web/src/portal/pages/Monitor.tsx`
- Modify: `web/src/portal/pages/QuickStart.tsx`
- Modify: `web/src/portal/pages/PackagesV2.tsx`
- Modify: `web/src/portal/pages/PackageDetailV2.tsx`
- Modify: `web/src/portal/pages/LiveStreamV2.tsx`
- Modify: `web/src/portal/components/ServiceUrlBar.tsx`
- Modify: `web/src/setup/SetupWizard.tsx`

Apply the var name mapping from the top of this plan to every file. The specific substitutions to make in each file:

- [ ] **Step 1: Find all remaining Stripe var references**

```bash
cd web && grep -rn "var(--heading)\|var(--body)\|var(--surface\|var(--stripe-purple)\|var(--success)\|var(--error)\|var(--lemon)\|shadow-soft\|shadow-elevated\|shadow-ambient\|text-on-surface\|bg-surface\|hover:bg-surface\|text-heading\|text-body\|text-label\|text-primary\|stripe-shadow\|font-display\|border-purple\|on-primary" src/ --include="*.tsx" --include="*.ts" -l
```

- [ ] **Step 2: Apply substitutions to each file**

For **every file** returned by the grep above, open the file and apply these replacements (use Edit tool per file):

```
var(--heading)           → var(--text)
var(--label)             → var(--text-muted)
var(--body)              → var(--text-soft)
var(--surface-low)       → var(--bg-soft)
var(--surface-container) → var(--bg-soft)
var(--surface-high)      → var(--bg-soft)
var(--surface-highest)   → var(--bg-soft)
var(--surface-bright)    → var(--bg-card)
var(--surface)           → var(--bg-card)
var(--bg)                → var(--bg-page)
var(--stripe-purple)     → var(--brand)
var(--on-primary)        → #ffffff
var(--success-text)      → var(--ok-text)
var(--success)           → var(--ok)
var(--error-container)   → var(--danger-fill)
var(--error)             → var(--danger)
var(--lemon)             → var(--warn-text)
var(--on-surface)        → var(--text)
var(--on-surface-variant)→ var(--text-soft)

Remove entirely any: box-shadow: var(--shadow-*)
Change: border: '1px solid var(--border)'  →  border: '0.5px solid var(--border)'
Change: borderBottom: '1px solid var(--border)' → borderBottom: '0.5px solid var(--border)'

Tailwind classes:
text-on-surface-variant → style={{ color: 'var(--text-soft)' }}
text-heading            → style={{ color: 'var(--text)' }}
text-body               → style={{ color: 'var(--text-soft)' }}
bg-surface-container    → style={{ background: 'var(--bg-soft)' }}
hover:bg-surface-container → onMouseEnter/Leave inline (see LangToggle pattern)
```

Special case in `UpstreamCard.tsx` — `beatColor()` uses hardcoded hex colors that are close to semantic tokens. Update:
```tsx
// Before:
if (latency < 0)   return '#dc3545'
if (latency < 80)  return '#3bd671'
if (latency < 200) return '#8cc152'
if (latency < 500) return '#f5a623'
return '#dc3545'
// After:
if (latency < 0)   return 'var(--danger)'
if (latency < 80)  return 'var(--ok)'
if (latency < 200) return 'var(--warn)'
if (latency < 500) return 'var(--warn-text)'
return 'var(--danger)'
```

Also in `UpstreamCard.tsx`: `var(--surface-container)` → `var(--bg-soft)`.

Special case in `Dashboard.tsx` — recharts `linearGradient` refs:
```tsx
// Before:
<stop stopColor="var(--stripe-purple)" stopOpacity={0.3} />
<stop stopColor="var(--stripe-purple)" stopOpacity={0.02} />
<stop stopColor="var(--error)" stopOpacity={0.25} />
<stop stopColor="var(--error)" stopOpacity={0.02} />
// After:
<stop stopColor="var(--brand)" stopOpacity={0.3} />
<stop stopColor="var(--brand)" stopOpacity={0.02} />
<stop stopColor="var(--danger)" stopOpacity={0.25} />
<stop stopColor="var(--danger)" stopOpacity={0.02} />
```

Also in `Dashboard.tsx` — range button active state:
```tsx
// Before:
background: range === r.value ? 'var(--stripe-purple)' : 'transparent',
color: range === r.value ? 'var(--on-primary)' : 'var(--body)',
border: range === r.value ? 'none' : '1px solid var(--border)',
// After:
background: range === r.value ? 'var(--brand)' : 'transparent',
color: range === r.value ? '#ffffff' : 'var(--text-soft)',
border: range === r.value ? 'none' : '0.5px solid var(--border)',
```

Also `Dashboard.tsx` storage alert:
```tsx
// Before:
background: dashboard.cache_usage_percent > 95 ? 'var(--error-container)' : 'rgba(245,166,35,0.1)',
color: dashboard.cache_usage_percent > 95 ? 'var(--error)' : 'var(--lemon, #9b6829)',
border: `1px solid ${...}`,
// After:
background: dashboard.cache_usage_percent > 95 ? 'var(--danger-fill)' : 'var(--warn-fill)',
color: dashboard.cache_usage_percent > 95 ? 'var(--danger-text)' : 'var(--warn-text)',
border: `0.5px solid ${dashboard.cache_usage_percent > 95 ? 'var(--danger-border)' : 'var(--warn-border)'}`,
```

Also `Dashboard.tsx` — top packages progress bar:
```tsx
// Before:
style={{ ..., background: 'var(--stripe-purple)' }}
// After:
style={{ ..., background: 'var(--brand)' }}
```

- [ ] **Step 3: Verify full TypeScript compile**

```bash
cd web && npx tsc --noEmit 2>&1 | grep "error TS" | wc -l
```

Expected: 0 errors.

- [ ] **Step 4: Build**

```bash
cd web && npm run build 2>&1 | tail -5
```

Expected: `built in X.XXs` with no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(ui): sweep Stripe var refs in admin/portal pages (heading→text, surface→bg-card, etc.)"
```

---

## Task 15: Final Verification

- [ ] **Step 1: Full build**

```bash
make build 2>&1 | tail -10
```

Expected: frontend + backend compile with no errors.

- [ ] **Step 2: Run linters**

```bash
make lint 2>&1 | tail -20
```

Expected: no TypeScript errors; golangci-lint passes.

- [ ] **Step 3: Start dev server and visual check**

```bash
make dev &
sleep 3 && curl -s http://localhost:23333/health | python3 -m json.tool
```

Open `http://localhost:23333` in a browser and verify:
- Page background is pure white with faint plum/cyan corner wash
- Logo shows gradient SVG mark
- Portal nav active indicator is a gradient bar (not solid underline)
- Status pill is rounded with hairline border
- QuickStart CodeBlock headers are light (not dark navy)
- Admin sidebar is white with plum active state

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "feat(ui): complete design system migration to new token system"
```
