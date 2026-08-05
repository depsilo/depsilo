import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'
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
  'PyPI primary': [
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

async function expectBefore(first: Locator, second: Locator) {
  await expect(first).toBeVisible()
  await expect(second).toBeVisible()
  const [firstBox, secondBox] = await Promise.all([
    first.boundingBox(),
    second.boundingBox(),
  ])
  expect(firstBox?.y).toBeLessThan(secondBox?.y ?? 0)
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

  await expectBefore(primary, optional)
  const primaryBox = await primary.boundingBox()
  expect(primaryBox?.y).toBeLessThan(260)
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
  const recentBox = await recentChoice.boundingBox()
  expect(recentBox?.height).toBeGreaterThanOrEqual(40)
  expect(recentBox?.height).toBeLessThanOrEqual(42)
})

test('recent ecosystem shortcuts keep three choices on one compact row', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.addInitScript(() => {
    localStorage.setItem(
      'depsilo.portal.recent-ecosystems.v1',
      JSON.stringify(['python', 'kubernetes', 'huggingface']),
    )
  })
  await page.goto('/')

  const recent = page.getByRole('region', { name: 'Recently used' })
  const choices = recent.getByRole('button')
  await expect(choices).toHaveCount(3)
  const boxes = await choices.evaluateAll(buttons => buttons.map(button => {
    const box = button.getBoundingClientRect()
    return { top: Math.round(box.top), height: Math.round(box.height) }
  }))
  expect(new Set(boxes.map(box => box.top)).size).toBe(1)
  expect(boxes.every(box => box.height >= 40 && box.height <= 42)).toBe(true)
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
  await expect(focusCode).toHaveCSS('background-color', 'rgb(16, 21, 18)')
  await expect(primary.locator('[data-code-tone="light"]')).not.toHaveCount(0)

  await primary.getByRole('button', { name: /^Debian\b/ }).click()
  await primary.getByRole('button', { name: /^Python\b/ }).click()
  await expect(managerPicker.getByRole('button', { name: 'pip', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
})

test('Quick Start keeps a centered task width on wide displays', async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1000 })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  const workbench = page.locator('[data-quickstart-shell]')
  const box = await workbench.boundingBox()
  expect(box?.width).toBeLessThanOrEqual(1440)
  expect(Math.abs((box?.x ?? 0) - (1920 - (box?.width ?? 0)) / 2)).toBeLessThanOrEqual(1)
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
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

test('Portal header groups equal-height controls by purpose', async ({ page }) => {
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

  const geometry = await page.locator(
    '.portal-endpoint-pill, .portal-status-pill, [data-language-toggle="portal"], [data-theme-toggle="portal"], .portal-admin-link',
  ).evaluateAll(elements => elements.map((element) => {
    const box = element.getBoundingClientRect()
    return { height: Math.round(box.height), center: Math.round((box.top + box.bottom) / 2) }
  }))
  expect(geometry.map(item => item.height)).toEqual([40, 40, 40, 40, 40])
  expect(new Set(geometry.map(item => item.center)).size).toBe(1)
})

for (const width of [320, 375, 390]) {
  test(`Portal header keeps every visible control in view at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 })
    await setUiPreferences(page, 'dark', 'en')
    await mockAdminApi(page, {
      'GET /api/v1/stats': populatedStats,
    })
    await page.goto('/')

    const status = page.locator('.portal-status-pill')
    await expect(status).toHaveAttribute('role', 'status')
    await expect(status).toHaveAttribute('aria-label', /Online/i)
    await expect(status.locator('.portal-status-label')).toBeAttached()
    await expect(status.locator('.portal-status-compact-icon')).toBeVisible()
    await expect(status.locator('.portal-status-dot')).toBeHidden()

    const admin = page.locator('.portal-admin-link')
    await expect(admin).toHaveAccessibleName(/Admin/i)
    const adminBox = await admin.boundingBox()
    expect(adminBox?.height).toBeGreaterThanOrEqual(40)

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
}

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
  await expect(detail).toHaveCSS('background-color', 'rgb(20, 24, 26)')
  await expect(detail).toHaveCSS('color', 'rgb(255, 255, 255)')

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

for (const route of ['/', '/monitor']) {
  for (const width of [320, 375, 390]) {
    test(`Portal ${route} has no document overflow at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 844 })
      await setUiPreferences(page, 'dark', 'en')
      await mockAdminApi(page, {
        'GET /api/v1/stats': populatedStats,
        'GET /api/v1/latency-series': latencySeries,
      })
      await page.goto(route)
      await expectNoDocumentOverflow(page)
    })
  }
}
