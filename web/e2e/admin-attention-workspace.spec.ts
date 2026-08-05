import type { Request } from '@playwright/test'
import type { SecurityVulnerability } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const securitySuggestion: SecurityVulnerability = {
  id: 41,
  osv_id: 'OSV-2026-41',
  ecosystem: 'pypi',
  package_name: 'unsafe-package',
  affected_ranges: '>=1.0.0 <1.0.2',
  severity: 'high',
  cvss_score: 8.4,
  summary: 'Fixture vulnerability',
  details: 'Fixture vulnerability details',
  aliases: '',
  references: '',
  published_at: '2026-07-28T00:00:00Z',
  modified_at: '2026-07-28T00:00:00Z',
  created_at: '2026-07-28T00:00:00Z',
  updated_at: '2026-07-28T00:00:00Z',
}

test('attention workspace brings operational risks into one direct queue', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': {
      last_24h: { total_requests: 12, hit_rate: 0.5, bytes_served: 2048, avg_latency_ms: 120 },
      prev_24h: { total_requests: 8, hit_rate: 0.4, bytes_served: 1024, avg_latency_ms: 100 },
      daily_stats: [],
      top_packages: {},
      cache_usage_percent: 92.5,
      upstreams: [
        { id: 7, name: 'PyPI primary', adapter: 'pypi', healthy: false, avg_latency_ms: 0, success_rate: 0 },
      ],
    },
    'GET /api/v1/admin/security/suggestions': {
      items: [securitySuggestion],
      total: 3,
      page: 1,
    },
    'GET /api/v1/admin/quarantine/events': {
      items: [{
        id: 91,
        ecosystem: 'pypi',
        package: 'fresh-package',
        version: '1.0.0',
        action: 'blocked',
        reason: 'Release is younger than the configured minimum age',
        created_at: '2026-07-28T01:00:00Z',
      }],
      total: 1,
    },
  })

  await page.goto('/admin/attention')

  await expect(page.locator('[data-admin-topbar] h1')).toHaveText(/待处理|Needs Attention/)
  await expect(page.getByText(/上游源需要关注|Upstreams need attention/)).toBeVisible()
  await expect(page.getByText(/有待决安全建议|Security suggestions are waiting/)).toBeVisible()
  await expect(page.getByText(/缓存容量偏高|Cache capacity is running high/)).toBeVisible()
  await expect(page.getByText('fresh-package')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)

  await page.getByRole('link', { name: /处理安全建议|Review suggestions/ }).click()
  await expect(page).toHaveURL(/\/admin\/security\?tab=suggestions$/)
  await expect(page.getByRole('tab', { name: /建议规则|Suggested Rules/ })).toHaveAttribute('aria-selected', 'true')
})

test('attention keeps available queue items beside an unavailable initial signal', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': {
      last_24h: { total_requests: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 },
      prev_24h: { total_requests: 0, hit_rate: 0, bytes_served: 0, avg_latency_ms: 0 },
      daily_stats: [],
      top_packages: {},
      cache_usage_percent: 20,
      upstreams: [],
    },
  })
  await page.goto('/admin/attention')
  await expect(page.getByText(/当前没有需要处理的运行风险|No active operational risks/)).toBeVisible()

  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': { status: 500, body: { code: 'FAILED', message: 'dashboard unavailable' } },
    'GET /api/v1/admin/security/suggestions': {
      items: [securitySuggestion],
      total: 1,
      page: 1,
    },
  })
  await page.reload()
  const queue = page.locator('section[aria-labelledby="attention-queue-title"]')
  await expect(queue.getByRole('alert')).toContainText(/不可用|unavailable/i)
  await expect(page.getByText(/有待决安全建议|Security suggestions are waiting/)).toBeVisible()
  await expect(queue.getByText(/当前没有需要处理的运行风险|No active operational risks/)).toHaveCount(0)
})

test('attention queue never reports all-clear when one initial signal is unavailable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': {
      status: 500,
      body: { code: 'FAILED', message: 'dashboard unavailable' },
    },
  })

  await page.goto('/admin/attention')

  const queue = page.locator('section[aria-labelledby="attention-queue-title"]')
  await expect(queue.getByRole('alert')).toContainText(/不可用|unavailable/i)
  await expect(queue.getByRole('button', { name: /刷新|重试|retry|refresh/i })).toBeVisible()
  await expect(queue.getByText(/当前没有需要处理的运行风险|No active operational risks/)).toHaveCount(0)
})

