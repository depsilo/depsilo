export type UserRole = 'admin' | 'readonly'
export type TokenPermissions = 'readonly' | 'readwrite'

export interface Principal {
  id: number
  username: string
  role: UserRole
  enabled: true
  auth_method: 'jwt' | 'api_token'
  token_permissions: TokenPermissions | null
  can_write: boolean
}

export interface LoginResponse {
  token: string
  expires_at: number
  user: { id: number; username: string; role: UserRole }
}

export interface RefreshResponse { token: string; expires_at: number }

export type SecuritySeverity = 'critical' | 'high' | 'medium' | 'low' | 'unknown'

export interface SecurityDashboard {
  total_vulnerabilities: number
  affected_packages: number
  by_severity: Partial<Record<SecuritySeverity, number>>
  auto_blocked_count: number
  last_scan_at: string | null
  scan_in_progress: boolean
}

export interface SecurityVulnerability {
  id: number
  osv_id: string
  ecosystem: string
  package_name: string
  affected_ranges: string
  severity: SecuritySeverity
  cvss_score: number
  summary: string
  details: string
  aliases: string
  references: string
  published_at: string
  modified_at: string
  created_at: string
  updated_at: string
}

export interface SecurityVulnerabilityCheck {
  id: number
  ecosystem: string
  package_name: string
  has_vulnerabilities: boolean
  vulnerability_count: number
  last_fetched_at: string
  next_fetch_at: string
  created_at: string
  updated_at: string
}

export interface SecurityPolicy {
  id: number
  ecosystem: string
  auto_block_enabled: boolean
  min_cvss_score: number
  created_by: string
  created_at: string
  updated_at: string
}

export interface SecurityPage<T> { items: T[]; total: number; page: number }
export type SecurityVulnerabilityPage = SecurityPage<SecurityVulnerability>
export type SecuritySuggestionPage = SecurityPage<SecurityVulnerability>
export type SecurityPackagePage = SecurityPage<SecurityVulnerabilityCheck>
export interface SecurityBaseQuery { page?: number; per_page?: number; ecosystem?: string }
export interface SecurityQuery extends SecurityBaseQuery { severity?: SecuritySeverity; package?: string }
export interface UpdateSecurityPolicyRequest { auto_block_enabled: boolean; min_cvss_score: number }
export interface ApproveSuggestionRequest { version?: string; reason?: string }
export interface ApproveSuggestionResponse { rule_id: number }
export interface DismissSuggestionResponse { status: 'dismissed' }
export interface SecurityScanResponse { status: 'scan_started' }
export interface SecurityImportResponse { imported: number }

export interface AccessLog {
  id: number
  adapter_type: string
  method: string
  cache_key: string
  package_name: string
  hit: boolean
  upstream: string
  latency_ms: number
  status_code: number
  client_ip: string
  bytes_sent: number
  created_at: string
}

export interface AccessLogQuery { page?: number; page_size?: number; search?: string; adapter_type?: string; hit?: boolean }
export interface AccessLogListResponse { items: AccessLog[]; total: number; page: number; page_size: number }

export interface AuditLog {
  id: number
  ecosystem: string
  package_name: string
  version: string
  action: string
  cache_result: 'hit' | 'miss' | 'error'
  client_ip: string
  user_agent: string
  upstream_url: string
  latency_ms: number
  bytes_sent: number
  status_code: number
  created_at: string
}

export interface AuditLogQuery {
  page?: number
  page_size?: number
  ecosystem?: string
  package?: string
  ip?: string
  result?: 'hit' | 'miss' | 'error'
  start?: string
  end?: string
}
export interface AuditLogListResponse { items: AuditLog[]; total: number; page: number }

export interface Project {
  id: number
  name: string
  slug: string
  description: string
  created_at: string
  updated_at: string
}

export interface ProjectSummary extends Project { package_count: number; last_activity_at: string | null }
export interface ProjectListResponse { items: ProjectSummary[]; total: number }
export interface CreateProjectRequest { name: string; description: string }
export interface UpdateProjectRequest { name?: string; description?: string }
export interface CreateProjectResponse { id: number; name: string; slug: string; description: string; token: string; proxy_url: string; created_at: string }
export interface ProjectDetail extends ProjectSummary { proxy_url: string; ecosystem_breakdown: Record<string, number> }
export interface ProjectPackage { ecosystem: string; package_name: string; version: string; first_seen_at: string; last_seen_at: string; download_count: number }
export interface ProjectPackageQuery { page?: number; per_page?: number; ecosystem?: string; search?: string }
export interface ProjectPackagesResponse { items: ProjectPackage[]; total: number; page: number }
export interface RegenerateProjectTokenResponse { token: string; proxy_url: string }
export interface DeleteProjectResponse { status: 'deleted' }
export type ProjectSBOMFormat = 'spdx' | 'cyclonedx'
export interface ProjectSBOMQuery { format: ProjectSBOMFormat; ecosystem?: string }

export interface AdminUser { id: number; username: string; role: UserRole; enabled: boolean; last_login_at: string | null; created_at: string; updated_at: string }
export interface CreateUserRequest { username: string; password: string; role: UserRole }
export interface UpdateUserRequest { password?: string; role?: UserRole; enabled?: boolean }
export interface APITokenSummary { id: number; user_id: number; name: string; permissions: TokenPermissions; expires_at: string | null; last_used_at: string | null; created_at: string }
export interface CreateAPITokenRequest { name: string; permissions: TokenPermissions; ttl: '7d' | '30d' | '90d' | 'never' }
export interface CreateAPITokenResponse { id: number; name: string; token: string; permissions: TokenPermissions; expires_at: string | null; warning: string }
