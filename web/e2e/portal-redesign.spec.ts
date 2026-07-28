import type { Locator, Page } from '@playwright/test'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const populatedStats = {
  service: { status: 'healthy', version: 'v0.9.3' },
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

test('Quick Start leads with package-manager configuration and defaults Python to pip', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const optional = page.locator('[data-quickstart-optional]')

  await expectBefore(primary, optional)
  await expect(primary.getByRole('heading', { name: 'Configure Python' })).toBeVisible()

  const pipManager = primary.getByRole('button', { name: /^pip\b/i })
  await expect(pipManager).toHaveAttribute('aria-pressed', 'true')
  await expect(primary).toContainText('~/.config/pip/pip.conf')
  await expect(primary).not.toContainText('Prompt for ChatGPT / Claude / Cursor')

  await expect(optional.getByRole('heading', {
    name: 'Let your AI coding agent configure a project',
  })).toBeVisible()
  await expect(optional.getByRole('button', { name: /Copy the full prompt/i })).toBeVisible()
  await expect(optional.getByRole('article', {
    name: 'Remote build cache for ccache and sccache',
  })).toBeVisible()
})

test('recommended ecosystems lead into searchable disclosure and persist the selected ecosystem', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.goto('/')

  const primary = page.locator('[data-quickstart-primary]')
  const recommended = primary.getByRole('region', { name: 'Recommended' })
  await expect(recommended.getByRole('button', { name: /^Python\b/ })).toBeVisible()
  await expect(recommended.getByRole('button', { name: /^Node\.js\b/ })).toBeVisible()
  await expect(recommended.getByRole('button', { name: /^Container\b/ })).toBeVisible()

  const allEcosystems = primary.locator('details').filter({
    hasText: 'Browse all 14 ecosystems',
  })
  await expect(allEcosystems).not.toHaveAttribute('open', '')
  await allEcosystems.getByText('Browse all 14 ecosystems', { exact: true }).click()
  await expect(allEcosystems).toHaveAttribute('open', '')
  await expect(allEcosystems.getByRole('button', { name: /^Java\b/ })).toBeVisible()

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
  await setUiPreferences(page, 'dark', 'en')
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
