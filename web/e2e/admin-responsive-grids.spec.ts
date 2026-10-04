import { expect, mockAdminApi, test } from './fixtures/admin-api'

function populatedDashboard(overrides: Record<string, unknown> = {}) {
  return {
    range: { key: '30d', start: '2026-09-03T00:00:00Z', end: '2026-10-03T00:00:00Z' },
    window: {
      total_requests: 1200,
      hit_count: 1104,
      miss_count: 96,
      hit_requests: 1104,
      miss_requests: 96,
      hit_rate: 0.92,
      bytes_served: 734003200,
      hit_bytes: 671088640,
      miss_bytes: 62914560,
      avg_latency_ms: 42,
      avg_hit_latency_ms: 18,
      avg_miss_latency_ms: 220,
      upstream_requests: 104,
      upstream_bytes: 66060288,
      errors: 2,
    },
    prev: {
      total_requests: 1000,
      hit_count: 900,
      miss_count: 100,
      hit_requests: 900,
      miss_requests: 100,
      hit_rate: 0.9,
      bytes_served: 629145600,
      hit_bytes: 566231040,
      miss_bytes: 62914560,
      avg_latency_ms: 48,
      avg_hit_latency_ms: 20,
      avg_miss_latency_ms: 240,
      upstream_requests: 110,
      upstream_bytes: 69206016,
      errors: 3,
    },
    origin_coverage: { measured: true, since: '2026-09-01T00:00:00Z', window_complete: true },
    daily_stats: [],
    last_24h: { total_requests: 40, hit_count: 36, hit_rate: 0.9, bytes_served: 2048, avg_latency_ms: 42 },
    prev_24h: { total_requests: 32, hit_rate: 0.85, bytes_served: 1024, avg_latency_ms: 50 },
    top_packages: {},
    cache_usage_percent: 12,
    upstreams: [],
    ...overrides,
  }
}

test('Overview resource metrics stay readable and never overflow on mobile', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  const grid = page.locator('[data-dashboard-kpis]')
  await expect(grid.locator(':scope > *')).toHaveCount(4)
  // Very narrow screens stack to one column instead of squeezing 32px values.
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(1)
  await expect(page.locator('[data-query-key="now"]')).not.toContainText(/NaN|undefined/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.setViewportSize({ width: 800, height: 900 })
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(2)

  await page.setViewportSize({ width: 1440, height: 900 })
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(4)
})

test('Overview title is rendered once with a single range control', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  await expect(page.getByRole('heading', { level: 1, name: '总览', exact: true })).toHaveCount(1)
  const rangeGroup = page.getByRole('group', { name: '统计周期' })
  await expect(rangeGroup.getByRole('button')).toHaveCount(4)
  await expect(rangeGroup.getByRole('button', { name: '30 天' })).toHaveAttribute('aria-pressed', 'true')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

test('Overview surfaces a degraded upstream as an actionable centered dialog', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': {
      status: 'degraded',
      uptime_seconds: 3600,
      now_unix: 1785200000,
      version: 'dev',
      last_activity: null,
      rate: {
        requests_per_min: 1,
        ingress_bps: 0,
        egress_bps: 0,
        has_data: true,
        measured: true,
        window_seconds: 60,
        service_requests_per_sec: 0.02,
        service_bytes_per_sec: 128,
        origin_requests_per_sec: 0.01,
        origin_bytes_per_sec: 64,
      },
      upstreams: { healthy: 1, total: 2 },
      sparkline: [],
    },
    'GET /api/v1/admin/dashboard': populatedDashboard({
      upstreams: [
        { id: 1, name: 'npmjs', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0.4 },
        { id: 2, name: 'PyPI', adapter: 'pypi', healthy: true, avg_latency_ms: 100, success_rate: 1 },
      ],
    }),
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  await expect(page.locator('[data-dashboard-status-strip]')).toContainText('部分能力异常')
  const problemsButton = page.getByRole('button', { name: '查看问题' })
  await expect(problemsButton).toBeVisible()
  await problemsButton.click()

  const dialog = page.locator('[data-slot="dialog-content"]').filter({ has: page.locator('[data-dashboard-info-dialog]') })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText(/npmjs/)).toBeVisible()
  await expect(page.locator('[data-slot="sheet-content"]')).toHaveCount(0)

  const geometry = await dialog.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return { center: rect.left + rect.width / 2, viewport: window.innerWidth / 2 }
  })
  expect(Math.abs(geometry.center - geometry.viewport)).toBeLessThan(2)

  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
})
