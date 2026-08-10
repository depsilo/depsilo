import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const populatedStats = {
  service: { status: 'healthy', version: 'v0.9.3' },
  extra_indexes: [{ name: 'pytorch', kind: 'pytorch', path: 'pypi-torch' }],
  week: {
    total_requests: 120,
    hit_count: 96,
    hit_rate: 0.8,
    bytes_saved: 4096,
  },
  upstreams: [
    {
      id: 101,
      name: 'PyPI primary',
      adapter: 'pypi',
      url: 'https://pypi.org/simple',
      healthy: true,
      avg_latency_ms: 84,
      success_rate: 1,
    },
  ],
}

const latencySeries = {
  '101': [
    {
      time: '2026-07-28T08:00:00.000Z',
      latency_ms: 101,
      healthy: true,
      requests: 4,
    },
    {
      time: '2026-07-28T08:30:00.000Z',
      latency_ms: 202,
      healthy: true,
      requests: 4,
    },
    {
      time: '2026-07-28T09:00:00.000Z',
      latency_ms: 303,
      healthy: true,
      requests: 4,
    },
  ],
}

async function expectNoDocumentOverflow(page: Page) {
  await expect.poll(() => page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    content: document.documentElement.scrollWidth,
  }))).toEqual(await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    content: document.documentElement.clientWidth,
  })))
}

test('Quick Start is compact and shows every Python manager', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const optional = page.locator('[data-quickstart-optional]')

  await expect(primary).toBeVisible()
  await expect(optional).toBeVisible()
  await expect(page.locator('ol[aria-label="Package setup path"]')).toHaveCount(0)
  await expect(primary.getByRole('heading', { name: 'Configure Python' })).toBeVisible()

  const managerPicker = primary.getByRole('group', { name: 'Package manager' })
  const pipManager = managerPicker.getByRole('button', { name: /^pip\b/i })
  await expect(pipManager).toHaveAttribute('aria-pressed', 'true')
  await expect(managerPicker.getByRole('button')).toHaveCount(7)
  for (const manager of [/^uv\b/i, /^venv\b/i, /^Poetry\b/i, /^Pipenv\b/i, /^PDM\b/i, /^Conda\b/i]) {
    await expect(managerPicker.getByRole('button', { name: manager })).toBeVisible()
  }
  await expect(managerPicker).not.toContainText('Recommended')
  await expect(primary.getByRole('button', { name: /alternatives/i })).toHaveCount(0)
  await expect(primary).toContainText('~/.config/pip/pip.conf')
  await expect(primary.locator('[data-code-tone="ink"]')).toHaveCount(1)
  await expect(primary.getByRole('link', { name: 'Open Monitor' })).toHaveCount(0)
  await expect(primary).not.toContainText('Prompt for ChatGPT / Claude / Cursor')

  await expect(optional.getByRole('heading', {
    name: 'Let your AI coding agent configure a project',
  })).toBeVisible()
  await expect(optional.getByRole('button', { name: /Copy the full prompt/i })).toBeVisible()
  await expect(optional.getByRole('article', {
    name: 'Remote build cache for ccache and sccache',
  })).toBeVisible()
})

