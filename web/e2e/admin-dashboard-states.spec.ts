import { mkdir } from 'node:fs/promises'
import type { Page, Request } from '@playwright/test'
import type { DashboardPeriod, DashboardResponse, DashboardTrendPoint, NowResponse, RuntimeResponse } from '../src/lib/adminApi.types'
import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

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

const populatedDashboard = adminApiDefaults['GET /api/v1/admin/dashboard'] as DashboardResponse
const sampledRuntime = adminApiDefaults['GET /api/v1/admin/runtime'] as RuntimeResponse
const emptyDashboard: DashboardResponse = {
  ...populatedDashboard,
  window: emptyPeriod,
  prev: emptyPeriod,
  last_24h: emptyPeriod,
  prev_24h: emptyPeriod,
  cache_usage_percent: 0,
}
const emptyRuntime: RuntimeResponse = {
  ...sampledRuntime,
  cache: { ...sampledRuntime.cache, logical_bytes: 0, entries: 0, packages: 0, quota_bytes: 0 },
}
const measuredIdle: NowResponse = {
  ...unmeasuredNow,
  rate: { ...unmeasuredNow.rate, measured: true },
}
const populatedPoints: DashboardTrendPoint[] = [8, 12, 6, 19, 30, 15, 28].map((requests, index) => ({
  bucket: 1789948800 + index * 86400,
  date: `2026-09-${21 + index}`,
  requests,
  hits: requests - 2,
  misses: 2,
  hit_rate: (requests - 2) / requests,
  bytes_served: requests * 2_097_152,
  bytes_hit: (requests - 2) * 2_097_152,
  bytes_miss: 4_194_304,
  upstream_requests: 3,
  upstream_bytes: 4_300_000,
  sum_latency_ms: requests * 42,
  avg_latency_ms: 42,
  errors: 0,
}))

async function reviewViewport(page: Page, scenario: string, width: number, height: number) {
  await page.setViewportSize({ width, height })
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  const cache = page.getByTestId('resource-cache')
  await expect(cache).toBeVisible()
  const cacheLabel = cache.getByText('缓存空间', { exact: true })
  const labelGeometry = await cacheLabel.evaluate(element => ({
    height: element.getBoundingClientRect().height,
    lineHeight: Number.parseFloat(getComputedStyle(element).lineHeight),
  }))
  expect(labelGeometry.height).toBeLessThanOrEqual(labelGeometry.lineHeight * 2 + 1)
  expect((await cache.boundingBox())!.height).toBeLessThan(240)
  if (process.env.DASHBOARD_REVIEW_SCREENSHOTS === '1') {
    const directory = process.env.DASHBOARD_REVIEW_DIR ?? 'test-results/dashboard-review'
    await mkdir(directory, { recursive: true })
    await page.screenshot({ path: `${directory}/${scenario}-${width}x${height}-mock.png`, fullPage: true })
  }
}

test('Overview with no usage evidence offers a real connection flow without inventing first use', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': measuredIdle,
    'GET /api/v1/admin/dashboard': emptyDashboard,
    'GET /api/v1/admin/runtime': emptyRuntime,
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const onboarding = page.locator('[data-dashboard-onboarding]')
  await expect(onboarding.getByRole('heading')).toHaveText('当前暂无请求')
  await expect(onboarding).toContainText('暂无记录不代表实例从未使用')
  await expect(page.locator('[data-dashboard-status-strip]')).toContainText('服务正常')
  const connect = onboarding.getByRole('link', { name: '接入客户端', exact: true })
  await expect(connect).toHaveAttribute('href', '/admin/connect')
  await expect(onboarding.getByRole('link', { name: '查看接入文档', exact: true })).toHaveAttribute('href', '/')
  expect((await connect.boundingBox())!.y).toBeLessThan(900)

  const cache = page.getByTestId('resource-cache')
  await expect(cache).toContainText('0 B')
  await expect(cache).toContainText('未设置缓存配额')
  await expect(cache.locator('[data-progress-ratio]')).toHaveCount(0)
  await expect(page.locator('[data-dashboard-benefits]')).toContainText('0 B')
  await expect(page.locator('[data-dashboard-benefits]')).toContainText('暂无请求样本')
  await expect(page.locator('[data-dashboard-benefits]')).not.toContainText('n=0')

  for (const viewport of [{ width: 1440, height: 900 }, { width: 1920, height: 1080 }, { width: 390, height: 844 }, { width: 320, height: 800 }]) {
    await reviewViewport(page, 'initialization-dark', viewport.width, viewport.height)
  }
  // A known positive quota may show a neutral track, but zero usage has no
  // colored fill. Unknown/unset quotas above do not draw a track at all.
  await mockAdminApi(page, { 'GET /api/v1/admin/runtime': { ...emptyRuntime, cache: { ...emptyRuntime.cache, quota_bytes: 21_474_836_480 } } })
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  const track = cache.locator('[data-progress-ratio="0"]')
  await expect(track).toBeVisible()
  await expect(track.locator(':scope > *')).toHaveCount(0)
  await connect.click()
  await expect(page).toHaveURL(/\/admin\/connect$/)
  await expect(page.getByRole('main')).toContainText(`pip config set global.index-url ${new URL(page.url()).origin}/pypi/simple/`)
})

