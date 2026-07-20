import type { Locator, Page, Request } from '@playwright/test'
import {
  adminApiDefaults,
  expect,
  mockAdminApi,
  test,
  type AdminApiOverrides,
  type JsonValue,
  type MockHttpResponse,
} from './fixtures/admin-api'

interface QueryCase {
  path: string
  endpoint: string
  success: JsonValue | MockHttpResponse
  siblings?: AdminApiOverrides
}

const populatedToken = {
  id: 8, user_id: 1, name: 'fixture-ci', permissions: 'readonly',
  expires_at: null, last_used_at: null, created_at: '2026-07-10T00:00:00Z',
}

const populatedDistribution = {
  total_size: 1024, max_size: 4096,
  by_type: [{ type: 'pypi', size: 1024, file_count: 1 }],
  top_packages: [{ name: 'requests', size: 1024, type: 'pypi', hit_count: 2 }],
}

const primaryQueries = [
  { path: '/admin', endpoint: 'GET /api/v1/admin/dashboard', success: adminApiDefaults['GET /api/v1/admin/dashboard'] },
  { path: '/admin/bandwidth', endpoint: 'GET /api/v1/admin/bandwidth', success: adminApiDefaults['GET /api/v1/admin/bandwidth'] },
  { path: '/admin/logs', endpoint: 'GET /api/v1/admin/logs', success: adminApiDefaults['GET /api/v1/admin/logs'] },
  { path: '/admin/audit', endpoint: 'GET /api/v1/admin/audit-logs', success: adminApiDefaults['GET /api/v1/admin/audit-logs'] },
  { path: '/admin/quarantine', endpoint: 'GET /api/v1/admin/quarantine/events', success: adminApiDefaults['GET /api/v1/admin/quarantine/events'] },
  { path: '/admin/cache', endpoint: 'GET /api/v1/admin/cache', success: adminApiDefaults['GET /api/v1/admin/cache'], siblings: { 'GET /api/v1/admin/cache/distribution': populatedDistribution } },
  { path: '/admin/upstreams', endpoint: 'GET /api/v1/admin/upstreams', success: adminApiDefaults['GET /api/v1/admin/upstreams'] },
  { path: '/admin/users', endpoint: 'GET /api/v1/admin/users', success: adminApiDefaults['GET /api/v1/admin/users'], siblings: { 'GET /api/v1/admin/tokens': [populatedToken] } },
  { path: '/admin/license', endpoint: 'GET /api/v1/admin/license/status', success: adminApiDefaults['GET /api/v1/admin/license/status'] },
  { path: '/admin/rules', endpoint: 'GET /api/v1/admin/rules', success: adminApiDefaults['GET /api/v1/admin/rules'] },
  { path: '/admin/security', endpoint: 'GET /api/v1/admin/security/dashboard', success: adminApiDefaults['GET /api/v1/admin/security/dashboard'] },
  { path: '/admin/projects', endpoint: 'GET /api/v1/admin/projects', success: { items: [], total: 0 } },
] satisfies readonly QueryCase[]

async function expectPrimarySiblingVisible(page: Page, path: string) {
  if (path === '/admin/users') await expect(page.getByText('fixture-ci')).toBeVisible()
  if (path === '/admin/cache') await expect(page.getByText('requests')).toBeVisible()
}

for (const query of primaryQueries) {
  test(`${query.path} recovers from an initial 500 only after manual Retry`, async ({ page }) => {
    let calls = 0
    await mockAdminApi(page, {
      ...query.siblings,
      [query.endpoint]: () => {
        calls += 1
        return calls === 1
          ? { status: 500, body: { code: 'FAILED', message: 'fixture initial failure' } }
          : query.success
      },
    })
    await page.goto(query.path)
    const error = page.getByRole('alert').filter({ hasText: 'fixture initial failure' })
    await expect(error).toBeVisible()
    await expectPrimarySiblingVisible(page, query.path)
    await expect.poll(() => calls).toBe(1)
    await expect(page.getByText(/暂无|没有数据|暂无数据/)).toHaveCount(0)
    await error.getByRole('button', { name: /重试/ }).click()
    await expect.poll(() => calls).toBe(2)
    await expect(error).toHaveCount(0)
    await expect(page.locator('h1')).toBeVisible()
  })

  test(`${query.path} renders 403 as permission denied rather than empty`, async ({ page }) => {
    await mockAdminApi(page, {
      ...query.siblings,
      [query.endpoint]: { status: 403, body: { code: 'FORBIDDEN', message: 'fixture forbidden' } },
    })
    await page.goto(query.path)
    await expect(page.getByRole('alert')).toContainText(/权限|permission/i)
    await expectPrimarySiblingVisible(page, query.path)
    await expect(page.getByText(/暂无|没有数据|暂无数据/)).toHaveCount(0)
  })
}

test('Projects 402 renders the Pro upgrade callout rather than empty or permission denied', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { status: 402, body: { code: 'PRO_REQUIRED', message: 'Pro required' } },
  })
  await page.goto('/admin/projects')
  await expect(page.getByText(/多项目工作区.*Pro/)).toBeVisible()
  await expect(page.getByRole('link', { name: /购买|升级|终身/ })).toBeVisible()
  await expect(page.getByText(/暂无项目|no projects/i)).toHaveCount(0)
  await expect(page.getByText(/权限不足|permission denied/i)).toHaveCount(0)
})