test('Python setup explains the channel-aware PyTorch cache route', async ({ page, context }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const notice = primary.getByRole('complementary', {
    name: 'PyTorch build indexes',
  })
  await expect(notice).toBeVisible()
  await expect(notice).toContainText('Isolated cache')
  await expect(notice).toContainText('different PyTorch builds')

  const disclosure = notice.locator('details')
  const summary = notice.locator('summary')
  await expect(disclosure).not.toHaveAttribute('open', '')
  await summary.focus()
  await summary.press('Enter')
  await expect(disclosure).toHaveAttribute('open', '')

  const serviceOrigin = new URL(page.url()).origin
  const serviceHost = new URL(page.url()).host
  const platforms = notice.getByRole('group', { name: 'Compute platform' })
  await expect(platforms.getByRole('button')).toHaveCount(6)
  await expect(platforms.getByRole('button', { name: 'CPU', exact: true }))
    .toHaveAttribute('aria-pressed', 'true')
  await platforms.getByRole('button', { name: 'CUDA 13.0', exact: true }).click()
  await expect(platforms.getByRole('button', { name: 'CUDA 13.0', exact: true }))
    .toHaveAttribute('aria-pressed', 'true')
  await expect(notice).toContainText(`${serviceOrigin}/pypi-torch/cu130/simple/`)
  await expect(notice).toContainText('use the isolated PyTorch URL below as the primary index instead of --extra-index-url')
  const command = notice.locator('pre')
  await expect(command).toContainText('pip install torch torchvision torchaudio')
  await expect(command).toContainText(`--index-url ${serviceOrigin}/pypi-torch/cu130/simple/`)
  await expect(command).toContainText('--trusted-host')
  await expect(command).not.toContainText('--extra-index-url')

  await notice.getByRole('button', {
    name: 'Copy code for PyTorch install command for pip (cu130)',
  }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(
    `pip install torch torchvision torchaudio \\\n` +
    `  --index-url ${serviceOrigin}/pypi-torch/cu130/simple/ \\\n` +
    `  --trusted-host ${serviceHost}`,
  )

  await platforms.getByRole('button', { name: 'Other', exact: true }).click()
  const channel = notice.getByRole('textbox', { name: 'Other PyTorch channel' })
  await expect(channel).toHaveValue('')
  await expect(notice.locator('pre')).toHaveCount(0)
  await channel.fill('../cu130')
  await expect(channel).toHaveAttribute('aria-invalid', 'true')
  await expect(notice).toContainText('Enter a valid PyTorch channel')
  await expect(notice.locator('pre')).toHaveCount(0)
  await channel.fill('cu128')
  await expect(notice).toContainText(`${serviceOrigin}/pypi-torch/cu128/simple/`)
  await platforms.getByRole('button', { name: 'ROCm 7.2', exact: true }).click()
  await expect(notice.getByRole('textbox', { name: 'Other PyTorch channel' })).toHaveCount(0)
  await expect(notice).toContainText(`${serviceOrigin}/pypi-torch/rocm7.2/simple/`)
  await expectNoDocumentOverflow(page)
  expect((await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze()).violations).toEqual([])

  await platforms.getByRole('button', { name: 'CPU', exact: true }).click()
  await primary.getByRole('button', { name: 'uv', exact: true }).click()
  const uvNotice = primary.locator('[data-pytorch-index]')
  await uvNotice.locator('summary').press('Enter')
  await expect(uvNotice.locator('pre')).toContainText('uv pip install torch')
  await expect(uvNotice).toContainText(`${serviceOrigin}/pypi-torch/cpu/simple/`)

  await primary.getByRole('button', { name: 'Poetry', exact: true }).click()
  await expect(primary.locator('[data-pytorch-index]')).toHaveCount(0)
  await primary.getByRole('button', { name: /^Debian\b/ }).click()
  await expect(primary.locator('[data-pytorch-index]')).toHaveCount(0)
})

test('PyTorch guidance follows the runtime capability', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'healthy', version: 'dev' },
      week: {},
      upstreams: [],
      extra_indexes: [],
    },
  })
  await page.goto('/')
  await expect(page.locator('[data-pytorch-index]')).toHaveCount(0)
})

test('all ecosystems are visible by default and selection persists', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const allEcosystems = primary.getByRole('region', { name: 'All ecosystems' })
  await expect(allEcosystems.getByRole('button')).toHaveCount(14)
  await expect(allEcosystems.getByRole('button', { name: /^Java\b/ })).toBeVisible()
  await expect(allEcosystems.getByRole('button', { name: /^Hugging Face\b/ })).toBeVisible()
  await expect(primary.getByText('Browse all 14 ecosystems')).toHaveCount(0)
  await expect(primary.getByRole('region', { name: 'Recommended' })).toHaveCount(0)

  const search = primary.getByRole('searchbox', { name: 'Search all ecosystems' })
  await search.fill('Hugging')
  await expect(primary.getByRole('button', { name: /^Hugging Face\b/ })).toBeVisible()
  await expect(primary.getByRole('button', { name: /^Go\b/ })).toHaveCount(0)

  await primary.getByRole('button', { name: /^Hugging Face\b/ }).click()
  await expect(primary.getByRole('heading', { name: 'Configure Hugging Face' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => {
    const stored = localStorage.getItem('depsilo.portal.recent-ecosystems.v1')
    return stored === null ? null : JSON.parse(stored)
  })).toEqual(['huggingface'])

  await page.reload()
  await expect(primary.getByRole('heading', { name: 'Configure Hugging Face' })).toBeVisible()
  const recent = primary.getByRole('region', { name: 'Recently used' })
  const recentChoice = recent.getByRole('button', { name: 'Hugging Face' })
  await expect(recentChoice).toBeVisible()
})

