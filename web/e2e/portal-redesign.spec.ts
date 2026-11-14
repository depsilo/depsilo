import AxeBuilder from '@axe-core/playwright'
import type { Page, Request } from '@playwright/test'
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

async function openManager(page: Page, ecosystem: RegExp, manager: string) {
  const primary = page.locator('[data-quickstart-primary]')
  await primary.getByRole('button', { name: ecosystem }).first().click()
  await primary.getByRole('group', { name: 'Package manager' })
    .getByRole('button', { name: manager, exact: true }).click()
}

test('Quick Start opens on AI setup and lists every Python manager after it', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')
  const origin = new URL(page.url()).origin

  const primary = page.locator('[data-quickstart-primary]')
  await expect(primary).toBeVisible()
  await expect(page.locator('[data-quickstart-optional]')).toHaveCount(0)

  // A first visit opens the AI hand-off: the catalog's first entry.
  const ai = primary.locator('[data-catalog-entry="ai"]')
  await expect(ai).toHaveAttribute('aria-pressed', 'true')
  await expect(primary.getByRole('heading', { name: 'Set up with AI' })).toBeVisible()
  await expect(primary.locator('pre').first()).toContainText(`${origin}/api/v1/integration-prompt `)

  await primary.getByRole('button', { name: /^Python\b/ }).click()
  await expect(primary.getByRole('heading', { name: 'Configure Python' })).toBeVisible()
  const managerPicker = primary.getByRole('group', { name: 'Package manager' })
  await expect(managerPicker.getByRole('button')).toHaveCount(8)
  await expect(managerPicker.getByRole('button').first()).toHaveText('AI setup')
  await expect(managerPicker.getByRole('button', { name: 'AI setup' })).toHaveAttribute('aria-pressed', 'true')
  await expect(managerPicker).toContainText('7 managers')
  for (const manager of ['pip', 'uv', 'venv', 'Poetry', 'Pipenv', 'PDM', 'Conda']) {
    await expect(managerPicker.getByRole('button', { name: manager, exact: true })).toBeVisible()
  }

  await managerPicker.getByRole('button', { name: 'pip', exact: true }).click()
  await expect(primary).toContainText('~/.config/pip/pip.conf')
  // Every Portal code block is flat ink: a hairline, no drop shadow.
  const blocks = primary.locator('.code-block')
  await expect(blocks).not.toHaveCount(0)
  await expect(primary.locator('.code-block:not([data-code-flat="true"])')).toHaveCount(0)
  await expect(blocks.first()).toHaveCSS('box-shadow', 'none')
})

test('an ecosystem AI tab hands over only that ecosystem', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  const requested: (string | null)[] = []
  await mockAdminApi(page, {
    'GET /api/v1/integration-prompt': (request: Request) => {
      const ecosystem = new URL(request.url()).searchParams.get('ecosystem')
      requested.push(ecosystem)
      return { status: 200, body: `## Scope — ${ecosystem ?? 'all'}\nline two`, contentType: 'text/plain; charset=utf-8', serialize: 'text' }
    },
  })
  await page.goto('/')
  const origin = new URL(page.url()).origin

  const primary = page.locator('[data-quickstart-primary]')
  await primary.getByRole('button', { name: /^Node\.js\b/ }).click()
  const pane = primary.locator('[data-quickstart-pane="node"]')
  await expect(pane.getByRole('button', { name: 'AI setup' })).toHaveAttribute('aria-pressed', 'true')
  await expect(pane.locator('[data-manager-description]')).toContainText('only this project\'s Node.js dependency settings')
  await expect(pane.locator('pre').first()).toContainText(`${origin}/api/v1/integration-prompt?ecosystem=node `)
  await expect(pane.locator('pre').first()).toContainText('route only this project\'s Node.js dependency installs')

  const full = pane.locator('[data-quickstart-full-prompt]')
  await expect(full.locator('summary')).toContainText('Show the full prompt · 2 lines')
  await full.locator('summary').click()
  await expect(full.locator('pre')).toContainText('## Scope — node')
  expect(requested).toContain('node')
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
})

test('the status band names failing mirrors and opens their ecosystem', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      ...populatedStats,
      service: { status: 'degraded', version: 'v0.9.3', uptime_seconds: 90_000 },
      today: { total_requests: 22, hit_count: 7, miss_count: 15, hit_rate: 0.318, bytes_served: 834_666_496, bytes_saved: 0 },
      series: { interval_minutes: 5, points: [] },
      upstreams: [
        ...populatedStats.upstreams,
        { id: 102, name: 'ruby-china', adapter: 'rubygems', url: 'https://gems.ruby-china.com', healthy: false, avg_latency_ms: 0, success_rate: 0 },
      ],
    },
  })
  await page.goto('/')

  const band = page.getByRole('region', { name: 'Service status' })
  // A degraded mirror does not take the service down.
  await expect(band.getByRole('status')).toHaveText('Serving')
  await expect(band).toContainText('Up 1d 1h')
  await expect(band).toContainText('Upstream mirrors · 2 total')
  await expect(band).toContainText('1 failing')
  // An idle hour is said in words, not drawn as an empty chart.
  await expect(band.locator('[data-status-band-hour]')).toHaveAttribute('data-status-band-hour', 'idle')
  await expect(band).toContainText('No requests')

  await band.getByRole('button', { name: 'Ruby · ruby-china is failing; open the Ruby configuration' }).click()
  await expect(page.locator('[data-quickstart-primary]').getByRole('heading', { name: 'Configure Ruby' })).toBeVisible()
})

