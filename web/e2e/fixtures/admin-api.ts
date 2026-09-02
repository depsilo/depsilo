import { test as base, expect, type Page, type Request, type Route } from '@playwright/test'
import type {
  AccessLogListResponse,
  AdminSettingsResponse,
  AdminSettingsSnapshot,
  AdminUpstreamLatenciesResponse,
  AdminUpstreamListResponse,
  AdminUpstreamUpdateListResponse,
  AuditLogListResponse,
  CompileCacheCredentialListResponse,
  CompileCacheStatusResponse,
  NowResponse,
  OnboardingStatusResponse,
  PolicyStatus,
  ProjectListResponse,
  RecentDownloadsResponse,
  RuleTestResponse,
  SecurityDashboard,
  SecurityPackagePage,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
  SetupStatusResponse,
} from '../../src/lib/adminApi.types'

export type JsonValue = unknown
export interface MockHttpResponse {
  status: number
  body: JsonValue
  contentType?: string
  serialize?: 'json' | 'text'
}
export type AdminApiResponder = (request: Request) => JsonValue | Promise<JsonValue>
export type AdminApiOverride = JsonValue
export type AdminApiOverrides = Record<string, AdminApiOverride>
export type UiLocale = 'zh' | 'en'
export type UiTheme = 'light' | 'dark'

const configuredSettings: AdminSettingsSnapshot = {
  server: { host: '127.0.0.1', port: 23333, log_level: 'info' },
  database: { driver: 'sqlite' },
  storage: { type: 'local', path: './data/cache' },
  cache: { max_size_gb: 20, ttl_index: '5m', ttl_blob: '96h', lru_threshold: 90 },
  auth: { token_ttl: '168h' },
}
const effectiveSettings: AdminSettingsSnapshot = {
  ...configuredSettings,
  cache: { ...configuredSettings.cache, ttl_blob: '72h' },
}
const settingSources: AdminSettingsResponse['sources'] = {
  'server.host': 'file', 'server.port': 'file', 'server.log_level': 'file',
  'database.driver': 'file', 'storage.type': 'file', 'storage.path': 'file',
  'cache.max_size_gb': 'file', 'cache.ttl_index': 'file', 'cache.ttl_blob': 'file',
  'cache.lru_threshold': 'file', 'auth.token_ttl': 'file',
}
const editableSettings: AdminSettingsResponse['editable'] = [
  'server.log_level', 'cache.max_size_gb', 'cache.ttl_index',
  'cache.ttl_blob', 'cache.lru_threshold', 'auth.token_ttl',
]

const existingAdminApiDefaults: Record<string, JsonValue> = {
  'GET /api/v1/auth/me': { id: 1, username: 'admin', role: 'admin', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: true },
  'GET /api/v1/integration-prompt': { status: 200, body: '# Depsilo project integration\nUse the configured Depsilo package mirror.', contentType: 'text/plain; charset=utf-8', serialize: 'text' },
  'GET /api/v1/stats': {
    service: { version: 'dev', status: 'healthy' },
    week: {},
    upstreams: [],
    extra_indexes: [{ name: 'pytorch', kind: 'pytorch', path: 'pypi-torch' }],
  },
  'GET /api/v1/latency-series': {},
  'GET /api/v1/now': {
    status: 'healthy',
    uptime_seconds: 0,
    now_unix: 1786032000,
    version: 'dev',
    rate: { requests_per_min: 0, ingress_bps: 0, egress_bps: 0, has_data: false },
    upstreams: { healthy: 0, total: 0 },
    sparkline: [],
  } satisfies NowResponse,
  'GET /api/v1/admin/dashboard': { summary: {}, top_packages: [], upstreams: [] },
  'GET /api/v1/admin/policy/status': {
    status: 'healthy',
    using_stale_snapshot: false,
    snapshot_loaded_at: '2026-09-02T01:12:00Z',
    snapshot_age_seconds: 0,
    refresh_failures: 0,
    on_load_error: 'use_stale_then_allow',
  } satisfies PolicyStatus,
  'GET /api/v1/admin/onboarding/status': {
    status: 'completed',
    started_at: '2026-08-06T00:00:00Z',
    next_after_id: 0,
    events: [],
    has_more: false,
  } satisfies OnboardingStatusResponse,
  'PUT /api/v1/admin/onboarding': { status: 'completed' },
  'GET /api/v1/admin/dashboard/trends': { points: [] },
  'GET /api/v1/admin/bandwidth': { summary: {}, daily: [], by_ecosystem: [], top_packages: [], by_upstream: [] },
  'GET /api/v1/admin/quarantine/events': { items: [], total: 0 },
  'GET /api/v1/admin/quarantine/approvals': { items: [], total: 0 },
  'GET /api/v1/admin/blocklist/status': { enabled: true, count: 0 },
  'GET /api/v1/admin/blocklist/overrides': { items: [] },
  'GET /api/v1/admin/cache/distribution': { total_size: 0, max_size: 1, by_type: [], top_packages: [] },
  'GET /api/v1/admin/cache': { items: [], total: 0 },
  'GET /api/v1/admin/cache/indexes': { items: [], summary: [], total: 0, page: 1, page_size: 25 },
  'GET /api/v1/admin/users': [{ id: 1, username: 'admin', role: 'admin', enabled: true }],
  'GET /api/v1/admin/tokens': [],
  'GET /api/v1/admin/license/status': { is_pro: false, source: 'none', days_left: 0, trial_used: false, trial_available: true, last_checked: '2026-07-10T00:00:00Z' },
  'GET /api/v1/admin/rules': [],
  'POST /api/v1/admin/rules/test': { allowed: true, matched_rule: null, candidates: [] } satisfies RuleTestResponse,
  'GET /api/v1/admin/security/policies': [],
  'GET /api/v1/admin/webhooks': [],
}