test('Debian setup uses the signed Depsilo repository-root routes', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  await primary.getByRole('button', { name: /^Debian\b/ }).click()
  await expect(primary.getByRole('heading', { name: 'Configure Debian' })).toBeVisible()
  await expect(primary).toContainText('/apt/debian')
  await expect(primary).toContainText('/apt/debian-security')
  await expect(primary).not.toContainText('/apt/tuna')
  await expect(primary).not.toContainText('/apt-security')
})

test('manager rail switches the focused configuration without hiding choices', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const managerPicker = primary.getByRole('group', { name: 'Package manager' })
  const pip = managerPicker.getByRole('button', { name: 'pip', exact: true })
  const uv = managerPicker.getByRole('button', { name: 'uv', exact: true })

  await uv.click()
  await expect(uv).toHaveAttribute('aria-pressed', 'true')
  await expect(pip).toHaveAttribute('aria-pressed', 'false')
  await expect(managerPicker.locator('[data-manager-description]')).toContainText(
    'Fast Python dependency resolver',
  )
  const focusCode = primary.locator('[data-code-tone="ink"]')
  await expect(focusCode).toHaveCount(1)
  await expect(focusCode).toContainText('pyproject.toml')
  await expect(primary.locator('[data-code-tone="light"]')).not.toHaveCount(0)

  await primary.getByRole('button', { name: /^Debian\b/ }).click()
  await primary.getByRole('button', { name: /^Python\b/ }).click()
  await expect(managerPicker.getByRole('button', { name: 'pip', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
})

test('Quick Start passes axe at 1440px', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze()
  expect(result.violations).toEqual([])
})

test('Portal header groups service and preference controls by purpose', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
  })
  await page.goto('/')

  const service = page.locator('[data-portal-control-group="service"]')
  const preferences = page.locator('[data-portal-control-group="preferences"]')
  const endpoint = service.getByRole('button', { name: /Copy service endpoint/i })
  const status = service.getByRole('status', { name: /Online/i })
  const language = preferences.getByRole('button', { name: /Switch to Chinese/i })
  const theme = preferences.getByRole('button', { name: /current theme: Light/i })
  const admin = page.locator('.portal-admin-link')

  await expect(endpoint).toBeVisible()
  await expect(status).toBeVisible()
  await expect(language).toContainText('EN')
  await expect(theme).toContainText('Light')
  await expect(admin.locator('.portal-admin-label')).toHaveText('Admin')
})

test('Portal theme changes recolor theme-sensitive ecosystem icons immediately', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
  })
  await page.goto('/')

  const goIcon = page.getByRole('button', { name: /^Go\b/ }).locator('svg').first()
  await expect(goIcon).toHaveAttribute('fill', '#00ADD8')

  await page.locator('[data-theme-toggle="portal"]').click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(goIcon).toHaveAttribute('fill', 'currentColor')
})

test('Portal service status distinguishes loading, unknown, and recoverable failure', async ({ page }) => {
  let releaseStats: (value: unknown) => void = () => undefined
  const pendingStats = new Promise<unknown>(resolve => {
    releaseStats = resolve
  })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => pendingStats,
  })
  await page.goto('/')

  const loading = page.locator('.portal-status-pill[data-query-state="loading"]')
  await expect(loading).toHaveAttribute('role', 'status')
  await expect(loading).toHaveAccessibleName(/Checking/i)
  await expect(loading).not.toHaveAccessibleName(/unknown/i)

  releaseStats({
    ...populatedStats,
    service: { ...populatedStats.service, status: 'mystery' },
  })
  const unknown = page.locator('.portal-status-pill[data-query-state="success"]')
  await expect(unknown).toHaveAttribute('data-status', 'unknown')
  await expect(unknown).toHaveAccessibleName(/Status unknown/i)

  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => {
      calls += 1
      return calls === 1
        ? { status: 503, body: { code: 'UNAVAILABLE', message: 'stats unavailable' } }
        : populatedStats
    },
  })
  await page.reload()

  const retry = page.locator('button.portal-status-pill[data-query-state="error"]')
  await expect(retry).toHaveAccessibleName(/unavailable.*retry/i)
  await expect(retry).not.toHaveAccessibleName(/unknown/i)
  await retry.click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.locator('.portal-status-pill[data-query-state="success"]')).toHaveAccessibleName(/Online/i)
})