async function refocus(page: Page) {
  const other = await page.context().newPage()
  await other.goto('about:blank')
  await other.bringToFront()
  await page.bringToFront()
  await page.evaluate(() => window.dispatchEvent(new Event('visibilitychange')))
  await other.close()
}

async function navigateClient(page: Page, path: string) {
  await page.evaluate((nextPath) => {
    window.history.pushState({}, '', nextPath)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, path)
}

async function pausePollingClock(page: Page) {
  // Resolve both lazy route trees with the default fixtures, then unmount the
  // admin tree before replacing browser timers. Client-side navigation keeps
  // those modules resolved in this document while disposing Dashboard and
  // NowStrip's real-time polling observers. The test mounts them again after
  // installing its API overrides, so every observed timer belongs to the
  // paused Playwright clock rather than to a Suspense fallback or old query.
  await page.goto('/')
  await navigateClient(page, '/admin')
  await expect(page.locator('[data-query-key="dashboard-trends"]')).toBeVisible()
  await expect(page.locator('[data-query-key="now"]')).toBeVisible()
  await navigateClient(page, '/')
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)
  // Move forward so the warm-up queries are stale on remount; moving the fake
  // clock behind their real dataUpdatedAt would make React Query treat the
  // cached defaults as fresh and skip the test-specific first request.
  await page.clock.install({ time: new Date('2099-01-01T00:00:00Z') })
  await page.clock.pauseAt(new Date('2099-01-01T00:00:01Z'))
}

async function settlePausedApp(page: Page, initialCalls: () => number) {
  for (let phase = 0; phase < 3; phase += 1) {
    await page.waitForLoadState('networkidle')
    await page.clock.runFor(100)
  }
  await page.clock.runFor(1_700)
  // Client navigation and React's concurrent scheduler can start the request
  // just after the last fixed clock advance. Wait for the test-specific
  // responder (rather than a same-URL warm-up response), then for its network
  // work to finish, before giving React/Recharts an explicit render window.
  await expect.poll(initialCalls).toBe(1)
  await page.waitForLoadState('networkidle')
  await page.clock.runFor(100)
}

function trendPoints(count: number, requestBase: number, bucketStep: number) {
  return Array.from({ length: count }, (_, index) => {
    const requests = requestBase + index
    const misses = 2 + (index % 2)
    const hits = requests - misses
    return {
      bucket: 1783641600 + index * bucketStep,
      date: '2026-07-10',
      requests,
      hits,
      misses,
      hit_rate: hits / requests,
      bytes_served: requests * 128,
      bytes_hit: hits * 128,
      bytes_miss: misses * 128,
      sum_latency_ms: requests * (10 + index),
      avg_latency_ms: 10 + index,
      errors: index % 2,
    }
  })
}

async function trendBucketLabel(page: Page, bucket: number, range: '1h' | '30d') {
  return page.evaluate(({ value, showSeconds }) => {
    const date = new Date(value * 1000)
    const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
    return date.toLocaleString(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: showSeconds ? '2-digit' : undefined,
      timeZoneName: 'short',
      timeZone,
    })
  }, { value: bucket, showSeconds: range === '1h' })
}

async function expectTrendTooltipLabel(page: Page, chart: Locator, expected: string, unexpected: string) {
  await page.mouse.move(0, 0)
  await chart.hover({ position: { x: 40, y: 80 } })
  const label = chart.locator('.recharts-tooltip-wrapper p').first()
  await expect(label).toHaveText(expected)
  await expect(label).not.toHaveText(unexpected)
}

test('Dashboard trends keep the previous chart while an uncached range loads', async ({ page }) => {
  const initialPoints = trendPoints(4, 12, 10)
  const nextPoints = trendPoints(6, 24, 2 * 60 * 60)
  const requestStarted = deferred<void>()
  const response = deferred<{ points: typeof nextPoints }>()
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range')
      if (range !== '30d') return { points: initialPoints }
      requestStarted.resolve()
      return response.promise
    },
  })

  await page.goto('/admin')
  const trends = page.locator('[data-query-key="dashboard-trends"]')
  const chart = trends.locator('.recharts-wrapper')
  const ranges = page.getByRole('group', { name: /活动趋势|Activity Trend/ })
  const nextRange = ranges.getByRole('button', { name: /30 天|30d/i })
  const oneHourLabel = await trendBucketLabel(page, initialPoints[0].bucket, '1h')
  const thirtyDayLabel = await trendBucketLabel(page, initialPoints[0].bucket, '30d')
  await expect(chart).toBeVisible()
  await nextRange.click()
  await requestStarted.promise

  await expect(chart).toBeVisible()
  await expect(ranges).toBeVisible()
  await expect(nextRange).toHaveAttribute('aria-pressed', 'true')
  await expect(trends).toHaveAttribute('aria-busy', 'true')
  const previousPath = await chart.locator('.recharts-area-curve').first().getAttribute('d')
  expect(previousPath?.match(/L/g) ?? []).toHaveLength(initialPoints.length - 1)
  await expectTrendTooltipLabel(page, chart, oneHourLabel, thirtyDayLabel)

  response.resolve({ points: nextPoints })
  await expect(trends).toHaveAttribute('aria-busy', 'false')
  await expect(nextRange).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(async () => {
    const path = await chart.locator('.recharts-area-curve').first().getAttribute('d')
    return path?.match(/L/g)?.length ?? 0
  }).toBe(nextPoints.length - 1)
  await expectTrendTooltipLabel(page, chart, thirtyDayLabel, oneHourLabel)
})

