import type { Page } from '@playwright/test'

import type { DashboardPeriod, NowResponse } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const emptyPeriod: DashboardPeriod = {
  total_requests: 0,
  hit_count: 0,
  miss_count: 0,
  hit_requests: 0,
  miss_requests: 0,
  hit_rate: 0,
  bytes_served: 0,
  hit_bytes: 0,
  miss_bytes: 0,
  avg_latency_ms: 0,
  avg_hit_latency_ms: 0,
  avg_miss_latency_ms: 0,
  time_saved_ms: 0,
  upstream_requests: 0,
  upstream_bytes: 0,
  errors: 0,
}

const measuredNow: NowResponse = {
  status: 'healthy',
  uptime_seconds: 600,
  now_unix: 1786032000,
  version: 'dev',
  rate: {
    requests_per_min: 12,
    ingress_bps: 112947,
    egress_bps: 1572864,
    has_data: true,
    measured: true,
    window_seconds: 60,
    service_requests_per_sec: 12.5,
    service_bytes_per_sec: 112947,
    origin_requests_per_sec: 0.5,
    origin_bytes_per_sec: 1572864,
  },
  upstreams: { healthy: 1, total: 1 },
  sparkline: [],
}

function tile(page: Page, id: string) {
  const root = page.locator(`[data-dashboard-kpis] [data-tile="${id}"]`)
  return { root, value: root.locator('[data-tile-value]'), sub: root.locator('.dash-tile-sub') }
}

// The response path now reads as tiles plus a live strip. The byte tiles keep
// the old flow's reading order (from cache, from Upstream, to clients); origin
// bytes are metered at the Upstream, so they need not equal the miss share of
// client downloads. The live strip carries the last-60-second delivery and
// origin rates. Blocked
// requests stay countless by design because the Overview aggregate excludes
// them (internal/api/admin/dashboard.go): no tile invents a blocked count and
// the failure tile counts only 5xx.
test('Overview download flow runs upstream to clients and preserves byte metrics', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/now': measuredNow })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const tiles = page.locator('[data-dashboard-kpis] > [data-tile]')
  await expect(tiles).toHaveCount(8)
  expect(await tiles.evaluateAll(nodes => nodes.map(node => node.getAttribute('data-tile')))).toEqual([
    'cache', 'origin', 'served', 'hit-rate', 'latency', 'time-saved', 'errors', 'cache-usage',
  ])

  const cache = tile(page, 'cache')
  await expect(cache.value).toHaveText('640.0MB')
  await expect(cache.sub).toContainText('占下载量 91%')

  const upstream = tile(page, 'origin')
  await expect(upstream.root).toHaveAttribute('data-testid', 'traffic-origin-total')
  await expect(upstream.value).toHaveText('63.0MB')
  await expect(upstream.sub).toHaveText('104 次回源请求')
  // Origin metering began 2026-09-01, inside the previous 30-day period, so a
  // period-over-period change would compare against a partly unmetered base.
  await expect(upstream.root.locator('.dash-tile-delta')).toHaveCount(0)

  const clients = tile(page, 'served')
  await expect(clients.root).toHaveAttribute('data-testid', 'traffic-served-total')
  await expect(clients.value).toHaveText('700.0MB')
  await expect(clients.sub).toContainText('1,200 次请求')
  await expect(clients.sub).toContainText('较上一周期上升 17%')

  const hitRate = tile(page, 'hit-rate')
  await expect(hitRate.value).toHaveText('92.0%')
  await expect(hitRate.sub).toContainText('1,104 / 1,200')
  await expect(hitRate.root.getByRole('img', { name: '1,104 / 1,200 个请求命中缓存' })).toBeVisible()

  const errors = tile(page, 'errors')
  await expect(errors.value).toHaveText('2')
  await expect(errors.root).toHaveAttribute('data-alert', 'true')
  await expect(errors.sub).toContainText('失败率 0.2%')
  await expect(page.locator('[data-dashboard-kpis]')).not.toContainText('拦截')

  const live = page.locator('[data-dashboard-live]')
  await expect(live).toContainText('近 60 秒')
  await expect(live).toContainText('12 次/分')
  await expect(page.locator('[data-testid="traffic-service-flow"]')).toHaveText('交付 110.3 KB/s')
  await expect(page.locator('[data-testid="traffic-origin-flow"]')).toHaveText('回源 1.5 MB/s')
})

// An empty period is not the same as an unmetered one: measured zeros stay
// zeros, figures that need a request sample stay dashes, and the live strip
// still reads because it is measured independently of the period aggregate.
test('Overview flow separates a measured zero from a missing sample', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': measuredNow,
    'GET /api/v1/admin/dashboard': {
      range: { key: '30d', start: '2026-09-07T00:00:00Z', end: '2026-10-07T00:00:00Z' },
      window: emptyPeriod,
      prev: emptyPeriod,
      last_24h: {},
      prev_24h: {},
      daily_stats: [],
      upstreams: [],
      top_packages: {},
      origin_coverage: { measured: true, since: '2026-09-01T00:00:00Z', window_complete: true },
    },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  // Measured zeros: the API counted bytes and requests and found none.
  const clients = tile(page, 'served')
  await expect(clients.value).toHaveText('0B')
  await expect(clients.sub).toHaveText('0 次请求')

  const upstream = tile(page, 'origin')
  await expect(upstream.value).toHaveText('0B')
  await expect(upstream.sub).toHaveText('0 次回源请求')

  const cache = tile(page, 'cache')
  await expect(cache.value).toHaveText('0B')
  await expect(cache.sub).toHaveText('暂无请求样本')

  const errors = tile(page, 'errors')
  await expect(errors.value).toHaveText('0')
  await expect(errors.sub).toHaveText('暂无请求样本')
  await expect(errors.root).not.toHaveAttribute('data-alert')

  // Ratios and averages need a sample; with none they are dashes, not 0%.
  const hitRate = tile(page, 'hit-rate')
  await expect(hitRate.value).toHaveText('—')
  await expect(hitRate.sub).toHaveText('暂无请求样本')
  await expect(hitRate.root.getByRole('img')).toHaveCount(0)

  const latency = page.locator('[data-dashboard-kpis] [data-tile="latency"] dd')
  await expect(latency).toHaveText(['—', '—'])

  const saved = tile(page, 'time-saved')
  await expect(saved.value).toHaveText('—')
  await expect(saved.sub).toHaveText('暂无请求样本')

  // No delta is claimed against an empty previous period.
  await expect(page.locator('[data-dashboard-kpis] .dash-tile-delta')).toHaveCount(0)

  // The live strip is measured independently of the period aggregate.
  await expect(page.locator('[data-testid="traffic-service-flow"]')).toHaveText('交付 110.3 KB/s')
  await expect(page.locator('[data-testid="traffic-origin-flow"]')).toHaveText('回源 1.5 MB/s')
})
