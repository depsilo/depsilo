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
  const cacheLabel = cache.getByText('当前缓存占用', { exact: true })
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
  await expect(onboarding).not.toContainText('历史记录不完整，无法确认是否首次使用')
  await expect(page.locator('[data-dashboard-status]')).toContainText('服务正常')
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
  await expect(track).toHaveRole('meter')
  await expect(track.locator('[data-slot="meter-indicator"]')).toHaveCSS('width', '0px')
  await connect.click()
  await expect(page).toHaveURL(/\/admin\/connect$/)
  await expect(page.getByRole('main')).toContainText(`python -m pip config --user set global.index-url '${new URL(page.url()).origin}/pypi/simple/'`)
})

test('Overview first-use guidance follows durable history and never regresses after a recorded success', async ({ page }) => {
  let usage: DashboardResponse['usage'] = { state: 'never-used', first_successful_request_at: null }
  let unavailable = false
  let requestWindow = emptyPeriod
  let now = measuredIdle
  await mockAdminApi(page, {
    'GET /api/v1/now': () => now,
    'GET /api/v1/admin/runtime': emptyRuntime,
    'GET /api/v1/admin/dashboard': () => unavailable
      ? { status: 500, body: { code: 'FAILED', message: 'usage history unavailable' } }
      : ({ ...emptyDashboard, window: requestWindow, usage }),
  })
  await page.goto('/admin')
  const guide = page.locator('[data-dashboard-onboarding]')
  await expect(guide.getByRole('heading')).toHaveText('等待首次依赖下载')
  await expect(guide.getByRole('link', { name: '接入客户端', exact: true })).toHaveAttribute('href', '/admin/connect')
  await expect(guide).not.toContainText('历史记录不完整，无法确认是否首次使用')
  await reviewViewport(page, 'first-use-dark', 1440, 900)
  await reviewViewport(page, 'first-use-dark', 390, 844)
  await page.setViewportSize({ width: 1440, height: 900 })

  unavailable = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText(/30 天最新刷新失败/)).toBeVisible()
  await expect(guide).toHaveCount(0)

  // A legacy instance with unknown lifetime coverage still needs neutral copy.
  unavailable = false
  usage = { state: 'unknown', first_successful_request_at: null }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(guide.getByRole('heading')).toHaveText('当前暂无请求')
  await expect(guide).not.toContainText('历史记录不完整，无法确认是否首次使用')

  // Successful metadata requests and latest-activity records are not completed
  // artifact downloads. Complete lifecycle evidence keeps the download guide.
  usage = { state: 'never-used', first_successful_request_at: null }
  requestWindow = {
    ...emptyPeriod,
    total_requests: 2, miss_count: 2, miss_requests: 2,
    bytes_served: 1024, miss_bytes: 1024, avg_latency_ms: 25, avg_miss_latency_ms: 25,
    upstream_requests: 2, upstream_bytes: 1024,
  }
  now = { ...measuredIdle, last_activity: { seconds_ago: 1, adapter_type: 'npm', hit: false, package_name: 'react' } }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('traffic-served-total')).toContainText('2 次请求')
  await expect(guide.getByRole('heading')).toHaveText('等待首次依赖下载')

  // Retention can remove every visible request while the durable usage marker
  // still proves success; no current-period metric is needed to recover it.
  usage = { state: 'used', first_successful_request_at: '2026-09-01T00:00:00Z' }
  requestWindow = emptyPeriod
  now = measuredIdle
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(guide).toHaveCount(0)
  await expect(page.locator('[data-dashboard-empty-period]')).toContainText('该时段暂无请求')

  usage = { state: 'never-used', first_successful_request_at: null }
  const staleResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/admin/dashboard')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await staleResponse
  await expect(page.getByRole('button', { name: '刷新', exact: true })).not.toHaveAttribute('aria-busy', 'true')
  await expect(guide).toHaveCount(0)
  await expect(page.locator('[data-dashboard-empty-period]')).toContainText('该时段暂无请求')
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
  await expect(page.locator('[data-dashboard-alerts]')).toHaveCount(0)
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toContainText('下载服务未就绪')
  const guide = page.locator('[data-dashboard-onboarding]')
  await expect(guide.getByRole('alert')).toContainText('数据库或存储问题')
  await expect(guide.getByRole('link', { name: '接入客户端', exact: true })).toHaveAttribute('href', '/admin/connect')
  await reviewViewport(page, 'storage-unavailable-dark', 1440, 900)
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toContainText('缓存存储健康检查失败')
  await expect(details.getByRole('link', { name: '查看实例设置' })).toHaveAttribute('href', '/admin/settings')
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
  await expect(page.locator('[data-dashboard-status]')).toContainText('状态未知')
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
  await page.getByLabel('查看 CPU 口径').click()
  await expect(page.getByText('cpu sampling is not available on this platform')).toBeVisible()
})

