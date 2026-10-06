import type { DashboardPeriod, NowResponse, RuntimeResponse } from '../src/lib/adminApi.types'
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

const unmeasuredNow: NowResponse = {
  status: 'healthy',
  uptime_seconds: 120,
  now_unix: 1786032000,
  version: 'dev',
  rate: {
    requests_per_min: 0,
    ingress_bps: 0,
    egress_bps: 0,
    has_data: false,
    measured: false,
    window_seconds: 60,
    service_requests_per_sec: 0,
    service_bytes_per_sec: 0,
    origin_requests_per_sec: 0,
    origin_bytes_per_sec: 0,
  },
  upstreams: { healthy: 0, total: 0 },
  sparkline: [],
}

const unsupportedRuntime: RuntimeResponse = {
  sampled_at: '2026-10-06T10:00:00Z',
  interval_seconds: 5,
  process: {
    cpu: { supported: false, reason: 'cpu sampling is not available on this platform' },
    memory: { supported: false, reason: 'memory sampling is not available on this platform' },
  },
  go_runtime: { heap_alloc_bytes: 1_048_576, sys_bytes: 4_194_304 },
  cache: {
    storage_type: 's3',
    storage_path: 's3://depsilo-cache',
    logical_bytes: 3_221_225_472,
    entries: 12,
    packages: 4,
    quota_bytes: 0,
    physical_bytes: null,
    physical_known: false,
  },
  disk: { supported: false, reason: 'object storage has no local disk usage' },
  host: { hostname: 'fixture-host' },
}

// The Overview must distinguish "never collected" from a measured zero, and a
// platform that cannot sample a resource must say so without leaking an
// English server reason into the tile or printing a unit next to a dash.
test('Overview separates uncollected data from a measured zero', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': unmeasuredNow,
    'GET /api/v1/admin/runtime': unsupportedRuntime,
    'GET /api/v1/admin/dashboard': {
      range: { key: '30d', start: '2026-09-07T00:00:00Z', end: '2026-10-07T00:00:00Z' },
      window: emptyPeriod,
      prev: emptyPeriod,
      last_24h: {},
      prev_24h: {},
      daily_stats: [],
      upstreams: [],
      top_packages: {},
      origin_coverage: { measured: false, since: null, window_complete: false },
    },
    'GET /api/v1/admin/dashboard/trends': { points: [] },
    'GET /api/v1/admin/logs': { items: [], total: 0, page: 1, page_size: 5 },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  // Upstream→Depsilo metering never ran for this range: the total is unknown,
  // not zero. Client-facing bytes were measured, so a real zero stays a zero.
  const originTotal = page.locator('[data-testid="traffic-origin-total"]')
  await expect(originTotal).toContainText('—')
  await expect(originTotal).toContainText('未采集')
  await expect(originTotal).not.toContainText('0 B')
  const servedTotal = page.locator('[data-testid="traffic-served-total"]')
  await expect(servedTotal.locator('.dash-metric-value')).toHaveText('0 B')
  await expect(servedTotal).toContainText('0 次请求')

  // Live rates are not collected yet: dash, localized detail, no unit.
  const serviceFlow = page.locator('[data-testid="traffic-service-flow"]')
  await expect(serviceFlow.locator('.dash-metric-value')).toHaveText('—')
  await expect(serviceFlow).toContainText('未采集')
  await expect(serviceFlow).not.toContainText('次/秒')

  const cpu = page.locator('[data-testid="resource-cpu"]')
  await expect(cpu.locator('.dash-metric-value')).toHaveText('—')
  await expect(cpu).not.toContainText('%')
  await expect(cpu).toContainText('当前平台不支持')
  await expect(cpu).not.toContainText('cpu sampling is not available')

  const network = page.locator('[data-testid="resource-network"]')
  await expect(network).not.toContainText('次/秒')
  await expect(network).toContainText('未采集')

  // The platform's own explanation stays available in the hint.
  await page.getByLabel('查看 CPU 口径').hover()
  await expect(page.getByText('cpu sampling is not available on this platform')).toBeVisible()
})