test('Dashboard trends keep the previous chart and warn when an uncached range fails', async ({ page }) => {
  const initialPoints = trendPoints(4, 12, 10)
  const requestStarted = deferred<void>()
  const response = deferred<MockHttpResponse>()
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range')
      if (range !== '30d') return { points: initialPoints }
      requestStarted.resolve()
      return response.promise
    },
  })

  await page.goto('/admin')
  const trends = page.locator('[data-query-key="dashboard-trends"]')
  const chart = trends.locator('.recharts-wrapper')
  const ranges = page.getByRole('group', { name: /活动趋势|Activity Trend/ })
  const nextRange = ranges.getByRole('button', { name: /30 天|30d/i })
  const oneHourLabel = await trendBucketLabel(page, initialPoints[0].bucket, '1h')
  const thirtyDayLabel = await trendBucketLabel(page, initialPoints[0].bucket, '30d')
  await expect(chart).toBeVisible()
  await nextRange.click()
  await requestStarted.promise
  await expect(chart).toBeVisible()
  await expect(trends).toHaveAttribute('aria-busy', 'true')

  response.resolve({ status: 500, body: { code: 'FAILED', message: 'fixture range failure' } })
  await expect(trends).toHaveAttribute('aria-busy', 'false')
  await expect(chart).toBeVisible()
  await expect(ranges).toBeVisible()
  await expect(nextRange).toHaveAttribute('aria-pressed', 'true')
  await expect(trends).toContainText(/陈旧|已过期|stale/i)
  const retainedPath = await chart.locator('.recharts-area-curve').first().getAttribute('d')
  expect(retainedPath?.match(/L/g) ?? []).toHaveLength(initialPoints.length - 1)
  await expectTrendTooltipLabel(page, chart, oneHourLabel, thirtyDayLabel)
})

test('Dashboard trends poll at range-specific intervals', async ({ page }) => {
  await pausePollingClock(page)
  const calls: Record<string, number> = {}
  const point = {
    bucket: 1783641600, date: '2026-07-10', requests: 12, hits: 10, misses: 2,
    hit_rate: 0.8333, bytes_served: 1024, bytes_hit: 800, bytes_miss: 224,
    sum_latency_ms: 120, avg_latency_ms: 10, errors: 1,
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': (request: Request) => {
      const range = new URL(request.url()).searchParams.get('range') ?? ''
      calls[range] = (calls[range] ?? 0) + 1
      return { points: [point] }
    },
  })

  await navigateClient(page, '/admin')
  await settlePausedApp(page, () => calls['1h'] ?? 0)
  await expect.poll(() => calls['1h'] ?? 0).toBe(1)
  const ranges = page.getByRole('group', { name: /活动趋势|Activity trends/ })
  await expect(ranges).toBeVisible()

  const oneHourRefetch = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/admin/dashboard/trends' && url.searchParams.get('range') === '1h'
  })
  await refocus(page)
  await (await oneHourRefetch).finished()
  await page.clock.runFor(0)
  await expect.poll(() => calls['1h'] ?? 0).toBe(2)

  await page.clock.runFor(4_999)
  expect(calls['1h']).toBe(2)
  await page.clock.runFor(1)
  await expect.poll(() => calls['1h'] ?? 0).toBe(3)

  const twentyFourHourInitial = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/admin/dashboard/trends' && url.searchParams.get('range') === '24h'
  })
  await ranges.getByRole('button', { name: /24 小时|24 hours/i }).click()
  await (await twentyFourHourInitial).finished()
  await page.clock.runFor(0)
  await expect.poll(() => calls['24h'] ?? 0).toBe(1)
  await page.clock.runFor(14_999)
  expect(calls['24h']).toBe(1)
  await page.clock.runFor(1)
  await expect.poll(() => calls['24h'] ?? 0).toBe(2)
})

test('NowStrip keeps healthy cached data when its focus refetch fails', async ({ page }) => {
  await pausePollingClock(page)
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/now': () => {
      calls += 1
      return calls === 1
        ? { status: 'healthy', last_activity: null, rate: { requests_per_min: 7, ingress_bps: 10, egress_bps: 20 }, upstreams: { healthy: 1, total: 1 } }
        : { status: 500, body: { code: 'FAILED', message: 'now refetch failed' } }
    },
  })
  await navigateClient(page, '/admin')
  await settlePausedApp(page, () => calls)
  const now = page.locator('[data-query-key="now"]')
  await expect(now).toContainText(/健康|healthy/i)
  await refocus(page)
  await page.clock.runFor(100)
  await expect.poll(() => calls).toBe(2)
  await page.waitForLoadState('networkidle')
  await page.clock.runFor(100)
  await expect(now).toContainText(/健康|healthy/i)
  await expect(now).toContainText(/陈旧|已过期|stale/i)
})

