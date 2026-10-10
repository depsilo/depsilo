import type { Locator } from '@playwright/test'

import type { RuntimeResponse } from '../src/lib/adminApi.types'
import { adminApiDefaults, expect, mockAdminApi, test } from './fixtures/admin-api'

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
  const resourcesToggle = page.getByRole('button', { name: '程序资源占用' })
  await expect(resourcesToggle).toHaveAttribute('aria-expanded', 'false')
  await expect(grid).toBeHidden()
  await resourcesToggle.click()
  await expect(resourcesToggle).toHaveAttribute('aria-expanded', 'true')
  await expect(grid).toBeVisible()
  await expect(grid.locator(':scope > *')).toHaveCount(4)
  // Very narrow screens stack to one column instead of squeezing 32px values.
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(1)
  await expect(page.locator('[data-query-key="now"]')).not.toContainText(/NaN|undefined/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.setViewportSize({ width: 800, height: 900 })
  await expect(resourcesToggle).toBeHidden()
  await expect(grid).toBeVisible()
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(2)

  await page.setViewportSize({ width: 1440, height: 900 })
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(4)
})

// Resource cells are size containers: the icon and headline scale from the
// cell's own inline size, so a long value ("1023.9 GB") keeps its slack at the
// cell edge instead of colliding with the icon.
function tileMetrics(locator: Locator) {
  return locator.evaluate(card => {
    const icon = card.querySelector('.dash-metric-icon')
    const value = card.querySelector('.dash-metric-value')
    const style = getComputedStyle(card)
    const contentRight = card.getBoundingClientRect().right -
      parseFloat(style.paddingRight) - parseFloat(style.borderRightWidth)
    return {
      icon: icon ? Math.round(icon.getBoundingClientRect().width) : 0,
      valueFont: value ? parseFloat(getComputedStyle(value).fontSize) : 0,
      // Space left between the last glyph and the card's content edge.
      slack: value ? Math.round(contentRight - value.getBoundingClientRect().right) : 0,
    }
  })
}

test('Overview resource cells keep a long value inside the shared card', async ({ page }) => {
  const runtime = adminApiDefaults['GET /api/v1/admin/runtime'] as RuntimeResponse
  await mockAdminApi(page, {
    'GET /api/v1/admin/runtime': {
      ...runtime,
      cache: { ...runtime.cache, logical_bytes: 1_099_400_000_000, quota_bytes: 2_199_023_255_552 },
    },
  })

  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')
  await page.getByRole('button', { name: '程序资源占用' }).click()
  const cell = page.locator('[data-testid="resource-cache"]')
  await expect(cell).toContainText('1023.9 GB')
  const compact = await tileMetrics(cell)
  expect(compact.icon).toBeLessThan(56)
  expect(compact.slack).toBeGreaterThan(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.setViewportSize({ width: 1920, height: 900 })
  const wide = await tileMetrics(cell)
  expect(wide.icon).toBe(56)
  expect(wide.slack).toBeGreaterThan(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1920)
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

test('Overview mobile priorities and touch targets remain usable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': {
      status: 'degraded', using_stale_snapshot: true,
      snapshot_loaded_at: '2026-10-06T01:00:00Z', snapshot_age_seconds: 720,
      refresh_failures: 2, on_load_error: 'use_stale_then_allow',
    },
  })
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  const issues = page.locator('[data-dashboard-status-issues]')
  const flow = page.locator('[data-dashboard-request-flow]')
  await expect(issues).toBeVisible()
  await expect(issues).toContainText('查看问题')
  await expect(flow).toBeVisible()
  expect((await issues.boundingBox())!.y).toBeLessThan((await flow.boundingBox())!.y)
  expect((await issues.boundingBox())!.height).toBeGreaterThanOrEqual(40)
  await expect(page.locator('[data-dashboard-attention]')).toHaveCount(0)
  await issues.click()
  await expect(page.locator('[data-dashboard-info-dialog]')).toContainText('包规则')
  await page.keyboard.press('Escape')
  await expect(page.locator('[data-dashboard-kpis]')).toBeHidden()
  const resources = page.locator('[data-dashboard-resources-panel]')
  const recent = page.locator('[data-dashboard-recent-requests]')
  expect((await resources.boundingBox())!.y).toBeGreaterThan((await recent.boundingBox())!.y)
  const targets = await page.locator('[data-dashboard-root] button[aria-label]').evaluateAll(buttons =>
    buttons.filter(button => button.getClientRects().length > 0).map(button => ({
      label: button.getAttribute('aria-label'),
      width: button.getBoundingClientRect().width,
      height: button.getBoundingClientRect().height,
    })),
  )
  expect(targets.length).toBeGreaterThan(0)
  expect(targets.every(target => target.width >= 40 && target.height >= 40), JSON.stringify(targets)).toBe(true)
  const ranges = await page.getByRole('group', { name: '统计周期' }).getByRole('button').evaluateAll(buttons =>
    buttons.map(button => button.getBoundingClientRect().height),
  )
  expect(ranges.every(height => height >= 40)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('Overview keeps the flow readable at 1024 and gives an empty tail no chart width', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1024, height: 900 })
  await page.goto('/admin')

  const flow = page.locator('[data-dashboard-request-flow]')
  await expect(flow.locator('[data-testid="traffic-served-total"]')).toContainText('700.0 MB')
  const clients = flow.locator('[data-testid="traffic-served-total"]')
  const depsilo = flow.locator('[data-testid="traffic-depsilo"]')
  const upstream = flow.locator('[data-testid="traffic-origin-total"]')
  const rail = flow.locator('[data-testid="traffic-service-flow"]')
  const value = clients.locator('p.font-mono')
  const lineHeight = await value.evaluate(node => parseFloat(getComputedStyle(node).lineHeight))
  expect((await value.boundingBox())!.height).toBeLessThanOrEqual(lineHeight + 1)
  const clientRect = (await clients.boundingBox())!
  const depsiloRect = (await depsilo.boundingBox())!
  const upstreamRect = (await upstream.boundingBox())!
  expect(clientRect.x).toBeLessThan(depsiloRect.x)
  expect(depsiloRect.x).toBeLessThan(upstreamRect.x)
  expect((await rail.boundingBox())!.y).toBeGreaterThan(clientRect.y + clientRect.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1024)

  await page.setViewportSize({ width: 1440, height: 900 })
  const chart = page.locator('[data-dashboard-trends]')
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(recent).toHaveAttribute('data-dashboard-recent-empty', 'true')
  const chartRect = (await chart.boundingBox())!
  const recentRect = (await recent.boundingBox())!
  const rootRect = (await page.locator('[data-dashboard-root]').boundingBox())!
  expect(chartRect.width).toBeGreaterThan(rootRect.width - 5)
  expect(recentRect.y).toBeGreaterThan(chartRect.y + chartRect.height)
})

