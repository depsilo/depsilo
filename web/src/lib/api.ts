import axios from 'axios'
import type {
  AccessLogListResponse,
  AccessLogQuery,
  AdminSettingsResponse,
  AdminUpstream,
  AdminUpstreamLatenciesResponse,
  AdminUpstreamListResponse,
  AdminUpstreamUpdateListResponse,
  AdminUpstreamUpdateQuery,
  AdminUser,
  APITokenSummary,
  ApproveSuggestionRequest,
  ApproveSuggestionResponse,
  AuditLogListResponse,
  AuditLogQuery,
  BandwidthReportResponse,
  CacheDistributionResponse,
  CacheDeleteResponse,
  CacheCleanupResponse,
  CacheIndexListResponse,
  CacheIndexQuery,
  CacheIndexRefreshResponse,
  CacheListResponse,
  CacheQuery,
  CheckUpstreamResponse,
  CreateAPITokenRequest,
  CreateAPITokenResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  CreateUserRequest,
  DeleteProjectResponse,
  DeleteUpstreamResponse,
  DashboardResponse,
  DashboardTrendsResponse,
  DismissSuggestionResponse,
  LoginResponse,
  Principal,
  Project,
  ProjectDetail,
  ProjectListResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  ProjectSBOMQuery,
  QuarantineQuery,
  RefreshResponse,
  RegenerateProjectTokenResponse,
  RuleListResponse,
  RuleRecord,
  RuleRequest,
  RuleTestRequest,
  RuleTestResponse,
  SecurityBaseQuery,
  SecurityDashboard,
  SecurityImportResponse,
  SecurityPackagePage,
  SecurityPolicy,
  SecurityQuery,
  SecurityScanResponse,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
  SetupRequest,
  SetupCompleteResponse,
  UpdateProjectRequest,
  UpdateAdminSettingsRequest,
  UpdateAdminSettingsResponse,
  UpdateSecurityPolicyRequest,
  UpdateUserRequest,
  UpstreamMutationRequest,
} from './adminApi.types'

const api = axios.create({ baseURL: '/api/v1' })

export const AUTH_SESSION_EXPIRED_EVENT = 'depsilo:auth-session-expired'

function isLoginRequest(url?: string) {
  if (!url) return false
  try {
    return new URL(url, window.location.origin).pathname.endsWith('/auth/login')
  } catch {
    return url.endsWith('/auth/login')
  }
}

// Request interceptor: attach JWT from localStorage
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token && !isLoginRequest(config.url)) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Let React Router own navigation so the attempted deep link survives. Only a
// request that actually carried the current session can expire it; a rejected
// login attempt must remain on the form and display its API error.
api.interceptors.response.use(
  (res) => res,
  (error: unknown) => {
    if (axios.isAxiosError(error) && error.response?.status === 401 && !isLoginRequest(error.config?.url)) {
      const authorization = error.config?.headers?.get?.('Authorization')
      const currentToken = localStorage.getItem('token')
      if (currentToken && typeof authorization === 'string' && authorization === `Bearer ${currentToken}`) {
        localStorage.removeItem('token')
        window.dispatchEvent(new Event(AUTH_SESSION_EXPIRED_EVENT))
      }
    }
    // 402 PRO_REQUIRED is left to each Pro-gated page to render an
    // inline ProRequiredCallout — the previous app-wide modal popup
    // proved too interruptive for users casually clicking around the
    // admin sidebar.
    return Promise.reject(error)
  }
)

export const statsApi = {
  getStats: () => api.get('/stats'),
  getNow: () => api.get('/now'),
  getLatencySeries: () => api.get('/latency-series'),
}

export const packagesApi = {
  list: (params?: { q?: string; type?: string; sort?: string; page?: number; per_page?: number }) =>
    api.get('/packages', { params }),
  detail: (type: string, name: string) =>
    api.get(`/packages/${type}/${name}`),
}

export const authApi = {
  login: (data: { username: string; password: string }) => api.post<LoginResponse>('/auth/login', data),
  logout: () => api.post<{ message: string }>('/auth/logout'),
  me: () => api.get<Principal>('/auth/me'),
  refresh: () => api.post<RefreshResponse>('/auth/refresh'),
}