test('attention history never reports no events when its initial query is unavailable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/quarantine/events': {
      status: 500,
      body: { code: 'FAILED', message: 'quarantine unavailable' },
    },
  })

  await page.goto('/admin/attention')

  const recent = page.locator('section[aria-labelledby="attention-recent-title"]')
  await expect(recent.getByRole('alert')).toContainText(/不可用|unavailable/i)
  await expect(recent.getByRole('button', { name: /刷新|重试|retry|refresh/i })).toBeVisible()
  await expect(recent.getByText(/暂无最近供应链决策|No recent supply-chain decisions/)).toHaveCount(0)
})

test('attention renders no false clear states when all three initial queries fail', async ({ page }) => {
  const failed = { status: 500, body: { code: 'FAILED', message: 'fixture unavailable' } }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': failed,
    'GET /api/v1/admin/security/suggestions': failed,
    'GET /api/v1/admin/quarantine/events': failed,
  })

  await page.goto('/admin/attention')

  await expect(page.getByRole('alert')).toHaveCount(2)
  await expect(page.getByText(/当前没有需要处理的运行风险|No active operational risks/)).toHaveCount(0)
  await expect(page.getByText(/暂无最近供应链决策|No recent supply-chain decisions/)).toHaveCount(0)
})

test('attention uses the canonical page query parameters', async ({ page }) => {
  let suggestionParams = ''
  let quarantineParams = ''
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': (request: Request) => {
      suggestionParams = new URL(request.url()).searchParams.toString()
      return { items: [], total: 0, page: 1 }
    },
    'GET /api/v1/admin/quarantine/events': (request: Request) => {
      quarantineParams = new URL(request.url()).searchParams.toString()
      return { items: [], total: 0 }
    },
  })

  await page.goto('/admin/attention')

  await expect.poll(() => suggestionParams).toBe('page=1&per_page=20')
  await expect.poll(() => quarantineParams).toBe('limit=100')
})

test('attention keeps cached results visible when background refreshes fail', async ({ page }) => {
  let failRefresh = false
  let dashboardCalls = 0
  let suggestionCalls = 0
  let quarantineCalls = 0
  const failed = { status: 500, body: { code: 'FAILED', message: 'fixture refresh unavailable' } }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': () => {
      dashboardCalls += 1
      return failRefresh ? failed : {
        last_24h: { total_requests: 12, hit_rate: 0.5, bytes_served: 2048, avg_latency_ms: 120 },
        prev_24h: { total_requests: 8, hit_rate: 0.4, bytes_served: 1024, avg_latency_ms: 100 },
        daily_stats: [],
        top_packages: {},
        cache_usage_percent: 20,
        upstreams: [
          { id: 7, name: 'Cached upstream', adapter: 'pypi', healthy: false, avg_latency_ms: 0, success_rate: 0 },
        ],
      }
    },
    'GET /api/v1/admin/security/suggestions': () => {
      suggestionCalls += 1
      return failRefresh ? failed : { items: [securitySuggestion], total: 1, page: 1 }
    },
    'GET /api/v1/admin/quarantine/events': () => {
      quarantineCalls += 1
      return failRefresh ? failed : {
        items: [{
          id: 91,
          ecosystem: 'pypi',
          package: 'cached-package',
          version: '1.0.0',
          action: 'blocked',
          reason: 'Cached decision',
          created_at: '2026-07-28T01:00:00Z',
        }],
        total: 1,
      }
    },
  })

  await page.goto('/admin/attention')
  await expect(page.getByText('Cached upstream')).toBeVisible()
  await expect(page.getByText('cached-package')).toBeVisible()
  await page.waitForTimeout(25)

  failRefresh = true
  await page.getByRole('link', { name: /系统设置|Settings/ }).click()
  await expect(page).toHaveURL(/\/admin\/settings$/)
  await expect(page.locator('[data-admin-topbar] h1')).toHaveText(/系统设置|Settings/)
  await page.goBack()
  await expect(page).toHaveURL(/\/admin\/attention$/)
  await expect(page.locator('[data-admin-topbar] h1')).toHaveText(/待处理|Needs Attention/)
  await expect.poll(() => Math.min(dashboardCalls, suggestionCalls, quarantineCalls)).toBeGreaterThan(1)

  const queue = page.locator('section[aria-labelledby="attention-queue-title"]')
  const recent = page.locator('section[aria-labelledby="attention-recent-title"]')
  await expect(queue.getByText(/最新刷新失败|latest refresh failed/i)).toBeVisible()
  await expect(recent.getByText(/最新刷新失败|latest refresh failed/i)).toBeVisible()
  await expect(page.getByText('Cached upstream')).toBeVisible()
  await expect(page.getByText('cached-package')).toBeVisible()
})
