# Admin UI System Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Admin route responsive, accessible, visually consistent with Portal, and truthful about loading, error, stale, permission, and mutation states.

**Architecture:** Establish real-browser tests and a small project-owned wrapper layer over Base UI before migrating pages. Shared primitives own accessibility, focus, local table scrolling, status feedback, and Instrument tokens; pages own their section-level query decisions and responsive composition. Settings and Upstreams consume the typed APIs delivered by Plans 02 and 03 rather than duplicating temporary response shapes.

**Tech Stack:** React 19, TypeScript 5.9, Tailwind CSS 4, Base UI 1.3, TanStack Query 5, Playwright, axe-core, i18next.

## Global Constraints

- Preserve all pre-existing dirty-worktree changes. Read each target file before editing; never use reset, checkout, clean, or broad file replacement.
- Record the dirty paths before Task 1. Current dirty overlaps include `web/src/i18n/en.ts`, `web/src/i18n/zh.ts`,
  `web/src/lib/api.ts`, `web/src/admin/pages/Settings.tsx`, and `DESIGN.md`; Plan 01/03 also own the Principal/type
  files and `Upstreams.tsx` before this plan consumes them. Use the exact partial-staging commands below and inspect
  `git diff --cached` whenever a task touches any overlap.
- Keep Admin dense, quiet, and operational. Do not redesign it as a landing page and do not add decorative cards or gradients.
- Page code may use project wrappers from `web/src/components/`; it must not import Base UI primitives directly.
- Mobile verification widths are exactly 320 and 390; responsive breakpoints are 640, 768, 1024, and 1280; content is capped at 1840px.
- Visible icon-only actions are at least 40x40, require a localized accessible label, and expose a tooltip.
- CTA colors use `--btn/--btn-fg`; solid signal colors use `--hit/--on-hit`; statuses use semantic fill/text/border token groups.
- Initial query errors, stale cached data, empty success, permission denial, and mutation errors remain distinct states.
- Global token changes require Portal `/` and `/monitor` regression checks in both themes.
- Frontend changes must pass type-check, build, i18n audit, Playwright, axe, and touched-file ESLint checks.
- Touched-file ESLint scope comes only from `web/admin-remediation-eslint-files.txt`; never derive it from a commit
  diff or lint whole Admin/Portal directories in this dirty worktree.
- Plan 01 produces `Principal`, `usePrincipal()`, and typed contract DTOs. Plan 02 produces `AdminSettingsResponse`. Plan 03 produces `AdminUpstream` and the runtime-aware Upstream API.

---

### Task 1: Install a strict Playwright and axe test harness

**Files:**
- Modify: `web/package.json`
- Modify: `web/package-lock.json`
- Create: `web/playwright.config.ts`
- Create: `web/e2e/fixtures/admin-api.ts`
- Create: `web/e2e/admin-smoke.spec.ts`
- Modify: `web/src/i18n/index.ts`
- Modify: `.gitignore`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `test`, `expect`, `mockAdminApi(page, overrides)`, and `AdminApiOverrides` for every later UI task.
- Produces: scripts `test:e2e` and `test:e2e:update`.

- [ ] **Step 1: Write the smoke test before installing Playwright**

```ts
// web/e2e/admin-smoke.spec.ts
import { test, expect } from './fixtures/admin-api'

test('renders every admin route without an unmatched API request', async ({ page }) => {
  const routes = [
    '/admin', '/admin/bandwidth', '/admin/logs', '/admin/audit',
    '/admin/quarantine', '/admin/cache', '/admin/upstreams', '/admin/users',
    '/admin/license', '/admin/rules', '/admin/security', '/admin/projects',
    '/admin/settings',
  ]

  for (const route of routes) {
    await page.goto(route)
    await expect(page.locator('h1')).toBeVisible()
  }
})
```

- [ ] **Step 2: Run the test and confirm the missing-runner failure**

Run: `cd web && npm run test:e2e`

Expected: FAIL because `test:e2e` and `@playwright/test` do not exist.

- [ ] **Step 3: Install only the approved development dependencies and scripts**

Run:

```bash
cd web
npm install --save-dev @playwright/test @axe-core/playwright
```

Add to `web/package.json`:

```json
{
  "scripts": {
    "test:e2e": "playwright test",
    "test:e2e:update": "playwright test --update-snapshots"
  }
}
```

- [ ] **Step 4: Add deterministic browser configuration**

```ts
// web/playwright.config.ts
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['html', { open: 'never' }], ['line']] : 'line',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    locale: 'zh-CN',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: !process.env.CI,
  },
})
```

- [ ] **Step 5: Implement a strict API fixture**

```ts
// web/e2e/fixtures/admin-api.ts
import { test as base, expect, type Page, type Request, type Route } from '@playwright/test'

export type JsonValue = unknown
export interface MockHttpResponse {
  status: number
  body: JsonValue
  contentType?: string
  serialize?: 'json' | 'text'
}
export type AdminApiResponder = (request: Request) => JsonValue | Promise<JsonValue>
export type AdminApiOverride = JsonValue
export type AdminApiOverrides = Record<string, AdminApiOverride>
export type UiLocale = 'zh' | 'en'
export type UiTheme = 'light' | 'dark'

const configuredSettings = {
  server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
  database: { driver: 'sqlite' },
  storage: { type: 'local', path: './data/cache' },
  cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
  auth: { token_ttl: '168h' },
}
const effectiveSettings = {
  ...configuredSettings,
  cache: { ...configuredSettings.cache, ttl_blob: '72h' },
}
const settingSources = {
  'server.host': 'file', 'server.port': 'file', 'server.log_level': 'file',
  'database.driver': 'file', 'storage.type': 'file', 'storage.path': 'file',
  'cache.max_size_gb': 'file', 'cache.ttl_index': 'file', 'cache.ttl_blob': 'file',
  'cache.lru_threshold': 'file', 'auth.token_ttl': 'file',
}
const editableSettings = [
  'server.log_level', 'cache.max_size_gb', 'cache.ttl_index',
  'cache.ttl_blob', 'cache.lru_threshold', 'auth.token_ttl',
]

export const adminApiDefaults: Record<string, JsonValue> = {
  'GET /api/v1/setup/status': { needs_setup: false },
  'GET /api/v1/auth/me': { id: 1, username: 'admin', role: 'admin', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: true },
  'GET /api/v1/integration-prompt': { status: 200, body: '# Depsilo project integration\nUse the configured Depsilo package mirror.', contentType: 'text/plain; charset=utf-8', serialize: 'text' },
  'GET /api/v1/stats': { service: { version: 'dev', status: 'healthy' }, week: {}, upstreams: [] },
  'GET /api/v1/latency-series': {},
  'GET /api/v1/now': { status: 'healthy', last_activity: null, rate: { requests_per_min: 0, ingress_bps: 0, egress_bps: 0 }, upstreams: { healthy: 0, total: 0 } },
  'GET /api/v1/admin/dashboard': { summary: {}, top_packages: [], upstreams: [] },
  'GET /api/v1/admin/dashboard/trends': { points: [] },
  'GET /api/v1/admin/bandwidth': { summary: {}, daily: [], by_ecosystem: [], top_packages: [], by_upstream: [] },
  'GET /api/v1/admin/logs': { items: [], total: 0, page: 1, page_size: 50 },
  'GET /api/v1/admin/audit-logs': { items: [], total: 0, page: 1 },
  'GET /api/v1/admin/quarantine/events': { items: [], total: 0 },
  'GET /api/v1/admin/quarantine/approvals': { items: [], total: 0 },
  'GET /api/v1/admin/blocklist/status': { enabled: true, count: 0 },
  'GET /api/v1/admin/blocklist/overrides': { items: [] },
  'GET /api/v1/admin/cache/distribution': { total_size: 0, max_size: 1, by_type: [], top_packages: [] },
  'GET /api/v1/admin/cache': { items: [], total: 0 },
  'GET /api/v1/admin/upstreams': { items: [], total: 0 },
  'GET /api/v1/admin/users': [{ id: 1, username: 'admin', role: 'admin', enabled: true }],
  'GET /api/v1/admin/tokens': [],
  'GET /api/v1/admin/license/status': { is_pro: false, source: 'none', days_left: 0, trial_used: false, trial_available: true, last_checked: '2026-07-10T00:00:00Z' },
  'GET /api/v1/admin/rules': [],
  'GET /api/v1/admin/security/dashboard': {
    total_vulnerabilities: 0,
    affected_packages: 0,
    by_severity: { critical: 0, high: 0, medium: 0, low: 0 },
    auto_blocked_count: 0,
    last_scan_at: null,
    scan_in_progress: false,
  },
  'GET /api/v1/admin/security/vulnerabilities': { items: [], total: 0, page: 1 },
  'GET /api/v1/admin/security/packages': { items: [], total: 0, page: 1 },
  'GET /api/v1/admin/security/suggestions': { items: [], total: 0, page: 1 },
  'GET /api/v1/admin/security/policies': [],
  'GET /api/v1/admin/projects': { status: 402, body: { code: 'PRO_REQUIRED', message: 'Pro required' } },
  'GET /api/v1/admin/settings': {
    configured: configuredSettings,
    effective: effectiveSettings,
    pending_restart: ['cache.ttl_blob'],
    overrides: {},
    sources: settingSources,
    editable: editableSettings,
    config_writable: true,
  },
  'GET /api/v1/admin/webhooks': [],
}

const keyFor = (route: Route) => {
  const request = route.request()
  const url = new URL(request.url())
  return `${request.method()} ${url.pathname}`
}

function isMockHttpResponse(value: JsonValue): value is MockHttpResponse {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.status === 'number' && 'body' in candidate
}

export async function setUiPreferences(page: Page, theme: UiTheme, locale: UiLocale) {
  await page.addInitScript(({ theme, locale }) => {
    localStorage.setItem('lang', locale)
    localStorage.setItem('depsilo-theme', theme)
    document.documentElement.dataset.theme = theme
    document.documentElement.classList.remove('light', 'dark')
    document.documentElement.classList.add(theme)
  }, { theme, locale })
}

export async function expectResolvedUiPreferences(page: Page, theme: UiTheme, locale: UiLocale) {
  await expect.poll(() => page.evaluate(() => ({
    storedLocale: localStorage.getItem('lang'),
    storedTheme: localStorage.getItem('depsilo-theme'),
    domLocale: document.documentElement.lang,
    domTheme: document.documentElement.dataset.theme,
    hasThemeClass: document.documentElement.classList.contains(document.documentElement.dataset.theme || ''),
  }))).toEqual({
    storedLocale: locale,
    storedTheme: theme,
    domLocale: locale === 'zh' ? 'zh-CN' : 'en',
    domTheme: theme,
    hasThemeClass: true,
  })
}

interface AdminApiFixtureState {
  overrides: AdminApiOverrides
  unmatched: string[]
  assertMatched(): void
}

const adminApiFixtureStates = new WeakMap<Page, AdminApiFixtureState>()

export async function mockAdminApi(page: Page, overrides: AdminApiOverrides = {}) {
  const existing = adminApiFixtureStates.get(page)
  if (existing) {
    Object.assign(existing.overrides, overrides)
    return existing
  }

  const unmatched: string[] = []
  const state: AdminApiFixtureState = {
    overrides: { ...overrides },
    unmatched,
    assertMatched: () => expect(unmatched, `unmatched API requests: ${unmatched.join(', ')}`).toEqual([]),
  }
  adminApiFixtureStates.set(page, state)
  await page.addInitScript(() => {
    localStorage.setItem('token', 'e2e-token')
    if (!localStorage.getItem('lang')) localStorage.setItem('lang', 'zh')
    if (!localStorage.getItem('depsilo-theme')) localStorage.setItem('depsilo-theme', 'dark')
    const theme = localStorage.getItem('depsilo-theme') === 'light' ? 'light' : 'dark'
    document.documentElement.dataset.theme = theme
    document.documentElement.classList.remove('light', 'dark')
    document.documentElement.classList.add(theme)
  })
  await page.route('**/api/v1/**', async route => {
    const key = keyFor(route)
    const selected = Object.prototype.hasOwnProperty.call(state.overrides, key) ? state.overrides[key] : adminApiDefaults[key]
    if (selected === undefined) {
      state.unmatched.push(key)
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'UNMATCHED_E2E_API', message: key }) })
      return
    }
    const response = typeof selected === 'function'
      ? await (selected as AdminApiResponder)(route.request())
      : selected
    const wrapped: MockHttpResponse = isMockHttpResponse(response) ? response : { status: 200, body: response }
    await route.fulfill({
      status: wrapped.status,
      contentType: wrapped.contentType ?? 'application/json',
      body: wrapped.serialize === 'text' ? String(wrapped.body) : JSON.stringify(wrapped.body),
    })
  })
  return state
}

export const test = base.extend<{ api: Awaited<ReturnType<typeof mockAdminApi>> }>({
  api: async ({ page }, use) => {
    const api = await mockAdminApi(page)
    await use(api)
    api.assertMatched()
  },
})

test.beforeEach(async ({ api }) => { void api })
export { expect }
```

- [ ] **Step 6: Synchronize i18n's resolved locale to the DOM and prove stored preferences resolve**

In `web/src/i18n/index.ts`, normalize the persisted value and keep `<html lang>` synchronized after every language change:

