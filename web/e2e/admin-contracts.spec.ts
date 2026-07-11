import type { Page, Route } from '@playwright/test'
import type {
  AccessLogListResponse,
  AccessLogQuery,
  AdminSettingsResponse,
  AdminUpstream,
  AdminUpstreamListResponse,
  AuditLogListResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  ProjectListResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  SecurityPolicy,
  SecurityVulnerabilityPage,
  UpdateAdminSettingsRequest,
  UpdateAdminSettingsResponse,
  UpdateSecurityPolicyRequest,
  UpstreamMutationRequest,
} from '../src/lib/adminApi.types'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

type AdminOperation =
  | 'getSettings' | 'updateSettings'
  | 'listUpstreams' | 'createUpstream'
  | 'listVulnerabilities' | 'updateSecurityPolicy'
  | 'listLogs' | 'exportLogs' | 'listAuditLogs'
  | 'listProjects' | 'createProject' | 'listProjectPackages'

interface ExpectedCall<T> {
  operation: AdminOperation
  args?: unknown[]
  method: 'GET' | 'POST' | 'PUT'
  path: string
  query?: Record<string, string>
  body?: unknown
  response: T
}

async function callAdminApi<T>(page: Page, operation: AdminOperation, args: unknown[]): Promise<T> {
  return page.evaluate(async ({ operation, args }) => {
    const moduleUrl = new URL('/src/lib/api.ts', window.location.origin).href
    const { adminApi } = await import(moduleUrl)
    const method = adminApi[operation] as (...input: unknown[]) => Promise<{ data: unknown }>
    return (await method(...args)).data
  }, { operation, args }) as Promise<T>
}

async function expectAdminCall<T>(page: Page, expected: ExpectedCall<T>): Promise<T> {
  let matched = false
  const routePattern = `**${expected.path}*`
  const handler = async (route: Route) => {
    const request = route.request()
    const url = new URL(request.url())
    expect(request.method()).toBe(expected.method)
    expect(url.pathname).toBe(expected.path)
    expect(Object.fromEntries([...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)))).toEqual(expected.query ?? {})
    if ('body' in expected) expect(request.postDataJSON()).toEqual(expected.body)
    else expect(request.postData()).toBeNull()
    matched = true
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(expected.response) })
  }
  await page.route(routePattern, handler)
  try {
    const result = await callAdminApi<T>(page, expected.operation, expected.args ?? [])
    expect(matched).toBe(true)
    expect(result as unknown).toEqual(expected.response)
    return result
  } finally {
    await page.unroute(routePattern, handler)
  }
}

async function expectLogExport(
  page: Page,
  query: AccessLogQuery,
  expectedQuery: Record<string, string>,
  csv: string,
) {
  let matched = false
  const routePattern = '**/api/v1/admin/logs/export*'
  const handler = async (route: Route) => {
    const request = route.request()
    const url = new URL(request.url())
    expect(request.method()).toBe('GET')
    expect(url.pathname).toBe('/api/v1/admin/logs/export')
    expect(Object.fromEntries([...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)))).toEqual(expectedQuery)
    expect(request.postData()).toBeNull()
    matched = true
    await route.fulfill({ status: 200, contentType: 'text/csv', body: csv })
  }
  await page.route(routePattern, handler)
  try {
    const result = await page.evaluate(async ({ operation, args }) => {
      const moduleUrl = new URL('/src/lib/api.ts', window.location.origin).href
      const { adminApi } = await import(moduleUrl)
      const method = adminApi[operation] as (...input: unknown[]) => Promise<{ data: Blob }>
      const blob = (await method(...args)).data
      return { type: blob.type, text: await blob.text() }
    }, { operation: 'exportLogs' as const, args: [query] })
    expect(matched).toBe(true)
    expect(result).toEqual({ type: 'text/csv', text: csv })
  } finally {
    await page.unroute(routePattern, handler)
  }
}

const settings: AdminSettingsResponse = {
  configured: {
    server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
    database: { driver: 'sqlite' },
    storage: { type: 'local', path: './data/cache' },
    cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
    auth: { token_ttl: '168h' },
  },
  effective: {
    server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
    database: { driver: 'sqlite' },
    storage: { type: 'local', path: './data/cache' },
    cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '72h', lru_threshold: 90 },
    auth: { token_ttl: '168h' },
  },
  pending_restart: ['cache.ttl_blob'],
  overrides: {},
  sources: {
    'server.host': 'file', 'server.port': 'file', 'server.log_level': 'file',
    'database.driver': 'file', 'storage.type': 'file', 'storage.path': 'file',
    'cache.max_size_gb': 'file', 'cache.ttl_index': 'file', 'cache.ttl_blob': 'file',
    'cache.lru_threshold': 'file', 'auth.token_ttl': 'file',
  },
  editable: ['server.log_level', 'cache.max_size_gb', 'cache.ttl_index', 'cache.ttl_blob', 'cache.lru_threshold', 'auth.token_ttl'],
  config_writable: true,
}

