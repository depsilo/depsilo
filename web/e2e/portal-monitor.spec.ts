import { expect, mockAdminApi, setUiPreferences, test, type JsonValue } from './fixtures/admin-api'

const emptyStats = {
  service: { status: 'healthy' },
  week: { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_saved: 0 },
  upstreams: [],
}

test.use({ initialToken: null })

test('Monitor reuses the Portal stats poll instead of starting a second poll', async ({ page }) => {
  let statsRequests = 0
  await page.clock.install()
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => {
      statsRequests += 1
      return emptyStats
    },
  })

  await page.goto('/monitor')
  await expect(page.locator('[data-monitor-upstreams]')).toContainText('暂无上游源')
  await page.waitForLoadState('networkidle')

  expect(statsRequests).toBe(1)

  await page.clock.runFor(30_000)
  await expect.poll(() => statsRequests).toBe(2)
})

test('anonymous Monitor uses only public APIs when latency history is empty', async ({ page }) => {
  const adminRequests: string[] = []
  let latencyRequests = 0
  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (pathname.startsWith('/api/v1/admin/')) {
      adminRequests.push(`${request.method()} ${pathname}`)
    }
  })
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'healthy' },
      week: { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_saved: 0 },
      upstreams: [
        {
          id: 101,
          name: 'public mirror',
          adapter: 'npm',
          url: 'https://registry.example',
          healthy: true,
          avg_latency_ms: 42,
          success_rate: 1,
        },
      ],
    },
    'GET /api/v1/latency-series': () => {
      latencyRequests += 1
      return {}
    },
  })

  await page.goto('/monitor')

  await expect(page.locator('[data-upstream-row]')).toContainText('public mirror')
  await expect.poll(() => latencyRequests).toBe(1)
  await page.waitForLoadState('networkidle')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBeNull()
  expect(adminRequests).toEqual([])
})

test('Monitor distinguishes initial loading, failure recovery, and a successful empty response', { tag: '@smoke' }, async ({ page }) => {
  let latencyRequests = 0
  let releaseStats!: (value: JsonValue) => void
  const pendingStats = new Promise<JsonValue>((resolve) => {
    releaseStats = resolve
  })
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => pendingStats,
    'GET /api/v1/latency-series': () => {
      latencyRequests += 1
      return {}
    },
  })

  await page.goto('/monitor')

  const upstreamRegion = page.locator('[data-monitor-upstreams]')
  await expect(upstreamRegion).toHaveAttribute('aria-busy', 'true')
  await expect(upstreamRegion).toContainText('正在加载上游健康状态')
  await expect(upstreamRegion).not.toContainText('暂无上游源')

  releaseStats(emptyStats)

  await expect(upstreamRegion).not.toHaveAttribute('aria-busy', 'true')
  await expect(upstreamRegion).toContainText('暂无上游源')
  await expect(upstreamRegion).toContainText('当前服务尚未配置可公开查看的上游镜像')
  await page.waitForLoadState('networkidle')
  expect(latencyRequests).toBe(0)

  let statsAvailable = false
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => statsAvailable
      ? emptyStats
      : { status: 503, body: { message: 'unavailable' } },
  })
  await page.reload()

  const failure = upstreamRegion.getByRole('alert')
  await expect(failure).toContainText('无法加载上游健康状态')
  statsAvailable = true
  await failure.getByRole('button', { name: '重试' }).click()
  await expect(upstreamRegion).toContainText('暂无上游源')
})

test('Monitor exposes unified healthy, degraded, and failed status text and a search-empty state', async ({ page }) => {
  let latencyRequests = 0
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'degraded' },
      week: { total_requests: 30, hit_count: 20, hit_rate: 2 / 3, bytes_saved: 2048 },
      upstreams: [
        { name: 'fast mirror', adapter: 'pypi', url: 'https://fast.example', healthy: true, avg_latency_ms: 42, success_rate: 1 },
        { name: 'slow mirror', adapter: 'pypi', url: 'https://slow.example', healthy: true, avg_latency_ms: 150, success_rate: 0.98 },
        { name: 'down mirror', adapter: 'pypi', url: 'https://down.example', healthy: false, avg_latency_ms: 20, success_rate: 0 },
      ],
    },
    'GET /api/v1/latency-series': () => {
      latencyRequests += 1
      return {}
    },
  })

  await page.goto('/monitor')

  await expect(page.locator('.portal-status-pill')).toContainText('Degraded')
  await expect(page.locator('[data-upstream-row][data-upstream-status="healthy"]')).toContainText('fast mirror')
  await expect(page.locator('[data-upstream-row][data-upstream-status="healthy"]')).toContainText('healthy')
  await expect(page.locator('[data-upstream-row][data-upstream-status="degraded"]')).toContainText('slow mirror')
  await expect(page.locator('[data-upstream-row][data-upstream-status="degraded"]')).toContainText('degraded')
  await expect(page.locator('[data-upstream-row][data-upstream-status="failed"]')).toContainText('down mirror')
  await expect(page.locator('[data-upstream-row][data-upstream-status="failed"]')).toContainText('failed')
  await expect(page.getByText('1/3 healthy')).toBeVisible()
  await expect(page.locator('[data-upstream-heartbeat][aria-label="Latency history for fast mirror"]')).toBeVisible()
  await expect.poll(() => latencyRequests).toBe(1)

  const search = page.getByRole('textbox', { name: 'Search ecosystems / upstreams' })
  await search.fill('missing')
  await expect(
    page.getByRole('paragraph').filter({ hasText: 'No upstreams match "missing"' }),
  ).toBeVisible()
  await expect(page.getByText('Try an ecosystem, mirror name, or host.')).toBeVisible()
  await expect(
    page.locator('p[role="status"][aria-live="polite"]'),
  ).toHaveText('No upstreams match "missing"')

  await page.getByRole('button', { name: 'Clear upstream search' }).click()
  await expect(page.locator('[data-upstream-row]')).toHaveCount(3)
})