export const adminApi = {
  getDashboard: () => api.get<DashboardResponse>('/admin/dashboard'),
  getDashboardTrends: (range_: string = '7d') =>
    api.get<DashboardTrendsResponse>('/admin/dashboard/trends', { params: { range: range_ } }),

  // Cache
  listCache: (params: CacheQuery) => api.get<CacheListResponse>('/admin/cache', { params }),
  deleteCache: (id: number) => api.delete<CacheDeleteResponse>(`/admin/cache/${id}`),
  cleanupCache: () => api.post<CacheCleanupResponse>('/admin/cache/cleanup'),
  getCacheDistribution: () => api.get<CacheDistributionResponse>('/admin/cache/distribution'),
  listCacheIndexes: (params: CacheIndexQuery) => api.get<CacheIndexListResponse>('/admin/cache/indexes', { params }),
  refreshCacheIndex: (id: number) => api.post<CacheIndexRefreshResponse>(`/admin/cache/indexes/${id}/refresh`),
  warmupCache: (data: { ecosystem: string; packages: string[] }) => api.post('/admin/cache/warmup', data),

  // Upstreams
  listUpstreams: () => api.get<AdminUpstreamListResponse>('/admin/upstreams'),
  createUpstream: (data: UpstreamMutationRequest) => api.post<AdminUpstream>('/admin/upstreams', data),
  updateUpstream: (id: number, data: UpstreamMutationRequest) => api.put<AdminUpstream>(`/admin/upstreams/${id}`, data),
  deleteUpstream: (id: number) => api.delete<DeleteUpstreamResponse>(`/admin/upstreams/${id}`),
  checkUpstream: (id: number) => api.post<CheckUpstreamResponse>(`/admin/upstreams/${id}/check`),
  getUpstreamLatencies: (range_: string = '24h') =>
    api.get<AdminUpstreamLatenciesResponse>('/admin/upstreams/latency', { params: { range: range_ } }),
  listUpstreamUpdates: (params: AdminUpstreamUpdateQuery) =>
    api.get<AdminUpstreamUpdateListResponse>('/admin/upstream-updates', { params }),

  // Logs
  listLogs: (params: AccessLogQuery) => api.get<AccessLogListResponse>('/admin/logs', { params }),
  exportLogs: (params: AccessLogQuery) => api.get<Blob>('/admin/logs/export', { params, responseType: 'blob' }),

  // Users
  listUsers: () => api.get<AdminUser[]>('/admin/users'),
  createUser: (data: CreateUserRequest) => api.post<AdminUser>('/admin/users', data),
  updateUser: (id: number, data: UpdateUserRequest) => api.put<AdminUser>(`/admin/users/${id}`, data),
  deleteUser: (id: number) => api.delete(`/admin/users/${id}`),

  // Tokens
  listTokens: () => api.get<APITokenSummary[]>('/admin/tokens'),
  createToken: (data: CreateAPITokenRequest) => api.post<CreateAPITokenResponse>('/admin/tokens', data),
  deleteToken: (id: number) => api.delete(`/admin/tokens/${id}`),

  // Settings
  getSettings: () => api.get<AdminSettingsResponse>('/admin/settings'),
  updateSettings: (data: UpdateAdminSettingsRequest) => api.put<UpdateAdminSettingsResponse>('/admin/settings', data),

  // Audit Logs (open source)
  listAuditLogs: (params: AuditLogQuery) => api.get<AuditLogListResponse>('/admin/audit-logs', { params }),
  exportAuditLogs: (params: AuditLogQuery) => api.get<Blob>('/admin/audit-logs/export', { params, responseType: 'blob' }),

  // Package Rules (Pro)
  listRules: () => api.get<RuleListResponse>('/admin/rules'),
  createRule: (data: RuleRequest) => api.post<RuleRecord>('/admin/rules', data),
  updateRule: (id: number, data: RuleRequest) => api.put<RuleRecord>(`/admin/rules/${id}`, data),
  deleteRule: (id: number) => api.delete(`/admin/rules/${id}`),
  testRule: (data: RuleTestRequest) => api.post<RuleTestResponse>('/admin/rules/test', data),

  // Bandwidth report
  getBandwidthReport: (params: { range?: string; start?: string; end?: string }) =>
    api.get<BandwidthReportResponse>('/admin/bandwidth', { params }),

  // Package Security (Pro)
  getSecurityDashboard: () => api.get<SecurityDashboard>('/admin/security/dashboard'),
  listVulnerabilities: (params: SecurityQuery) => api.get<SecurityVulnerabilityPage>('/admin/security/vulnerabilities', { params }),
  listVulnerablePackages: (params: SecurityBaseQuery) => api.get<SecurityPackagePage>('/admin/security/packages', { params }),
  listSuggestions: (params: SecurityBaseQuery) => api.get<SecuritySuggestionPage>('/admin/security/suggestions', { params }),
  approveSuggestion: (vulnerabilityID: number, data: ApproveSuggestionRequest = {}) => api.post<ApproveSuggestionResponse>(`/admin/security/suggestions/${vulnerabilityID}/approve`, data),
  dismissSuggestion: (vulnerabilityID: number) => api.post<DismissSuggestionResponse>(`/admin/security/suggestions/${vulnerabilityID}/dismiss`),
  triggerSecurityScan: () => api.post<SecurityScanResponse>('/admin/security/scan'),
  importVulnerabilities: (formData: FormData) => api.post<SecurityImportResponse>('/admin/security/import', formData, { headers: { 'Content-Type': 'multipart/form-data' } }),
  listSecurityPolicies: () => api.get<SecurityPolicy[]>('/admin/security/policies'),
  updateSecurityPolicy: (ecosystem: string, data: UpdateSecurityPolicyRequest) => api.put<SecurityPolicy>(`/admin/security/policies/${ecosystem}`, data),

  // Supply-chain quarantine (open-source wedge — NOT Pro)
  listQuarantineEvents: (params: QuarantineQuery) => api.get('/admin/quarantine/events', { params }),
  listQuarantineApprovals: (params: QuarantineQuery) => api.get('/admin/quarantine/approvals', { params }),
  approveQuarantine: (data: { ecosystem: string; package: string; version: string; reason: string }) =>
    api.post('/admin/quarantine/approve', data),
  revokeQuarantineApproval: (id: number, data: { reason: string }) =>
    api.delete(`/admin/quarantine/approvals/${id}`, { data }),

  // Known-malicious blocklist (open-source, DIRECTION Task 2)
  getBlocklistStatus: () => api.get('/admin/blocklist/status'),
  triggerBlocklistSync: () => api.post('/admin/blocklist/sync'),
  listBlocklistOverrides: () => api.get('/admin/blocklist/overrides'),
  createBlocklistOverride: (data: { ecosystem: string; package: string; version: string; reason: string }) =>
    api.post('/admin/blocklist/overrides', data),
  revokeBlocklistOverride: (id: number, data: { reason: string }) =>
    api.delete(`/admin/blocklist/overrides/${id}`, { data }),

  // Projects (Pro)
  listProjects: () => api.get<ProjectListResponse>('/admin/projects'),
  createProject: (data: CreateProjectRequest) => api.post<CreateProjectResponse>('/admin/projects', data),
  getProject: (id: number) => api.get<ProjectDetail>(`/admin/projects/${id}`),
  updateProject: (id: number, data: UpdateProjectRequest) => api.put<Project>(`/admin/projects/${id}`, data),
  deleteProject: (id: number) => api.delete<DeleteProjectResponse>(`/admin/projects/${id}`),
  listProjectPackages: (id: number, params: ProjectPackageQuery) => api.get<ProjectPackagesResponse>(`/admin/projects/${id}/packages`, { params }),
  regenerateProjectToken: (id: number) => api.post<RegenerateProjectTokenResponse>(`/admin/projects/${id}/token`),
  exportSbom: (id: number, params: ProjectSBOMQuery) => api.get<Blob>(`/admin/projects/${id}/sbom`, { params, responseType: 'blob' }),
}