```ts
const savedLang = localStorage.getItem('lang') === 'en' ? 'en' : 'zh'
const htmlLang = (language: string) => language.startsWith('zh') ? 'zh-CN' : 'en'

i18n.use(initReactI18next).init({
  resources: {
    zh,
    en,
  },
  lng: savedLang,
  fallbackLng: 'zh',
  interpolation: {
    escapeValue: false,
  },
})

document.documentElement.lang = htmlLang(i18n.resolvedLanguage || savedLang)
i18n.on('languageChanged', language => {
  localStorage.setItem('lang', language.startsWith('zh') ? 'zh' : 'en')
  document.documentElement.lang = htmlLang(language)
})
```

Replace the Step 1 import in `admin-smoke.spec.ts` with the combined import below, then append the test:

```ts
import { expect, expectResolvedUiPreferences, setUiPreferences, test } from './fixtures/admin-api'

test('resolves persisted locale and theme into the document', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'en')
})
```

- [ ] **Step 7: Add CI and artifact ignores**

Append to `.gitignore`:

```gitignore
web/playwright-report/
web/test-results/
```

Add after the frontend build step in `.github/workflows/ci.yml`:

```yaml
      - name: Install Playwright Chromium
        run: npx playwright install --with-deps chromium

      - name: Browser tests
        run: npm run test:e2e
```

- [ ] **Step 8: Run the smoke test**

Run: `cd web && npx playwright install chromium && npm run test:e2e -- admin-smoke.spec.ts`

Expected: PASS, 2 tests; no unmatched API requests, and the preference test observes `lang=en`, `depsilo-theme=light`, `<html lang="en">`, `.light`, and `data-theme="light"`.

- [ ] **Step 9: Commit the harness only**

```bash
git add web/package.json web/package-lock.json web/playwright.config.ts web/e2e/fixtures/admin-api.ts web/e2e/admin-smoke.spec.ts web/src/i18n/index.ts .gitignore .github/workflows/ci.yml
git commit -m "test(admin): add strict browser regression harness"
```

### Task 2: Correct Instrument tokens, primary buttons, and badges

**Files:**
- Modify: `web/src/index.css`
- Modify: `web/src/components/Button.tsx`
- Modify: `web/src/components/Badge.tsx`
- Create: `web/e2e/admin-contrast.spec.ts`

**Interfaces:**
- Produces: `--focus-ring`, WCAG-compliant light secondary/status tokens, and stable Button/Badge variants.

- [ ] **Step 1: Add failing contrast and token assertions**

```ts
// web/e2e/admin-contrast.spec.ts
import AxeBuilder from '@axe-core/playwright'
import { expectResolvedUiPreferences, setUiPreferences, test, expect } from './fixtures/admin-api'

test('light theme admin chrome has no color-contrast violations', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'zh')
  const results = await new AxeBuilder({ page }).withTags(['wcag2aa']).analyze()
  expect(results.violations.filter(v => v.id === 'color-contrast')).toEqual([])
})

test('primary command does not use a brightness filter', async ({ page }) => {
  await page.goto('/admin/upstreams')
  const button = page.getByRole('button', { name: /添加上游源/i })
  await button.hover()
  expect(await button.evaluate(el => getComputedStyle(el).filter)).toBe('none')
})
```

- [ ] **Step 2: Run and verify the current contrast/filter failures**

Run: `cd web && npm run test:e2e -- admin-contrast.spec.ts`

Expected: FAIL on light secondary text and/or primary hover filter.

- [ ] **Step 3: Update light and focus tokens in both token declarations**

Use these exact values in `@theme` and `:root`:

```css
--text-soft: #626A72;
--text-subtle: #646C74;
--warn-text: #8A4F00;
--danger-text: #A52E2E;
--focus-ring: #0C8D5D;
```

Under the dark selector add:

```css
--focus-ring: #3DDC91;
```

Change `.stripe-focus-ring` to use `--focus-ring` rather than the general brand token.

- [ ] **Step 4: Remove filter-based primary hover**

In `Button.tsx`, replace the primary variant and fallback colors with semantic tokens:

```ts
primary: 'text-[var(--btn-fg)] bg-[var(--btn)] hover:bg-[var(--btn-press)]',
```

Remove `filter` from the transition list and remove `hover:brightness-110`. Keep `background: var(--btn)` and `color: var(--btn-fg)` as the inline fallback-free style.

- [ ] **Step 5: Make the Pro badge a readable tinted badge**

```ts
pro: { bg: 'var(--brand-soft)', color: 'var(--brand-text)', border: 'var(--brand-border)' },
```

Extend the style record with optional `border` and render `0.5px solid` only for Pro. Do not use `--grad-aurora`.

- [ ] **Step 6: Run targeted and Portal regression tests**

Run:

```bash
cd web
npm run test:e2e -- admin-contrast.spec.ts
npm run type-check
npm run build
```

Expected: all commands PASS.

- [ ] **Step 7: Commit the token foundation**

```bash
git add web/src/index.css web/src/components/Button.tsx web/src/components/Badge.tsx web/e2e/admin-contrast.spec.ts
git commit -m "fix(ui): align Instrument colors and contrast"
```

### Task 3: Make all shared form controls labelable and validatable

**Files:**
- Modify: `web/src/components/Input.tsx`
- Modify: `web/src/components/Select.tsx`
- Create: `web/src/components/Textarea.tsx`
- Create: `web/src/components/Switch.tsx`
- Create: `web/e2e/admin-forms.spec.ts`

**Interfaces:**
- Produces: `FieldFeedbackProps { label?, hint?, error? }` behavior on Input/Select/Textarea.
- Produces: `SwitchV2 { label, checked, onCheckedChange, disabled? }`.

- [ ] **Step 1: Write label and switch keyboard failures**

```ts
// web/e2e/admin-forms.spec.ts
import { test, expect } from './fixtures/admin-api'

test('login fields are addressable by their visible labels', async ({ page }) => {
  await page.goto('/admin/login')
  await expect(page.getByLabel('用户名')).toBeVisible()
  await expect(page.getByLabel('密码')).toHaveAttribute('type', 'password')
})

test('security policy switch toggles with Space', async ({ page }) => {
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /策略/ }).click()
  const control = page.getByRole('switch').first()
  await control.focus()
  const before = await control.getAttribute('aria-checked')
  await page.keyboard.press('Space')
  expect(await control.getAttribute('aria-checked')).not.toBe(before)
})
```

- [ ] **Step 2: Run and verify label/switch failures**

Run: `cd web && npm run test:e2e -- admin-forms.spec.ts`

Expected: FAIL because labels are not associated and the policy control is not a switch.

- [ ] **Step 3: Add one shared ID/description pattern to Input and Select**

Use this exact pattern in both components:

```tsx
const generatedId = useId()
const controlId = rest.id ?? generatedId
const descriptionId = hint || error ? `${controlId}-description` : undefined

<label htmlFor={controlId}>{label}</label>
<input
  {...rest}
  id={controlId}
  aria-invalid={Boolean(error) || undefined}
  aria-describedby={descriptionId}
/>
{(error || hint) && (
  <p id={descriptionId} role={error ? 'alert' : undefined}>
    {error || hint}
  </p>
)}
```

Define shared props locally in each file to avoid creating a type-only abstraction until a third consumer exists:

```ts
interface FeedbackProps { label?: string; hint?: string; error?: string }
```

- [ ] **Step 4: Add Textarea with the same contract**

`TextareaV2` extends `TextareaHTMLAttributes<HTMLTextAreaElement>` and implements the exact ID, label, hint, error, focus-ring, semantic token, and 16px mobile font behavior used by Input.

- [ ] **Step 5: Wrap Base UI Switch**

```tsx
// web/src/components/Switch.tsx
import { Switch } from '@base-ui/react/switch'

interface SwitchV2Props {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
}

export default function SwitchV2(props: SwitchV2Props) {
  return (
    <label className="inline-flex min-h-10 items-center gap-3 text-[13px]">
      <Switch.Root
        checked={props.checked}
        onCheckedChange={props.onCheckedChange}
        disabled={props.disabled}
        className="relative h-6 w-10 rounded-full bg-[var(--bg-soft)] data-[checked]:bg-[var(--btn)] stripe-focus-ring"
      >
        <Switch.Thumb className="block h-5 w-5 translate-x-0.5 rounded-full bg-white transition-transform data-[checked]:translate-x-[18px]" />
      </Switch.Root>
      <span>{props.label}</span>
    </label>
  )
}
```

- [ ] **Step 6: Migrate Security policy controls and file upload**

Replace the policy toggle with `SwitchV2`. Replace the clickable upload `div` with a real labelled button that calls `fileInputRef.current?.click()` and keeps the hidden file input accessible by an explicit label. Use `InputV2`, `SelectV2`, and `TextareaV2` for policy fields.

- [ ] **Step 7: Run form and type checks**

Run: `cd web && npm run test:e2e -- admin-forms.spec.ts && npm run type-check`

Expected: PASS.

- [ ] **Step 8: Commit the form primitives**

```bash
git add web/src/components/Input.tsx web/src/components/Select.tsx web/src/components/Textarea.tsx web/src/components/Switch.tsx web/src/admin/pages/Security.tsx web/e2e/admin-forms.spec.ts
git commit -m "fix(ui): add accessible field and switch primitives"
```

### Task 4: Replace hand-rolled overlays and feedback primitives

**Files:**
- Modify: `web/src/components/Modal.tsx`
- Modify: `web/src/components/Icon.tsx`
- Create: `web/src/components/Tooltip.tsx`
- Create: `web/src/components/IconButton.tsx`
- Create: `web/src/components/Toast.tsx`
- Create: `web/src/components/Drawer.tsx`
- Create: `web/src/components/InlineNotice.tsx`
- Create: `web/src/components/QueryErrorState.tsx`
- Modify: `web/src/App.tsx`
- Create: `web/e2e/admin-dialog-actions.spec.ts`

**Interfaces:**
- Keeps: `ModalV2({open,onClose,title,children,width})`; adds optional `initialFocus` and `finalFocus` refs.
- Produces: `IconButton({icon,label,tone,loading,...buttonProps})`.
- Produces: `useAppToast().show({tone,message})`.
- Produces: `DrawerV2({open,onOpenChange,title,children,initialFocus})` for mobile navigation.
- Produces: `InlineNotice({tone,title?,children})` and `QueryErrorState({message,onRetry})`.

- [ ] **Step 1: Write failing focus and icon-action tests**

```ts
// web/e2e/admin-dialog-actions.spec.ts
import { test, expect } from './fixtures/admin-api'

test('dialog traps focus and restores its trigger', async ({ page }) => {
  await page.goto('/admin/users')
  const trigger = page.getByRole('button', { name: /添加用户/ })
  await trigger.click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  for (let i = 0; i < 12; i += 1) await page.keyboard.press('Tab')
  expect(await dialog.evaluate((el, active) => el.contains(active), await page.evaluateHandle(() => document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
})

test('every visible icon action is named and at least 40px', async ({ page }) => {
  await page.goto('/admin/users')
  await page.getByRole('button', { name: /添加用户/ }).click()
  const button = page.getByRole('dialog').locator('[data-icon-button]:visible')
  await expect(button).toHaveCount(1)
  await expect(button).toHaveAttribute('aria-label', /关闭/)
  const box = await button.boundingBox()
  expect(box?.width).toBeGreaterThanOrEqual(40)
  expect(box?.height).toBeGreaterThanOrEqual(40)
  await button.hover()
  await expect(page.getByRole('tooltip')).toContainText(/关闭/)
})
```

- [ ] **Step 2: Run and confirm focus/size failures**

Run: `cd web && npm run test:e2e -- admin-dialog-actions.spec.ts`

Expected: FAIL because Modal has no focus trap/restore and pages have no `data-icon-button` contract.

- [ ] **Step 3: Reimplement Modal using Base UI Dialog**

Use `Dialog.Root open={open} onOpenChange={next => !next && onClose()} modal`, then render
`Portal -> Backdrop -> Viewport -> Popup -> Title -> Close`. Keep the existing visual classes, render
the project `IconButton` through `Dialog.Close` for the localized close action, and wire
`initialFocus`/`finalFocus` through Base UI refs. Remove
the hand-written portal, Escape listener, body overflow mutation, and inline `<style>` animation;
move motion styles to `index.css` with `prefers-reduced-motion`.

- [ ] **Step 4: Make Icon decorative by default**

```tsx
return <span aria-hidden="true" className={`icon ${sizeClass} ${className}`} style={style}>{name}</span>
```

Visible semantic names belong on the containing button or adjacent text, not in the icon font glyph.

- [ ] **Step 5: Add Tooltip and IconButton wrappers**

```tsx
// web/src/components/IconButton.tsx
import { forwardRef, type ButtonHTMLAttributes } from 'react'
import Icon from './Icon'
import TooltipV2 from './Tooltip'

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: string
  label: string
  tone?: 'neutral' | 'danger'
  loading?: boolean
}

export default forwardRef<HTMLButtonElement, Props>(function IconButton(
  { icon, label, tone = 'neutral', loading, disabled, className = '', ...rest }, ref,
) {
  return (
    <TooltipV2 content={label}>
      <button
        {...rest}
        ref={ref}
        type={rest.type ?? 'button'}
        data-icon-button
        aria-label={label}
        aria-busy={loading || undefined}
        disabled={disabled || loading}
        className={`inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-[6px] stripe-focus-ring ${className}`}
        style={{ color: tone === 'danger' ? 'var(--danger-text)' : 'var(--text-soft)' }}
      >
        <Icon name={loading ? 'progress_activity' : icon} size="sm" className={loading ? 'animate-spin' : ''} />
      </button>
    </TooltipV2>
  )
})
```