test('the compile cache is a catalog entry with its own client steps', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')
  const origin = new URL(page.url()).origin

  const primary = page.locator('[data-quickstart-primary]')
  await primary.getByRole('region', { name: 'Optional' }).getByRole('button', { name: /^Compile cache\b/ }).click()
  const pane = primary.locator('[data-quickstart-pane="compile-cache"]')
  await expect(pane.getByRole('heading', { name: 'Configure the compile cache' })).toBeVisible()
  await expect(pane.locator('pre').first()).toContainText(`${origin}/ccache/v1/<namespace>|bearer-token=<build-credential>`)
  await pane.getByRole('group', { name: 'Client' }).getByRole('button', { name: 'sccache' }).click()
  await expect(pane.locator('pre').first()).toContainText(`endpoint = "${origin}/sccache/v1/<namespace>"`)
  await pane.locator('details summary').click()
  await expect(pane).toContainText('not sccache-dist')
  await expect(pane).toContainText('Single-instance only')
  await expect(pane.getByRole('link', { name: 'Open cache workspace' })).toHaveAttribute('href', '/admin/compile-cache')
  await expectNoDocumentOverflow(page)
})

test('Python setup explains the channel-aware PyTorch cache route', async ({ page, context }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')
  await openManager(page, /^Python\b/, 'pip')

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
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'healthy', version: 'dev' },
      week: {},
      upstreams: [],
      extra_indexes: [],
    },
  })
  await page.goto('/')
  await openManager(page, /^Python\b/, 'pip')
  await expect(page.locator('[data-pytorch-index]')).toHaveCount(0)
})

test('all ecosystems are visible by default and selection persists', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const catalog = primary.getByRole('navigation', { name: 'Choose ecosystem' })
  // AI setup, the 14 ecosystems, and the compile cache.
  await expect(catalog.locator('[data-catalog-entry]')).toHaveCount(16)
  await expect(catalog.locator('[data-catalog-entry]').first()).toHaveAttribute('data-catalog-entry', 'ai')
  await expect(catalog.getByRole('button', { name: /^Java\b/ })).toBeVisible()
  await expect(catalog.getByRole('button', { name: /^Hugging Face\b/ })).toBeVisible()
  await expect(catalog.getByRole('region', { name: 'Recently used' })).toHaveCount(0)

  const search = catalog.getByRole('searchbox', { name: 'Search all ecosystems' })
  await expect(search).toHaveAttribute('placeholder', 'Search ecosystems or tools')
  await search.fill('Hugging')
  await expect(catalog.getByRole('button', { name: /^Hugging Face\b/ })).toBeVisible()
  await expect(catalog.getByRole('button', { name: /^Go\b/ })).toHaveCount(0)

  await catalog.getByRole('button', { name: /^Hugging Face\b/ }).click()
  await expect(primary.getByRole('heading', { name: 'Configure Hugging Face' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('depsilo.portal.quickstart-selection.v1')))
    .toBe('huggingface')

  await page.reload()
  await expect(primary.getByRole('heading', { name: 'Configure Hugging Face' })).toBeVisible()
})