// Setup wizard (no auth)
export type SetupDecision =
  | { kind: 'configured' }
  | { kind: 'setup-required'; tokenRequired: boolean }

function decodeSetupStatus(value: unknown): SetupDecision {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new TypeError('Invalid setup status response')
  }
  const status = value as Record<string, unknown>
  if (typeof status.needs_setup !== 'boolean' || typeof status.token_required !== 'boolean') {
    throw new TypeError('Invalid setup status response')
  }
  return status.needs_setup
    ? { kind: 'setup-required', tokenRequired: status.token_required }
    : { kind: 'configured' }
}

export const setupApi = {
  getStatus: async (): Promise<SetupDecision> => {
    const response = await api.get<unknown>('/setup/status')
    return decodeSetupStatus(response.data)
  },
  complete: (data: SetupRequest, bootstrapToken?: string) => api.post<SetupCompleteResponse>('/setup/complete', data, {
    headers: bootstrapToken ? { 'X-Depsilo-Bootstrap-Token': bootstrapToken } : undefined,
  }),
}

// License / entitlement types
export type EntitlementSource = 'none' | 'trial' | 'paid'

export interface EntitlementStatus {
  is_pro: boolean
  source: EntitlementSource
  expires_at?: string
  days_left: number
  trial_used: boolean
  trial_available: boolean
  license_key_masked?: string
  license_error?: string
  last_checked: string
  // Deprecated aliases retained for compatibility with older servers.
  key_masked?: string
  activated_at?: string
}

export const licenseApi = {
  status:        () => api.get<EntitlementStatus>('/admin/license/status'),
  revalidate:    () => api.post('/admin/license/revalidate'),
  activateTrial: () => api.post<EntitlementStatus>('/admin/license/trial/activate'),
  setKey:        (key: string) => api.put<EntitlementStatus>('/admin/license/key', { key }),
  clearKey:      () => api.delete<EntitlementStatus>('/admin/license/key'),
}

// Webhook notification types
export interface WebhookConfig {
  id: number
  name: string
  platform: 'slack' | 'dingtalk' | 'wecom' | 'feishu' | 'generic'
  url: string
  enabled: boolean
  events: string
  cooldown_minutes: number
  last_sent_at: string | null
  created_at: string
  updated_at: string
}

export const webhookApi = {
  list:   () => api.get<WebhookConfig[]>('/admin/webhooks'),
  create: (data: Partial<WebhookConfig>) => api.post<WebhookConfig>('/admin/webhooks', data),
  update: (id: number, data: Partial<WebhookConfig>) => api.put(`/admin/webhooks/${id}`, data),
  delete: (id: number) => api.delete(`/admin/webhooks/${id}`),
  test:   (id: number) => api.post(`/admin/webhooks/${id}/test`),
}

export default api
