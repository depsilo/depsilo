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

// The request path is one card: live rails between three nodes, then the
// period's real outcomes. Blocked requests stay countless by design because
// the Overview aggregate excludes them (internal/api/admin/dashboard.go).
test('Overview request flow ties live rails and period outcomes to real data', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/now': measuredNow })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const flow = page.locator('[data-dashboard-request-flow]')
  await expect(flow).toBeVisible()

  const clients = page.locator('[data-testid="traffic-served-total"]')
  await expect(clients).toContainText('700.0 MB')
  await expect(clients).toContainText('1,200 次请求')

  const serviceRail = page.locator('[data-testid="traffic-service-flow"]')
  await expect(serviceRail).toContainText('110.3 KB/s')
  await expect(serviceRail).toContainText('Depsilo → 客户端')

  const depsilo = page.locator('[data-testid="traffic-depsilo"]')
  await expect(depsilo).toContainText('92.0%')
  await expect(depsilo).toContainText('请求命中率')

  const originRail = page.locator('[data-testid="traffic-origin-flow"]')
  await expect(originRail).toContainText('1.5 MB/s')
  await expect(originRail).toContainText('上游 → Depsilo')

  const upstream = page.locator('[data-testid="traffic-origin-total"]')
  await expect(upstream).toContainText('63.0 MB')
  await expect(upstream).toContainText('104 次回源请求')

  const outcomes = page.locator('[data-dashboard-flow-outcomes]')
  await expect(outcomes).toContainText('缓存命中')
  await expect(outcomes).toContainText('1,104')
  await expect(outcomes).toContainText('回源完成')
  await expect(outcomes).toContainText('96')

  const blocked = outcomes.getByRole('link', { name: '策略拦截，前往安全页查看' })
  await expect(blocked).toHaveAttribute('href', '/admin/security')
  await expect(blocked).not.toContainText(/\d/)
})

// An empty period is not the same as an unmetered one: measured zeros stay
// zeros, missing hit/miss samples stay dashes, and the live rails still read.
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

  const clients = page.locator('[data-testid="traffic-served-total"]')
  await expect(clients).toContainText('0 B')
  await expect(clients).toContainText('0 次请求')

  const depsilo = page.locator('[data-testid="traffic-depsilo"]')
  await expect(depsilo).toContainText('—')

  const upstream = page.locator('[data-testid="traffic-origin-total"]')
  await expect(upstream).toContainText('0 B')
  await expect(upstream).toContainText('0 次回源请求')

  const outcomes = page.locator('[data-dashboard-flow-outcomes]')
  await expect(outcomes).toContainText('缓存命中')
  await expect(outcomes).toContainText('—')

  // Live rails are measured independently of the period aggregate.
  await expect(page.locator('[data-testid="traffic-service-flow"]')).toContainText('110.3 KB/s')
  await expect(page.locator('[data-testid="traffic-origin-flow"]')).toContainText('1.5 MB/s')
})