const canonicalAdminApiDefaults = {
  'GET /api/v1/setup/status': {
    needs_setup: false,
    token_required: false,
  } satisfies SetupStatusResponse,
  'GET /api/v1/admin/settings': {
    configured: configuredSettings,
    effective: effectiveSettings,
    pending_restart: ['cache.ttl_blob'],
    overrides: {},
    sources: settingSources,
    editable: editableSettings,
    config_writable: true,
  } satisfies AdminSettingsResponse,
  'GET /api/v1/admin/upstreams': { items: [], total: 0 } satisfies AdminUpstreamListResponse,
  'GET /api/v1/admin/upstreams/latency': { series: [] } satisfies AdminUpstreamLatenciesResponse,
  'GET /api/v1/admin/upstream-updates': { items: [], total: 0, next_cursor: null } satisfies AdminUpstreamUpdateListResponse,
  'GET /api/v1/admin/compile-cache/status': {
    enabled: true,
    endpoint: 'http://localhost:23333/ccache/v1/{namespace}',
    endpoints: {
      ccache: 'http://localhost:23333/ccache/v1/{namespace}',
      sccache: 'http://localhost:23333/sccache/v1/{namespace}',
    },
    stats: { size_bytes: 0, max_bytes: 21474836480, entries: 0, max_entries: 500000, hits: 0, namespace_count: 0 },
  } satisfies CompileCacheStatusResponse,
  'GET /api/v1/admin/compile-cache/credentials': {
    items: [],
    total: 0,
  } satisfies CompileCacheCredentialListResponse,
  'GET /api/v1/admin/dashboard/recent-downloads': { items: [] } satisfies RecentDownloadsResponse,
  'GET /api/v1/admin/logs': { items: [], total: 0, page: 1, page_size: 50 } satisfies AccessLogListResponse,
  'GET /api/v1/admin/audit-logs': { items: [], total: 0, page: 1 } satisfies AuditLogListResponse,
  'GET /api/v1/admin/security/dashboard': {
    total_vulnerabilities: 0,
    affected_packages: 0,
    by_severity: { critical: 0, high: 0, medium: 0, low: 0 },
    auto_blocked_count: 0,
    last_scan_at: null,
    scan_in_progress: false,
  } satisfies SecurityDashboard,
  'GET /api/v1/admin/security/vulnerabilities': { items: [], total: 0, page: 1 } satisfies SecurityVulnerabilityPage,
  'GET /api/v1/admin/security/packages': { items: [], total: 0, page: 1 } satisfies SecurityPackagePage,
  'GET /api/v1/admin/security/suggestions': { items: [], total: 0, page: 1 } satisfies SecuritySuggestionPage,
  'GET /api/v1/admin/projects': { items: [], total: 0 } satisfies ProjectListResponse,
}

export const adminApiDefaults: Record<string, JsonValue> = {
  ...existingAdminApiDefaults,
  ...canonicalAdminApiDefaults,
}

const keyFor = (route: Route) => {
  const request = route.request()
  const url = new URL(request.url())
  return `${request.method()} ${url.pathname}`
}