`TooltipV2` uses Base UI `Tooltip.Root`, `Trigger render={children}`, `Portal`, `Positioner`, and `Popup`.

- [ ] **Step 6: Add one application Toast provider**

Wrap `AppRoutes` with project `ToastProvider`. Internally use Base UI `Toast.Provider`,
`useToastManager`, `Viewport`, `Root`, `Content`, `Description`, and `Close`. Expose:

```ts
type ToastTone = 'success' | 'danger' | 'warning'
interface ToastPayload { tone: ToastTone; message: string }
interface AppToastApi { show(payload: ToastPayload): string; close(id?: string): void }
```

Success/warning use polite priority; danger uses high priority. Render every Toast root with
`data-toast-tone={toast.type}` so browser tests can distinguish a success announcement from an inline error without
matching translated prose. Close uses `IconButton` with the localized close label.

- [ ] **Step 7: Add inline status components**

`InlineNotice` maps success/warning/danger/info to the corresponding fill/text/border tokens and uses
`role="alert"` only for danger. `QueryErrorState` renders `InlineNotice tone="danger"`, a message, and
a secondary Retry button.

- [ ] **Step 8: Add a project-owned mobile Drawer wrapper**

`DrawerV2` uses Base UI Dialog internally and renders a left-anchored Popup with Backdrop. Its title is available
to assistive technology, `modal` remains true, `initialFocus` is forwarded to `Dialog.Popup`, and closing by Escape,
backdrop, or a child action calls `onOpenChange(false)`. MainLayout must consume this wrapper rather than importing
Base UI directly.

- [ ] **Step 9: Run tests and commit**

Run: `cd web && npm run test:e2e -- admin-dialog-actions.spec.ts && npm run type-check`

Expected: PASS.

```bash
git add web/src/components/Modal.tsx web/src/components/Icon.tsx web/src/components/Tooltip.tsx web/src/components/IconButton.tsx web/src/components/Toast.tsx web/src/components/Drawer.tsx web/src/components/InlineNotice.tsx web/src/components/QueryErrorState.tsx web/src/App.tsx web/src/index.css web/e2e/admin-dialog-actions.spec.ts
git commit -m "fix(ui): standardize accessible overlays and feedback"
```

### Task 5: Make tabs, section headers, and tables responsive primitives

**Files:**
- Modify: `web/src/components/Tabs.tsx`
- Modify: `web/src/components/SectionHeader.tsx`
- Modify: `web/src/components/DataTable.tsx`
- Create: `web/src/components/TableViewport.tsx`
- Modify: `web/src/components/Segmented.tsx`
- Create: `web/src/hooks/useMediaQuery.ts`
- Create: `web/e2e/admin-layout-primitives.spec.ts`

**Interfaces:**
- Produces: `TabsV2({items,value,onValueChange,ariaLabel,orientation})` where every item contains `content`.
- Produces: `TableViewport({label,minWidth,children})` and a typed DataTable with `rowKey`.

- [ ] **Step 1: Write failing keyboard and local-scroll tests**

```ts
// web/e2e/admin-layout-primitives.spec.ts
import { test, expect } from './fixtures/admin-api'

test('tabs use arrow keys and expose their selected panel', async ({ page }) => {
  await page.goto('/admin/security')
  const first = page.getByRole('tab').first()
  await first.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab').nth(1)).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tabpanel')).toBeVisible()
})

test('mobile tables scroll locally without widening the page', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/logs')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  await expect(page.getByRole('region', { name: /访问日志表格/ })).toHaveCSS('overflow-x', 'auto')
})
```

- [ ] **Step 2: Run and verify semantic/scroll failures**

Run: `cd web && npm run test:e2e -- admin-layout-primitives.spec.ts`

Expected: FAIL because Tabs has no tab roles/panel and raw tables have no region.

- [ ] **Step 3: Wrap Base UI Tabs with content-owned items**

```ts
export interface TabItem {
  key: string
  label: string
  icon?: ReactNode
  disabled?: boolean
  content: ReactNode
}

export interface TabsV2Props {
  items: TabItem[]
  value: string
  onValueChange: (value: string) => void
  ariaLabel: string
  orientation?: 'horizontal' | 'vertical'
}
```

Render `Tabs.Root`, a locally scrollable `Tabs.List aria-label`, one `Tabs.Tab` per item, and one
`Tabs.Panel` per item inside a `min-w-0` panel container. Tab labels use `whitespace-nowrap`; the list owns
horizontal scrolling. Use this exact orientation layout so Plan 02 can compose Settings without page-owned tab CSS:

```tsx
<Tabs.Root
  value={value}
  onValueChange={onValueChange}
  orientation={orientation}
  className="min-w-0 data-[orientation=vertical]:grid data-[orientation=vertical]:grid-cols-[180px_minmax(0,1fr)] data-[orientation=vertical]:gap-6"
>
  <Tabs.List
    aria-label={ariaLabel}
    activateOnFocus
    className="flex min-w-0 overflow-x-auto border-b border-[var(--border)] data-[orientation=vertical]:flex-col data-[orientation=vertical]:overflow-visible data-[orientation=vertical]:border-b-0 data-[orientation=vertical]:border-r"
  >
    {items.map(item => (
      <Tabs.Tab key={item.key} value={item.key} disabled={item.disabled} className="min-h-10 whitespace-nowrap px-4 py-2.5 stripe-focus-ring">
        {item.icon}{item.label}
      </Tabs.Tab>
    ))}
  </Tabs.List>
  <div className="min-w-0">
    {items.map(item => <Tabs.Panel key={item.key} value={item.key} className="min-w-0">{item.content}</Tabs.Panel>)}
  </div>
</Tabs.Root>
```

Add a project hook with `window.matchMedia` and subscription cleanup:

```ts
export function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches)
  useEffect(() => {
    const media = window.matchMedia(query)
    const update = () => setMatches(media.matches)
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [query])
  return matches
}
```

- [ ] **Step 4: Make SectionHeader mobile-first**

Use:

```tsx
<header className="flex flex-col gap-3 border-b border-[var(--border)] pb-2 mb-4 sm:flex-row sm:items-end sm:justify-between">
  <div className="min-w-0">...</div>
  {action && <div className="flex min-w-0 flex-wrap items-center gap-2">{action}</div>}
</header>
```

Remove negative letter spacing from the heading.

- [ ] **Step 5: Add TableViewport and remove row-index identity**

```tsx
export default function TableViewport({ label, minWidth = 720, children }: Props) {
  return (
    <div data-table-viewport role="region" aria-label={label} tabIndex={0} className="w-full overflow-x-auto stripe-focus-ring">
      <div style={{ minWidth }}>{children}</div>
    </div>
  )
}
```

Change DataTable props to require `rowKey(row,index)` and `ariaLabel`; compose TableViewport internally.
Remove clickable `<tr>` behavior. Pages needing navigation render an explicit link or button column.

- [ ] **Step 6: Keep Segmented controls stable**

Add `whitespace-nowrap`, a minimum 40px control height, and allow the parent to wrap. Do not use viewport-scaled fonts.

- [ ] **Step 7: Migrate Security tabs to the new content API**

Build items as:

```tsx
const items = [
  { key: 'overview', label: t('security.overview'), icon: <Icon name="dashboard" size="sm" />, content: <OverviewTab /> },
  { key: 'vulnerabilities', label: t('security.vulnerabilities'), icon: <Icon name="bug_report" size="sm" />, content: <VulnerabilitiesTab /> },
  { key: 'suggestions', label: t('security.suggestions'), icon: <Icon name="lightbulb" size="sm" />, content: <SuggestionsTab /> },
  { key: 'policies', label: t('security.policies'), icon: <Icon name="policy" size="sm" />, content: <PoliciesTab /> },
]
```

- [ ] **Step 8: Run tests and commit**

Run: `cd web && npm run test:e2e -- admin-layout-primitives.spec.ts && npm run type-check`

Expected: PASS.

```bash
git add web/src/components/Tabs.tsx web/src/components/SectionHeader.tsx web/src/components/DataTable.tsx web/src/components/TableViewport.tsx web/src/components/Segmented.tsx web/src/hooks/useMediaQuery.ts web/src/admin/pages/Security.tsx web/e2e/admin-layout-primitives.spec.ts
git commit -m "fix(ui): add responsive tabs headers and tables"
```

### Task 6: Fix the Admin shell, mobile drawer, live status, and brand lockup

**Depends on:** Plan 01 Task 8's committed `usePrincipal`, Principal DTO, `RequireAuth` gate, and removal of the
`localStorage.user` role snapshot.

**Files:**
- Modify: `web/src/admin/components/MainLayout.tsx`
- Modify: `web/src/admin/components/NowStrip.tsx`
- Modify: `web/src/admin/AdminApp.tsx`
- Modify: `web/src/i18n/zh.ts`
- Modify: `web/src/i18n/en.ts`
- Create: `web/e2e/admin-shell.spec.ts`

**Interfaces:**
- Consumes: `usePrincipal()` from Plan 01.
- Produces: desktop sidebar and controlled Base UI mobile Dialog using the same `SidebarContent` function.

- [ ] **Step 1: Add failing drawer and unavailable-status tests**

```ts
// web/e2e/admin-shell.spec.ts
import { adminApiDefaults, test, expect, mockAdminApi } from './fixtures/admin-api'

test('closed mobile drawer has no focusable offscreen links', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin')
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: /管理导航/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
})

test('failed now request never displays healthy', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/now': { status: 500, body: { code: 'FAILED', message: 'down' } } })
  await page.goto('/admin')
  await expect(page.getByText(/状态不可用/)).toBeVisible()
  await expect(page.getByText(/健康|已就绪/)).toHaveCount(0)
})

test('principal failure gates the outlet until Retry succeeds', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': () => {
      calls += 1
      return calls === 1
        ? { status: 500, body: { code: 'FAILED', message: 'principal unavailable' } }
        : adminApiDefaults['GET /api/v1/auth/me']
    },
  })
  await page.goto('/admin')
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)
  await page.getByRole('button', { name: /重试/ }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.locator('[data-admin-outlet]')).toBeVisible()
})
```

- [ ] **Step 2: Run and verify focus/health failures**

Run: `cd web && npm run test:e2e -- admin-shell.spec.ts`

Expected: FAIL because the translated aside remains tabbable and NowStrip defaults to healthy.

- [ ] **Step 3: Split desktop and mobile navigation shells**

Extract `SidebarContent({onNavigate})`. Render a desktop `<aside className="hidden lg:flex">` and render the
mobile copy only inside `DrawerV2` while open. Associate the menu button through a stable ID,
add `aria-expanded` and `aria-controls`, focus the first navigation link after open, and let Dialog restore focus.
Delete the route-change `setState` effect; close the drawer directly in each navigation callback.

- [ ] **Step 4: Load Principal at the shell boundary**

Retain Plan 01's ownership boundary: `RequireAuth` calls `usePrincipal(Boolean(token))` and never renders protected
children before a Principal exists. Replace only its plain error element with the shared state component:

```tsx
function RequireAuth({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const location = useLocation()
  const token = localStorage.getItem('token')
  const { principal, isPending, isError, refetch } = usePrincipal(Boolean(token))

  if (!token) return <Navigate to="/admin/login" state={{ from: location }} replace />
  if (isPending) return <div aria-busy="true" className="min-h-screen" />
  if (isError || !principal) {
    return (
      <main className="grid min-h-screen place-items-center p-4">
        <QueryErrorState message={t('auth.principalLoadError')} onRetry={() => { void refetch() }} />
      </main>
    )
  }
  return <>{children}</>
}
```

The existing Axios 401 interceptor removes the rejected token and redirects to Login. `MainLayout` calls the cached
`const { principal, canWrite } = usePrincipal()` only for identity and capability presentation; it must not become a
second auth gate. Use `canWrite`, not a local role comparison, for mutation-only shell affordances. Readonly users
retain all readable page links.

- [ ] **Step 5: Fix NowStrip query semantics**

Destructure `isPending`, `isError`, `isRefetchError`, `dataUpdatedAt`, and `refetch`. Put
`data-query-key="now"` on the strip root. Pending renders a neutral status; initial error renders
`now.statusUnavailable` with a non-pulsing warning dot; stale cached data remains visible with a stale label and a
secondary `now.refresh` button that invokes `refetch`. Never assign `data?.status ?? 'healthy'`.

- [ ] **Step 6: Align shell layout and brand**

Render lowercase `depsilo` at 15px/700 and use `--hit/--on-hit` for the avatar. Put `data-admin-main` on the
`min-w-0 flex-1` main content region beside the 220px desktop sidebar. Inside it, put `data-admin-outlet` on the
outlet container with `w-full max-w-[1840px] mx-auto` and 16px mobile padding. Ensure the topbar title can shrink
before right-side controls.

- [ ] **Step 7: Add translated labels and run tests**

Add exact zh/en keys for open navigation, close navigation, admin navigation, `auth.principalLoadError`, status
unavailable, stale data, refresh, retry, permission denied, and close.