const settingsPatch = { cache: { ttl_blob: '120h' } } satisfies UpdateAdminSettingsRequest
const settingsUpdate = {
  ...settings,
  configured: { ...settings.configured, cache: { ...settings.configured.cache, ttl_blob: '120h' } },
  pending_restart: ['cache.ttl_blob'],
  changed: ['cache.ttl_blob'],
  applied_now: [],
  restart_required: ['cache.ttl_blob'],
  blocked_by_override: [],
} satisfies UpdateAdminSettingsResponse

const upstream = {
  id: 7, adapter_type: 'pypi', name: 'primary', url: 'https://pypi.example/simple', proxy: '',
  priority: 1, probe_mode: 'active', probe_interval: '30m', healthy: true, avg_latency_ms: 18,
  success_rate: 0.99, last_checked_at: '2026-07-10T00:00:00Z', worker_running: true,
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
} satisfies AdminUpstream
const upstreamList = { items: [upstream], total: 1 } satisfies AdminUpstreamListResponse
const upstreamCreate = {
  adapter_type: 'pypi', name: 'secondary', url: 'https://backup.example/simple', proxy: '', priority: 2,
  probe_mode: 'passive', probe_interval: '30m',
} satisfies UpstreamMutationRequest
const createdUpstream = { ...upstream, id: 8, ...upstreamCreate } satisfies AdminUpstream

const vulnerabilities = {
  items: [{
    id: 11, osv_id: 'GHSA-fixture', ecosystem: 'pypi', package_name: 'requests', affected_ranges: '<2.32.0',
    severity: 'high', cvss_score: 8.1, summary: 'fixture summary', details: 'fixture details', aliases: 'CVE-2026-0001',
    references: 'https://osv.dev/GHSA-fixture', published_at: '2026-07-01T00:00:00Z',
    modified_at: '2026-07-02T00:00:00Z', created_at: '2026-07-03T00:00:00Z', updated_at: '2026-07-04T00:00:00Z',
  }],
  total: 1,
  page: 2,
} satisfies SecurityVulnerabilityPage
const policyRequest = { auto_block_enabled: true, min_cvss_score: 8.5 } satisfies UpdateSecurityPolicyRequest
const policy = {
  id: 3, ecosystem: 'pypi', ...policyRequest, created_by: 'admin',
  created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
} satisfies SecurityPolicy

const logs = {
  items: [{
    id: 21, adapter_type: 'pypi', method: 'GET', cache_key: 'pypi/requests', package_name: 'requests', hit: true,
    upstream: 'primary', latency_ms: 12, status_code: 200, client_ip: '127.0.0.1', bytes_sent: 1024,
    created_at: '2026-07-10T00:00:00Z',
  }],
  total: 1, page: 2, page_size: 50,
} satisfies AccessLogListResponse
const logQuery = { page: 2, page_size: 50, search: 'requests', adapter_type: 'pypi', hit: true } satisfies AccessLogQuery
const expectedLogQuery = { adapter_type: 'pypi', hit: 'true', page: '2', page_size: '50', search: 'requests' }
const logsCSV = 'Time,Method,Ecosystem,Package,Hit,Status,Latency(ms),Bytes,Upstream,Client IP,Cache Key\n2026-07-10T00:00:00Z,GET,pypi,requests,true,200,12,1024,primary,127.0.0.1,pypi/requests\n'

const audit = {
  items: [{
    id: 31, ecosystem: 'pypi', package_name: 'requests', version: '2.32.0', action: 'proxy', cache_result: 'hit',
    client_ip: '127.0.0.1', user_agent: 'fixture-agent', upstream_url: 'https://pypi.example/simple',
    latency_ms: 12, bytes_sent: 1024, status_code: 200, created_at: '2026-07-10T00:00:00Z',
  }],
  total: 1, page: 3,
} satisfies AuditLogListResponse

const projects = {
  items: [{
    id: 41, name: 'Fixture', slug: 'fixture', description: 'contract fixture', package_count: 2,
    last_activity_at: '2026-07-10T00:00:00Z', created_at: '2026-07-01T00:00:00Z', updated_at: '2026-07-10T00:00:00Z',
  }],
  total: 1,
} satisfies ProjectListResponse
const projectRequest = { name: 'Created Fixture', description: 'created by contract test' } satisfies CreateProjectRequest
const createdProject = {
  id: 42, name: projectRequest.name, slug: 'created-fixture', description: projectRequest.description,
  token: 'project-token-once', proxy_url: '/p/created-fixture', created_at: '2026-07-10T00:00:00Z',
} satisfies CreateProjectResponse
const projectPackageQuery = { page: 2, per_page: 25, ecosystem: 'pypi', search: 'requests' } satisfies ProjectPackageQuery
const projectPackages = {
  items: [{
    ecosystem: 'pypi', package_name: 'requests', version: '2.32.0',
    first_seen_at: '2026-07-01T00:00:00Z', last_seen_at: '2026-07-10T00:00:00Z', download_count: 17,
  }],
  total: 1,
  page: 2,
} satisfies ProjectPackagesResponse