test('Dashboard trends keeps its rendered chart when its focus refetch fails', async ({ page }) => {
  await pausePollingClock(page)
  let calls = 0
  const point = {
    bucket: 1783641600, date: '2026-07-10', requests: 12, hits: 10, misses: 2,
    hit_rate: 0.8333, bytes_served: 1024, bytes_hit: 800, bytes_miss: 224,
    sum_latency_ms: 120, avg_latency_ms: 10, errors: 1,
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': () => {
      calls += 1
      return calls === 1
        ? { points: [point] }
        : { status: 500, body: { code: 'FAILED', message: 'trends refetch failed' } }
    },
  })
  await navigateClient(page, '/admin')
  await settlePausedApp(page, () => calls)
  const trends = page.locator('[data-query-key="dashboard-trends"]')
  await expect(trends.locator('.recharts-wrapper')).toBeVisible()
  await refocus(page)
  await page.clock.runFor(100)
  await expect.poll(() => calls).toBe(2)
  await page.waitForLoadState('networkidle')
  await page.clock.runFor(100)
  await expect(trends.locator('.recharts-wrapper')).toBeVisible()
  await expect(trends).toContainText(/陈旧|已过期|stale/i)
})

for (const status of [500, 403] as const) {
  test(`Users stay visible when Tokens initially return ${status}`, async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/admin/users': adminApiDefaults['GET /api/v1/admin/users'],
      'GET /api/v1/admin/tokens': { status, body: { code: status === 403 ? 'FORBIDDEN' : 'FAILED', message: 'fixture tokens failure' } },
    })
    await page.goto('/admin/users')
    await expect(page.getByRole('row', { name: /admin/ })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText(status === 403 ? /权限|permission/i : 'fixture tokens failure')
  })
}

const populatedOverride = {
  id: 4, ecosystem: 'pypi', package: 'false-positive', version: '1.0.0', reason: 'reviewed',
  actor_id: 1, created_at: '2026-07-10T00:00:00Z', expires_at: '2099-07-11T00:00:00Z',
}

for (const status of [500, 403] as const) {
  test(`Blocklist overrides stay visible when status initially returns ${status}`, async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/admin/blocklist/status': { status, body: { code: status === 403 ? 'FORBIDDEN' : 'FAILED', message: 'fixture status failure' } },
      'GET /api/v1/admin/blocklist/overrides': { items: [populatedOverride], now: '2026-07-10T00:00:00Z' },
    })
    await page.goto('/admin/quarantine')
    await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
    await expect(page.getByText('false-positive')).toBeVisible()
    await expect(page.getByRole('alert')).toContainText(status === 403 ? /权限|permission/i : 'fixture status failure')
  })

  test(`Blocklist status stays visible when overrides initially return ${status}`, async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/admin/blocklist/status': { enabled: true, entry_count: 7, last_sync_at: null, last_success_at: null, last_error: '', duration_ms: 0, per_ecosystem: { pypi: 7 }, ecosystems: ['pypi'], running: false, next_sync_at: null },
      'GET /api/v1/admin/blocklist/overrides': { status, body: { code: status === 403 ? 'FORBIDDEN' : 'FAILED', message: 'fixture overrides failure' } },
    })
    await page.goto('/admin/quarantine')
    await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
    await expect(page.getByText('7', { exact: true })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText(status === 403 ? /权限|permission/i : 'fixture overrides failure')
  })
}

test('Dashboard bandwidth failure is explicit while dashboard siblings remain visible', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/bandwidth': { status: 500, body: { code: 'FAILED', message: 'fixture dashboard bandwidth failure' } },
  })
  await page.goto('/admin')
  await expect(page.getByRole('alert').filter({ hasText: 'fixture dashboard bandwidth failure' })).toBeVisible()
  await expect(page.getByText(/热门包|Top packages/)).toBeVisible()
})

test('Cache distribution failure is explicit while cache entries remain visible', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/cache/distribution': { status: 500, body: { code: 'FAILED', message: 'fixture distribution failure' } },
    'GET /api/v1/admin/cache': { items: [{ id: 1, key: 'fixture/cache-key', adapter_type: 'pypi', size: 64, hit_count: 2, last_accessed: '2026-07-10T00:00:00Z' }], total: 1 },
  })
  await page.goto('/admin/cache')
  await expect(page.getByRole('alert').filter({ hasText: 'fixture distribution failure' })).toBeVisible()
  await expect(page.getByText('fixture/cache-key')).toBeVisible()
})

const projectSummary = {
  id: 1, name: 'fixture-project', slug: 'fixture-project', description: 'fixture',
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
  package_count: 1, last_activity_at: '2026-07-10T00:00:00Z',
}

test('Project detail failure is explicit while the selected project context remains mounted', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { items: [projectSummary], total: 1 },
    'GET /api/v1/admin/projects/1': { status: 500, body: { code: 'FAILED', message: 'fixture detail failure' } },
    'GET /api/v1/admin/projects/1/packages': { items: [], total: 0, page: 1 },
  })
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: /查看 fixture-project|View fixture-project/ }).click()
  await expect(page.getByRole('heading', { name: 'fixture-project' })).toBeVisible()
  await expect(page.getByRole('alert').filter({ hasText: 'fixture detail failure' })).toBeVisible()
})