Run: `cd web && npm run test:e2e -- admin-shell.spec.ts && npm run type-check && npm run build && cd .. && python3 scripts/i18n-audit.py`

Expected: all commands PASS.

- [ ] **Step 8: Commit the shell**

```bash
git add web/src/admin/components/MainLayout.tsx web/src/admin/components/NowStrip.tsx web/src/admin/AdminApp.tsx web/e2e/admin-shell.spec.ts
git add -p -- web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "fix(admin): make shell status and navigation accessible"
```

### Task 7: Rebuild Webhook on the shared system

**Depends on:** Plan 02 Task 6's committed Settings Tabs composition. This task changes only `WebhookTab` and its
shared client type; it does not edit `Settings.tsx` or Settings DTOs.

**Files:**
- Modify: `web/src/admin/components/WebhookTab.tsx`
- Modify: `web/src/lib/api.ts`
- Modify: `web/src/i18n/zh.ts`
- Modify: `web/src/i18n/en.ts`
- Create: `web/e2e/admin-webhook.spec.ts`

**Interfaces:**
- Consumes: `usePrincipal().canWrite`, `TabsV2`, accessible fields, Toast, InlineNotice, and IconButton.

- [ ] **Step 1: Write mobile and truthful Webhook feedback failures**

```ts
// web/e2e/admin-webhook.spec.ts
import type { Page } from '@playwright/test'
import type { WebhookConfig } from '../src/lib/api'
import { test, expect, mockAdminApi } from './fixtures/admin-api'

test('webhook rows and actions fit 390px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  await expect(page.getByRole('button', { name: /添加 Webhook/ })).toBeVisible()
})

test('failed webhook test renders a danger toast', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [{ id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/hook', enabled: true, events: '*', cooldown_minutes: 30, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }],
    'POST /api/v1/admin/webhooks/1/test': { status: 502, body: { code: 'WEBHOOK_FAILED', message: 'delivery failed' } },
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  await page.getByRole('button', { name: /测试/ }).click()
  await expect(page.getByRole('alert')).toContainText('delivery failed')
})
```

- [ ] **Step 2: Run and verify current legacy-style/feedback failures**

Run: `cd web && npm run test:e2e -- admin-webhook.spec.ts`

Expected: FAIL because Webhook uses undefined legacy classes/tokens and its fixed toast does not expose the shared alert contract.

- [ ] **Step 3: Remove all legacy Webhook styling**

Replace `.btn`, `.btn-primary`, `.btn-danger`, `--accent`, `--green`, and `--red` with ButtonV2, BadgeV2,
IconButton, accessible fields, ModalV2, Toast, and semantic status tokens. On mobile, each Webhook row stacks
details above actions. Translate Test/Edit/Delete/disabled and relative-time copy. Masked readonly URLs remain text,
and mutation controls do not render when the hook's `canWrite` is false. The query container uses
`aria-busy={query.isPending || undefined}`; loading never renders EmptyState. Action labels include the row name,
for example `测试 ops`, `编辑 ops`, and `删除 ops`.

In `web/src/lib/api.ts`, change `WebhookConfig.last_sent_at?: string` to the exact wire shape
`last_sent_at: string | null`. Do not add a client fallback or omit the field; the backend always serializes the
nullable timestamp.

- [ ] **Step 4: Test Webhook states**

Append these exact fixture cases to the spec:

```ts
const webhookRows = [
  { id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/one', enabled: true, events: '*', cooldown_minutes: 30, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' },
  { id: 2, name: 'audit', platform: 'generic', url: 'https://example.test/two', enabled: false, events: 'tamper_detected', cooldown_minutes: 60, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' },
] satisfies WebhookConfig[]

async function openWebhookTab(page: Page) {
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  return page.getByRole('tabpanel')
}

test('renders loading before an empty successful Webhook response', async ({ page }) => {
  let release!: (rows: WebhookConfig[]) => void
  const response = new Promise<WebhookConfig[]>(resolve => { release = resolve })
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': async () => response })
  const panel = await openWebhookTab(page)
  await expect(panel.locator('[aria-busy="true"]')).toBeVisible()
  await expect(page.getByText(/尚未配置 Webhook/)).toHaveCount(0)
  release([])
  await expect(page.getByText(/尚未配置 Webhook/)).toBeVisible()
})

test('renders one Webhook row without an empty or duplicate action state', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': [webhookRows[0]] })
  await openWebhookTab(page)
  await expect(page.getByText('ops')).toHaveCount(1)
  await expect(page.getByRole('button', { name: /测试 ops/ })).toHaveCount(1)
  await expect(page.getByText(/尚未配置 Webhook/)).toHaveCount(0)
})

test('renders enabled and disabled webhooks with shared actions', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': webhookRows })
  await openWebhookTab(page)
  await expect(page.getByText('ops')).toBeVisible()
  const disabled = page.getByText(/^已禁用$/)
  await expect(disabled).toBeVisible()
  await expect(page.getByRole('button', { name: /删除 audit/ })).toBeVisible()
  const colors = await disabled.evaluate(element => {
    const style = getComputedStyle(element)
    return { background: style.backgroundColor, color: style.color }
  })
  expect(colors.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(colors.color).not.toBe('rgba(0, 0, 0, 0)')
  expect(colors.color).not.toBe('')
})

test('keeps Test stable while pending and announces success', async ({ page }) => {
  let release!: (value: { status: string }) => void
  const response = new Promise<{ status: string }>(resolve => { release = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[0]],
    'POST /api/v1/admin/webhooks/1/test': async () => response,
  })
  await openWebhookTab(page)
  const button = page.getByRole('button', { name: /测试 ops/ })
  const before = await button.boundingBox()
  await button.click()
  await expect(button).toBeDisabled()
  await expect(button).toHaveAttribute('aria-busy', 'true')
  expect(await button.boundingBox()).toEqual(before)
  release({ status: 'test sent' })
  await expect(page.locator('[data-toast-tone="success"]')).toContainText(/测试通知已发送/)
})

test('shows the service error and no success Toast when Test fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[0]],
    'POST /api/v1/admin/webhooks/1/test': { status: 502, body: { code: 'WEBHOOK_FAILED', message: 'fixture webhook failure' } },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /测试 ops/ }).click()
  await expect(page.getByRole('alert')).toContainText('fixture webhook failure')
  await expect(page.locator('[data-toast-tone="success"]')).toHaveCount(0)
})

test('opens a named delete Dialog and restores the Delete trigger', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': webhookRows })
  await openWebhookTab(page)
  const trigger = page.getByRole('button', { name: /删除 audit/ })
  await trigger.click()
  await expect(page.getByRole('dialog', { name: /删除.*Webhook/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
})
```

- [ ] **Step 5: Run and commit**

Run: `cd web && npm run test:e2e -- admin-webhook.spec.ts && npm run type-check && cd .. && python3 scripts/i18n-audit.py`

Expected: PASS.

```bash
git add web/src/admin/components/WebhookTab.tsx web/e2e/admin-webhook.spec.ts
git add -p -- web/src/lib/api.ts web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "fix(admin): rebuild webhook workflow"
```

### Task 8: Repair fixed metric and analysis grids

**Files:**
- Modify: `web/src/admin/pages/Dashboard.tsx`
- Modify: `web/src/admin/components/TrendsCard.tsx`
- Modify: `web/src/admin/pages/BandwidthReport.tsx`
- Modify: `web/src/admin/pages/CacheManage.tsx`
- Modify: `web/src/admin/pages/Security.tsx`
- Modify: `web/src/admin/pages/License.tsx`
- Modify: `web/src/admin/pages/Rules.tsx`
- Modify: `web/src/components/Metric.tsx`
- Create: `web/e2e/admin-responsive-grids.spec.ts`

- [ ] **Step 1: Add width-matrix failures**

Use this exact matrix and overflow predicate:

```ts
import { expect, test } from './fixtures/admin-api'

const widths = [320, 390, 768, 1024, 1440]
const routes = ['/admin', '/admin/bandwidth', '/admin/cache', '/admin/security', '/admin/license']

for (const width of widths) {
  for (const route of routes) {
    test(`${route} fits ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: width < 700 ? 844 : 1000 })
      await page.goto(route)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
      const escaped = await page.locator('body *:visible').evaluateAll(elements => elements.filter(element => {
        if (element.closest('[data-table-viewport]')) return false
        const rect = element.getBoundingClientRect()
        return rect.left < -1 || rect.right > innerWidth + 1
      }).map(element => element.tagName + '.' + element.className))
      expect(escaped).toEqual([])
      const metricValues = page.locator('[data-metric-value]')
      await expect(metricValues.first()).toBeVisible()
      const wrappedMetrics = await metricValues.evaluateAll(elements => elements
        .filter(element => getComputedStyle(element).whiteSpace === 'normal')
        .map(element => element.textContent))
      expect(wrappedMetrics).toEqual([])
    })
  }
}
```

Add `data-metric-value` to the value element in `Metric.tsx`.

- [ ] **Step 2: Run at 320 and confirm current wrapping**

Run: `cd web && npm run test:e2e -- admin-responsive-grids.spec.ts --grep 320`

Expected: FAIL on Bandwidth values, Trends controls, and fixed grids.

- [ ] **Step 3: Apply exact grid classes**

- Metric: add `whitespace-nowrap` to values so unit/value pairs never split.
- Dashboard/Trends: action groups wrap; all labels add `whitespace-nowrap`; active range uses `--hit/--on-hit`.
- Bandwidth/Security KPI: `grid-cols-2 gap-6 lg:grid-cols-4 lg:gap-8`.
- Bandwidth three-panel analysis: `grid-cols-1 gap-y-12 xl:grid-cols-3 xl:gap-x-10`.
- Cache overview: `grid-cols-1 gap-y-12 xl:grid-cols-3 xl:gap-x-10`; treemap is `xl:col-span-2`.
- License comparison: `grid-cols-1 gap-8 md:grid-cols-2`.
- Rules allow/result colors: `--ok-fill`, `--ok-text`, `--ok-border` only.
- Cache treemap labels: theme text on a tokenized opaque label backing, never fixed white on translucent cells.

- [ ] **Step 4: Run matrix and commit**

Run: `cd web && npm run test:e2e -- admin-responsive-grids.spec.ts && npm run type-check`

Expected: PASS at all widths.

```bash
git add web/src/components/Metric.tsx web/src/admin/pages/Dashboard.tsx web/src/admin/components/TrendsCard.tsx web/src/admin/pages/BandwidthReport.tsx web/src/admin/pages/CacheManage.tsx web/src/admin/pages/Security.tsx web/src/admin/pages/License.tsx web/src/admin/pages/Rules.tsx web/e2e/admin-responsive-grids.spec.ts
git commit -m "fix(admin): make dashboards responsive at narrow widths"
```

### Task 9: Migrate every table and icon action

**Depends on:** Plan 03 Task 9's committed `Upstreams.tsx` runtime-response/cache migration. Re-read that file after
the dependency lands; this task may change only table/action presentation around its typed data flow.

**Files:**
- Modify: `web/src/admin/pages/AccessLogs.tsx`
- Modify: `web/src/admin/pages/AuditLogs.tsx`
- Modify: `web/src/admin/pages/CacheManage.tsx`
- Modify: `web/src/admin/pages/Quarantine.tsx`
- Modify: `web/src/admin/pages/Rules.tsx`
- Modify: `web/src/admin/pages/Users.tsx`
- Modify: `web/src/admin/pages/Projects.tsx`
- Modify: `web/src/admin/pages/Upstreams.tsx`
- Modify: `web/src/admin/pages/Security.tsx`
- Modify: `web/src/components/ThemeToggle.tsx`
- Modify: `web/src/components/LangToggle.tsx`
- Create: `web/e2e/admin-tables-actions.spec.ts`

- [ ] **Step 1: Add populated table fixtures and failures**

Use this fixture map so every table really renders:

```ts
import type { AccessLogListResponse, AdminUpstreamListResponse, AuditLogListResponse } from '../src/lib/adminApi.types'

