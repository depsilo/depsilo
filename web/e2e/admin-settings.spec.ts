import type { Page, Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

interface ExpectedHttpFailure {
  method: string
  pathname: string
  status: number
}

interface BrowserAudit {
  runtimeErrors: string[]
  resourceErrors: string[]
  expectedHttpFailures: ExpectedHttpFailure[]
  observedHttpFailures: ExpectedHttpFailure[]
}

const browserAudits = new WeakMap<Page, BrowserAudit>()

function expectHttpFailure(page: Page, failure: ExpectedHttpFailure) {
  browserAudits.get(page)?.expectedHttpFailures.push(failure)
}

test.beforeEach(async ({ page }) => {
  const audit: BrowserAudit = {
    runtimeErrors: [],
    resourceErrors: [],
    expectedHttpFailures: [],
    observedHttpFailures: [],
  }
  browserAudits.set(page, audit)
  page.on('pageerror', error => audit.runtimeErrors.push(error.message))
  page.on('console', message => {
    if (message.type() !== 'error') return
    const text = message.text()
    if (text.startsWith('Failed to load resource: the server responded with a status of')) {
      audit.resourceErrors.push(text)
    } else {
      audit.runtimeErrors.push(text)
    }
  })
  page.on('response', response => {
    if (response.status() < 400) return
    const request = response.request()
    audit.observedHttpFailures.push({
      method: request.method(),
      pathname: new URL(response.url()).pathname,
      status: response.status(),
    })
  })
})

test.afterEach(async ({ page }) => {
  const audit = browserAudits.get(page)
  expect(audit, 'browser audit was installed').toBeDefined()
  if (!audit) return
  const unexpectedHttpFailures = audit.observedHttpFailures.filter(observed => (
    !audit.expectedHttpFailures.some(expected => (
      expected.method === observed.method
      && expected.pathname === observed.pathname
      && expected.status === observed.status
    ))
  ))
  const missingHttpFailures = audit.expectedHttpFailures.filter(expected => (
    !audit.observedHttpFailures.some(observed => (
      expected.method === observed.method
      && expected.pathname === observed.pathname
      && expected.status === observed.status
    ))
  ))
  expect(unexpectedHttpFailures, 'unexpected failed HTTP responses').toEqual([])
  expect(missingHttpFailures, 'expected failed HTTP responses were not observed').toEqual([])
  expect(audit.resourceErrors.length, 'resource console errors without an accounted HTTP failure')
    .toBeLessThanOrEqual(audit.observedHttpFailures.length)
  expect(audit.runtimeErrors, 'unexpected browser runtime errors').toEqual([])
})

const configured = {
  server: { host: '127.0.0.1', port: 23333, log_level: 'info' as const },
  database: { driver: 'sqlite' },
  storage: { type: 'local', path: './data/cache' },
  cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
  auth: { token_ttl: '168h' },
}

const sources = {
  'server.host': 'file',
  'server.port': 'file',
  'server.log_level': 'file',
  'database.driver': 'file',
  'storage.type': 'file',
  'storage.path': 'file',
  'cache.max_size_gb': 'file',
  'cache.ttl_index': 'file',
  'cache.ttl_blob': 'file',
  'cache.lru_threshold': 'file',
  'auth.token_ttl': 'file',
} as const

const editable = [
  'server.log_level',
  'cache.max_size_gb',
  'cache.ttl_index',
  'cache.ttl_blob',
  'cache.lru_threshold',
  'auth.token_ttl',
] as const

const snapshot = {
  configured,
  effective: configured,
  pending_restart: [],
  overrides: {},
  sources,
  editable,
  config_writable: true,
}

const updateResponse = {
  ...snapshot,
  configured: { ...configured, cache: { ...configured.cache, ttl_index: '10m' } },
  effective: configured,
  pending_restart: ['cache.ttl_index'] as const,
  changed: ['cache.ttl_index'] as const,
  applied_now: [] as const,
  restart_required: ['cache.ttl_index'] as const,
  blocked_by_override: [] as const,
}

async function openCacheSettings(page: Page) {
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /缓存策略/ }).click()
}