test('Overview keeps the normal dashboard for an empty range with positive historical evidence', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  await mockAdminApi(page, {
    'GET /api/v1/now': { ...measuredIdle, last_activity: { seconds_ago: 7200, adapter_type: 'npm', hit: true } },
    'GET /api/v1/admin/dashboard': (request: Request) => ({
      ...emptyDashboard,
      range: { ...emptyDashboard.range, key: new URL(request.url()).searchParams.get('range') },
      prev: populatedDashboard.window,
    }),
    'GET /api/v1/admin/runtime': emptyRuntime,
  })
  await page.goto('/admin')
  await expect(page.locator('[data-dashboard-empty-period]')).toContainText('该时段暂无请求')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
  await expect(page.locator('[data-dashboard-benefits]')).toBeVisible()
  await page.locator('[data-dashboard-empty-period]').getByRole('button', { name: '查看最近 30 天' }).click()
  await expect(page.getByRole('group', { name: '统计周期' }).getByRole('button', { name: '30 天' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-dashboard-empty-period]')).toContainText('该时段暂无请求')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
})

test('Overview preserves a failed storage readiness check before connection guidance', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /ready': { status: 503, body: { status: 'not_ready', version: 'dev', checks: { database: 'ready', storage: 'unavailable' } } },
    'GET /api/v1/now': measuredIdle,
    'GET /api/v1/admin/dashboard': emptyDashboard,
    'GET /api/v1/admin/runtime': emptyRuntime,
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')
  const alerts = page.locator('[data-dashboard-alerts]')
  await expect(alerts).toContainText('存储')
  await expect(alerts).toContainText('下载')
  await expect(page.locator('[data-dashboard-status-strip]')).toContainText('下载服务未就绪')
  await expect(alerts.getByRole('link')).toHaveCount(1)
  expect((await alerts.boundingBox())!.y).toBeLessThan((await page.locator('[data-dashboard-onboarding]').boundingBox())!.y)
  await reviewViewport(page, 'storage-unavailable-dark', 1440, 900)
})

test('Overview statistics failure stays distinct from zero and can recover', async ({ page }) => {
  let unavailable = true
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': () => unavailable
      ? { status: 503, body: { code: 'UNAVAILABLE', message: 'fixture statistics unavailable' } }
      : populatedDashboard,
  })
  await page.goto('/admin')
  const statistics = page.locator('[data-dashboard-period]')
  await expect(statistics).toContainText('统计暂不可用')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
  await expect(page.locator('[data-dashboard-status-strip]')).toContainText('服务正常')
  await expect(statistics).not.toContainText('0 B')
  unavailable = false
  await statistics.getByRole('button', { name: /重试/ }).click()
  await expect(page.locator('[data-dashboard-benefits]')).toContainText('92.0%')
  await expect(statistics).not.toContainText('统计暂不可用')
})