test('Debian setup uses the signed Depsilo repository-root routes', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  await openManager(page, /^Debian\b/, 'apt')
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
  await primary.getByRole('button', { name: /^Python\b/ }).click()
  const managerPicker = primary.getByRole('group', { name: 'Package manager' })
  const pip = managerPicker.getByRole('button', { name: 'pip', exact: true })
  const uv = managerPicker.getByRole('button', { name: 'uv', exact: true })

  await uv.click()
  await expect(uv).toHaveAttribute('aria-pressed', 'true')
  await expect(pip).toHaveAttribute('aria-pressed', 'false')
  await expect(managerPicker.locator('[data-manager-description]')).toContainText(
    'Fast Python dependency resolver',
  )
  await expect(primary.locator('.code-block').first()).toContainText('pyproject.toml')

  // The three step numbers share one column, including the disclosure step.
  const numbers = await primary.locator('[data-quickstart-step-number]').evaluateAll(
    elements => elements.map(element => Math.round(element.getBoundingClientRect().left)),
  )
  expect(numbers.length).toBeGreaterThanOrEqual(3)
  expect(new Set(numbers).size).toBe(1)

  // Another ecosystem starts on its own AI tab again.
  await primary.getByRole('button', { name: /^Debian\b/ }).click()
  await primary.getByRole('button', { name: /^Python\b/ }).click()
  await expect(managerPicker.getByRole('button', { name: 'AI setup' })).toHaveAttribute('aria-pressed', 'true')
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

test('Quick Start keeps the ecosystem directory to one rail on phones', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const rail = primary.locator('[data-eco-rail]')

  await expect(rail).toBeVisible()
  await expect(rail.getByRole('button')).toHaveCount(16)
  await expect(rail.getByRole('button').first()).toHaveAttribute('data-catalog-entry', 'ai')
  // Stacked, the directory used to run ~560px and pushed the configuration
  // past the first screen. As a rail it overflows sideways instead.
  expect(await rail.evaluate(rail => rail.scrollWidth > rail.clientWidth)).toBe(true)

  const offsets = await primary.evaluate(primary => {
    const catalog = primary.querySelector('.eco-catalog')
    const pane = primary.querySelector('[data-quickstart-shell] > *:nth-child(2)')
    if (!catalog || !pane) return null
    return {
      catalogHeight: catalog.getBoundingClientRect().height,
      paneOffset: pane.getBoundingClientRect().top - catalog.getBoundingClientRect().top,
    }
  })
  expect(offsets).not.toBeNull()
  expect(offsets?.catalogHeight).toBeLessThan(260)
  expect(offsets?.paneOffset).toBeLessThan(300)

  await expectNoDocumentOverflow(page)
})

test('Portal header and page content share a left edge on wide displays', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': populatedStats,
    'GET /api/v1/latency-series': latencySeries,
  })

  // Quick Start used to cap itself at 1440px inside a shell that kept growing
  // with 92vw, so above ~1635px its heading sat ~131px right of the brand.
  for (const route of ['/', '/monitor']) {
    for (const width of [1440, 1920, 2560]) {
      await page.setViewportSize({ width, height: 1000 })
      await page.goto(route)
      await expect(page.locator('h1')).toBeVisible()

      const edges = await page.evaluate(() => {
        const brand = document.querySelector('.portal-header-brand')
        const title = document.querySelector('h1')
        if (!brand || !title) return null
        return {
          brand: brand.getBoundingClientRect().left,
          title: title.getBoundingClientRect().left,
        }
      })
      expect(edges).not.toBeNull()
      expect(Math.abs((edges?.brand ?? 0) - (edges?.title ?? 0))).toBeLessThanOrEqual(1)
    }
  }
})

test('Portal and Admin share the brand accent; only the Portal tints its hover surface', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/')

  await expect(page.locator('.portal-admin-link')).toHaveCSS(
    'background-color',
    'rgb(43, 95, 218)',
  )
  const tokens = () => page.evaluate(() => {
    const style = getComputedStyle(document.documentElement)
    return { brand: style.getPropertyValue('--brand').trim(), accent: style.getPropertyValue('--accent').trim() }
  })
  expect(await tokens()).toEqual({ brand: '#2b5fda', accent: '#e2ebfd' })

  // Admin keeps the same Deep Blue interaction accent but neutral hovers, and
  // the Portal surface marker must be gone as soon as the Portal unmounts.
  await page.goto('/admin/upstreams')
  await expect.poll(() => page.evaluate(() => (
    document.documentElement.getAttribute('data-surface')
  ))).toBeNull()
  const admin = await tokens()
  expect(admin.brand).toBe('#2b5fda')
  expect(admin.accent).not.toBe('#e2ebfd')
})

test('Portal header stays pinned while the page scrolls', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    // Enough upstreams that Monitor scrolls at a short viewport.
    'GET /api/v1/stats': {
      ...populatedStats,
      upstreams: ['pypi', 'npm', 'crates'].flatMap((adapter, group) => [
        { id: group * 2 + 1, name: `${adapter}-one`, adapter, url: `https://${adapter}1.example`, healthy: true, avg_latency_ms: 40, success_rate: 1 },
        { id: group * 2 + 2, name: `${adapter}-two`, adapter, url: `https://${adapter}2.example`, healthy: false, avg_latency_ms: 0, success_rate: 0 },
      ]),
    },
    'GET /api/v1/latency-series': {},
  })
  await page.setViewportSize({ width: 1440, height: 380 })

  for (const route of ['/', '/monitor']) {
    await page.goto(route)
    await expect(page.locator('h1')).toBeVisible()
    await page.evaluate(() => window.scrollTo(0, 600))
    await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0)

    // A sticky header keeps its box at the top of the viewport. It silently
    // stopped doing that once, because an `overflow-x: hidden` ancestor became
    // the scrollport instead of the viewport.
    await expect.poll(() => page.evaluate(() => (
      Math.round(document.querySelector('header')?.getBoundingClientRect().top ?? -1)
    ))).toBe(0)

    // ...and it has to paint above the content it is covering.
    expect(await page.evaluate(() => {
      const header = document.querySelector('header')
      if (!header) return false
      const box = header.getBoundingClientRect()
      const under = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2)
      return under !== null && header.contains(under)
    })).toBe(true)
  }
})