function probeSeries(values: Array<number | false>) {
  return values.map((value, index) => ({
    time: new Date(Date.UTC(2026, 6, 28, 8, index * 30)).toISOString(),
    latency_ms: value === false ? 0 : value,
    healthy: value !== false,
    requests: 1,
  }))
}

test('Monitor encodes latency as column height and a failed probe as the tallest column', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'degraded' },
      week: {},
      upstreams: [
        {
          id: 101,
          name: 'flapping mirror',
          adapter: 'pypi',
          url: 'https://flap.example',
          healthy: true,
          avg_latency_ms: 180,
          success_rate: 0.7,
        },
      ],
    },
    'GET /api/v1/latency-series': {
      '101': probeSeries([80, 90, false, 140, 900, false, 160, 120]),
    },
  })

  await page.goto('/monitor')
  await expect(page.locator('[data-upstream-row]')).toContainText('flapping mirror')

  const columns = await page
    .locator('[data-upstream-heartbeat] [data-heartbeat-beat]')
    .evaluateAll(beats => beats.map(beat => ({
      state: beat.getAttribute('data-heartbeat-state'),
      height: Number(beat.getAttribute('data-heartbeat-height')),
    })))

  const answered = columns.filter(column => column.state === 'ok' || column.state === 'slow')
  const failed = columns.filter(column => column.state === 'down')
  const unmeasured = columns.filter(column => column.state === 'none')

  expect(answered.length).toBe(6)
  expect(failed.length).toBe(2)
  // "Down" is a shape, not only a colour: every failed probe is taller than
  // every answered one, and a faster probe never draws a taller column.
  expect(Math.max(...answered.map(column => column.height)))
    .toBeLessThan(Math.min(...failed.map(column => column.height)))
  expect(answered[0].height).toBeLessThan(answered[answered.length - 1].height)
  // A slot with no sample stays empty so it can never read as a fast answer.
  expect(unmeasured.every(column => column.height === 0)).toBe(true)
})

test('Monitor can reduce the directory to the upstreams that need attention', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'degraded' },
      week: {},
      upstreams: [
        { name: 'fast mirror', adapter: 'pypi', url: 'https://fast.example', healthy: true, avg_latency_ms: 42, success_rate: 1 },
        { name: 'slow mirror', adapter: 'npm', url: 'https://slow.example', healthy: true, avg_latency_ms: 400, success_rate: 0.98 },
        { name: 'down mirror', adapter: 'npm', url: 'https://down.example', healthy: false, avg_latency_ms: 20, success_rate: 0 },
      ],
    },
    'GET /api/v1/latency-series': {},
  })

  await page.goto('/monitor')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(3)

  const filter = page.getByRole('button', { name: 'Only issues' })
  await expect(filter).toHaveAttribute('aria-pressed', 'false')
  await expect(filter).toHaveAccessibleName('Only issues')

  await filter.click()
  await expect(filter).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(2)
  await expect(
    page.locator('[data-upstream-row]').filter({ hasText: 'fast mirror' }),
  ).toHaveCount(0)
  await expect(
    page.locator('p[role="status"][aria-live="polite"]'),
  ).toHaveText('Showing the 2 upstreams that need attention')

  await filter.click()
  await expect(page.locator('[data-upstream-row]')).toHaveCount(3)
})

test('Monitor pins the upstream counts under the header once they scroll away', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 320 })
  await mockAdminApi(page, {
    'GET /api/v1/stats': {
      service: { status: 'degraded' },
      week: { total_requests: 40, hit_count: 30, hit_rate: 0.75, bytes_saved: 2048 },
      upstreams: ['pypi', 'npm', 'crates', 'maven', 'go', 'rubygems'].flatMap((adapter, group) => [
        { name: `${adapter}-one`, adapter, url: `https://${adapter}1.example`, healthy: true, avg_latency_ms: 40 + group, success_rate: 1 },
        { name: `${adapter}-two`, adapter, url: `https://${adapter}2.example`, healthy: group !== 1, avg_latency_ms: group === 1 ? 0 : 400 + group, success_rate: 1 },
      ]),
    },
    'GET /api/v1/latency-series': {},
  })

  await page.goto('/monitor')

  const bar = page.locator('[data-monitor-summary-bar]')
  // A visual duplicate of the inline row: it stays out of the accessibility
  // tree and off the pointer path so it cannot intercept a click or read the
  // counts twice.
  await expect(bar).toHaveAttribute('aria-hidden', 'true')
  await expect(bar).not.toHaveAttribute('data-pinned', 'true')
  await expect(bar).toHaveCSS('opacity', '0')
  await expect(bar).toHaveCSS('pointer-events', 'none')

  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  await expect(bar).toHaveAttribute('data-pinned', 'true')
  await expect(bar).toHaveCSS('opacity', '1')
  await expect(bar).toContainText('Upstream mirrors')
  await expect(bar).toContainText('degraded')
  await expect(bar).toContainText('75.0%')
  await expect(bar).toContainText('2 KB')

  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(bar).not.toHaveAttribute('data-pinned', 'true')
  await expect(bar).toHaveCSS('opacity', '0')
})