test('Project package failure never renders the successful empty state', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { items: [projectSummary], total: 1 },
    'GET /api/v1/admin/projects/1': { ...projectSummary, proxy_url: 'https://example.test/p/fixture-project', ecosystem_breakdown: { pypi: 1 } },
    'GET /api/v1/admin/projects/1/packages': { status: 500, body: { code: 'FAILED', message: 'fixture packages failure' } },
  })
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: /查看 fixture-project|View fixture-project/ }).click()
  await expect(page.getByRole('alert').filter({ hasText: 'fixture packages failure' })).toBeVisible()
  await expect(page.getByText(/暂无包|No packages/)).toHaveCount(0)
})

// The page clock remains paused after the 2-second render allowance. This keeps
// the 5-second and 30-second polling intervals from consuming the failure that
// each test reserves for the explicit focus transition.

interface MutationCase {
  name: string
  path: string
  endpoint: string
  status: 422 | 500
  fixtures?: AdminApiOverrides
  submit(page: Page): Promise<void>
  retained(page: Page): Locator
}

const webhook = {
  id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/hook', enabled: true,
  events: '*', cooldown_minutes: 30, last_sent_at: null,
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
}

const mutationCases: MutationCase[] = [
  {
    name: 'Cache delete', path: '/admin/cache', endpoint: 'DELETE /api/v1/admin/cache/41', status: 500,
    fixtures: {
      'GET /api/v1/admin/cache': {
        items: [{
          id: 41, key: 'pypi/simple/fixture/index.html', adapter_type: 'pypi', package_name: 'fixture',
          size: 512, hit_count: 3, last_accessed: '2026-07-10T00:00:00Z', expires_at: '2026-07-11T00:00:00Z',
        }],
        total: 1, page: 1, page_size: 20,
      },
    },
    submit: async page => {
      await page.getByRole('button', { name: /删除缓存条目 pypi\/simple\/fixture\/index\.html|Delete cache entry pypi\/simple\/fixture\/index\.html/ }).click()
      await page.getByRole('dialog').getByRole('button', { name: /^删除$|^Delete$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /确认删除|Confirm Delete/ }),
  },
  {
    name: 'Cache cleanup', path: '/admin/cache', endpoint: 'POST /api/v1/admin/cache/cleanup', status: 500,
    submit: async page => {
      await page.getByRole('button', { name: /清理过期/ }).click()
      await page.getByRole('dialog').getByRole('button', { name: /确认清理/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /清理过期缓存/ }),
  },
  {
    name: 'Upstream save', path: '/admin/upstreams', endpoint: 'POST /api/v1/admin/upstreams', status: 422,
    fixtures: {
      'GET /api/v1/admin/upstreams': {
        items: [{
          id: 1, adapter_type: 'pypi', name: 'tuna', url: 'https://pypi.example/simple', proxy: '', priority: 1,
          probe_mode: 'active', probe_interval: '30m', healthy: true, avg_latency_ms: 12, success_rate: 1,
          last_checked_at: '2026-07-10T00:00:00Z', worker_running: true,
          created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
        }],
        total: 1,
      },
    },
    submit: async page => {
      await page.getByRole('button', { name: /添加上游源/ }).click()
      await page.getByLabel(/^名称$/).fill('fixture')
      await page.getByLabel(/^URL$/).fill('https://fixture.example/simple')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加上游源/ }),
  },
  {
    name: 'User save', path: '/admin/users', endpoint: 'POST /api/v1/admin/users', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /添加用户/ }).click()
      await page.getByLabel(/用户名/).fill('fixture-user')
      await page.getByLabel(/^密码$/).fill('fixture-password')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加用户/ }),
  },
  {
    name: 'Token create', path: '/admin/users', endpoint: 'POST /api/v1/admin/tokens', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /生成 Token/ }).click()
      await page.getByRole('dialog').getByLabel(/^名称$/).fill('fixture-token')
      await page.getByRole('dialog').getByRole('button', { name: /^生成$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /生成 Token/ }),
  },
  {
    name: 'License update', path: '/admin/license', endpoint: 'PUT /api/v1/admin/license/key', status: 422,
    submit: async page => {
      await page.getByPlaceholder(/depsilo-/).fill('depsilo-fixture-invalid')
      await page.getByRole('button', { name: /激活/ }).click()
    },
    retained: page => page.getByPlaceholder(/depsilo-/),
  },
  {
    name: 'Rule save', path: '/admin/rules', endpoint: 'POST /api/v1/admin/rules', status: 422,
    submit: async page => {
      await page.getByRole('button', { name: /添加规则/ }).click()
      await page.getByLabel(/包名/).fill('fixture-package')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /添加规则/ }),
  },
  {
    name: 'Security policy save', path: '/admin/security', endpoint: 'PUT /api/v1/admin/security/policies/pypi', status: 422,
    fixtures: {
      'GET /api/v1/admin/security/policies': [{ id: 1, ecosystem: 'pypi', auto_block_enabled: true, min_cvss_score: 8.5, created_by: 'admin', created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }],
    },
    submit: async page => {
      await page.getByRole('tab', { name: /策略/ }).click()
      await page.getByRole('tabpanel').getByRole('button', { name: /PYPI.*保存|PYPI.*Save/ }).click()
    },
    retained: page => page.getByRole('tabpanel'),
  },
  {
    name: 'Project save', path: '/admin/projects', endpoint: 'POST /api/v1/admin/projects', status: 422,
    fixtures: { 'GET /api/v1/admin/projects': { items: [], total: 0 } },
    submit: async page => {
      await page.getByRole('button', { name: /创建项目/ }).first().click()
      await page.getByLabel(/项目名称/).fill('fixture-project')
      await page.getByRole('dialog').getByRole('button', { name: /^保存$/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /创建项目/ }),
  },
  {
    name: 'Webhook test', path: '/admin/settings', endpoint: 'POST /api/v1/admin/webhooks/1/test', status: 500,
    fixtures: { 'GET /api/v1/admin/webhooks': [webhook] },
    submit: async page => {
      await page.getByRole('tab', { name: /Webhook/ }).click()
      await page.getByRole('button', { name: /测试 ops/ }).click()
    },
    retained: page => page.getByText('ops'),
  },
  {
    name: 'Quarantine approval', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/quarantine/approve', status: 422,
    fixtures: {
      'GET /api/v1/admin/quarantine/events': { items: [{ id: 1, ecosystem: 'pypi', package: 'fixture-package', version: '1.0.0', action: 'blocked', reason: 'minimum age', created_at: '2026-07-10T00:00:00Z' }], total: 1 },
    },
    submit: async page => {
      await page.getByRole('button', { name: /^放行$/ }).click()
      await page.getByPlaceholder(/填写理由/).fill('fixture approval reason')
      await page.getByRole('dialog').getByRole('button', { name: /确认放行/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /放行此版本/ }),
  },
  {
    name: 'Blocklist override creation', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/blocklist/overrides', status: 422,
    submit: async page => {
      await page.getByRole('tab', { name: /恶意封锁/ }).click()
      await page.getByRole('button', { name: /添加豁免/ }).click()
      await page.getByPlaceholder(/^包名$/).fill('fixture-package')
      await page.getByPlaceholder(/填写理由/).fill('fixture override reason')
      await page.getByRole('dialog').getByRole('button', { name: /创建豁免/ }).click()
    },
    retained: page => page.getByRole('dialog', { name: /创建恶意封锁豁免/ }),
  },
  {
    name: 'Blocklist sync', path: '/admin/quarantine', endpoint: 'POST /api/v1/admin/blocklist/sync', status: 500,
    submit: async page => {
      await page.getByRole('tab', { name: /恶意封锁/ }).click()
      await page.getByRole('button', { name: /立即同步/ }).click()
    },
    retained: page => page.getByRole('button', { name: /立即同步/ }),
  },
]

for (const mutation of mutationCases) {
  test(`${mutation.name} retains context and never reports success after failure`, async ({ page }) => {
    const message = `fixture ${mutation.name} failure`
    await mockAdminApi(page, {
      ...mutation.fixtures,
      [mutation.endpoint]: { status: mutation.status, body: { code: 'MUTATION_FAILED', message } },
    })
    await page.goto(mutation.path)
    await mutation.submit(page)
    await expect(page.getByRole('alert').filter({ hasText: message })).toBeVisible()
    await expect(mutation.retained(page)).toBeVisible()
    await expect(page.locator('[data-toast-tone="success"]')).toHaveCount(0)
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}

test('Cache cleanup stays busy until success then closes and toasts the service message', async ({ page }) => {
  const cleanup = deferred<void>()
  await mockAdminApi(page, {
    'POST /api/v1/admin/cache/cleanup': async () => {
      await cleanup.promise
      return { message: 'fixture cleanup completed', deleted: 3 }
    },
  })
  await page.goto('/admin/cache')
  await page.getByRole('button', { name: /清理过期|Clean Expired/ }).click()
  const confirm = page.getByRole('dialog').getByRole('button', { name: /确认清理|Confirm/ })
  await confirm.click()
  const cleaning = page.getByRole('dialog').getByRole('button', { name: /清理中|Cleaning/ })
  await expect(cleaning).toHaveAttribute('aria-busy', 'true')
  await expect(cleaning).toBeDisabled()
  cleanup.resolve()
  await expect(page.getByRole('dialog', { name: /清理过期缓存|Clean Expired Cache/ })).toHaveCount(0)
  await expect(page.locator('[data-toast-tone="success"]')).toContainText('fixture cleanup completed')
})

test('Cache delete clears an old failure before the confirmation dialog is reopened', async ({ page }) => {
  const entry = {
    id: 41, key: 'pypi/simple/fixture/index.html', adapter_type: 'pypi', package_name: 'fixture',
    size: 512, hit_count: 3, last_accessed: '2026-07-10T00:00:00Z', expires_at: '2026-07-11T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/cache': { items: [entry], total: 1, page: 1, page_size: 20 },
    'DELETE /api/v1/admin/cache/41': {
      status: 500,
      body: { code: 'CACHE_REMOVE_INCOMPLETE', message: 'fixture delete failure' },
    },
  })
  await page.goto('/admin/cache')

  const openDelete = page.getByRole('button', { name: /删除缓存条目 pypi\/simple\/fixture\/index\.html|Delete cache entry pypi\/simple\/fixture\/index\.html/ })
  await openDelete.click()
  let dialog = page.getByRole('dialog', { name: /确认删除|Confirm Delete/ })
  await dialog.getByRole('button', { name: /^删除$|^Delete$/ }).click()
  await expect(dialog.getByRole('alert')).toContainText('fixture delete failure')
  await dialog.getByRole('button', { name: /取消|Cancel/ }).click()
  await expect(dialog).toHaveCount(0)

  await openDelete.click()
  dialog = page.getByRole('dialog', { name: /确认删除|Confirm Delete/ })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toHaveCount(0)
})

test('Cache cleanup clears an old failure before the confirmation dialog is reopened', async ({ page }) => {
  await mockAdminApi(page, {
    'POST /api/v1/admin/cache/cleanup': {
      status: 500,
      body: { code: 'CACHE_CLEANUP_PARTIAL', message: 'fixture cleanup failure', deleted: 1, failed: 1, reclaimed_bytes: 512 },
    },
  })
  await page.goto('/admin/cache')

  const openCleanup = page.getByRole('button', { name: /清理过期|Clean Expired/ })
  await openCleanup.click()
  let dialog = page.getByRole('dialog', { name: /清理过期缓存|Clean Expired Cache/ })
  await dialog.getByRole('button', { name: /确认清理|Confirm/ }).click()
  await expect(dialog.getByRole('alert')).toContainText('fixture cleanup failure')
  await dialog.getByRole('button', { name: /取消|Cancel/ }).click()
  await expect(dialog).toHaveCount(0)

  await openCleanup.click()
  dialog = page.getByRole('dialog', { name: /清理过期缓存|Clean Expired Cache/ })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toHaveCount(0)
})

test('Rule create inserts the returned entity before closing', async ({ page }) => {
  const createdRule = {
    id: 9, ecosystem: 'pypi', package_name: 'fixture-package', version: '*',
    action: 'deny', reason: 'fixture reason', created_at: '2026-07-10T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/rules': [],
    'POST /api/v1/admin/rules': createdRule,
  })
  await page.goto('/admin/rules')
  await page.getByRole('button', { name: /添加规则|Add Rule/ }).click()
  await page.getByLabel(/包名|Package Name/).fill('fixture-package')
  await page.getByRole('dialog').getByRole('button', { name: /^保存$|^Save$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('row', { name: /fixture-package/ })).toBeVisible()
})

test('Blocklist override create inserts the complete returned entity before closing', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/blocklist/overrides': { items: [], now: '2026-07-10T00:00:00Z' },
    'POST /api/v1/admin/blocklist/overrides': populatedOverride,
  })
  await page.goto('/admin/quarantine')
  await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
  await page.getByRole('button', { name: /添加豁免|Add override/ }).click()
  await page.getByPlaceholder(/^包名$|^Package$/).fill('false-positive')
  await page.getByPlaceholder(/填写理由|Reason/).fill('reviewed')
  await page.getByRole('dialog').getByRole('button', { name: /创建豁免|Create override/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('row', { name: /false-positive/ })).toBeVisible()
})