const populated = {
  'GET /api/v1/admin/logs': {
    items: [{ id: 1, adapter_type: 'pypi', method: 'GET', package_name: 'requests', cache_key: 'pypi/requests', hit: true, latency_ms: 12, upstream: 'tuna', status_code: 200, client_ip: '127.0.0.1', bytes_sent: 1024, created_at: '2026-07-10T00:00:00Z' }],
    total: 1, page: 1, page_size: 50,
  } satisfies AccessLogListResponse,
  'GET /api/v1/admin/audit-logs': {
    items: [{ id: 1, ecosystem: 'pypi', package_name: 'requests', version: '2.32.0', action: 'proxy', cache_result: 'hit', latency_ms: 12, bytes_sent: 1024, client_ip: '127.0.0.1', user_agent: 'fixture', upstream_url: 'https://pypi.example/simple', status_code: 200, created_at: '2026-07-10T00:00:00Z' }],
    total: 1, page: 1,
  } satisfies AuditLogListResponse,
  'GET /api/v1/admin/cache': { items: [{ id: 1, adapter_type: 'pypi', package_name: 'requests', key: 'pypi/requests', size: 1024, hit_count: 2, expires_at: '2026-07-11T00:00:00Z', last_accessed: '2026-07-10T00:00:00Z' }], total: 1 },
  'GET /api/v1/admin/quarantine/events': { items: [{ id: 1, ecosystem: 'pypi', package: 'unsafe', version: '1.0.0', action: 'blocked', reason: 'minimum age', created_at: '2026-07-10T00:00:00Z' }], total: 1 },
  'GET /api/v1/admin/rules': [{ id: 1, ecosystem: 'pypi', package_name: 'unsafe', version: '*', action: 'deny', reason: 'blocked', created_at: '2026-07-10T00:00:00Z' }],
  'GET /api/v1/admin/users': [{ id: 1, username: 'admin', role: 'admin', enabled: true, created_at: '2026-07-10T00:00:00Z' }],
  'GET /api/v1/admin/upstreams': { items: [{ id: 1, adapter_type: 'pypi', name: 'tuna', url: 'https://pypi.example/simple', proxy: '', priority: 1, probe_mode: 'active', probe_interval: '30m', healthy: true, avg_latency_ms: 12, success_rate: 1, last_checked_at: '2026-07-10T00:00:00Z', worker_running: true, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }], total: 1 } satisfies AdminUpstreamListResponse,
}
```

For `/admin/logs`, `/admin/audit`, `/admin/cache`, `/admin/quarantine`, `/admin/rules`, `/admin/users`, and
`/admin/upstreams`, assert page scroll width equals viewport width, the named table region can scroll, every
`[data-icon-button]` has a name and 40x40 box, and Tab never focuses a `<tr>`.

- [ ] **Step 2: Run and confirm clipping/action failures**

Run: `cd web && npm run test:e2e -- admin-tables-actions.spec.ts`

Expected: FAIL on raw Access/Audit/Cache/Quarantine tables and hand-written action buttons.

- [ ] **Step 3: Wrap all raw tables**

Use `TableViewport` with these minimum widths: AccessLogs 860, AuditLogs 980, Cache 820, Quarantine events 920,
Quarantine approvals 820, Quarantine overrides 760. Remove every `overflow-hidden` wrapper around a data table.

- [ ] **Step 4: Migrate DataTable call sites**

Pass stable resource IDs as `rowKey`, localized `ariaLabel`, and page-appropriate `minWidth`. Remove `onRowClick`.
Projects renders an explicit localized View button/link in the first or action column.

- [ ] **Step 5: Replace hand-written icon actions**

Migrate edit/delete/check/toggle/copy/page actions in Rules, Users, Projects, Upstreams, Cache, Security, and
Quarantine to IconButton. Destructive actions use `tone="danger"`; pending state uses `loading`; all labels come from i18n.
Use the same 40px labelled/tooltip contract for ThemeToggle; give LangToggle a stable minimum 40px target.

- [ ] **Step 6: Run and commit**

Run: `cd web && npm run test:e2e -- admin-tables-actions.spec.ts && npm run type-check && cd .. && python3 scripts/i18n-audit.py`

Expected: PASS.

```bash
git add web/src/admin/pages/AccessLogs.tsx web/src/admin/pages/AuditLogs.tsx web/src/admin/pages/CacheManage.tsx web/src/admin/pages/Quarantine.tsx web/src/admin/pages/Rules.tsx web/src/admin/pages/Users.tsx web/src/admin/pages/Projects.tsx web/src/admin/pages/Security.tsx web/src/components/ThemeToggle.tsx web/src/components/LangToggle.tsx web/e2e/admin-tables-actions.spec.ts
git add -p -- web/src/admin/pages/Upstreams.tsx web/src/i18n/zh.ts web/src/i18n/en.ts
git commit -m "fix(admin): make tables and row actions accessible"
```

### Task 10: Apply the query and mutation state contract to all routes

**Depends on:** Plan 03 Task 9's committed `Upstreams.tsx` runtime-response/cache migration and this plan's Task 7
Webhook state tests. Preserve those data and feedback contracts while applying the shared query-state pattern.

**Files:**
- Modify: `web/src/admin/pages/Dashboard.tsx`
- Modify: `web/src/admin/components/NowStrip.tsx`
- Modify: `web/src/admin/components/TrendsCard.tsx`
- Modify: `web/src/admin/components/WebhookTab.tsx`
- Modify: `web/src/admin/pages/BandwidthReport.tsx`
- Modify: `web/src/admin/pages/AccessLogs.tsx`
- Modify: `web/src/admin/pages/AuditLogs.tsx`
- Modify: `web/src/admin/pages/Quarantine.tsx`
- Modify: `web/src/admin/pages/CacheManage.tsx`
- Modify: `web/src/admin/pages/Upstreams.tsx`
- Modify: `web/src/admin/pages/Users.tsx`
- Modify: `web/src/admin/pages/License.tsx`
- Modify: `web/src/admin/pages/Rules.tsx`
- Modify: `web/src/admin/pages/Security.tsx`
- Modify: `web/src/admin/pages/Projects.tsx`
- Create: `web/src/lib/apiError.ts`
- Create: `web/e2e/admin-query-states.spec.ts`

**Interfaces:**
- Produces: `getApiError(error): {status?:number; code?:string; message:string}`.

- [ ] **Step 1: Write a route-by-route 500/403/stale matrix**

Create `web/e2e/admin-query-states.spec.ts` with this complete suite:

```ts
// web/e2e/admin-query-states.spec.ts
import type { Locator, Page } from '@playwright/test'
import {
  adminApiDefaults,
  expect,
  mockAdminApi,
  test,
  type AdminApiOverrides,
  type JsonValue,
  type MockHttpResponse,
} from './fixtures/admin-api'

interface QueryCase {
  path: string
  endpoint: string
  success: JsonValue | MockHttpResponse
}

const primaryQueries = [
  { path: '/admin', endpoint: 'GET /api/v1/admin/dashboard', success: adminApiDefaults['GET /api/v1/admin/dashboard'] },
  { path: '/admin/bandwidth', endpoint: 'GET /api/v1/admin/bandwidth', success: adminApiDefaults['GET /api/v1/admin/bandwidth'] },
  { path: '/admin/logs', endpoint: 'GET /api/v1/admin/logs', success: adminApiDefaults['GET /api/v1/admin/logs'] },
  { path: '/admin/audit', endpoint: 'GET /api/v1/admin/audit-logs', success: adminApiDefaults['GET /api/v1/admin/audit-logs'] },
  { path: '/admin/quarantine', endpoint: 'GET /api/v1/admin/quarantine/events', success: adminApiDefaults['GET /api/v1/admin/quarantine/events'] },
  { path: '/admin/cache', endpoint: 'GET /api/v1/admin/cache', success: adminApiDefaults['GET /api/v1/admin/cache'] },
  { path: '/admin/upstreams', endpoint: 'GET /api/v1/admin/upstreams', success: adminApiDefaults['GET /api/v1/admin/upstreams'] },
  { path: '/admin/users', endpoint: 'GET /api/v1/admin/users', success: adminApiDefaults['GET /api/v1/admin/users'] },
  { path: '/admin/license', endpoint: 'GET /api/v1/admin/license/status', success: adminApiDefaults['GET /api/v1/admin/license/status'] },
  { path: '/admin/rules', endpoint: 'GET /api/v1/admin/rules', success: adminApiDefaults['GET /api/v1/admin/rules'] },
  { path: '/admin/security', endpoint: 'GET /api/v1/admin/security/dashboard', success: adminApiDefaults['GET /api/v1/admin/security/dashboard'] },
  { path: '/admin/projects', endpoint: 'GET /api/v1/admin/projects', success: { items: [], total: 0 } },
] satisfies readonly QueryCase[]

for (const query of primaryQueries) {
  test(`${query.path} recovers from an initial 500 only after manual Retry`, async ({ page }) => {
    let calls = 0
    await mockAdminApi(page, {
      [query.endpoint]: () => {
        calls += 1
        return calls === 1
          ? { status: 500, body: { code: 'FAILED', message: 'fixture initial failure' } }
          : query.success
      },
    })
    await page.goto(query.path)
    const error = page.getByRole('alert').filter({ hasText: 'fixture initial failure' })
    await expect(error).toBeVisible()
    await expect.poll(() => calls).toBe(1)
    await expect(page.getByText(/暂无|没有数据|暂无数据/)).toHaveCount(0)
    await error.getByRole('button', { name: /重试/ }).click()
    await expect.poll(() => calls).toBe(2)
    await expect(error).toHaveCount(0)
    await expect(page.locator('h1')).toBeVisible()
  })

  test(`${query.path} renders 403 as permission denied rather than empty`, async ({ page }) => {
    await mockAdminApi(page, {
      [query.endpoint]: { status: 403, body: { code: 'FORBIDDEN', message: 'fixture forbidden' } },
    })
    await page.goto(query.path)
    await expect(page.getByRole('alert')).toContainText(/权限|permission/i)
    await expect(page.getByText(/暂无|没有数据|暂无数据/)).toHaveCount(0)
  })
}

test('Projects 402 renders the Pro upgrade callout rather than empty or permission denied', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { status: 402, body: { code: 'PRO_REQUIRED', message: 'Pro required' } },
  })
  await page.goto('/admin/projects')
  await expect(page.getByText(/多项目工作区.*Pro/)).toBeVisible()
  await expect(page.getByRole('link', { name: /购买|升级|终身/ })).toBeVisible()
  await expect(page.getByText(/暂无项目|no projects/i)).toHaveCount(0)
  await expect(page.getByText(/权限不足|permission denied/i)).toHaveCount(0)
})

async function refocus(page: Page) {
  const other = await page.context().newPage()
  await other.goto('about:blank')
  await other.bringToFront()
  await page.bringToFront()
  await other.close()
}

async function pausePollingClock(page: Page) {
  await page.clock.install({ time: new Date('2026-07-10T00:00:00Z') })
  await page.clock.pauseAt(new Date('2026-07-10T00:00:01Z'))
}

test('NowStrip keeps healthy cached data when its focus refetch fails', async ({ page }) => {
  await pausePollingClock(page)
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/now': () => {
      calls += 1
      return calls === 1
        ? { status: 'healthy', last_activity: null, rate: { requests_per_min: 7, ingress_bps: 10, egress_bps: 20 }, upstreams: { healthy: 1, total: 1 } }
        : { status: 500, body: { code: 'FAILED', message: 'now refetch failed' } }
    },
  })
  await page.goto('/admin')
  await page.clock.runFor(2_000)
  const now = page.locator('[data-query-key="now"]')
  await expect(now).toContainText(/健康|healthy/i)
  await refocus(page)
  await expect.poll(() => calls).toBe(2)
  await expect(now).toContainText(/健康|healthy/i)
  await expect(now).toContainText(/陈旧|已过期|stale/i)
})

test('Dashboard trends keeps its rendered chart when its focus refetch fails', async ({ page }) => {
  await pausePollingClock(page)
  let calls = 0
  const point = {
    bucket: 1783641600, date: '2026-07-10', requests: 12, hits: 10, misses: 2,
    hit_rate: 0.8333, bytes_served: 1024, bytes_hit: 800, bytes_miss: 224,
    sum_latency_ms: 120, avg_latency_ms: 10, errors: 1,
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': () => {
      calls += 1
      return calls === 1
        ? { points: [point] }
        : { status: 500, body: { code: 'FAILED', message: 'trends refetch failed' } }
    },
  })
  await page.goto('/admin')
  await page.clock.runFor(2_000)
  const trends = page.locator('[data-query-key="dashboard-trends"]')
  await expect(trends.locator('.recharts-wrapper')).toBeVisible()
  await refocus(page)
  await expect.poll(() => calls).toBe(2)
  await expect(trends.locator('.recharts-wrapper')).toBeVisible()
  await expect(trends).toContainText(/陈旧|已过期|stale/i)
})

// The page clock remains paused after the 2-second render allowance. This keeps
// the 5-second and 30-second polling intervals from consuming the failure that
// each test reserves for the explicit focus transition.

interface MutationCase {
  name: string
  path: string
  endpoint: string
  status: 422 | 500
  fixtures?: AdminApiOverrides
  submit(page: Page): Promise<void>
  retained(page: Page): Locator
}

const webhook = {
  id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/hook', enabled: true,
  events: '*', cooldown_minutes: 30, last_sent_at: null,
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
}

