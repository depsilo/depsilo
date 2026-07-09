import axios from 'axios'

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
  login: (data: { username: string; password: string }) => api.post('/auth/login', data),
  logout: () => api.post('/auth/logout'),
  refresh: () => api.post('/auth/refresh'),
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
  listUpstreams: () => api.get('/admin/upstreams'),
  createUpstream: (data: any) => api.post('/admin/upstreams', data),
  updateUpstream: (id: number, data: any) => api.put(`/admin/upstreams/${id}`, data),
  deleteUpstream: (id: number) => api.delete(`/admin/upstreams/${id}`),
  checkUpstream: (id: number) => api.post(`/admin/upstreams/${id}/check`),
  getUpstreamLatency: (id: number, range_: string = '24h') =>
    api.get(`/admin/upstreams/${id}/latency`, { params: { range: range_ } }),

  // Logs
  listLogs: (params: Record<string, any>) => api.get('/admin/logs', { params }),
  exportLogs: (params: Record<string, any>) => api.get('/admin/logs/export', { params, responseType: 'blob' }),

  // Users
  listUsers: () => api.get('/admin/users'),
  createUser: (data: any) => api.post('/admin/users', data),
  updateUser: (id: number, data: any) => api.put(`/admin/users/${id}`, data),
  deleteUser: (id: number) => api.delete(`/admin/users/${id}`),

  // Tokens
  listTokens: () => api.get('/admin/tokens'),
  createToken: (data: any) => api.post('/admin/tokens', data),
  deleteToken: (id: number) => api.delete(`/admin/tokens/${id}`),

  // Settings
  getSettings: () => api.get('/admin/settings'),
  updateSettings: (data: any) => api.put('/admin/settings', data),

  // Audit Logs (Pro)
  listAuditLogs: (params: Record<string, any>) => api.get('/admin/audit-logs', { params }),
  exportAuditLogs: (params: Record<string, any>) => api.get('/admin/audit-logs/export', { params, responseType: 'blob' }),

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
  getSecurityDashboard: () => api.get('/admin/security/dashboard'),
  listVulnerabilities: (params: Record<string, any>) => api.get('/admin/security/vulnerabilities', { params }),
  listVulnerablePackages: (params: Record<string, any>) => api.get('/admin/security/packages', { params }),
  listSuggestions: (params: Record<string, any>) => api.get('/admin/security/suggestions', { params }),
  approveSuggestion: (vulnId: number, data?: any) => api.post(`/admin/security/suggestions/${vulnId}/approve`, data),
  dismissSuggestion: (vulnId: number) => api.post(`/admin/security/suggestions/${vulnId}/dismiss`),
  triggerSecurityScan: () => api.post('/admin/security/scan'),
  importVulnerabilities: (formData: FormData) => api.post('/admin/security/import', formData, { headers: { 'Content-Type': 'multipart/form-data' } }),
  listSecurityPolicies: () => api.get('/admin/security/policies'),
  updateSecurityPolicy: (ecosystem: string, data: any) => api.put(`/admin/security/policies/${ecosystem}`, data),

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
  listProjects: () => api.get('/admin/projects'),
  createProject: (data: any) => api.post('/admin/projects', data),
  getProject: (id: number) => api.get(`/admin/projects/${id}`),
  updateProject: (id: number, data: any) => api.put(`/admin/projects/${id}`, data),
  deleteProject: (id: number) => api.delete(`/admin/projects/${id}`),
  listProjectPackages: (id: number, params: Record<string, any>) => api.get(`/admin/projects/${id}/packages`, { params }),
  regenerateProjectToken: (id: number) => api.post(`/admin/projects/${id}/token`),
  exportSbom: (id: number, params: { format: string; ecosystem?: string }) =>
    api.get(`/admin/projects/${id}/sbom`, { params, responseType: 'blob' }),
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
  // Deprecated aliases — to be removed in 0.5.0 per backend spec §16.2.
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
  last_sent_at?: string
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