test('Overview opens grouped upstream checks in one click without repeating banners', async ({ page }) => {
  const requests: string[] = []
  page.on('request', request => requests.push(`${request.method()} ${new URL(request.url()).pathname}`))
  await mockAdminApi(page, {
    'GET /api/v1/admin/capabilities/summary': {
      version: 'dev-20261020t070013z',
      commit: 'bd6b4808928f6892f8c0728b1267a702e86920c3-dirty',
      build_date: '2026-10-20T07:00:13Z',
    },
    'GET /api/v1/now': { ...measuredIdle, status: 'down', upstreams: { total: 1, healthy: 0 } },
    'GET /api/v1/admin/dashboard': {
      ...emptyDashboard,
      upstreams: [{ id: 42, name: 'official', adapter: 'npm', healthy: false, avg_latency_ms: 1600, success_rate: 0.2 }],
    },
    'GET /api/v1/admin/runtime': { ...emptyRuntime, cache: { ...emptyRuntime.cache, quota_bytes: 21_474_836_480 } },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('1 个上游需关注')
  await expect(page.locator('[data-admin-version]').first()).toHaveText('dev·bd6b480')
  await expect(page.locator('[data-admin-page-actions]')).toContainText('1 个上游需关注')
  await expect(page.locator('[data-dashboard-alerts], [data-dashboard-status-strip]')).toHaveCount(0)
  const guide = page.locator('[data-dashboard-onboarding]')
  await expect(guide).not.toContainText('历史记录不完整')
  await expect(guide.getByRole('alert')).toHaveCount(0)
  await expect(guide.getByRole('link', { name: '接入客户端', exact: true })).toBeVisible()

  const chart = page.locator('[data-dashboard-trends]')
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(chart).toContainText('当前时段暂无趋势数据')
  await expect(recent).toContainText('暂无可展示的请求记录')
  const [chartBox, recentBox] = await Promise.all([chart.boundingBox(), recent.boundingBox()])
  expect(chartBox!.height).toBeLessThan(240)
  expect(recentBox!.height).toBeLessThan(240)
  expect(Math.abs(chartBox!.width - recentBox!.width)).toBeLessThan(2)
  expect(Math.abs(chartBox!.x - recentBox!.x)).toBeLessThan(2)
  expect(recentBox!.y).toBeGreaterThanOrEqual(chartBox!.y + chartBox!.height)
  const capacity = page.locator('[data-cache-capacity-group]')
  const amount = (await capacity.locator('.dash-metric-value').boundingBox())!
  const bar = (await capacity.getByRole('meter').boundingBox())!
  expect(bar.x - amount.x - amount.width).toBeLessThan(120)
  await expect(capacity.getByRole('meter')).toHaveAttribute('aria-valuenow', '0')
  await expect(capacity.getByRole('meter').locator('[data-slot="meter-indicator"]')).toHaveCSS('width', '0px')
  await reviewViewport(page, 'refined-overview-dark', 1440, 900)
  await reviewViewport(page, 'refined-overview-wide', 1920, 1080)
  await reviewViewport(page, 'refined-overview-mobile', 320, 800)
  await page.setViewportSize({ width: 1440, height: 900 })

  await status.focus()
  await page.keyboard.press('Enter')
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toBeVisible()
  await expect(details.locator('[data-upstream-health-id]')).toHaveCount(1)
  const upstream = details.locator('[data-upstream-health-id="42"]')
  await expect(upstream).toContainText('official')
  await expect(upstream).toContainText('npm · #42 · 2 项异常检查')
  await expect(upstream).toContainText('不可用')
  await expect(upstream).toContainText('1,600 ms · 响应较慢')
  await expect(details).toContainText('1 个上游需关注 · 2 项异常检查')
  await expect(details).toContainText('运行时移动平均')
  await expect(details).toContainText('接口未提供检查时间')
  await expect(details).toContainText('状态数据获取于')
  await expect(details.getByRole('link', { name: '查看上游源' })).toHaveAttribute('href', '/admin/upstreams')
  const refreshes = requests.filter(request => request === 'GET /api/v1/admin/dashboard').length
  await details.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect.poll(() => requests.filter(request => request === 'GET /api/v1/admin/dashboard').length).toBeGreaterThan(refreshes)
  expect(requests.every(request => request.startsWith('GET '))).toBe(true)
  if (process.env.DASHBOARD_REVIEW_SCREENSHOTS === '1') {
    const directory = process.env.DASHBOARD_REVIEW_DIR ?? 'test-results/dashboard-review'
    await page.screenshot({ path: `${directory}/refined-status-details-dark-1440x900-mock.png`, fullPage: false })
    await page.setViewportSize({ width: 320, height: 800 })
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
    await page.screenshot({ path: `${directory}/refined-status-details-mobile-320x800-mock.png`, fullPage: false })
  }
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()

  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.reload()
  await expect(status).toHaveText('1 upstream needs attention')
  await expect(page.getByRole('link', { name: 'Connect a client', exact: true })).toBeVisible()
  await status.click()
  const englishDetails = page.getByRole('dialog', { name: 'Service status details' })
  await expect(englishDetails).toContainText('1 affected upstream · 2 failing checks')
  await expect(englishDetails.locator('[data-upstream-health-id="42"]')).toContainText('1,600 ms · Slow response')
  await expect(englishDetails.getByRole('button', { name: 'Refresh status' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

test('Overview preserves the latest upstream observation across cached range changes', async ({ page }) => {
  let failWide = false
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '30d'))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': async (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range')
      if (range === '30d' && failWide) return { status: 500, body: { code: 'FAILED', message: 'fixture refresh failed' } }
      return {
        ...populatedDashboard,
        range: { ...populatedDashboard.range, key: range },
        upstreams: [{ id: 7, name: 'official', adapter: 'npm', healthy: true, avg_latency_ms: range === '1h' ? 1600 : 100, success_rate: 1 }],
      }
    },
  })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('服务正常')
  await page.getByRole('button', { name: '1 小时', exact: true }).click()
  await expect(status).toHaveText('1 个上游需关注')
  failWide = true
  await page.getByRole('button', { name: '30 天', exact: true }).click()
  await expect(status).not.toContainText('服务正常')
  await expect(page.locator('[data-dashboard-period]')).toContainText('最新刷新失败')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details.locator('[data-upstream-health-id="7"]')).toContainText('1,600 ms')
  await expect(details.locator('[data-upstream-health-id="7"]')).toContainText('上次状态')
  await expect(details).toContainText('上游健康状态刷新失败')
})