const mutationCases: MutationCase[] = [
  {
    name: 'Cache cleanup', path: '/admin/cache', endpoint: 'POST /api/v1/admin/cache/cleanup', status: 500,
    submit: async page => {
      await page.getByRole('button', { name: /清理过期/ }).click()
      await page.getByRole('dialog').getByRole('button', { name: /确认清理/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /清理过期缓存/ }),
  },
  {
    name: 'Upstream save', path: '/admin/upstreams', endpoint: 'POST /api/v1/admin/upstreams', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /添加上游源/ }).click()
      await page.getByLabel(/^名称$/).fill('fixture')
      await page.getByLabel(/^URL$/).fill('https://fixture.example/simple')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加上游源/ }),
  },
  {
    name: 'User save', path: '/admin/users', endpoint: 'POST /api/v1/admin/users', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /添加用户/ }).click()
      await page.getByLabel(/用户名/).fill('fixture-user')
      await page.getByLabel(/^密码$/).fill('fixture-password')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加用户/ }),
  },
  {
    name: 'Token create', path: '/admin/users', endpoint: 'POST /api/v1/admin/tokens', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /生成 Token/ }).click()
      await page.getByRole('dialog').getByLabel(/^名称$/).fill('fixture-token')
      await page.getByRole('dialog').getByRole('button', { name: /^生成$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /生成 Token/ }),
  },
  {
    name: 'License update', path: '/admin/license', endpoint: 'PUT /api/v1/admin/license/key', status: 422,
    submit: async page => {
      await page.getByPlaceholder(/depsilo-/).fill('depsilo-fixture-invalid')
      await page.getByRole('button', { name: /激活/ }).click()
    },
    retained: page => page.getByPlaceholder(/depsilo-/),
  },
  {
    name: 'Rule save', path: '/admin/rules', endpoint: 'POST /api/v1/admin/rules', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /添加规则/ }).click()
      await page.getByLabel(/包名/).fill('fixture-package')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加规则/ }),
  },
  {
    name: 'Security policy save', path: '/admin/security', endpoint: 'PUT /api/v1/admin/security/policies/pypi', status: 422,
    fixtures: {
      'GET /api/v1/admin/security/policies': [{ id: 1, ecosystem: 'pypi', auto_block_enabled: true, min_cvss_score: 8.5, created_by: 'admin', created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }],
    },
    submit: async page => {
      await page.getByRole('tab', { name: /策略/ }).click()
      await page.getByRole('tabpanel').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('tabpanel'),
  },
  {
    name: 'Project save', path: '/admin/projects', endpoint: 'POST /api/v1/admin/projects', status: 422,
    fixtures: { 'GET /api/v1/admin/projects': { items: [], total: 0 } },
    submit: async page => {
      await page.getByRole('button', { name: /创建项目/ }).first().click()
      await page.getByLabel(/项目名称/).fill('fixture-project')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /创建项目/ }),
  },
  {
    name: 'Webhook test', path: '/admin/settings', endpoint: 'POST /api/v1/admin/webhooks/1/test', status: 500,
    fixtures: { 'GET /api/v1/admin/webhooks': [webhook] },
    submit: async page => {
      await page.getByRole('tab', { name: /Webhook/ }).click()
      await page.getByRole('button', { name: /测试 ops/ }).click()
    },
    retained: page => page.getByText('ops'),
  },
  {
    name: 'Quarantine approval', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/quarantine/approve', status: 422,
    fixtures: {
      'GET /api/v1/admin/quarantine/events': { items: [{ id: 1, ecosystem: 'pypi', package: 'fixture-package', version: '1.0.0', action: 'blocked', reason: 'minimum age', created_at: '2026-07-10T00:00:00Z' }], total: 1 },
    },
    submit: async page => {
      await page.getByRole('button', { name: /^放行$/ }).click()
      await page.getByPlaceholder(/填写理由/).fill('fixture approval reason')
      await page.getByRole('dialog').getByRole('button', { name: /确认放行/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /放行此版本/ }),
  },
  {
    name: 'Blocklist override creation', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/blocklist/overrides', status: 422,
    submit: async page => {
      await page.getByRole('tab', { name: /恶意封锁/ }).click()
      await page.getByRole('button', { name: /添加豁免/ }).click()
      await page.getByPlaceholder(/^包名$/).fill('fixture-package')
      await page.getByPlaceholder(/填写理由/).fill('fixture override reason')
      await page.getByRole('dialog').getByRole('button', { name: /创建豁免/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /创建恶意封锁豁免/ }),
  },
  {
    name: 'Blocklist sync', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/blocklist/sync', status: 500,
    submit: async page => {
      await page.getByRole('tab', { name: /恶意封锁/ }).click()
      await page.getByRole('button', { name: /立即同步/ }).click()
    },
    retained: page => page.getByRole('button', { name: /立即同步/ }),
  },
]

for (const mutation of mutationCases) {
  test(`${mutation.name} retains context and never reports success after failure`, async ({ page }) => {
    const message = `fixture ${mutation.name} failure`
    await mockAdminApi(page, {
      ...mutation.fixtures,
      [mutation.endpoint]: { status: mutation.status, body: { code: 'MUTATION_FAILED', message } },
    })
    await page.goto(mutation.path)
    await mutation.submit(page)
    await expect(page.getByRole('alert').filter({ hasText: message })).toBeVisible()
    await expect(mutation.retained(page)).toBeVisible()
    await expect(page.locator('[data-toast-tone="success"]')).toHaveCount(0)
  })
}
```

- [ ] **Step 2: Run the matrix and verify failures**

Run: `cd web && npm run test:e2e -- admin-query-states.spec.ts`

Expected: FAIL on every page that currently only checks `isLoading`.

- [ ] **Step 3: Add one typed Axios error normalizer**

```ts
// web/src/lib/apiError.ts
import axios from 'axios'

export interface ApiErrorInfo { status?: number; code?: string; message: string }

export function getApiError(error: unknown): ApiErrorInfo {
  if (axios.isAxiosError<{ code?: string; message?: string }>(error)) {
    return {
      status: error.response?.status,
      code: error.response?.data?.code,
      message: error.response?.data?.message || error.message,
    }
  }
  return { message: error instanceof Error ? error.message : 'Unknown error' }
}
```

- [ ] **Step 4: Migrate primary query states by page**

At each query boundary, destructure `data`, `error`, `isPending`, `isError`, `isRefetchError`, and `refetch`. Apply
this exact precedence:

1. `isPending`: render the page's existing loading container with `aria-busy="true"`; its skeleton is
   `aria-hidden="true"`, and no EmptyState is mounted.
2. `isError && !data`: normalize `error`; status 403 renders translated `permissionDenied`, every other status uses
   the service message; render `QueryErrorState` whose Retry calls `void refetch()`.
3. `data && isRefetchError`: keep the populated/empty success DOM mounted and prepend `InlineNotice tone="warning"`
   with translated `staleData` plus a secondary Refresh button calling `void refetch()`.
4. `data` with zero typed items: render the page's existing EmptyState. Otherwise render the typed data view.

Set `retry: false` on every Admin query migrated in this task so an initial request failure reaches the explicit
`QueryErrorState` once and only its Retry button starts the next request. Do not change the application-wide
`QueryClient` defaults. On the `NowStrip` and Dashboard trends queries also set
`refetchOnWindowFocus: 'always'` (and `retry: false`) so the focus-refetch acceptance cases run even while cached data
is still fresh; preserve their existing polling intervals and stale times.

Apply the four branches at page level to Dashboard, Bandwidth, License, and Projects; before empty branching in
AccessLogs, AuditLogs, Cache, Upstreams, Users, and Rules; and independently inside each Security and Quarantine
section. Wrap the Dashboard trend region with `data-query-key="dashboard-trends"`. The Step 1 Retry, 403, and stale
tests are the executable acceptance for every branch.

- [ ] **Step 5: Migrate mutation states by page**

For each mutation named in Step 1, remove dialog-closing setters from `onSettled`, `onError`, and submit handlers;
invoke them only after the typed request resolves in `onSuccess`. Normalize `mutation.error` with `getApiError` and
render its service `message` in `InlineNotice tone="danger"` inside the still-mounted dialog/form or beside the
action-only trigger. Bind the submit/action button to `loading={mutation.isPending}` and
`disabled={mutation.isPending || !canWrite}` so its fixed dimensions do not change.

Cache cleanup, Webhook test, and blocklist sync have no close setter and keep their trigger mounted. Upstream,
User/Token, License, Rule, Security policy, Project, Quarantine approval, and override creation keep their existing
form state until success. Each `onSuccess` updates the page's existing typed query key from the service response,
then closes if applicable, then sends the service-authored result message through a success-tone Toast.
Readonly principals do not render mutation triggers. The twelve executable cases in Step 1 prove retained context,
service error visibility, and absence of `[data-toast-tone="success"]` on failure.

- [ ] **Step 6: Run and commit**

Run: `cd web && npm run test:e2e -- admin-query-states.spec.ts && npm run type-check && npm run build`

Expected: PASS.

```bash
git add web/src/lib/apiError.ts web/e2e/admin-query-states.spec.ts
git add web/src/admin/pages/Dashboard.tsx web/src/admin/components/NowStrip.tsx web/src/admin/components/TrendsCard.tsx web/src/admin/components/WebhookTab.tsx
git add web/src/admin/pages/BandwidthReport.tsx web/src/admin/pages/AccessLogs.tsx web/src/admin/pages/AuditLogs.tsx web/src/admin/pages/Quarantine.tsx
git add web/src/admin/pages/CacheManage.tsx web/src/admin/pages/Users.tsx web/src/admin/pages/License.tsx
git add web/src/admin/pages/Rules.tsx web/src/admin/pages/Security.tsx web/src/admin/pages/Projects.tsx
git add -p -- web/src/admin/pages/Upstreams.tsx
git commit -m "fix(admin): distinguish error stale and empty states"
```

### Task 11: Close all lint errors in touched frontend files

**Files:**
- Create: `web/admin-remediation-eslint-files.txt`
- Modify: every Admin-remediation `.ts`/`.tsx` path from Plans 01-04 listed in `web/admin-remediation-eslint-files.txt` that ESLint reports
- Modify: `web/src/admin/pages/Quarantine.tsx`
- Modify: `web/src/admin/components/MainLayout.tsx`
- Modify: `web/src/admin/components/TrendsCard.tsx`

- [ ] **Step 1: Record the exact touched-file lint failure set**

Create `web/admin-remediation-eslint-files.txt` with paths relative to `web/`, one path per line and no globbing:

```text
playwright.config.ts
e2e/fixtures/admin-api.ts
e2e/admin-smoke.spec.ts
e2e/admin-contrast.spec.ts
e2e/admin-forms.spec.ts
e2e/admin-dialog-actions.spec.ts
e2e/admin-layout-primitives.spec.ts
e2e/admin-shell.spec.ts
e2e/admin-webhook.spec.ts
e2e/admin-responsive-grids.spec.ts
e2e/admin-tables-actions.spec.ts
e2e/admin-query-states.spec.ts
src/App.tsx
src/i18n/index.ts
src/i18n/en.ts
src/i18n/zh.ts
src/hooks/useMediaQuery.ts
src/hooks/usePrincipal.ts
src/lib/api.ts
src/lib/adminApi.types.ts
src/lib/adminApi.types.type-test.ts
src/lib/apiError.ts
src/components/Button.tsx
src/components/Badge.tsx
src/components/Input.tsx
src/components/Select.tsx
src/components/Textarea.tsx
src/components/Switch.tsx
src/components/Modal.tsx
src/components/Icon.tsx
src/components/Tooltip.tsx
src/components/IconButton.tsx
src/components/Toast.tsx
src/components/Drawer.tsx
src/components/InlineNotice.tsx
src/components/QueryErrorState.tsx
src/components/Tabs.tsx
src/components/SectionHeader.tsx
src/components/DataTable.tsx
src/components/TableViewport.tsx
src/components/Segmented.tsx
src/components/Metric.tsx
src/components/ThemeToggle.tsx
src/components/LangToggle.tsx
src/admin/AdminApp.tsx
src/admin/components/MainLayout.tsx
src/admin/components/NowStrip.tsx
src/admin/components/WebhookTab.tsx
src/admin/components/TrendsCard.tsx
src/admin/pages/Dashboard.tsx
src/admin/pages/BandwidthReport.tsx
src/admin/pages/CacheManage.tsx
src/admin/pages/Security.tsx
src/admin/pages/License.tsx
src/admin/pages/Rules.tsx
src/admin/pages/AccessLogs.tsx
src/admin/pages/AuditLogs.tsx
src/admin/pages/Quarantine.tsx
src/admin/pages/Users.tsx
src/admin/pages/Projects.tsx
src/admin/pages/Upstreams.tsx
src/admin/pages/Login.tsx
src/admin/pages/Settings.tsx
```

Run:

```bash
cd web
xargs -r npx eslint < admin-remediation-eslint-files.txt
```

Expected: FAIL only on remaining `no-explicit-any`, hook purity, or hook dependency findings in the exact Plans 01-04
Admin remediation manifest; no unrelated Portal path is linted.

- [ ] **Step 2: Replace API and page-local `any` types**

Use the DTOs from Plans 01-03 for API data. For chart library callbacks, import the exported Recharts prop types.
For unknown error values, use `getApiError`. For generic row rendering, use `unknown` plus a typed page row interface.
Do not suppress rules or add `eslint-disable` comments.

- [ ] **Step 3: Remove remaining purity and effect findings**

MainLayout must already close navigation in event callbacks rather than a synchronous route effect. In Quarantine,
replace render-time `Date.now()` with a stable mount timestamp:

```ts
const [mountedAt] = useState(() => Date.now())
const now = overridesQ.data?.now ? new Date(overridesQ.data.now).getTime() : mountedAt
```

Correct TrendsCard dependency arrays based on the values actually read; do not silence the hook rule.
Remove every nonzero `letter-spacing`/`tracking-*` utility from touched Admin components; this interface uses zero
letter spacing at all viewport sizes.

- [ ] **Step 4: Re-run exact-scope lint and regression tests**

Run:

```bash
cd web
xargs -r npx eslint < admin-remediation-eslint-files.txt
npm run type-check
npm run test:e2e
```

Expected: all commands PASS with zero warnings in touched files.

- [ ] **Step 5: Commit typed cleanup**

```bash
cd web
git add admin-remediation-eslint-files.txt
rg -v '^(src/i18n/(en|zh)\.ts|src/admin/pages/(Upstreams|Login|Settings)\.tsx|src/hooks/usePrincipal\.ts|src/lib/(api|adminApi\.types|adminApi\.types\.type-test)\.ts)$' admin-remediation-eslint-files.txt | xargs -r git add --
git add -p -- src/i18n/en.ts src/i18n/zh.ts src/admin/pages/Upstreams.tsx src/admin/pages/Login.tsx src/admin/pages/Settings.tsx src/hooks/usePrincipal.ts src/lib/api.ts src/lib/adminApi.types.ts src/lib/adminApi.types.type-test.ts
git commit -m "refactor(admin): type all remediated UI paths"
```

The partial command may report that a cross-plan or pre-existing dirty path has no Task 11 hunks; that is expected.
Before committing, verify `git diff --cached --name-only` and `git diff --cached`. If any partial-staged path contains
unrelated hunks, run:

```bash
cd web
git restore --staged -- src/i18n/en.ts src/i18n/zh.ts src/admin/pages/Upstreams.tsx src/admin/pages/Login.tsx src/admin/pages/Settings.tsx src/hooks/usePrincipal.ts src/lib/api.ts src/lib/adminApi.types.ts src/lib/adminApi.types.type-test.ts
git add -p -- src/i18n/en.ts src/i18n/zh.ts src/admin/pages/Upstreams.tsx src/admin/pages/Login.tsx src/admin/pages/Settings.tsx src/hooks/usePrincipal.ts src/lib/api.ts src/lib/adminApi.types.ts src/lib/adminApi.types.type-test.ts
```

Do not modify worktree contents while correcting the staged selection.

### Task 12: Complete API contract, accessibility, and visual matrices

**Depends on:** Plan 02 Task 6's committed Settings DTO/API methods and Plan 03 Task 9's committed Upstream DTO/API
methods. This avoids duplicating either plan's canonical types in the browser harness.

**Files:**
- Modify: `web/package.json`
- Create: `web/tsconfig.e2e.json`
- Modify: `web/admin-remediation-eslint-files.txt`
- Modify: `web/e2e/fixtures/admin-api.ts`
- Create: `web/e2e/admin-contracts.spec.ts`
- Create: `web/e2e/admin-axe.spec.ts`
- Create: `web/e2e/admin-visual-matrix.spec.ts`
- Modify: `web/playwright.config.ts`
- Modify: `.github/workflows/ci.yml`
- Modify: `DESIGN.md`

- [ ] **Step 1: Add a type-checked browser API contract suite**

Add `"type-check:e2e": "tsc -p tsconfig.e2e.json --noEmit"` beside `type-check` in `web/package.json`, then create:

```json
// web/tsconfig.e2e.json
{
  "extends": "./tsconfig.app.json",
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.e2e.tsbuildinfo",
    "types": ["vite/client", "node"],
    "noEmit": true
  },
  "include": ["e2e/**/*.ts"]
}
```

Add this step after the existing frontend Type check step in `.github/workflows/ci.yml`:

```yaml
      - name: Type check browser contracts
        run: npm run type-check:e2e
