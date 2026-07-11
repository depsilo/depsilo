import axios from 'axios'
import type {
  AccessLogListResponse,
  AccessLogQuery,
  AdminSettingsResponse,
  AdminUpstream,
  AdminUpstreamListResponse,
  AdminUser,
  APITokenSummary,
  ApproveSuggestionRequest,
  ApproveSuggestionResponse,
  AuditLogListResponse,
  AuditLogQuery,
  CheckUpstreamResponse,
  CreateAPITokenRequest,
  CreateAPITokenResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  CreateUserRequest,
  DeleteProjectResponse,
  DeleteUpstreamResponse,
  DismissSuggestionResponse,
  LoginResponse,
  Principal,
  Project,
  ProjectDetail,
  ProjectListResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  ProjectSBOMQuery,
  RefreshResponse,
  RegenerateProjectTokenResponse,
  SecurityBaseQuery,
  SecurityDashboard,
  SecurityImportResponse,
  SecurityPackagePage,
  SecurityPolicy,
  SecurityQuery,
  SecurityScanResponse,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
  UpdateProjectRequest,
  UpdateAdminSettingsRequest,
  UpdateAdminSettingsResponse,
  UpdateSecurityPolicyRequest,
  UpdateUserRequest,
  UpstreamMutationRequest,
} from './adminApi.types'

const api = axios.create({ baseURL: '/api/v1' })

// Request interceptor: attach JWT from localStorage
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: 401 → redirect to /admin/login; 402 → dispatch pro-required event
api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401 && window.location.pathname.startsWith('/admin')) {
      localStorage.removeItem('token')
      window.location.href = '/admin/login'
    }
    // 402 PRO_REQUIRED is left to each Pro-gated page to render an
    // inline ProRequiredCallout — the previous app-wide modal popup
    // proved too interruptive for users casually clicking around the
    // admin sidebar.
    return Promise.reject(err)
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
  getDashboard: () => api.get('/admin/dashboard'),
  getDashboardTrends: (range_: string = '7d') =>
    api.get('/admin/dashboard/trends', { params: { range: range_ } }),

  // Cache
  listCache: (params: Record<string, any>) => api.get('/admin/cache', { params }),
  deleteCache: (id: number) => api.delete(`/admin/cache/${id}`),
  cleanupCache: () => api.post('/admin/cache/cleanup'),
  getCacheDistribution: () => api.get('/admin/cache/distribution'),
  warmupCache: (data: { ecosystem: string; packages: string[] }) => api.post('/admin/cache/warmup', data),

  // Upstreams
  listUpstreams: () => api.get<AdminUpstreamListResponse>('/admin/upstreams'),
  createUpstream: (data: UpstreamMutationRequest) => api.post<AdminUpstream>('/admin/upstreams', data),
  updateUpstream: (id: number, data: UpstreamMutationRequest) => api.put<AdminUpstream>(`/admin/upstreams/${id}`, data),
  deleteUpstream: (id: number) => api.delete<DeleteUpstreamResponse>(`/admin/upstreams/${id}`),
  checkUpstream: (id: number) => api.post<CheckUpstreamResponse>(`/admin/upstreams/${id}/check`),
  getUpstreamLatency: (id: number, range_: string = '24h') =>
    api.get(`/admin/upstreams/${id}/latency`, { params: { range: range_ } }),

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
  listRules: () => api.get('/admin/rules'),
  createRule: (data: any) => api.post('/admin/rules', data),
  updateRule: (id: number, data: any) => api.put(`/admin/rules/${id}`, data),
  deleteRule: (id: number) => api.delete(`/admin/rules/${id}`),
  testRule: (data: { ecosystem: string; package: string; version: string }) => api.post('/admin/rules/test', data),

  // Bandwidth report
  getBandwidthReport: (params: { range?: string; start?: string; end?: string }) =>
    api.get('/admin/bandwidth', { params }),

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
  listQuarantineEvents: (params: Record<string, any>) => api.get('/admin/quarantine/events', { params }),
  listQuarantineApprovals: (params: Record<string, any>) => api.get('/admin/quarantine/approvals', { params }),
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
export const setupApi = {
  getStatus: () => api.get('/setup/status'),
  complete: (data: any) => api.post('/setup/complete', data),
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
