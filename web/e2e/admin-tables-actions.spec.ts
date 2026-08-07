import type { Page } from '@playwright/test'
import type {
  AccessLogListResponse,
  AdminUpstreamListResponse,
  AuditLogListResponse,
} from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

const populated = {
  'GET /api/v1/admin/logs': {
    items: [{ id: 1, adapter_type: 'pypi', method: 'GET', package_name: 'requests', cache_key: 'pypi/requests', hit: true, latency_ms: 12, upstream: 'tuna', status_code: 200, client_ip: '127.0.0.1', bytes_sent: 1024, created_at: '2026-07-10T00:00:00Z' }],
    total: 1, page: 1, page_size: 50,
  } satisfies AccessLogListResponse,
  'GET /api/v1/admin/audit-logs': {
    items: [{ id: 1, ecosystem: 'pypi', package_name: 'requests', version: '2.32.0', action: 'proxy', cache_result: 'hit', latency_ms: 12, bytes_sent: 1024, client_ip: '127.0.0.1', user_agent: 'fixture', upstream_url: 'https://pypi.example/simple', status_code: 200, created_at: '2026-07-10T00:00:00Z' }],
    total: 1, page: 1,
  } satisfies AuditLogListResponse,
  'GET /api/v1/admin/cache': { items: [{ id: 1, adapter_type: 'pypi', package_name: 'requests', key: 'pypi/requests', size: 1024, hit_count: 2, expires_at: '2026-07-11T00:00:00Z', last_accessed: '2026-07-10T00:00:00Z' }], total: 1 },
  'GET /api/v1/admin/quarantine/events': { items: [{ id: 1, ecosystem: 'pypi', package: 'unsafe', version: '1.0.0', action: 'blocked', reason: 'minimum age', threshold_seconds: 86400, age_at_call_seconds: 60, actor_id: 1, client_ip: '127.0.0.1', created_at: '2026-07-10T00:00:00Z' }], total: 1 },
  'GET /api/v1/admin/rules': [{ id: 1, ecosystem: 'pypi', package_name: 'unsafe', version: '*', action: 'deny', reason: 'blocked', created_at: '2026-07-10T00:00:00Z' }],
  'GET /api/v1/admin/users': [{ id: 2, username: 'operator', role: 'readonly', enabled: true, last_login_at: null, created_at: '2026-07-10T00:00:00Z' }],
  'GET /api/v1/admin/tokens': [{ id: 9, user_id: 1, name: 'ci', permissions: 'readonly', expires_at: null, last_used_at: null, created_at: '2026-07-10T00:00:00Z' }],
  'GET /api/v1/admin/upstreams': { items: [{ id: 1, adapter_type: 'pypi', name: 'tuna', url: 'https://pypi.example/simple', proxy: '', priority: 1, probe_mode: 'active', probe_interval: '30m', healthy: true, avg_latency_ms: 12, success_rate: 1, last_checked_at: '2026-07-10T00:00:00Z', worker_running: true, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }], total: 1 } satisfies AdminUpstreamListResponse,
}

const cases = [
  { path: '/admin/logs', endpoint: 'GET /api/v1/admin/logs', table: true, actionCount: 0 },
  { path: '/admin/audit', endpoint: 'GET /api/v1/admin/audit-logs', table: true, actionCount: 0 },
  { path: '/admin/cache', endpoint: 'GET /api/v1/admin/cache', table: true, actionCount: 1 },
  { path: '/admin/quarantine', endpoint: 'GET /api/v1/admin/quarantine/events', table: true, actionCount: 0 },
  { path: '/admin/rules', endpoint: 'GET /api/v1/admin/rules', table: true, actionCount: 2 },
  { path: '/admin/users', endpoint: 'GET /api/v1/admin/users', table: true, actionCount: 2 },
  // Upstreams intentionally uses cards. Its actions still follow the shared icon-button contract.
  { path: '/admin/upstreams', endpoint: 'GET /api/v1/admin/upstreams', table: false, actionCount: 3 },
] as const