test('Rule create recovers the list from an initial query failure with the returned entity', async ({ page }) => {
  const createdRule = {
    id: 10, ecosystem: 'npm', package_name: 'recovered-rule', version: '*',
    action: 'deny', reason: 'fixture recovery', created_at: '2026-07-10T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/rules': { status: 500, body: { code: 'FAILED', message: 'fixture rules unavailable' } },
    'POST /api/v1/admin/rules': createdRule,
  })
  await page.goto('/admin/rules')
  await expect(page.getByRole('alert')).toContainText('fixture rules unavailable')
  await page.getByRole('button', { name: /添加规则|Add Rule/ }).click()
  await page.getByLabel(/包名|Package Name/).fill('recovered-rule')
  await page.getByRole('dialog').getByRole('button', { name: /^保存$|^Save$/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('row', { name: /recovered-rule/ })).toBeVisible()
  await expect(page.getByText('fixture rules unavailable')).toHaveCount(0)
})

test('Blocklist override create recovers the list from an initial query failure with the returned entity', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/blocklist/overrides': { status: 500, body: { code: 'FAILED', message: 'fixture overrides unavailable' } },
    'POST /api/v1/admin/blocklist/overrides': populatedOverride,
  })
  await page.goto('/admin/quarantine')
  await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
  await expect(page.getByRole('alert')).toContainText('fixture overrides unavailable')
  await page.getByRole('button', { name: /添加豁免|Add override/ }).click()
  await page.getByPlaceholder(/^包名$|^Package$/).fill('false-positive')
  await page.getByPlaceholder(/填写理由|Reason/).fill('reviewed')
  await page.getByRole('dialog').getByRole('button', { name: /创建豁免|Create override/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByRole('row', { name: /false-positive/ })).toBeVisible()
  await expect(page.getByText('fixture overrides unavailable')).toHaveCount(0)
})

