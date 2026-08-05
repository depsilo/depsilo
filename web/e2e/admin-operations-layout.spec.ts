import type { Locator, Page } from '@playwright/test'
import type {
  AccessLogListResponse,
  AdminUpstreamListResponse,
  AuditLogListResponse,
  CacheIndexListResponse,
  CacheListResponse,
  ProjectListResponse,
} from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const populatedUpstream = {
  id: 1,
  adapter_type: 'pypi',
  name: 'tuna',
  url: 'https://pypi.example/simple',
  proxy: '',
  priority: 1,
  probe_mode: 'active',
  probe_interval: '30m',
  healthy: true,
  avg_latency_ms: 12,
  success_rate: 1,
  last_checked_at: '2026-07-10T00:00:00Z',
  worker_running: true,
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
} as const

const accessLog = {
  id: 1,
  adapter_type: 'pypi',
  method: 'GET',
  package_name: 'requests',
  cache_key: 'pypi/requests',
  hit: true,
  latency_ms: 12,
  upstream: 'tuna',
  status_code: 200,
  client_ip: '127.0.0.1',
  bytes_sent: 1024,
  created_at: '2026-07-10T00:00:00Z',
} as const

const auditLog = {
  id: 1,
  ecosystem: 'pypi',
  package_name: 'requests',
  version: '2.32.0',
  action: 'proxy',
  cache_result: 'hit',
  latency_ms: 12,
  bytes_sent: 1024,
  client_ip: '127.0.0.1',
  user_agent: 'fixture',
  upstream_url: 'https://pypi.example/simple',
  status_code: 200,
  created_at: '2026-07-10T00:00:00Z',
} as const

const cacheEntry = {
  id: 1,
  adapter_type: 'pypi',
  package_name: 'requests',
  key: 'pypi/requests',
  size: 1024,
  hit_count: 2,
  expires_at: '2026-07-11T00:00:00Z',
  last_accessed: '2026-07-10T00:00:00Z',
} as const

const cacheIndexEntry = {
  id: 1,
  key: 'pypi/simple/requests',
  adapter_type: 'pypi',
  package_name: 'requests',
  size: 1024,
  hit_count: 2,
  etag: '"fixture"',
  last_modified: 'Thu, 10 Jul 2026 00:00:00 GMT',
  last_accessed: '2026-07-10T00:00:00Z',
  expires_at: '2026-07-11T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
  status: 'fresh',
} as const

async function expectInViewport(page: Page, locator: Locator) {
  await expect(locator).toBeVisible()
  await locator.scrollIntoViewIfNeeded()
  const box = await locator.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return {
      left: rect.left,
      right: rect.right,
      top: rect.top,
      bottom: rect.bottom,
      width: rect.width,
      height: rect.height,
    }
  })
  const viewport = page.viewportSize()
  expect(viewport).not.toBeNull()
  expect(box.width).toBeGreaterThan(0)
  expect(box.height).toBeGreaterThan(0)
  expect(box.left).toBeGreaterThanOrEqual(-1)
  expect(box.right).toBeLessThanOrEqual(viewport!.width + 1)
  expect(box.top).toBeGreaterThanOrEqual(-1)
  expect(box.bottom).toBeLessThanOrEqual(viewport!.height + 1)
}

async function expectNoPageOverflow(page: Page, width: number) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
}

test('Upstream heartbeat, actions, and empty-state creation stay operable at 320px', async ({ page }) => {
  const width = 320
  await page.setViewportSize({ width, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': {
      items: [populatedUpstream],
      total: 1,
    } satisfies AdminUpstreamListResponse,
    'GET /api/v1/admin/upstreams/latency': {
      series: [{
        upstream_id: 1,
        points: [
          { latency_ms: 12, time: '2026-07-10T00:00:00Z', healthy: true },
          { latency_ms: 18, time: '2026-07-10T00:01:00Z', healthy: true },
          { latency_ms: 9, time: '2026-07-10T00:02:00Z', healthy: true },
        ],
      }],
    },
  })
  await page.goto('/admin/upstreams')

  const heartbeat = page.locator('[data-upstream-heartbeat]')
  await expect(heartbeat).toHaveCount(1)
  await expectInViewport(page, heartbeat)

  for (const actionName of [/全部检测/, /添加上游源/, /检测 tuna/, /编辑 tuna/, /删除 tuna/]) {
    await expectInViewport(page, page.getByRole('button', { name: actionName }))
  }
  await expectNoPageOverflow(page, width)

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': { items: [], total: 0 } satisfies AdminUpstreamListResponse,
  })
  await page.reload()
  await page.getByRole('button', { name: /添加上游源/ }).click()

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const ecosystemSelect = dialog.getByLabel('类型')
  await expectInViewport(page, ecosystemSelect)
  await expect(ecosystemSelect).toHaveValue('pypi')
  const optionValues = await ecosystemSelect.locator('option').evaluateAll(options => (
    options.map(option => (option as HTMLOptionElement).value)
  ))
  expect(optionValues).toEqual(expect.arrayContaining(['pypi', 'npm', 'huggingface']))
  expect(optionValues).not.toContain('')
  await expectNoPageOverflow(page, width)
})