async function expectNoFocusableRows(page: Page) {
  await expect.poll(async () => {
    await page.locator('body').press('Home')
    for (let index = 0; index < 40; index += 1) {
      await page.keyboard.press('Tab')
      if (await page.evaluate(() => document.activeElement?.tagName === 'TR')) return false
    }
    return true
  }).toBe(true)
  await expect(page.locator('tr[tabindex]')).toHaveCount(0)
}

async function expectWithinViewport(page: Page, selector: string, width: number) {
  const bounds = await page.locator(selector).boundingBox()
  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1)
}

async function navigateClient(page: Page, path: string) {
  await page.evaluate((nextPath) => {
    window.history.pushState({}, '', nextPath)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, path)
}

for (const routeCase of cases) {
  test(`${routeCase.path} keeps data and row actions accessible at 390px`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockAdminApi(page, {
      [routeCase.endpoint]: populated[routeCase.endpoint],
      'GET /api/v1/admin/tokens': populated['GET /api/v1/admin/tokens'],
    })
    await page.goto(routeCase.path)

    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)

    if (routeCase.path === '/admin/quarantine') {
      const mobileList = page.locator('[data-quarantine-mobile-list="events"]')
      await expect(mobileList).toBeVisible()
      await expect(mobileList.getByText('unsafe', { exact: true })).toBeVisible()
      await expect(page.locator('[data-table-viewport]')).toBeHidden()
    } else if (routeCase.table) {
      const region = page.locator('[data-table-viewport]').first()
      await expect(region).toBeVisible()
      await expect(region).toHaveAttribute('aria-label', /.+/)
      expect(await region.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
    } else {
      await expect(page.getByText('tuna')).toBeVisible()
      await expect(page.locator('[data-table-viewport]')).toHaveCount(0)
    }

    const iconButtons = page.locator('main [data-icon-button]:visible')
    await expect(iconButtons).toHaveCount(routeCase.actionCount)
    for (const action of await iconButtons.all()) {
      await expect(action).toHaveAccessibleName(/.+/)
      const box = await action.boundingBox()
      expect(box?.width).toBeGreaterThanOrEqual(40)
      expect(box?.height).toBeGreaterThanOrEqual(40)
    }
    await expectNoFocusableRows(page)
  })
}

test('wide-table query states stay in the mobile canvas and only data mounts a scroll region', async ({ page }) => {
  const width = 320
  let mode: 'loading' | 'empty' | 'error' | 'populated' = 'loading'
  let releaseLoading!: () => void
  const loadingResponse = new Promise<AccessLogListResponse>((resolve) => {
    releaseLoading = () => resolve({ items: [], total: 0, page: 1, page_size: 50 })
  })

  await page.setViewportSize({ width, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': async () => {
      if (mode === 'loading') return loadingResponse
      if (mode === 'error') return { status: 500, body: { code: 'FAILED', message: 'fixture table failure' } }
      if (mode === 'populated') return populated['GET /api/v1/admin/logs']
      return { items: [], total: 0, page: 1, page_size: 50 } satisfies AccessLogListResponse
    },
  })

  await page.goto('/admin/logs')
  const loading = page.locator('main [aria-busy="true"]').filter({ hasText: /加载|Loading/ }).last()
  await expect(loading).toBeVisible()
  await expect(page.locator('[data-table-viewport]')).toHaveCount(0)
  await expectWithinViewport(page, 'main [aria-busy="true"]', width)

  mode = 'empty'
  releaseLoading()
  await expect(page.getByText(/暂无日志|No logs/, { exact: true })).toBeVisible()
  await expect(page.locator('[data-table-viewport]')).toHaveCount(0)

  mode = 'error'
  await navigateClient(page, '/admin/logs?package=fixture-error')
  const error = page.getByRole('alert').filter({ hasText: 'fixture table failure' })
  await expect(error).toBeVisible()
  await expect(page.locator('[data-table-viewport]')).toHaveCount(0)
  await expectWithinViewport(page, 'main [role="alert"]', width)

  mode = 'populated'
  await navigateClient(page, '/admin/logs?package=requests')
  const table = page.getByRole('region', { name: /访问日志表格|Access logs table/ })
  await expect(table).toBeVisible()

  await navigateClient(page, '/admin/audit')
  await expect(table).toHaveCount(0)
  mode = 'error'
  await navigateClient(page, '/admin/logs?package=requests')
  const stale = page.getByText(/数据已过期|Stale data/, { exact: true })
  await expect(stale).toBeVisible()
  await expect(table).toBeVisible()
  await expect(table.getByText(/数据已过期|Stale data/, { exact: true })).toHaveCount(0)
  await expectWithinViewport(page, 'main [class*="rounded-"][class*="border"]:has-text("数据已过期")', width)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
})

