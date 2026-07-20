import type { AdminUpstream, AdminUpstreamListResponse } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const upstreams: AdminUpstream[] = Array.from({ length: 27 }, (_, index) => ({
  id: index + 1,
  adapter_type: 'pypi',
  name: `source-${index + 1}`,
  url: `https://source-${index + 1}.example/simple`,
  proxy: '',
  priority: index + 1,
  probe_mode: 'active',
  probe_interval: '30m',
  healthy: true,
  avg_latency_ms: 20 + index,
  success_rate: 1,
  last_checked_at: '2026-07-10T00:00:00Z',
  worker_running: true,
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
}))

const batchLatencyResponse = {
  series: upstreams.map(upstream => ({
    upstream_id: upstream.id,
    points: [{
      time: '2026-07-10T00:01:00Z',
      latency_ms: 300 + upstream.id,
      healthy: true,
    }],
  })),
}

test('27 Upstreams share one latency request and render the matching batch heartbeat', async ({ page }) => {
  const latencyPaths: string[] = []
  page.on('request', request => {
    const path = new URL(request.url()).pathname
    if (/^\/api\/v1\/admin\/upstreams(?:\/\d+)?\/latency$/.test(path)) {
      latencyPaths.push(path)
    }
  })
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': {
      items: upstreams,
      total: upstreams.length,
    } satisfies AdminUpstreamListResponse,
    'GET /api/v1/admin/upstreams/latency': batchLatencyResponse,
  })

  await page.goto('/admin/upstreams')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(27)

  const source27 = page.locator('[data-upstream-row]').filter({
    has: page.getByText('source-27', { exact: true }),
  })
  const heartbeat = source27.locator('[data-upstream-heartbeat]')
  await expect(heartbeat).toHaveCount(1)
  const bars = heartbeat.locator(':scope > div').first().locator(':scope > div')
  await expect(bars).toHaveCount(44)
  await bars.last().hover()
  await expect(heartbeat).toContainText('327ms')

  expect(latencyPaths).toEqual(['/api/v1/admin/upstreams/latency'])
})

test('Upstream rows render while the batch latency request is still pending', async ({ page }) => {
  let markLatencyStarted: (() => void) | undefined
  let releaseLatency: (() => void) | undefined
  const latencyStarted = new Promise<void>(resolve => { markLatencyStarted = resolve })
  const latencyReleased = new Promise<void>(resolve => { releaseLatency = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': {
      items: [upstreams[0]],
      total: 1,
    } satisfies AdminUpstreamListResponse,
    'GET /api/v1/admin/upstreams/latency': async () => {
      markLatencyStarted?.()
      await latencyReleased
      return { series: [batchLatencyResponse.series[0]] }
    },
  })

  await page.goto('/admin/upstreams')
  await latencyStarted
  try {
    await expect(page.getByText('source-1', { exact: true })).toBeVisible({ timeout: 1000 })
  } finally {
    releaseLatency?.()
  }
  const heartbeat = page.locator('[data-upstream-heartbeat]')
  await heartbeat.locator(':scope > div').first().locator(':scope > div').last().hover()
  await expect(heartbeat).toContainText('301ms')
})

test('a batch latency failure stays non-blocking and can be retried', async ({ page }) => {
  let attempts = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': {
      items: [upstreams[0]],
      total: 1,
    } satisfies AdminUpstreamListResponse,
    'GET /api/v1/admin/upstreams/latency': () => {
      attempts += 1
      return attempts === 1
        ? { status: 500, body: { code: 'DB_ERROR', message: 'history unavailable' } }
        : { series: [batchLatencyResponse.series[0]] }
    },
  })

  await page.goto('/admin/upstreams')
  await expect(page.getByText('source-1', { exact: true })).toBeVisible()
  const warning = page.locator('[data-upstream-history-error]')
  await expect(warning).toContainText('延迟历史暂时无法加载')
  await warning.getByRole('button', { name: '重试' }).click()
  await expect(warning).toHaveCount(0)
  const heartbeat = page.locator('[data-upstream-heartbeat]')
  await heartbeat.locator(':scope > div').first().locator(':scope > div').last().hover()
  await expect(heartbeat).toContainText('301ms')
})

test('client navigation to Upstreams does not load the simple-icons barrel', async ({ page }) => {
  const loadedScripts: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': {
      items: [upstreams[0]],
      total: 1,
    } satisfies AdminUpstreamListResponse,
    // Keep this dependency assertion independent from the latency transport
    // migration so its only failure signal is the cold route module graph.
    'GET /api/v1/admin/upstreams/latency': { series: [] },
  })
  await page.goto('/admin/users')
  await expect(page.locator('[data-admin-topbar] h1')).toHaveText('用户管理')

  page.on('request', request => {
    if (request.resourceType() === 'script') loadedScripts.push(request.url())
  })
  const navigation = page.locator('[data-admin-nav-surface="sidebar"]')
  await navigation.getByRole('link', { name: '上游源', exact: true }).click()
  await expect(page.getByText('source-1', { exact: true })).toBeVisible()

  expect(loadedScripts.filter(url => (
    new URL(url).pathname === '/node_modules/.vite/deps/simple-icons.js'
  ))).toEqual([])
})