test('sends only dirty settings and renders server-authored restart status', async ({ page }) => {
  let requestBody: unknown
  await page.route('**/api/v1/admin/settings', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    requestBody = route.request().postDataJSON()
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(updateResponse) })
  })
  await openCacheSettings(page)
  await page.getByLabel(/索引 TTL/).fill('10m')
  await page.getByRole('button', { name: /^保存$/ }).click()
  expect(requestBody).toEqual({ cache: { ttl_index: '10m' } })
  await expect(page.getByText(/重启后生效/)).toBeVisible()
  await expect(page.getByLabel(/索引 TTL/)).toHaveValue('10m')
})

test('keeps dirty input and never reports success after 422', async ({ page }) => {
  expectHttpFailure(page, { method: 'PUT', pathname: '/api/v1/admin/settings', status: 422 })
  await mockAdminApi(page, {
    'PUT /api/v1/admin/settings': { status: 422, body: { code: 'INVALID_SETTING', message: 'bad ttl' } },
  })
  await openCacheSettings(page)
  const ttl = page.getByLabel(/索引 TTL/)
  await ttl.fill('999999999999999999999999h')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect(page.getByRole('alert')).toContainText('bad ttl')
  await expect(ttl).toHaveValue('999999999999999999999999h')
  await expect(page.getByText(/^已保存$/)).toHaveCount(0)
})

test('validates cache constraints locally and focuses the first invalid field', async ({ page }) => {
  let updateRequests = 0
  await mockAdminApi(page, {
    'PUT /api/v1/admin/settings': () => {
      updateRequests += 1
      return updateResponse
    },
  })
  await openCacheSettings(page)

  const maxSize = page.getByLabel(/最大缓存容量|Max Cache Size/)
  const threshold = page.getByLabel(/清理阈值|Cleanup Threshold/)
  const indexTTL = page.getByLabel(/索引 TTL|Index TTL/)
  const blobTTL = page.getByLabel(/文件 TTL|File TTL/)
  await maxSize.fill('0')
  await threshold.fill('101')
  await indexTTL.fill('7d')
  await blobTTL.fill('')
  await page.getByRole('button', { name: /^保存$|^Save$/ }).click()

  expect(updateRequests).toBe(0)
  await expect(maxSize).toBeFocused()
  await expect(maxSize).toHaveAttribute('aria-invalid', 'true')
  await expect(threshold).toHaveAttribute('aria-invalid', 'true')
  await expect(indexTTL).toHaveAttribute('aria-invalid', 'true')
  await expect(blobTTL).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText(/请输入大于 0 的整数|whole number greater than zero/)).toBeVisible()
  await expect(page.getByText(/请输入 1 到 100 之间的整数|whole number from 1 to 100/)).toBeVisible()
  await expect(page.getByText(/请输入有效的 Go duration|valid Go duration/)).toHaveCount(2)

  await maxSize.fill('24')
  await expect(maxSize).not.toHaveAttribute('aria-invalid', 'true')
})