test('Security policy controls remain visible at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/policies': [{
      id: 1,
      ecosystem: 'pypi',
      auto_block_enabled: true,
      min_cvss_score: 8.5,
      created_by: 'admin',
      created_at: '2026-07-10T00:00:00Z',
      updated_at: '2026-07-10T00:00:00Z',
    }],
  })
  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /策略配置/ }).click()

  const policy = page.locator('[data-policy-ecosystem="pypi"]')
  await expectInViewport(page, policy)
  await expectInViewport(page, policy.getByRole('switch'))
  await expectInViewport(page, policy.getByRole('spinbutton'))
  await expectInViewport(page, policy.getByRole('button', { name: /保存/ }))
  await expectNoPageOverflow(page, 320)
})

test('Project proxy details and copy action remain visible at 320px', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  const project = {
    id: 7,
    name: 'critical-platform',
    slug: 'critical-platform-service-with-a-deliberately-long-slug',
    description: 'Critical platform dependencies',
    package_count: 3,
    last_activity_at: '2026-07-10T00:00:00Z',
    created_at: '2026-07-10T00:00:00Z',
    updated_at: '2026-07-10T00:00:00Z',
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': { items: [project], total: 1 } satisfies ProjectListResponse,
    'GET /api/v1/admin/projects/7': {
      ...project,
      proxy_url: 'https://depsilo.internal.example.company/p/critical-platform-service-with-a-deliberately-long-slug',
      ecosystem_breakdown: { pypi: 3 },
    },
    'GET /api/v1/admin/projects/7/packages': { items: [], total: 0, page: 1 },
  })
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: /查看 critical-platform/ }).click()

  const proxyRow = page.locator('[data-project-proxy-row]')
  const proxyUrl = page.locator('[data-project-proxy-value]')
  await expectInViewport(page, proxyRow)
  await expectInViewport(page, proxyUrl)
  await expect(proxyUrl).toContainText('critical-platform-service-with-a-deliberately-long-slug')
  await expectInViewport(page, proxyRow.getByRole('button', { name: /复制代理地址/ }))
  await expectNoPageOverflow(page, 320)
})

const paginationFixtures = [
  {
    route: '/admin/logs',
    override: {
      'GET /api/v1/admin/logs': {
        items: [accessLog],
        total: 51,
        page: 1,
        page_size: 50,
      } satisfies AccessLogListResponse,
    },
  },
  {
    route: '/admin/audit',
    override: {
      'GET /api/v1/admin/audit-logs': {
        items: [auditLog],
        total: 51,
        page: 1,
      } satisfies AuditLogListResponse,
    },
  },
  {
    route: '/admin/cache',
    override: {
      'GET /api/v1/admin/cache': {
        items: [cacheEntry],
        total: 21,
        page: 1,
        page_size: 20,
      } satisfies CacheListResponse,
    },
  },
  {
    route: '/admin/indexes',
    override: {
      'GET /api/v1/admin/cache/indexes': {
        items: [cacheIndexEntry],
        summary: [],
        total: 26,
        page: 1,
        page_size: 25,
      } satisfies CacheIndexListResponse,
    },
  },
] as const

test('Operation pagination remains fully visible at 320px', async ({ page }) => {
  const width = 320
  await page.setViewportSize({ width, height: 844 })

  for (const fixture of paginationFixtures) {
    await mockAdminApi(page, fixture.override)
    await page.goto(fixture.route)

    const pagination = page.locator('[data-admin-pagination]')
    await expect(pagination).toHaveCount(1)
    await expectInViewport(page, pagination)
    await expectInViewport(page, pagination.getByRole('button', { name: '上一页' }))
    await expectInViewport(page, pagination.getByRole('button', { name: '下一页' }))
    await expectNoPageOverflow(page, width)
  }
})