test('Overview failed range changes keep the old range and do not show onboarding', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': (request: Request) => new URL(request.url()).searchParams.get('range') === '1h'
      ? { ...populatedDashboard, range: { ...populatedDashboard.range, key: '1h' } }
      : { status: 503, body: { code: 'UNAVAILABLE', message: 'fixture range unavailable' } },
  })
  await page.goto('/admin')
  await expect(page.locator('[data-dashboard-benefits]')).toContainText('92.0%')
  await page.getByRole('group', { name: '统计周期' }).getByRole('button', { name: '7 天' }).click()
  await expect(page.locator('[data-dashboard-period]')).toHaveAttribute('data-period-range', '1h')
  await expect(page.getByText(/1 小时最新刷新失败/)).toBeVisible()
  await expect(page.locator('[data-dashboard-benefits]')).toContainText('92.0%')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
})

test('Overview populated benefits, download path and recent requests fit desktop and mobile', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedPoints },
    'GET /api/v1/now': { ...measuredIdle, last_activity: { seconds_ago: 8, adapter_type: 'npm', hit: true }, rate: { ...measuredIdle.rate, has_data: true, service_bytes_per_sec: 2_097_152, origin_bytes_per_sec: 180_000 } },
    'GET /api/v1/admin/logs': {
      items: ['react', 'typescript', 'vite'].map((packageName, index) => ({
        id: index + 1, adapter_type: 'npm', package_name: packageName, version: '1.0.0',
        method: 'GET', cache_key: `npm/${packageName}`, hit: index !== 1, cache_result: index === 1 ? 'miss' : 'hit',
        upstream: index === 1 ? 'npmjs' : '', latency_ms: index === 1 ? 220 : 18,
        status_code: 200, bytes_sent: 2_097_152, client_ip: '192.168.1.100', created_at: new Date(Date.now() - index * 60_000).toISOString(),
      })),
      total: 3, page: 1, page_size: 5,
    },
  })
  await page.goto('/admin')
  const benefits = page.locator('[data-dashboard-benefits]')
  await expect(benefits).toContainText('92.0%')
  await expect(benefits).toContainText('640.0 MB')
  await expect(benefits).toContainText('估算')
  await expect(page.getByTestId('resource-memory')).toContainText('内存占用')
  await expect(page.locator('[data-recent-request-id]')).toHaveCount(3)
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
  for (const viewport of [{ width: 1440, height: 900 }, { width: 1920, height: 1080 }, { width: 390, height: 844 }, { width: 320, height: 800 }]) {
    await reviewViewport(page, 'populated-dark', viewport.width, viewport.height)
  }
  await setUiPreferences(page, 'light', 'zh')
  await page.reload()
  await expect(benefits).toContainText('92.0%')
  await reviewViewport(page, 'populated-light', 1440, 900)
})

// The Overview must distinguish "never collected" from a measured zero, and a
// platform that cannot sample a resource must say so without leaking an
// English server reason into the tile or printing a unit next to a dash.
test('Overview separates uncollected data from a measured zero', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': unmeasuredNow,
    'GET /api/v1/admin/runtime': {
      ...unsupportedRuntime,
      cache: { ...unsupportedRuntime.cache, logical_bytes: 0, entries: null, packages: null },
    },
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
  await expect(servedTotal).toContainText('0 B')
  await expect(servedTotal).toContainText('0 次请求')

  // Live rails are not collected yet: dash, localized detail, no unit.
  const serviceFlow = page.locator('[data-testid="traffic-service-flow"]')
  await expect(serviceFlow).toContainText('—')
  await expect(serviceFlow).toContainText('未采集')
  await expect(serviceFlow).not.toContainText('次/秒')

  // A failed first inventory query carries null counts and a zero fallback
  // byte field. It must remain unknown instead of claiming an empty cache.
  const cache = page.getByTestId('resource-cache')
  await expect(cache.locator('.dash-metric-value')).toHaveText('—')
  await expect(cache).toContainText('缓存库存尚未采集成功')
  await expect(cache).not.toContainText('0 B')
  await expect(cache.locator('[data-progress-ratio]')).toHaveCount(0)

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