test('rejects unsupported token never and submits a valid duration with Enter', async ({ page }) => {
  let updateRequests = 0
  let releaseUpdate!: (value: unknown) => void
  const pendingUpdate = new Promise<unknown>((resolve) => { releaseUpdate = resolve })
  const authUpdateResponse = {
    ...snapshot,
    configured: { ...configured, auth: { token_ttl: '24h' } },
    effective: { ...configured, auth: { token_ttl: '24h' } },
    changed: ['auth.token_ttl'] as const,
    applied_now: ['auth.token_ttl'] as const,
    restart_required: [] as const,
    blocked_by_override: [] as const,
  }
  await mockAdminApi(page, {
    'PUT /api/v1/admin/settings': (request: Request) => {
      updateRequests += 1
      expect(request.postDataJSON()).toEqual({ auth: { token_ttl: '24h' } })
      return pendingUpdate
    },
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /认证与安全|Auth & Security/ }).click()

  const tokenTTL = page.getByLabel(/Token 有效期|Token Validity/)
  await tokenTTL.fill('never')
  await page.keyboard.press('Enter')
  expect(updateRequests).toBe(0)
  await expect(tokenTTL).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText(/不支持.*never|never.*not supported/i)).toBeVisible()

  await tokenTTL.fill('24h')
  await page.keyboard.press('Enter')
  await expect.poll(() => updateRequests).toBe(1)
  const save = page.getByRole('button', { name: /保存中|Saving/ })
  await expect(save).toHaveAttribute('aria-busy', 'true')
  await expect(save).toBeDisabled()
  await expect(tokenTTL).toBeDisabled()

  releaseUpdate(authUpdateResponse)
  await expect(page.getByText(/已立即生效|Applied now/).first()).toBeVisible()
  await expect(tokenTTL).toHaveValue('24h')
})

test('renders applied-now results from the update response', async ({ page }) => {
  await mockAdminApi(page, {
    'PUT /api/v1/admin/settings': {
      ...snapshot,
      configured: { ...configured, server: { ...configured.server, log_level: 'debug' } },
      effective: { ...configured, server: { ...configured.server, log_level: 'debug' } },
      changed: ['server.log_level'],
      applied_now: ['server.log_level'],
      restart_required: [],
      blocked_by_override: [],
    },
  })
  await page.goto('/admin/settings')
  await page.getByLabel(/日志级别/).selectOption('debug')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect(page.getByText(/已立即生效/).first()).toBeVisible()
})

test('does not send a request when there are no changes', async ({ page }) => {
  await page.goto('/admin/settings')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect(page.getByText(/^没有变更$/)).toBeVisible()
})

test('renders environment-blocked results from the update response', async ({ page }) => {
  await mockAdminApi(page, {
    'PUT /api/v1/admin/settings': {
      ...snapshot,
      configured: { ...configured, server: { ...configured.server, log_level: 'debug' } },
      effective: { ...configured, server: { ...configured.server, log_level: 'warn' } },
      overrides: { 'server.log_level': 'DEPSILO_SERVER_LOG_LEVEL' },
      sources: { ...sources, 'server.log_level': 'env' },
      changed: ['server.log_level'],
      applied_now: [],
      restart_required: [],
      blocked_by_override: ['server.log_level'],
    },
  })
  await page.goto('/admin/settings')
  await page.getByLabel(/日志级别/).selectOption('debug')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect(page.getByText(/被环境变量覆盖/).first()).toBeVisible()
  await expect(page.getByText(/DEPSILO_SERVER_LOG_LEVEL/)).toBeVisible()
})

test('names environment overrides and shows configured and effective values', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': {
      ...snapshot,
      effective: { ...configured, server: { ...configured.server, log_level: 'warn' } },
      overrides: { 'server.log_level': 'DEPSILO_SERVER_LOG_LEVEL' },
      sources: { ...sources, 'server.log_level': 'env' },
    },
  })
  await page.goto('/admin/settings')
  await expect(page.getByText(/DEPSILO_SERVER_LOG_LEVEL/)).toBeVisible()
  await expect(page.getByText(/有效值.*warn/)).toBeVisible()
})

test('disables settings when the config file is not writable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': { ...snapshot, config_writable: false },
  })
  await openCacheSettings(page)
  await expect(page.getByText(/配置文件只读/)).toBeVisible()
  await expect(page.getByLabel(/索引 TTL/)).toBeDisabled()
  await expect(page.getByRole('button', { name: /^保存$/ })).toBeDisabled()
})

test('disables settings for a principal whose wire DTO has can_write false', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': {
      id: 2,
      username: 'viewer',
      role: 'viewer',
      enabled: true,
      auth_method: 'jwt',
      token_permissions: null,
      can_write: false,
    },
  })
  await openCacheSettings(page)
  await expect(page.getByText(/当前账号为只读/)).toBeVisible()
  await expect(page.getByLabel(/索引 TTL/)).toBeDisabled()
  await expect(page.getByRole('button', { name: /^保存$/ })).toBeDisabled()
})