test('License key activation exposes a stable held pending state', async ({ page }) => {
  const activation = deferred<void>()
  await mockAdminApi(page, {
    'PUT /api/v1/admin/license/key': async () => {
      await activation.promise
      return { is_pro: true, source: 'paid', days_left: 0, trial_used: false, trial_available: false, last_checked: '2026-07-10T00:00:00Z', license_key_masked: 'depsilo-***' }
    },
  })
  await page.goto('/admin/license')
  await page.getByPlaceholder(/depsilo-/).fill('depsilo-fixture-valid')
  const activate = page.getByRole('button', { name: /激活|Activate/ })
  await activate.click()
  await expect(activate).toHaveAttribute('aria-busy', 'true')
  await expect(activate).toBeDisabled()
  activation.resolve()
  await expect(page.getByText(/Pro 已激活|Pro activated/)).toBeVisible()
})

test('Blocklist sync exposes a stable held pending state', async ({ page }) => {
  const sync = deferred<void>()
  await mockAdminApi(page, {
    'POST /api/v1/admin/blocklist/sync': async () => {
      await sync.promise
      return { status: 'started' }
    },
  })
  await page.goto('/admin/quarantine')
  await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
  const syncButton = page.getByRole('button', { name: /立即同步|Sync now/ })
  await syncButton.click()
  const syncing = page.getByRole('button', { name: /同步中|Syncing/ })
  await expect(syncing).toHaveAttribute('aria-busy', 'true')
  await expect(syncing).toBeDisabled()
  sync.resolve()
  await expect(page.getByRole('button', { name: /立即同步|Sync now/ })).not.toHaveAttribute('aria-busy', 'true')
})