test.beforeEach(async ({ page }) => {
  await page.goto('/admin/login')
})

test('Settings client emits exact GET and PUT contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'getSettings', method: 'GET', path: '/api/v1/admin/settings', response: settings })
  await expectAdminCall(page, { operation: 'updateSettings', args: [settingsPatch], method: 'PUT', path: '/api/v1/admin/settings', body: settingsPatch, response: settingsUpdate })
})

test('Upstreams client emits exact list and create contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'listUpstreams', method: 'GET', path: '/api/v1/admin/upstreams', response: upstreamList })
  await expectAdminCall(page, { operation: 'createUpstream', args: [upstreamCreate], method: 'POST', path: '/api/v1/admin/upstreams', body: upstreamCreate, response: createdUpstream })
})

test('Security client uses package/per_page and canonical policy fields', async ({ page }) => {
  const query = { page: 2, per_page: 25, ecosystem: 'pypi', severity: 'high', package: 'requests' } as const
  await expectAdminCall(page, {
    operation: 'listVulnerabilities', args: [query], method: 'GET', path: '/api/v1/admin/security/vulnerabilities',
    query: { ecosystem: 'pypi', package: 'requests', page: '2', per_page: '25', severity: 'high' }, response: vulnerabilities,
  })
  await expectAdminCall(page, {
    operation: 'updateSecurityPolicy', args: ['pypi', policyRequest], method: 'PUT',
    path: '/api/v1/admin/security/policies/pypi', body: policyRequest, response: policy,
  })
})

test('Logs client emits canonical pagination and filters', async ({ page }) => {
  await expectAdminCall(page, {
    operation: 'listLogs', args: [logQuery], method: 'GET', path: '/api/v1/admin/logs',
    query: expectedLogQuery, response: logs,
  })
  await expectLogExport(page, logQuery, expectedLogQuery, logsCSV)
})

test('Audit client emits package rather than legacy search', async ({ page }) => {
  const query = {
    page: 3, page_size: 50, ecosystem: 'pypi', package: 'requests', ip: '127.0.0.1', result: 'hit',
    start: '2026-07-01T00:00:00Z', end: '2026-07-10T00:00:00Z',
  } as const
  await expectAdminCall(page, {
    operation: 'listAuditLogs', args: [query], method: 'GET', path: '/api/v1/admin/audit-logs',
    query: {
      ecosystem: 'pypi', end: '2026-07-10T00:00:00Z', ip: '127.0.0.1', package: 'requests', page: '3',
      page_size: '50', result: 'hit', start: '2026-07-01T00:00:00Z',
    },
    response: audit,
  })
})

test('Projects client emits exact list and create contracts', async ({ page }) => {
  await expectAdminCall(page, { operation: 'listProjects', method: 'GET', path: '/api/v1/admin/projects', response: projects })
  await expectAdminCall(page, { operation: 'createProject', args: [projectRequest], method: 'POST', path: '/api/v1/admin/projects', body: projectRequest, response: createdProject })
  await expectAdminCall(page, {
    operation: 'listProjectPackages', args: [41, projectPackageQuery], method: 'GET',
    path: '/api/v1/admin/projects/41/packages',
    query: { ecosystem: 'pypi', page: '2', per_page: '25', search: 'requests' },
    response: projectPackages,
  })

  const fallbackProject = { ...createdProject, proxy_url: '' } satisfies CreateProjectResponse
  await mockAdminApi(page, { 'POST /api/v1/admin/projects': fallbackProject })
  await page.addInitScript(() => {
    const clipboardState = window as Window & { __copiedProjectProxy?: string }
    clipboardState.__copiedProjectProxy = ''
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (text: string) => {
          clipboardState.__copiedProjectProxy = text
        },
      },
    })
  })
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin/projects')
  await page.getByRole('button', { name: 'Create Project' }).first().click()
  await page.getByLabel('Project Name').fill(projectRequest.name)
  await page.getByLabel('Description').fill(projectRequest.description)
  await page.getByRole('button', { name: 'Save' }).click()

  const expectedProxy = `${new URL(page.url()).origin}/p/created-fixture`
  const tokenDialog = page.getByRole('dialog', { name: 'Project Token' })
  await expect(tokenDialog.getByText(expectedProxy, { exact: true })).toBeVisible()
  await tokenDialog.getByRole('button', { name: 'Copy proxy URL' }).click()
  await expect.poll(() => page.evaluate(() => (
    window as Window & { __copiedProjectProxy?: string }
  ).__copiedProjectProxy)).toBe(expectedProxy)
})