// 1024–1279px keeps four columns from squeezing the status labels and the
// request table from clipping its time column; both were visible regressions
// when the strip used the lg breakpoint and the table demanded 680px.
test('Overview status strip and recent table keep their content at 1024 and 1440', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': {
      status: 'degraded',
      uptime_seconds: 3 * 86400,
      now_unix: 1786032000,
      version: 'dev',
      rate: {
        requests_per_min: 744,
        ingress_bps: 3_400_000,
        egress_bps: 380_000,
        has_data: true,
        measured: true,
        window_seconds: 60,
        service_requests_per_sec: 12.4,
        service_bytes_per_sec: 3_400_000,
        origin_requests_per_sec: 0.7,
        origin_bytes_per_sec: 380_000,
      },
      upstreams: { healthy: 1, total: 2 },
      sparkline: [],
    },
    'GET /api/v1/admin/dashboard': populatedDashboard({
      upstreams: [
        { id: 1, name: 'ruby-china', adapter: 'rubygems', healthy: false, avg_latency_ms: 890, success_rate: 0.71 },
        { id: 2, name: 'tuna', adapter: 'pypi', healthy: true, avg_latency_ms: 42, success_rate: 0.99 },
      ],
    }),
    'GET /api/v1/admin/logs': {
      items: [
        {
          id: 1, adapter_type: 'rubygems', package_name: 'nokogiri', version: '1.18.4',
          hit: false, cache_result: 'miss', upstream: 'crash', latency_ms: 30_120,
          status_code: 500, bytes_sent: 0, client_ip: '10.0.0.9',
          created_at: '2026-10-06T10:00:00Z',
        },
      ],
      total: 1, page: 1, page_size: 5,
    },
  })

  await page.setViewportSize({ width: 1024, height: 900 })
  await page.goto('/admin')

  const strip = page.locator('[data-dashboard-status-strip]')
  await expect(strip).toContainText('部分能力异常')
  // Every truncating label inside the strip must fit its box at 1024.
  const clippedStrip = await strip.locator('.truncate').evaluateAll(elements =>
    elements.filter(element => element.scrollWidth > element.clientWidth + 1).map(element => element.textContent),
  )
  expect(clippedStrip).toEqual([])

  await page.setViewportSize({ width: 1440, height: 900 })
  const table = page.locator('[data-table-viewport]').filter({ hasText: '包名及版本' })
  await expect(table).toContainText('30.1 s')
  const tableFits = await table.evaluate(element => element.scrollWidth <= element.clientWidth + 1)
  expect(tableFits).toBe(true)

  await expect(page.locator('[data-testid="traffic-service-flow"]')).toContainText('Depsilo → 客户端')
  await expect(page.locator('[data-testid="traffic-origin-flow"]')).toContainText('上游 → Depsilo')
})

// Idle installs must stay readable too: the "no recent activity" guidance is
// the longest string in the strip and used to truncate in the 4-column layout.
test('Overview idle strip keeps its guidance text intact', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const strip = page.locator('[data-dashboard-status-strip]')
  await expect(strip).toContainText('暂无请求')
  await expect(strip).toContainText('接入客户端后显示最近请求')
  const clipped = await strip.locator('.truncate').evaluateAll(elements =>
    elements.filter(element => element.scrollWidth > element.clientWidth + 1).map(element => element.textContent),
  )
  expect(clipped).toEqual([])
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
  // The strip counts problem categories, while the dialog lists affected
  // upstreams; do not present the two counts as the same unit.
  await expect(page.locator('[data-dashboard-status-strip]')).toContainText('1 类问题需要关注')
  const problemsButton = page.locator('[data-dashboard-status-issues]')
  await expect(problemsButton).toBeVisible()
  await expect(problemsButton).toHaveAttribute('aria-haspopup', 'dialog')
  await expect(page.locator('[data-dashboard-attention]')).toHaveCount(0)
  const status = page.locator('[data-dashboard-status-strip]')
  const traffic = page.locator('[data-dashboard-traffic]')
  const root = page.locator('[data-dashboard-root]')
  expect((await status.boundingBox())!.height).toBeLessThan(140)
  expect((await traffic.boundingBox())!.y - (await root.boundingBox())!.y).toBeLessThan(330)
  await problemsButton.focus()
  await page.keyboard.press('Enter')

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
  await expect(problemsButton).toBeFocused()
})
