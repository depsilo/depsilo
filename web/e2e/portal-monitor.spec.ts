import { expect, mockAdminApi, setUiPreferences, test, type JsonValue } from './fixtures/admin-api'

const emptyStats = {
  service: { status: 'healthy' },
  week: { total_requests: 0, hit_count: 0, hit_rate: 0, bytes_saved: 0 },
  upstreams: [],
}

test('Monitor distinguishes initial loading, failure recovery, and a successful empty response', async ({ page }) => {
  let releaseStats!: (value: JsonValue) => void
  const pendingStats = new Promise<JsonValue>((resolve) => {
    releaseStats = resolve
  })
  await mockAdminApi(page, {
    'GET /api/v1/stats': () => pendingStats,
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