function isMockHttpResponse(value: JsonValue): value is MockHttpResponse {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return false
  const candidate = value as Record<string, unknown>
  return typeof candidate.status === 'number' && 'body' in candidate
}

export async function setUiPreferences(page: Page, theme: UiTheme, locale: UiLocale) {
  await page.addInitScript(({ theme, locale }) => {
    localStorage.setItem('lang', locale)
    localStorage.setItem('depsilo-theme', theme)
    const applyTheme = () => {
      const root = document.documentElement
      if (!root) return false
      root.dataset.theme = theme
      root.classList.remove('light', 'dark')
      root.classList.add(theme)
      return true
    }
    if (!applyTheme()) window.addEventListener('DOMContentLoaded', applyTheme, { once: true })
  }, { theme, locale })
}

export async function expectResolvedUiPreferences(page: Page, theme: UiTheme, locale: UiLocale) {
  await expect.poll(() => page.evaluate(() => ({
    storedLocale: localStorage.getItem('lang'),
    storedTheme: localStorage.getItem('depsilo-theme'),
    domLocale: document.documentElement.lang,
    domTheme: document.documentElement.dataset.theme,
    hasThemeClass: document.documentElement.classList.contains(document.documentElement.dataset.theme || ''),
  }))).toEqual({
    storedLocale: locale,
    storedTheme: theme,
    domLocale: locale === 'zh' ? 'zh-CN' : 'en',
    domTheme: theme,
    hasThemeClass: true,
  })
}

interface AdminApiFixtureState {
  overrides: AdminApiOverrides
  unmatched: string[]
  assertMatched(): void
}

const adminApiFixtureStates = new WeakMap<Page, AdminApiFixtureState>()

interface AdminApiFixtureOptions {
  initialToken?: string | null
}

export async function mockAdminApi(
  page: Page,
  overrides: AdminApiOverrides = {},
  options: AdminApiFixtureOptions = {},
) {
  const existing = adminApiFixtureStates.get(page)
  if (existing) {
    Object.assign(existing.overrides, overrides)
    return existing
  }

  const unmatched: string[] = []
  const state: AdminApiFixtureState = {
    overrides: { ...overrides },
    unmatched,
    assertMatched: () => expect(unmatched, `unmatched API requests: ${unmatched.join(', ')}`).toEqual([]),
  }
  adminApiFixtureStates.set(page, state)
  const initialToken = options.initialToken === undefined ? 'e2e-token' : options.initialToken
  await page.addInitScript((token) => {
    if (token === null) localStorage.removeItem('token')
    else localStorage.setItem('token', token)
    if (!localStorage.getItem('lang')) localStorage.setItem('lang', 'zh')
    if (!localStorage.getItem('depsilo-theme')) localStorage.setItem('depsilo-theme', 'dark')
    const theme = localStorage.getItem('depsilo-theme') === 'light' ? 'light' : 'dark'
    const applyTheme = () => {
      const root = document.documentElement
      if (!root) return false
      root.dataset.theme = theme
      root.classList.remove('light', 'dark')
      root.classList.add(theme)
      return true
    }
    if (!applyTheme()) window.addEventListener('DOMContentLoaded', applyTheme, { once: true })
  }, initialToken)
  await page.route('**/api/v1/**', async route => {
    const key = keyFor(route)
    const selected = Object.prototype.hasOwnProperty.call(state.overrides, key) ? state.overrides[key] : adminApiDefaults[key]
    if (selected === undefined) {
      state.unmatched.push(key)
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ code: 'UNMATCHED_E2E_API', message: key }) })
      return
    }
    const response = typeof selected === 'function'
      ? await (selected as AdminApiResponder)(route.request())
      : selected
    const wrapped: MockHttpResponse = isMockHttpResponse(response) ? response : { status: 200, body: response }
    await route.fulfill({
      status: wrapped.status,
      contentType: wrapped.contentType ?? 'application/json',
      body: wrapped.serialize === 'text' ? String(wrapped.body) : JSON.stringify(wrapped.body),
    })
  })
  return state
}

export const test = base.extend<{
  api: Awaited<ReturnType<typeof mockAdminApi>>
  initialToken: string | null
}>({
  initialToken: ['e2e-token', { option: true }],
  api: [async ({ page, initialToken }, use) => {
    const api = await mockAdminApi(page, {}, { initialToken })
    await use(api)
    api.assertMatched()
  }, { auto: true }],
})

export { expect }
