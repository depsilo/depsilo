import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('Dashboard keeps its primary metrics in a compact mobile 2 by 2 grid', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  const grid = page.locator('[data-dashboard-kpis]')
  await expect(grid.locator(':scope > *')).toHaveCount(4)
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(2)
})

test('Dashboard metric deltas reflect domain intent instead of raw sign', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': {
      last_24h: {
        total_requests: 80,
        hit_count: 72,
        hit_rate: 0.9,
        bytes_served: 800,
        avg_latency_ms: 120,
      },
      prev_24h: {
        total_requests: 100,
        hit_count: 80,
        hit_rate: 0.8,
        bytes_served: 1000,
        avg_latency_ms: 100,
      },
      cache_usage_percent: 10,
      upstreams: [],
      top_packages: { pypi: [], apt: [] },
    },
  })
  await page.goto('/admin')

  const changes = page.locator('[data-dashboard-kpis] [data-metric-change]')
  await expect(changes).toHaveCount(4)
  const tones = await changes.evaluateAll(
    elements => elements.map(element => ({
      intent: element.getAttribute('data-change-intent'),
      tone: element.getAttribute('data-change-tone'),
    })),
  )
  expect(tones).toEqual([
    { intent: 'neutral', tone: 'neutral' },
    { intent: 'higher-is-better', tone: 'positive' },
    { intent: 'neutral', tone: 'neutral' },
    { intent: 'lower-is-better', tone: 'negative' },
  ])
})

test('Dashboard trend controls keep a touch-safe segmented layout on mobile', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin')

  const controls = page.locator('[data-query-key="dashboard-trends"] button[aria-pressed]')
  await expect(controls).toHaveCount(8)
  const sizes = await controls.evaluateAll(elements => elements.map(element => {
    const rect = element.getBoundingClientRect()
    return { width: rect.width, height: rect.height }
  }))
  expect(Math.min(...sizes.map(size => size.height))).toBeGreaterThanOrEqual(40)
  const rangeControl = page.locator('[data-trend-range-control]')
  await expect(rangeControl).toBeVisible()
  expect(await rangeControl.evaluate(element => element.getBoundingClientRect().height)).toBeLessThanOrEqual(42)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
})

test('Dashboard turns degraded operations into flowline actions without restoring summary lists', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': {
      status: 'degraded',
      uptime_seconds: 3600,
      now_unix: 1785200000,
      version: 'dev',
      last_activity: null,
      rate: { requests_per_min: 1, ingress_bps: 0, egress_bps: 0, has_data: true },
      upstreams: { healthy: 4, total: 6 },
      sparkline: [],
    },
    'GET /api/v1/admin/dashboard': {
      last_24h: {
        total_requests: 80,
        hit_count: 72,
        hit_rate: 0.9,
        bytes_served: 800,
        avg_latency_ms: 120,
      },
      prev_24h: {
        total_requests: 60,
        hit_count: 45,
        hit_rate: 0.75,
        bytes_served: 600,
        avg_latency_ms: 140,
      },
      daily_stats: [],
      cache_usage_percent: 87.4,
      upstreams: [
        { id: 1, name: 'npmjs', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0.4 },
        { id: 2, name: 'PyPI', adapter: 'pypi', healthy: true, avg_latency_ms: 240, success_rate: 1 },
      ],
      top_packages: {
        npm: [{ name: '@depsilo/ui', hit_count: 42 }],
        maven: [{ name: 'org.example:core', hit_count: 21 }],
      },
    },
  })
  await page.goto('/admin')

  const flowline = page.locator('[data-query-key="now"]')
  await expect(flowline.getByRole('heading', { name: '实时依赖流线' })).toBeVisible()
  await expect(flowline.getByRole('group', { name: /请求从客户端入口进入 Depsilo 缓存/ })).toBeVisible()
  await expect(flowline.getByText('客户端入口', { exact: true })).toBeVisible()
  await expect(flowline.getByText('Depsilo 缓存', { exact: true })).toBeVisible()
  await expect(flowline.getByText('上游源', { exact: true })).toBeVisible()

  const attention = page.locator('section[aria-labelledby="dashboard-attention-title"]')
  await expect(attention.getByRole('heading', { name: '待处理' })).toBeVisible()
  await expect(page.getByText('2 个上游需要关注：npmjs、PyPI')).toBeVisible()
  await expect(page.getByRole('link', { name: '查看上游源', exact: true })).toHaveAttribute('href', '/admin/upstreams')
  await expect(page.getByRole('link', { name: '管理缓存', exact: true })).toHaveAttribute('href', '/admin/cache')
  await expect(page.getByRole('link', { name: /4.*6 个上游健康/ })).toHaveAttribute('href', '/admin/upstreams')
  await expect(page.getByRole('heading', { name: /热门包 TOP 10|Top 10 Packages/ })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: /上游源状态|Upstream Status/ })).toHaveCount(0)
  await expect(page.getByText('@depsilo/ui', { exact: true })).toHaveCount(0)
  await expect(page.getByText('org.example:core', { exact: true })).toHaveCount(0)
})