test('empty states for wide Admin tables do not create local scroll regions at 320px', async ({ page }) => {
  const width = 320
  await page.setViewportSize({ width, height: 844 })
  const emptyCases = [
    { path: '/admin/logs', text: /暂无日志|No logs/ },
    { path: '/admin/audit', text: /暂无审计日志|No audit logs/ },
    { path: '/admin/cache', text: /暂无缓存数据|No cache entries/ },
    { path: '/admin/indexes', text: /没有符合条件的索引缓存|No matching cached indexes/ },
  ]

  for (const emptyCase of emptyCases) {
    await page.goto(emptyCase.path)
    const emptyState = page.getByText(emptyCase.text, { exact: true })
    await expect(emptyState).toBeVisible()
    await expect(page.locator('[data-table-viewport]')).toHaveCount(0)
    const bounds = await emptyState.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  }
})

test('project and security tables expose named local scroll regions and explicit actions', async ({ page }) => {
  const duplicateKeyWarnings: string[] = []
  page.on('console', message => {
    if (message.text().includes('Encountered two children with the same key')) {
      duplicateKeyWarnings.push(message.text())
    }
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/projects': {
      items: [{ id: 7, name: 'api', slug: 'api', description: 'API', package_count: 3, last_activity_at: '2026-07-10T00:00:00Z', created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }],
      total: 1,
    },
    'GET /api/v1/admin/projects/7': { id: 7, name: 'api', slug: 'api', description: 'API', package_count: 3, last_activity_at: '2026-07-10T00:00:00Z', proxy_url: 'https://depsilo.example/p/api', ecosystem_breakdown: { pypi: 3 }, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' },
    'GET /api/v1/admin/projects/7/packages': { items: [{ ecosystem: 'pypi', package_name: 'requests', version: '2.32.0', first_seen_at: '2026-07-10T00:00:00Z', last_seen_at: '2026-07-10T00:00:00Z', download_count: 3 }], total: 1, page: 1 },
    'GET /api/v1/admin/security/vulnerabilities': {
      items: [
        { id: 3, osv_id: 'OSV-2026-1', ecosystem: 'pypi', package_name: 'unsafe', version: '1.0.0', severity: 'high', cvss_score: 8.1, summary: 'fixture', details: 'fixture', affected_versions: ['1.0.0'], fixed_versions: ['1.0.1'], published_at: '2026-07-10T00:00:00Z', modified_at: '2026-07-10T00:00:00Z', created_at: '2026-07-10T00:00:00Z' },
        { id: 4, osv_id: 'OSV-2026-1', ecosystem: 'npm', package_name: 'unsafe-js', version: '2.0.0', severity: 'medium', cvss_score: 6.1, summary: 'fixture two', details: 'fixture two', affected_versions: ['2.0.0'], fixed_versions: ['2.0.1'], published_at: '2026-07-10T00:00:00Z', modified_at: '2026-07-10T00:00:00Z', created_at: '2026-07-10T00:00:00Z' },
      ],
      total: 21,
      page: 1,
    },
  })

  await page.goto('/admin/projects')
  const projectsTable = page.getByRole('region', { name: /项目表格|Projects table/ })
  await expect(projectsTable).toBeVisible()
  expect(await projectsTable.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await expect(page.getByRole('button', { name: /查看 api|View api/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /删除 api|Delete api/ })).toBeVisible()
  await page.getByRole('button', { name: /查看 api|View api/ }).click()
  const packagesTable = page.getByRole('region', { name: /项目包表格|Project packages table/ })
  await expect(packagesTable).toBeVisible()
  expect(await packagesTable.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await expect(packagesTable.locator('tbody tr')).toHaveCount(1)
  await expect(packagesTable.locator('tr[tabindex]')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)

  await page.goto('/admin/security')
  await page.getByRole('tab', { name: /漏洞|Vulnerabilities/ }).click()
  const securityTable = page.getByRole('region', { name: /安全漏洞表格|Security vulnerabilities table/ })
  await expect(securityTable).toBeVisible()
  await expect(securityTable.locator('tbody tr')).toHaveCount(2)
  await expect(securityTable.getByText('unsafe', { exact: true })).toBeVisible()
  await expect(securityTable.getByText('unsafe-js', { exact: true })).toBeVisible()
  await expect(securityTable.locator('tr[tabindex]')).toHaveCount(0)
  expect(duplicateKeyWarnings).toEqual([])
  expect(await securityTable.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await expect(page.getByRole('button', { name: /上一页|Previous/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /下一页|Next/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
})

test('users API-token table is populated, named, locally scrollable, and has non-focusable rows', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/users': populated['GET /api/v1/admin/users'],
    'GET /api/v1/admin/tokens': populated['GET /api/v1/admin/tokens'],
  })
  await page.goto('/admin/users')

  const tokensTable = page.getByRole('region', { name: /API Token 表格|API tokens table/ })
  await expect(tokensTable).toBeVisible()
  expect(await tokensTable.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await expect(tokensTable.locator('tbody tr')).toHaveCount(1)
  await expect(tokensTable.locator('tr[tabindex]')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
})

test('upstream row checks keep independent loading state when responses finish out of order', async ({ page }) => {
  const alpha = { ...populated['GET /api/v1/admin/upstreams'].items[0], id: 1, name: 'alpha', url: 'https://alpha.example/simple' }
  const beta = { ...alpha, id: 2, name: 'beta', url: 'https://beta.example/simple' }
  let alphaStarted!: () => void
  let betaStarted!: () => void
  let releaseAlpha!: () => void
  let releaseBeta!: () => void
  const alphaRequestStarted = new Promise<void>(resolve => { alphaStarted = resolve })
  const betaRequestStarted = new Promise<void>(resolve => { betaStarted = resolve })
  const alphaResponse = new Promise<void>(resolve => { releaseAlpha = resolve })
  const betaResponse = new Promise<void>(resolve => { releaseBeta = resolve })

  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': { items: [alpha, beta], total: 2 },
    'POST /api/v1/admin/upstreams/1/check': async () => {
      alphaStarted()
      await alphaResponse
      return { upstream: alpha, check: { healthy: true, latency_ms: 12, checked_at: '2026-07-10T00:01:00Z', error: null } }
    },
    'POST /api/v1/admin/upstreams/2/check': async () => {
      betaStarted()
      await betaResponse
      return { upstream: beta, check: { healthy: true, latency_ms: 14, checked_at: '2026-07-10T00:01:00Z', error: null } }
    },
  })
  await page.goto('/admin/upstreams')

  const alphaCheck = page.getByRole('button', { name: /检测 alpha|Check alpha/ })
  const betaCheck = page.getByRole('button', { name: /检测 beta|Check beta/ })
  await alphaCheck.click()
  await betaCheck.click()
  await Promise.all([alphaRequestStarted, betaRequestStarted])
  await expect(alphaCheck).toHaveAttribute('aria-busy', 'true')
  await expect(betaCheck).toHaveAttribute('aria-busy', 'true')

  releaseBeta()
  await expect(betaCheck).not.toHaveAttribute('aria-busy', 'true')
  await expect(alphaCheck).toHaveAttribute('aria-busy', 'true')

  releaseAlpha()
  await expect(alphaCheck).not.toHaveAttribute('aria-busy', 'true')
})

test('direct user enables keep independent loading state when responses finish out of order', async ({ page }) => {
  const alpha = { id: 2, username: 'operator-alpha', role: 'readonly', enabled: false, last_login_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }
  const beta = { ...alpha, id: 3, username: 'operator-beta' }
  let alphaStarted!: () => void
  let betaStarted!: () => void
  let releaseAlpha!: () => void
  let releaseBeta!: () => void
  const alphaRequestStarted = new Promise<void>(resolve => { alphaStarted = resolve })
  const betaRequestStarted = new Promise<void>(resolve => { betaStarted = resolve })
  const alphaResponse = new Promise<void>(resolve => { releaseAlpha = resolve })
  const betaResponse = new Promise<void>(resolve => { releaseBeta = resolve })

  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [alpha, beta],
    'PUT /api/v1/admin/users/2': async () => {
      alphaStarted()
      await alphaResponse
      return { ...alpha, enabled: true }
    },
    'PUT /api/v1/admin/users/3': async () => {
      betaStarted()
      await betaResponse
      return { ...beta, enabled: true }
    },
  })
  await page.goto('/admin/users')

  const alphaRow = page.getByRole('row', { name: /operator-alpha/ })
  const betaRow = page.getByRole('row', { name: /operator-beta/ })
  const alphaToggle = alphaRow.getByRole('button', { name: /启用 operator-alpha|Enable operator-alpha/ })
  const betaToggle = betaRow.getByRole('button', { name: /启用 operator-beta|Enable operator-beta/ })
  await alphaToggle.click()
  await betaToggle.click()
  await Promise.all([alphaRequestStarted, betaRequestStarted])
  await expect(alphaToggle).toHaveAttribute('aria-busy', 'true')
  await expect(betaToggle).toHaveAttribute('aria-busy', 'true')

  releaseBeta()
  await expect(betaRow.getByRole('button', { name: /禁用 operator-beta|Disable operator-beta/ })).not.toHaveAttribute('aria-busy', 'true')
  await expect(alphaToggle).toHaveAttribute('aria-busy', 'true')

  releaseAlpha()
  await expect(alphaRow.getByRole('button', { name: /禁用 operator-alpha|Disable operator-alpha/ })).not.toHaveAttribute('aria-busy', 'true')
})

test('quarantine uses direct mobile lists and preserves named desktop table regions', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/quarantine/approvals': {
      items: [{ id: 2, ecosystem: 'pypi', package: 'trusted', version: '1.0.0', reason: 'reviewed', approved_by: 1, created_at: '2026-07-10T00:00:00Z' }],
      total: 1,
    },
    'GET /api/v1/admin/blocklist/status': { enabled: true, count: 1, entry_count: 1, last_sync_at: null, last_success_at: null, last_error: '', duration_ms: 0, per_ecosystem: { pypi: 1 }, ecosystems: ['pypi'], running: false, next_sync_at: null },
    'GET /api/v1/admin/blocklist/overrides': {
      items: [{ id: 4, ecosystem: 'pypi', package: 'false-positive', version: '1.0.0', reason: 'reviewed', created_by: 1, created_at: '2026-07-10T00:00:00Z', expires_at: '2099-07-11T00:00:00Z' }],
    },
  })
  await page.goto('/admin/quarantine')

  await page.getByRole('tab', { name: /已放行|Approvals/ }).click()
  const approvals = page.getByRole('region', { name: /供应链隔离放行表格|Quarantine approvals table/ })
  const approvalList = page.locator('[data-quarantine-mobile-list="approvals"]')
  await expect(approvalList).toBeVisible()
  await expect(approvalList.getByText('trusted', { exact: true })).toBeVisible()
  await expect(approvalList.getByRole('button', { name: /撤销|Revoke/ })).toBeVisible()
  await expect(approvals).toBeHidden()

  await page.getByRole('tab', { name: /恶意封锁|Malware blocklist/ }).click()
  const overrides = page.getByRole('region', { name: /恶意封锁豁免表格|Malware override table/ })
  const overrideList = page.locator('[data-quarantine-mobile-list="overrides"]')
  await expect(overrideList).toBeVisible()
  await expect(overrideList.getByText('false-positive', { exact: true })).toBeVisible()
  await expect(overrideList.getByRole('button', { name: /撤销|Revoke/ })).toBeVisible()
  await expect(overrides).toBeHidden()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)

  await page.setViewportSize({ width: 639, height: 900 })
  await expect(overrideList).toBeVisible()
  await expect(overrides).toBeHidden()

  await page.setViewportSize({ width: 640, height: 900 })
  await expect(overrideList).toBeHidden()
  await expect(overrides).toBeVisible()
  expect(await overrides.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)

  await page.getByRole('tab', { name: /已放行|Approvals/ }).click()
  await expect(approvalList).toBeHidden()
  await expect(approvals).toBeVisible()
  expect(await approvals.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
})
