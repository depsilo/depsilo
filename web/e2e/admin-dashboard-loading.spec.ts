import AxeBuilder from '@axe-core/playwright'
import type { Locator } from '@playwright/test'

import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'
import type { AccessLog, DashboardTrendPoint } from '../src/lib/adminApi.types'

/** Rows the Overview's live request tail shows (Dashboard RECENT_REQUESTS). */
const RECENT_ROWS = 8

function recentLog(id: number): AccessLog {
  return {
    id,
    adapter_type: 'pypi',
    method: 'GET',
    cache_key: `pypi/simple/pkg-${id}/pkg-${id}-1.0.0.whl`,
    package_name: `pkg-${id}`,
    version: '1.0.0',
    hit: true,
    cache_result: 'hit',
    upstream: '',
    latency_ms: 18,
    status_code: 200,
    client_ip: '192.168.1.100',
    bytes_sent: 2_097_152,
    created_at: new Date(Date.now() - id * 60_000).toISOString(),
  }
}

/**
 * Hourly buckets across the fixture's 30-day range, with three isolated busy
 * hours that add up to the period's 1,200 requests. One hour per burst keeps
 * each burst inside a single clock-aligned bar in any time zone.
 */
function trendPoints(): DashboardTrendPoint[] {
  const start = Date.parse('2026-09-03T00:00:00Z') / 1000
  const busyHours = new Set([100, 400, 700])
  return Array.from({ length: 720 }, (_, hour) => {
    const requests = busyHours.has(hour) ? 400 : 0
    const hits = busyHours.has(hour) ? 368 : 0
    return {
      bucket: start + hour * 3600,
      date: new Date((start + hour * 3600) * 1000).toISOString(),
      requests,
      hits,
      misses: requests - hits,
      hit_rate: requests > 0 ? hits / requests : 0,
      bytes_served: requests * 611_669,
      bytes_hit: hits * 611_669,
      bytes_miss: (requests - hits) * 611_669,
      upstream_requests: requests - hits,
      upstream_bytes: (requests - hits) * 611_669,
      sum_latency_ms: requests * 42,
      avg_latency_ms: requests > 0 ? 42 : 0,
      errors: 0,
    }
  })
}

async function height(locator: Locator): Promise<number> {
  const box = await locator.boundingBox()
  expect(box).not.toBeNull()
  return box!.height
}

// First load must hold its shape: the skeletons match the settled regions, and
// no tile claims a storage/measurement fact before its payload exists.
test('Overview first-load skeletons cover the settled layout', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  const afterRelease = (key: string, body: unknown = adminApiDefaults[key]) => async () => {
    await pending
    return body
  }
  await mockAdminApi(page, {
    'GET /ready': afterRelease('GET /ready'),
    'GET /api/v1/now': afterRelease('GET /api/v1/now'),
    'GET /api/v1/admin/runtime': afterRelease('GET /api/v1/admin/runtime'),
    'GET /api/v1/admin/dashboard': afterRelease('GET /api/v1/admin/dashboard'),
    'GET /api/v1/admin/dashboard/trends': afterRelease('GET /api/v1/admin/dashboard/trends', { points: trendPoints() }),
    // A full tail, so the settled card can be compared with the slots its
    // skeleton reserved.
    'GET /api/v1/admin/logs': afterRelease('GET /api/v1/admin/logs', {
      items: Array.from({ length: RECENT_ROWS }, (_, index) => recentLog(index + 1)),
      total: RECENT_ROWS,
      page: 1,
      page_size: RECENT_ROWS,
    }),
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('正在检查服务状态')
  await expect(status).not.toContainText('服务正常')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toBeVisible()
  await expect(details).toContainText('未知')
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()

  // Loading: the recent-request card reserves one slot per row of the tail.
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(recent).toHaveAttribute('aria-busy', 'true')
  const skeletonRows = recent.locator('[data-recent-skeleton] > *')
  await expect(skeletonRows).toHaveCount(RECENT_ROWS)
  const recentLoadingHeight = await height(recent)

  // Loading: no period tile claims a measured figure (a zero included) before
  // the statistics payload exists; each one is busy instead.
  const tiles = page.locator('[data-dashboard-kpis]')
  for (const id of ['cache', 'origin', 'served', 'hit-rate', 'latency', 'time-saved', 'errors']) {
    await expect(tiles.locator(`[data-tile="${id}"]`)).toHaveAttribute('aria-busy', 'true')
  }
  await expect(tiles.locator('[data-tile-value]')).toHaveCount(0)
  await expect(tiles.locator('[data-tile="latency"]')).not.toContainText('ms')

  // Loading: the cache tile must not claim a missing quota before the sample.
  const cache = page.locator('[data-testid="resource-cache"]')
  await expect(cache).toHaveAttribute('aria-busy', 'true')
  await expect(cache).not.toContainText('未设置缓存配额')
  await expect(cache).not.toContainText('0 B')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
  await expect(page.locator('[data-dashboard-benefits]')).not.toContainText('0 B')

  // Loading: the activity card already occupies the chart's footprint and
  // announces that statistics are loading.
  const trends = page.locator('[data-dashboard-trends]')
  await expect(trends).toHaveAttribute('aria-busy', 'true')
  await expect(trends.getByRole('status', { name: '正在加载统计' })).toBeVisible()
  await expect(trends.locator('[data-trend-chart]')).toHaveCount(0)
  const trendsLoadingHeight = await height(trends)

  // Loading: every first load passes through this state, and an audit of any
  // Admin page can sample it, so it meets the same contract as settled pages.
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])

  // Settled: the same regions render real content at a comparable height.
  release()
  await expect(recent).not.toHaveAttribute('aria-busy', 'true')
  await expect(recent.locator('[data-recent-request-id]')).toHaveCount(RECENT_ROWS)
  await expect(recent.locator('[data-recent-skeleton]')).toHaveCount(0)

  const served = page.locator('[data-testid="traffic-served-total"]')
  await expect(served).not.toHaveAttribute('aria-busy', 'true')
  await expect(served.locator('[data-tile-value]')).toHaveText(/^700\.0\s*MB$/)
  await expect(served).toContainText('1,200 次请求')
  await expect(cache.locator('[data-tile-value]')).toHaveText('2.5%')
  await expect(cache).toContainText('512.0 MB / 20.0 GB')

  await expect(trends).not.toHaveAttribute('aria-busy', 'true')
  await expect(trends.locator('[data-trend-chart]')).toBeVisible()
  await expect(trends.locator('.dash-chart-col[data-requests="400"]')).toHaveCount(3)
  await expect(trends.locator('[data-activity-bursts] > li')).toHaveCount(3)
  await expect(status).toHaveText('服务正常')

  // Comparable height: the reserved footprint keeps the cards below from
  // jumping when data arrives, so a reader's place on the page survives the
  // first load. A few pixels of row-metric difference are tolerated.
  expect(Math.abs(await height(recent) - recentLoadingHeight)).toBeLessThanOrEqual(12)
  expect(Math.abs(await height(trends) - trendsLoadingHeight)).toBeLessThanOrEqual(12)
})