test('recovers from an initial settings 500 via Retry', async ({ page }) => {
  expectHttpFailure(page, { method: 'GET', pathname: '/api/v1/admin/settings', status: 500 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': { status: 500, body: { code: 'SETTINGS_UNAVAILABLE', message: 'offline' } },
  })
  await page.goto('/admin/settings')
  await expect(page.getByText(/无法加载设置/)).toBeVisible()

  await mockAdminApi(page, { 'GET /api/v1/admin/settings': snapshot })
  await page.getByRole('button', { name: /重试/ }).click()
  await expect(page.getByRole('tab', { name: /基础配置/ })).toBeVisible()
  await expect(page.getByLabel(/监听地址/)).toHaveValue('127.0.0.1')
})

test('retains cached settings when a remount refetch fails', async ({ page }) => {
  expectHttpFailure(page, { method: 'GET', pathname: '/api/v1/admin/settings', status: 500 })
  let settingsRequests = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': () => {
      settingsRequests += 1
      return settingsRequests === 1
        ? snapshot
        : { status: 500, body: { code: 'SETTINGS_STALE', message: 'refetch failed' } }
    },
  })
  await page.goto('/admin/settings')
  await expect(page.getByLabel(/监听地址/)).toHaveValue('127.0.0.1')
  await page.getByRole('link', { name: /用户管理/ }).click()
  await expect(page.getByRole('heading', { name: '用户', exact: true })).toBeVisible()
  await page.getByRole('link', { name: /^系统设置$/ }).click()

  await expect(page.getByText(/显示的是上次成功加载的数据/)).toBeVisible()
  await expect(page.getByLabel(/监听地址/)).toHaveValue('127.0.0.1')
})

test('rebases untouched draft fields before building a patch after refetch', async ({ page }) => {
  let getRequests = 0
  let requestBody: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': () => {
      getRequests += 1
      return getRequests === 1
        ? snapshot
        : {
            ...snapshot,
            configured: { ...configured, cache: { ...configured.cache, max_size_gb: 30 } },
            effective: { ...configured, cache: { ...configured.cache, max_size_gb: 30 } },
          }
    },
    'PUT /api/v1/admin/settings': (request: Request) => {
      requestBody = request.postDataJSON()
      return updateResponse
    },
  })
  await openCacheSettings(page)
  await page.getByLabel(/索引 TTL/).fill('10m')
  await page.evaluate(() => {
    window.dispatchEvent(new Event('offline'))
    window.dispatchEvent(new Event('online'))
  })
  await expect.poll(() => getRequests).toBeGreaterThan(1)
  await expect(page.getByLabel(/最大缓存容量/)).toHaveValue('30')
  await page.getByRole('button', { name: /^保存$/ }).click()
  expect(requestBody).toEqual({ cache: { ttl_index: '10m' } })
})

for (const width of [320, 390]) {
  test(`uses horizontal tabs without page overflow at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 780 })
    await page.goto('/admin/settings')
    const tablist = page.getByRole('tablist')
    await expect(tablist).toHaveAttribute('data-orientation', 'horizontal')
    await expect(page.getByRole('tablist')).toHaveCount(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    expect(await page.evaluate(() => window.innerWidth)).toBe(width)
  })
}

test('uses a 180px vertical tab rail at 768px', async ({ page }) => {
  await page.setViewportSize({ width: 768, height: 900 })
  await page.goto('/admin/settings')
  const tablist = page.getByRole('tablist')
  await expect(tablist).toHaveAttribute('aria-orientation', 'vertical')
  await expect(tablist).toHaveCount(1)
  await expect.poll(() => tablist.evaluate(element => Math.round(element.getBoundingClientRect().width))).toBe(180)
})