test('Overview keeps an idle policy payload distinct from a failed status refresh', async ({ page }) => {
  let fail = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': () => fail
      ? { status: 500, body: { code: 'FAILED', message: 'fixture policy read failed' } }
      : { status: 'unavailable', degraded: false, using_stale_snapshot: false, refresh_failures: 0, snapshot_age_seconds: 0, on_load_error: 'block' },
  })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('服务正常')
  fail = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(status).toHaveText('状态未知')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toContainText('包规则健康状态刷新失败')
  await expect(details).not.toContainText('包规则状态暂时不可用')
})

test('Overview keeps a failed health refresh stale across ranges until fresh success, including unchanged payloads', async ({ page }) => {
  let fail = false
  let holdWide = false
  let markWideStarted!: () => void
  let releaseWide!: () => void
  const wideStarted = new Promise<void>(resolve => { markWideStarted = resolve })
  const wideReleased = new Promise<void>(resolve => { releaseWide = resolve })
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '30d'))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': async (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range')
      if (fail) return { status: 500, body: { code: 'FAILED', message: 'fixture health read failed' } }
      if (range === '30d' && holdWide) {
        markWideStarted()
        await wideReleased
      }
      return {
        ...populatedDashboard,
        range: { ...populatedDashboard.range, key: range },
        upstreams: [{ id: 7, name: 'official', adapter: 'npm', healthy: true, avg_latency_ms: 100, success_rate: 1 }],
      }
    },
  })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('服务正常')
  await page.getByRole('button', { name: '1 小时', exact: true }).click()
  await expect(page.locator('[data-dashboard-period]')).toHaveAttribute('data-period-range', '1h')
  fail = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(status).toHaveText('状态未知')

  fail = false
  holdWide = true
  await page.getByRole('button', { name: '30 天', exact: true }).click()
  await wideStarted
  try {
    await expect(status).toHaveText('状态未知')
    await status.click()
    const details = page.getByRole('dialog', { name: '服务状态详情' })
    await expect(details).toContainText('上游健康状态刷新失败')
    await expect(details.locator('[data-upstream-health-id="7"]')).toContainText('上次状态')
    await page.keyboard.press('Escape')
  } finally {
    releaseWide()
  }
  await expect(status).toHaveText('服务正常')

  // A revalidated identical JSON payload may keep its object identity through
  // query structural sharing, but it must still supersede the failed read.
  fail = true
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(status).toHaveText('状态未知')
  fail = false
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(status).toHaveText('服务正常')
  await status.click()
  await expect(page.getByRole('dialog', { name: '服务状态详情' })).not.toContainText('上游健康状态刷新失败')
})