test('Portal header keeps every visible control in view at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
  })
  await page.goto('/')

  const brand = page.getByRole('link', { name: 'Depsilo', exact: true })
  await expect(brand).toBeVisible()
  await expect(brand.locator('.depsilo-logo-mark')).toBeVisible()
  await expect(brand.locator('.portal-brand-name')).toBeHidden()

  const navigation = page.getByRole('navigation', { name: 'Portal navigation' })
  await expect(navigation.getByRole('link', { name: 'Quick Start', exact: true })).toBeVisible()
  await expect(navigation.getByRole('link', { name: 'Monitor', exact: true })).toBeVisible()
  await expect(navigation.locator('.portal-nav-compact-label')).toHaveCount(2)
  for (const compactLabel of await navigation.locator('.portal-nav-compact-label').all()) {
    await expect(compactLabel).toBeVisible()
  }

  const status = page.locator('.portal-status-pill')
  await expect(status).toHaveAttribute('role', 'status')
  await expect(status).toHaveAttribute('aria-label', /Online/i)
  await expect(status.locator('.portal-status-label')).toBeAttached()
  await expect(status.locator('.portal-status-compact-icon')).toBeVisible()
  await expect(status.locator('.portal-status-dot')).toBeHidden()

  const theme = page.locator('[data-theme-toggle="portal"]')
  await expect(theme).toBeVisible()

  const admin = page.locator('.portal-admin-link')
  await expect(admin).toHaveAccessibleName(/Admin/i)

  const clippedControls = await page.locator(
    'header a:visible, header button:visible, header [role="status"]:visible',
  ).evaluateAll((elements) => elements.flatMap((element) => {
    const rect = element.getBoundingClientRect()
    return rect.left < 0 || rect.right > window.innerWidth + 0.5
      ? [{
          name: element.getAttribute('aria-label') || element.textContent?.trim() || element.tagName,
          left: rect.left,
          right: rect.right,
        }]
      : []
  }))
  expect(clippedControls).toEqual([])
  await expectNoDocumentOverflow(page)
})

for (const { locale, copyName, copiedText } of [
  { locale: 'en' as const, copyName: /^Copy code for /i, copiedText: 'Copied' },
  { locale: 'zh' as const, copyName: /^复制.+代码$/, copiedText: '已复制' },
]) {
  test(`CodeBlock copy announces success in ${locale}`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await setUiPreferences(page, 'dark', locale)
    await page.goto('/')

    const primary = page.locator('[data-quickstart-primary]')
    const copy = primary.getByRole('button', { name: copyName }).first()
    await expect(copy).toBeVisible()
    const copyLabels = await primary
      .getByRole('button', { name: copyName })
      .evaluateAll(buttons => buttons.map(button => button.getAttribute('aria-label')))
    expect(new Set(copyLabels).size).toBe(copyLabels.length)
    await copy.click()

    const announcement = primary.locator('[role="status"], [aria-live]').filter({
      hasText: copiedText,
    })
    await expect(announcement).toHaveAttribute('aria-live', 'polite')
    await expect(announcement).toContainText(copiedText)
  })
}

test('Monitor heartbeat detail works with keyboard and tap', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
    'GET /api/v1/latency-series': latencySeries,
  })
  await page.goto('/monitor')

  const heartbeat = page.locator('[data-upstream-heartbeat]')
  await expect(heartbeat).toHaveAttribute('tabindex', '0')
  await heartbeat.focus()

  const detail = heartbeat.locator('[data-heartbeat-detail]')
  await expect(detail).toBeVisible()
  await expect(detail).toContainText('303ms')

  await heartbeat.press('Home')
  await expect(detail).toContainText('101ms')
  await heartbeat.press('ArrowRight')
  await expect(detail).toContainText('202ms')
  await heartbeat.press('Escape')
  await expect(detail).toBeHidden()

  await heartbeat.locator('[data-heartbeat-beat]').last().click()
  await expect(detail).toBeVisible()
  await expect(detail).toContainText('303ms')
  await expectNoDocumentOverflow(page)
})

test('Portal routes avoid document overflow at the narrowest supported width', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
    'GET /api/v1/latency-series': latencySeries,
  })

  for (const route of ['/', '/monitor']) {
    await page.goto(route)
    await expectNoDocumentOverflow(page)
  }
})