test('Security policy saves retain per-ecosystem busy, error, and normalized response state', async ({ page }) => {
  const pypiSuccess = deferred<void>()
  const npmFailure = deferred<void>()
  const npmSuccess = deferred<void>()
  let npmCalls = 0
  const policy = (ecosystem: string, score: number) => ({
    id: ecosystem === 'pypi' ? 1 : 2, ecosystem, auto_block_enabled: true, min_cvss_score: score,
    created_by: 'admin', created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
  })
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [policy('pypi', 8.5), policy('npm', 7.5)],
    'PUT /api/v1/admin/security/policies/pypi': async () => {
      await pypiSuccess.promise
      return policy('pypi', 9.7)
    },
    'PUT /api/v1/admin/security/policies/npm': async () => {
      npmCalls += 1
      if (npmCalls === 1) {
        await npmFailure.promise
        return { status: 422, body: { code: 'INVALID_POLICY', message: 'fixture npm rejected' } }
      }
      await npmSuccess.promise
      return policy('npm', 6.4)
    },
  })
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /策略|Policies/ }).click()
  const pypiRow = page.locator('[data-policy-ecosystem="pypi"]')
  const npmRow = page.locator('[data-policy-ecosystem="npm"]')
  const pypiSave = pypiRow.getByRole('button', { name: /PYPI.*保存|PYPI.*Save/ })
  const npmSave = npmRow.getByRole('button', { name: /NPM.*保存|NPM.*Save/ })
  await pypiRow.getByRole('spinbutton').fill('8.1')
  await npmRow.getByRole('spinbutton').fill('7.1')
  await pypiSave.click()
  await expect(pypiRow.getByRole('spinbutton')).toBeDisabled()
  await expect(pypiRow.getByRole('switch')).toBeDisabled()
  await expect(npmRow.getByRole('spinbutton')).toBeEnabled()
  await expect(npmRow.getByRole('switch')).toBeEnabled()
  await npmSave.click()
  await expect(pypiSave).toHaveAttribute('aria-busy', 'true')
  await expect(npmSave).toHaveAttribute('aria-busy', 'true')
  await expect(pypiSave).toBeDisabled()
  await expect(npmSave).toBeDisabled()
  await expect(npmRow.getByRole('spinbutton')).toBeDisabled()
  await expect(npmRow.getByRole('switch')).toBeDisabled()

  npmFailure.resolve()
  await expect(npmRow.getByRole('alert')).toContainText('fixture npm rejected')
  await expect(npmSave).not.toHaveAttribute('aria-busy', 'true')
  await expect(pypiSave).toHaveAttribute('aria-busy', 'true')

  await npmSave.click()
  await expect(npmSave).toHaveAttribute('aria-busy', 'true')
  pypiSuccess.resolve()
  await expect(pypiRow.getByRole('spinbutton')).toHaveValue('9.7')
  await expect(pypiRow.getByRole('spinbutton')).toBeEnabled()
  await expect(pypiRow.getByRole('switch')).toBeEnabled()
  await expect(pypiSave).not.toHaveAttribute('aria-busy', 'true')
  await expect(npmSave).toHaveAttribute('aria-busy', 'true')

  npmSuccess.resolve()
  await expect(npmRow.getByRole('spinbutton')).toHaveValue('6.4')
  await expect(npmRow.getByRole('spinbutton')).toBeEnabled()
  await expect(npmRow.getByRole('switch')).toBeEnabled()
  await expect(npmRow.getByRole('alert')).toHaveCount(0)
  await expect(npmSave).not.toHaveAttribute('aria-busy', 'true')
})