```

Append these exact Task 12 paths to `web/admin-remediation-eslint-files.txt`:

```text
e2e/admin-contracts.spec.ts
e2e/admin-axe.spec.ts
e2e/admin-visual-matrix.spec.ts
```

Now that Plans 01-03 have produced every canonical DTO, import these types into `web/e2e/fixtures/admin-api.ts`,
annotate the existing Settings constants, and spread the typed defaults last in `adminApiDefaults`:

```ts
import type {
  AccessLogListResponse,
  AdminSettingsResponse,
  AdminSettingsSnapshot,
  AdminUpstreamListResponse,
  AuditLogListResponse,
  ProjectListResponse,
  SecurityDashboard,
  SecurityPackagePage,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
} from '../../src/lib/adminApi.types'

const configuredSettings: AdminSettingsSnapshot = {
  server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
  database: { driver: 'sqlite' },
  storage: { type: 'local', path: './data/cache' },
  cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
  auth: { token_ttl: '168h' },
}
const effectiveSettings: AdminSettingsSnapshot = {
  ...configuredSettings,
  cache: { ...configuredSettings.cache, ttl_blob: '72h' },
}
const settingSources: AdminSettingsResponse['sources'] = {
  'server.host': 'file', 'server.port': 'file', 'server.log_level': 'file',
  'database.driver': 'file', 'storage.type': 'file', 'storage.path': 'file',
  'cache.max_size_gb': 'file', 'cache.ttl_index': 'file', 'cache.ttl_blob': 'file',
  'cache.lru_threshold': 'file', 'auth.token_ttl': 'file',
}
const editableSettings: AdminSettingsResponse['editable'] = [
  'server.log_level', 'cache.max_size_gb', 'cache.ttl_index',
  'cache.ttl_blob', 'cache.lru_threshold', 'auth.token_ttl',
]

const canonicalAdminApiDefaults = {
  'GET /api/v1/admin/settings': {
    configured: configuredSettings,
    effective: effectiveSettings,
    pending_restart: ['cache.ttl_blob'],
    overrides: {},
    sources: settingSources,
    editable: editableSettings,
    config_writable: true,
  } satisfies AdminSettingsResponse,
  'GET /api/v1/admin/upstreams': { items: [], total: 0 } satisfies AdminUpstreamListResponse,
  'GET /api/v1/admin/logs': { items: [], total: 0, page: 1, page_size: 50 } satisfies AccessLogListResponse,
  'GET /api/v1/admin/audit-logs': { items: [], total: 0, page: 1 } satisfies AuditLogListResponse,
  'GET /api/v1/admin/security/dashboard': {
    total_vulnerabilities: 0,
    affected_packages: 0,
    by_severity: { critical: 0, high: 0, medium: 0, low: 0 },
    auto_blocked_count: 0,
    last_scan_at: null,
    scan_in_progress: false,
  } satisfies SecurityDashboard,
  'GET /api/v1/admin/security/vulnerabilities': { items: [], total: 0, page: 1 } satisfies SecurityVulnerabilityPage,
  'GET /api/v1/admin/security/packages': { items: [], total: 0, page: 1 } satisfies SecurityPackagePage,
  'GET /api/v1/admin/security/suggestions': { items: [], total: 0, page: 1 } satisfies SecuritySuggestionPage,
  'GET /api/v1/admin/projects': { items: [], total: 0 } satisfies ProjectListResponse,
}

export const adminApiDefaults: Record<string, JsonValue> = {
  ...existingAdminApiDefaults,
  ...canonicalAdminApiDefaults,
}
```

Implement that last object by renaming Task 1's current literal to `existingAdminApiDefaults`; do not keep a second
export. The spread order is exact: typed canonical defaults win over Task 1's temporary partial/402 values. Pro
callout tests explicitly override Projects with `402 PRO_REQUIRED` when they need that state.

Create the complete contract suite below. It loads the production `adminApi` module through the running Vite app;
the route assertion therefore observes requests emitted by the real Axios methods, not hand-written test fetches.

```ts
// web/e2e/admin-contracts.spec.ts
import type { Page, Route } from '@playwright/test'
import type {
  AccessLogQuery,
  AccessLogListResponse,
  AdminSettingsResponse,
  AdminUpstream,
  AdminUpstreamListResponse,
  AuditLogListResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  ProjectListResponse,
  SecurityPolicy,
  SecurityVulnerabilityPage,
  UpdateAdminSettingsRequest,
  UpdateAdminSettingsResponse,
  UpdateSecurityPolicyRequest,
  UpstreamMutationRequest,
} from '../src/lib/adminApi.types'
import { expect, test } from './fixtures/admin-api'

type AdminOperation =
  | 'getSettings' | 'updateSettings'
  | 'listUpstreams' | 'createUpstream'
  | 'listVulnerabilities' | 'updateSecurityPolicy'
  | 'listLogs' | 'exportLogs' | 'listAuditLogs'
  | 'listProjects' | 'createProject' | 'listProjectPackages'

interface ExpectedCall<T> {
  operation: AdminOperation
  args?: unknown[]
  method: 'GET' | 'POST' | 'PUT'
  path: string
  query?: Record<string, string>
  body?: unknown
  response: T
}

async function callAdminApi<T>(page: Page, operation: AdminOperation, args: unknown[]): Promise<T> {
  return page.evaluate(async ({ operation, args }) => {
    const moduleUrl = new URL('/src/lib/api.ts', window.location.origin).href
    const { adminApi } = await import(moduleUrl)
    const method = adminApi[operation] as (...input: unknown[]) => Promise<{ data: unknown }>
    return (await method(...args)).data
  }, { operation, args }) as Promise<T>
}

async function expectAdminCall<T>(page: Page, expected: ExpectedCall<T>): Promise<T> {
  let matched = false
  const handler = async (route: Route) => {
    const request = route.request()
    const url = new URL(request.url())
    expect(request.method()).toBe(expected.method)
    expect(url.pathname).toBe(expected.path)
    expect(Object.fromEntries([...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)))).toEqual(expected.query ?? {})
    if ('body' in expected) expect(request.postDataJSON()).toEqual(expected.body)
    else expect(request.postData()).toBeNull()
    matched = true
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(expected.response) })
  }
  await page.route('**/api/v1/**', handler)
  try {
    const result = await callAdminApi<T>(page, expected.operation, expected.args ?? [])
    expect(matched).toBe(true)
    expect(result).toEqual(expected.response)
    return result
  } finally {
    await page.unroute('**/api/v1/**', handler)
  }
}

async function expectLogExport(
  page: Page,
  query: AccessLogQuery,
  expectedQuery: Record<string, string>,
  csv: string,
) {
  let matched = false
  const handler = async (route: Route) => {
    const request = route.request()
    const url = new URL(request.url())
    expect(request.method()).toBe('GET')
    expect(url.pathname).toBe('/api/v1/admin/logs/export')
    expect(Object.fromEntries([...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)))).toEqual(expectedQuery)
    expect(request.postData()).toBeNull()
    matched = true
    await route.fulfill({ status: 200, contentType: 'text/csv', body: csv })
  }
  await page.route('**/api/v1/**', handler)
  try {
    const result = await page.evaluate(async ({ operation, args }) => {
      const moduleUrl = new URL('/src/lib/api.ts', window.location.origin).href
      const { adminApi } = await import(moduleUrl)
      const method = adminApi[operation] as (...input: unknown[]) => Promise<{ data: Blob }>
      const blob = (await method(...args)).data
      return { type: blob.type, text: await blob.text() }
    }, { operation: 'exportLogs' as const, args: [query] })
    expect(matched).toBe(true)
    expect(result).toEqual({ type: 'text/csv', text: csv })
  } finally {
    await page.unroute('**/api/v1/**', handler)
  }
}

const settings: AdminSettingsResponse = {
  configured: {
    server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
    database: { driver: 'sqlite' },
    storage: { type: 'local', path: './data/cache' },
    cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
    auth: { token_ttl: '168h' },
  },
  effective: {
    server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
    database: { driver: 'sqlite' },
    storage: { type: 'local', path: './data/cache' },
    cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '72h', lru_threshold: 90 },
    auth: { token_ttl: '168h' },
  },
  pending_restart: ['cache.ttl_blob'],
  overrides: {},
  sources: {
    'server.host': 'file', 'server.port': 'file', 'server.log_level': 'file',
    'database.driver': 'file', 'storage.type': 'file', 'storage.path': 'file',
    'cache.max_size_gb': 'file', 'cache.ttl_index': 'file', 'cache.ttl_blob': 'file',
    'cache.lru_threshold': 'file', 'auth.token_ttl': 'file',
  },
  editable: ['server.log_level', 'cache.max_size_gb', 'cache.ttl_index', 'cache.ttl_blob', 'cache.lru_threshold', 'auth.token_ttl'],
  config_writable: true,
}

const settingsPatch = { cache: { ttl_blob: '120h' } } satisfies UpdateAdminSettingsRequest
const settingsUpdate = {
  ...settings,
  configured: { ...settings.configured, cache: { ...settings.configured.cache, ttl_blob: '120h' } },
  pending_restart: ['cache.ttl_blob'],
  changed: ['cache.ttl_blob'],
  applied_now: [],
  restart_required: ['cache.ttl_blob'],
  blocked_by_override: [],
} satisfies UpdateAdminSettingsResponse

const upstream = {
  id: 7, adapter_type: 'pypi', name: 'primary', url: 'https://pypi.example/simple', proxy: '',
  priority: 1, probe_mode: 'active', probe_interval: '30m', healthy: true, avg_latency_ms: 18,
  success_rate: 0.99, last_checked_at: '2026-07-10T00:00:00Z', worker_running: true,
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
} satisfies AdminUpstream
const upstreamList = { items: [upstream], total: 1 } satisfies AdminUpstreamListResponse
const upstreamCreate = {
  adapter_type: 'pypi', name: 'secondary', url: 'https://backup.example/simple', proxy: '', priority: 2,
  probe_mode: 'passive', probe_interval: '30m',
} satisfies UpstreamMutationRequest
const createdUpstream = { ...upstream, id: 8, ...upstreamCreate } satisfies AdminUpstream

const vulnerabilities = {
  items: [{
    id: 11, osv_id: 'GHSA-fixture', ecosystem: 'pypi', package_name: 'requests', affected_ranges: '<2.32.0',
    severity: 'high', cvss_score: 8.1, summary: 'fixture summary', details: 'fixture details', aliases: 'CVE-2026-0001',
    references: 'https://osv.dev/GHSA-fixture', published_at: '2026-07-01T00:00:00Z',
    modified_at: '2026-07-02T00:00:00Z', created_at: '2026-07-03T00:00:00Z', updated_at: '2026-07-04T00:00:00Z',
  }],
  total: 1,
  page: 2,
} satisfies SecurityVulnerabilityPage
const policyRequest = { auto_block_enabled: true, min_cvss_score: 8.5 } satisfies UpdateSecurityPolicyRequest
const policy = {
  id: 3, ecosystem: 'pypi', ...policyRequest, created_by: 'admin',
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
} satisfies SecurityPolicy

const logs = {
  items: [{
    id: 21, adapter_type: 'pypi', method: 'GET', cache_key: 'pypi/requests', package_name: 'requests', hit: true,
    upstream: 'primary', latency_ms: 12, status_code: 200, client_ip: '127.0.0.1', bytes_sent: 1024,
    created_at: '2026-07-10T00:00:00Z',
  }],
  total: 1, page: 2, page_size: 50,
} satisfies AccessLogListResponse
const logQuery = { page: 2, page_size: 50, search: 'requests', adapter_type: 'pypi', hit: true } satisfies AccessLogQuery
const expectedLogQuery = { adapter_type: 'pypi', hit: 'true', page: '2', page_size: '50', search: 'requests' }
const logsCSV = 'Time,Method,Ecosystem,Package,Hit,Status,Latency(ms),Bytes,Upstream,Client IP,Cache Key\n2026-07-10T00:00:00Z,GET,pypi,requests,true,200,12,1024,primary,127.0.0.1,pypi/requests\n'

const audit = {
  items: [{
    id: 31, ecosystem: 'pypi', package_name: 'requests', version: '2.32.0', action: 'proxy', cache_result: 'hit',
    client_ip: '127.0.0.1', user_agent: 'fixture-agent', upstream_url: 'https://pypi.example/simple',
    latency_ms: 12, bytes_sent: 1024, status_code: 200, created_at: '2026-07-10T00:00:00Z',
  }],
  total: 1, page: 3,
} satisfies AuditLogListResponse

const projects = {
  items: [{
    id: 41, name: 'Fixture', slug: 'fixture', description: 'contract fixture', package_count: 2,
    last_activity_at: '2026-07-10T00:00:00Z', created_at: '2026-07-01T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
  }],
  total: 1,
} satisfies ProjectListResponse
const projectRequest = { name: 'Created Fixture', description: 'created by contract test' } satisfies CreateProjectRequest
const createdProject = {
  id: 42, name: projectRequest.name, slug: 'created-fixture', description: projectRequest.description,
  token: 'project-token-once', proxy_url: '/p/created-fixture', created_at: '2026-07-10T00:00:00Z',
} satisfies CreateProjectResponse
const projectPackageQuery = { page: 2, per_page: 25, ecosystem: 'pypi', search: 'requests' } satisfies ProjectPackageQuery
const projectPackages = {
  items: [{
    ecosystem: 'pypi', package_name: 'requests', version: '2.32.0',
    first_seen_at: '2026-07-01T00:00:00Z', last_seen_at: '2026-07-10T00:00:00Z', download_count: 17,
  }],
  total: 1,
  page: 2,
} satisfies ProjectPackagesResponse

test.beforeEach(async ({ page }) => {
  await page.goto('/admin/login')
})

test('Settings client emits exact GET and PUT contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'getSettings', method: 'GET', path: '/api/v1/admin/settings', response: settings })
  await expectAdminCall(page, { operation: 'updateSettings', args: [settingsPatch], method: 'PUT', path: '/api/v1/admin/settings', body: settingsPatch, response: settingsUpdate })
})

test('Upstreams client emits exact list and create contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'listUpstreams', method: 'GET', path: '/api/v1/admin/upstreams', response: upstreamList })
  await expectAdminCall(page, { operation: 'createUpstream', args: [upstreamCreate], method: 'POST', path: '/api/v1/admin/upstreams', body: upstreamCreate, response: createdUpstream })
})

test('Security client uses package/per_page and canonical policy fields', async ({ page }) => {
  const query = { page: 2, per_page: 25, ecosystem: 'pypi', severity: 'high', package: 'requests' } as const
  await expectAdminCall(page, {
    operation: 'listVulnerabilities', args: [query], method: 'GET', path: '/api/v1/admin/security/vulnerabilities',
    query: { ecosystem: 'pypi', package: 'requests', page: '2', per_page: '25', severity: 'high' }, response: vulnerabilities,
  })
  await expectAdminCall(page, {
    operation: 'updateSecurityPolicy', args: ['pypi', policyRequest], method: 'PUT',
    path: '/api/v1/admin/security/policies/pypi', body: policyRequest, response: policy,
  })
})

test('Logs client emits canonical pagination and filters', async ({ page }) => {
  await expectAdminCall(page, {
    operation: 'listLogs', args: [logQuery], method: 'GET', path: '/api/v1/admin/logs',
    query: expectedLogQuery, response: logs,
  })
  await expectLogExport(page, logQuery, expectedLogQuery, logsCSV)
})

test('Audit client emits package rather than legacy search', async ({ page }) => {
  const query = {
    page: 3, page_size: 50, ecosystem: 'pypi', package: 'requests', ip: '127.0.0.1', result: 'hit',
    start: '2026-07-01T00:00:00Z', end: '2026-07-10T00:00:00Z',
  } as const
  await expectAdminCall(page, {
    operation: 'listAuditLogs', args: [query], method: 'GET', path: '/api/v1/admin/audit-logs',
    query: {
      ecosystem: 'pypi', end: '2026-07-10T00:00:00Z', ip: '127.0.0.1', package: 'requests', page: '3',
      page_size: '50', result: 'hit', start: '2026-07-01T00:00:00Z',
    },
    response: audit,
  })
})

test('Projects client emits exact list and create contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'listProjects', method: 'GET', path: '/api/v1/admin/projects', response: projects })
  await expectAdminCall(page, { operation: 'createProject', args: [projectRequest], method: 'POST', path: '/api/v1/admin/projects', body: projectRequest, response: createdProject })
  await expectAdminCall(page, {
    operation: 'listProjectPackages', args: [41, projectPackageQuery], method: 'GET',
    path: '/api/v1/admin/projects/41/packages',
    query: { ecosystem: 'pypi', page: '2', per_page: '25', search: 'requests' },
    response: projectPackages,
  })
})
```

Run: `cd web && npm run type-check:e2e && npm run test:e2e -- admin-contracts.spec.ts`

Expected: PASS; six tests execute the real Axios client, every JSON fixture satisfies its canonical DTO, Logs list
and CSV export share identical filters, Project packages use only canonical package/timestamp/count fields, and any
method, path, query name, body field, response field, CSV Blob, or `/p/{slug}` proxy fallback drift fails.

- [ ] **Step 2: Add the axe route matrix**

Create `web/e2e/admin-axe.spec.ts` with the shared runner and both exact loops:

```ts
import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import {
  expect,
  expectResolvedUiPreferences,
  setUiPreferences,
  test,
  type UiLocale,
  type UiTheme,
} from './fixtures/admin-api'

const routes = ['/admin', '/admin/bandwidth', '/admin/logs', '/admin/audit', '/admin/quarantine', '/admin/cache', '/admin/upstreams', '/admin/users', '/admin/license', '/admin/rules', '/admin/security', '/admin/projects', '/admin/settings'] as const
const themes = ['light', 'dark'] as const satisfies readonly UiTheme[]
const locales = ['zh', 'en'] as const satisfies readonly UiLocale[]

async function assertRouteMatrix(page: Page, route: string, width: number, theme: UiTheme, locale: UiLocale) {
  await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 })
  await setUiPreferences(page, theme, locale)
  await page.goto(route)
  await expectResolvedUiPreferences(page, theme, locale)
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
}

for (const width of [390, 1440]) for (const theme of themes) for (const locale of locales) {
  for (const route of routes) {
    test(`${route} ${width} ${theme} ${locale} passes axe`, async ({ page }) => {
      await assertRouteMatrix(page, route, width, theme, locale)
    })
  }
}

const targetedRoutes = ['/admin/settings', '/admin/bandwidth', '/admin/cache', '/admin/logs', '/admin/security', '/admin/quarantine'] as const
for (const width of [320, 768, 1024]) for (const theme of themes) for (const locale of locales) {
  for (const route of targetedRoutes) {
    test(`${route} targeted ${width} ${theme} ${locale} passes axe`, async ({ page }) => {
      await assertRouteMatrix(page, route, width, theme, locale)
    })
  }
}
```

- [ ] **Step 3: Add structural visual assertions and the 1920px cap regression**

Before the Axe scan in `assertRouteMatrix`, add these exact assertions; `width` is the helper parameter:

```ts
expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
expect(await page.locator('button:visible').evaluateAll(buttons => buttons.filter(button => {
  const rect = button.getBoundingClientRect()
  return button.dataset.iconButton === '' && (rect.width < 40 || rect.height < 40)
}).length)).toBe(0)
expect(await page.locator('#root *:visible').evaluateAll(elements => elements.filter(element => {
  const spacing = getComputedStyle(element).letterSpacing
  return spacing !== 'normal' && Math.abs(Number.parseFloat(spacing)) > 0.01
}).length)).toBe(0)
```

Append this independent wide-screen test to `web/e2e/admin-visual-matrix.spec.ts`:

```ts
import { expect, expectResolvedUiPreferences, setUiPreferences, test } from './fixtures/admin-api'

for (const width of [1920, 2560]) {
  test(`Admin outlet is centered in main and capped at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1080 })
    await setUiPreferences(page, 'light', 'en')
    await page.goto('/admin')
    await expectResolvedUiPreferences(page, 'light', 'en')
    const metrics = await page.evaluate(() => {
      const main = document.querySelector<HTMLElement>('[data-admin-main]')
      const outlet = document.querySelector<HTMLElement>('[data-admin-outlet]')
      if (!main || !outlet) throw new Error('admin geometry hooks missing')
      const mainRect = main.getBoundingClientRect()
      const outletRect = outlet.getBoundingClientRect()
      return {
        mainLeft: mainRect.left,
        mainWidth: mainRect.width,
        outletLeft: outletRect.left,
        outletWidth: outletRect.width,
      }
    })
    expect(metrics.outletWidth).toBeLessThanOrEqual(1840)
    expect(Math.abs(
      (metrics.outletLeft - metrics.mainLeft) - (metrics.mainWidth - metrics.outletWidth) / 2,
    )).toBeLessThanOrEqual(1)
    if (width === 2560) expect(metrics.outletWidth).toBeCloseTo(1840, 0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  })
}
```

Keep Playwright's `screenshot: 'only-on-failure'`. Do not create or commit full-page pixel snapshots.

- [ ] **Step 4: Add Portal token regressions**

Append this loop to `web/e2e/admin-visual-matrix.spec.ts`:

```ts
import AxeBuilder from '@axe-core/playwright'

for (const route of ['/', '/monitor']) for (const width of [390, 1440]) for (const theme of ['light', 'dark'] as const) {
  test(`Portal ${route} ${width} ${theme} has no token regression`, async ({ page }) => {
    const consoleErrors: string[] = []
    page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
    await setUiPreferences(page, theme, 'zh')
    await page.goto(route)
    await expectResolvedUiPreferences(page, theme, 'zh')
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
    expect(consoleErrors).toEqual([])
  })
}
```

- [ ] **Step 5: Run the complete frontend gate**

Run:

```bash
cd web
npm run type-check
npm run type-check:e2e
npm run build
npm run test:e2e
xargs -r npx eslint < admin-remediation-eslint-files.txt
cd ..
python3 scripts/i18n-audit.py
```

Expected: source and E2E type-check, build, Playwright, i18n, and the exact manifest-driven ESLint scope all PASS.
The lint command never discovers paths from Git history and therefore cannot include unrelated pre-existing Portal
changes.

- [ ] **Step 6: Synchronize the design system documentation**

Update `DESIGN.md` with the new shared primitives, exact breakpoints, Settings/Upstream control-plane states,
query state contract, 40x40 icon target rule, and Playwright verification command. Remove statements contradicted
by the implemented components.

- [ ] **Step 7: Commit final UI verification and documentation**

```bash
git add web/package.json web/tsconfig.e2e.json web/admin-remediation-eslint-files.txt web/e2e/fixtures/admin-api.ts web/e2e/admin-contracts.spec.ts web/e2e/admin-axe.spec.ts web/e2e/admin-visual-matrix.spec.ts web/playwright.config.ts .github/workflows/ci.yml
git add -p -- DESIGN.md
git commit -m "test(admin): enforce accessibility and responsive matrix"
```

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-07-10-admin-remediation-04-ui-system.md`. Two execution options:

1. **Subagent-Driven (recommended)** - Dispatch a fresh subagent per task, then run specification and quality review
   gates between tasks with `superpowers:subagent-driven-development`.
2. **Inline Execution** - Execute tasks in this session in ordered batches with review checkpoints using
   `superpowers:executing-plans`.
