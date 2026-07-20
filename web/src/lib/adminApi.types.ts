export type UserRole = 'admin' | 'readonly'
export type TokenPermissions = 'readonly' | 'readwrite'

export const ADMIN_ECOSYSTEMS = [
  'pip', 'pypi', 'apt', 'npm', 'go', 'goproxy', 'cargo', 'crates',
  'maven', 'rubygems', 'composer', 'nuget', 'conda', 'cran', 'helm',
  'docker', 'huggingface', 'alpine',
] as const
export type AdminEcosystem = (typeof ADMIN_ECOSYSTEMS)[number]
const ADMIN_ECOSYSTEM_SET: ReadonlySet<string> = new Set(ADMIN_ECOSYSTEMS)
export function isAdminEcosystem(value: string): value is AdminEcosystem {
  return ADMIN_ECOSYSTEM_SET.has(value)
}

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
export interface SecurityImportResponse {
  imported: number
  received: number
  packages: number
  duplicates: number
  skipped: number
  rules_created: number
}

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

export interface CacheQuery { page?: number; page_size?: number; search?: string; adapter_type?: string }
export interface CacheEntry { id: number; key: string; adapter_type: string; package_name: string; size: number; hit_count: number; last_accessed: string; expires_at: string }
export interface CacheListResponse { items: CacheEntry[]; total: number; page: number; page_size: number }
export interface CacheDeleteResponse {
  message: string
  deleted: number
  reclaimed_bytes: number
}
export interface CacheCleanupResponse {
  message: string
  deleted: number
  failed: number
  reclaimed_bytes: number
  examined: number
  expired_removed: number
  lru_removed: number
  usage_before: number
  usage_after: number
}
export interface CacheTypeBreakdown { type: string; size: number; file_count: number }
export interface CachePackageSize { name: string; type: string; size: number; hit_count: number }
export interface CacheDistributionResponse {
  total_size: number
  max_size: number
  usage_percent: number
  by_type: CacheTypeBreakdown[]
  top_packages: CachePackageSize[]
}

export type CacheIndexStatus = 'fresh' | 'stale'
export interface CacheIndexQuery {
  page?: number
  page_size?: number
  search?: string
  adapter_type?: string
  status?: CacheIndexStatus
}
export interface CacheIndexEntry {
  id: number
  key: string
  adapter_type: string
  package_name: string
  size: number
  hit_count: number
  etag: string
  last_modified: string
  last_accessed: string
  expires_at: string
  updated_at: string
  status: CacheIndexStatus
}
export interface CacheIndexSummary {
  adapter_type: string
  total: number
  fresh: number
  stale: number
  last_updated: string | null
}
export interface CacheIndexListResponse {
  items: CacheIndexEntry[]
  total: number
  page: number
  page_size: number
  summary: CacheIndexSummary[]
}
export interface CacheIndexRefreshResponse {
  message: string
  id: number
  item?: CacheIndexEntry
}

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

export interface RuleRequest { ecosystem: string; package_name: string; version: string; action: 'allow' | 'deny'; reason: string }
export interface RuleRecord extends RuleRequest { id: number; created_by: string; created_at: string; updated_at: string }
export type RuleListResponse = RuleRecord[] | { items: RuleRecord[]; total?: number }
export interface RuleTestRequest { ecosystem: string; package: string; version: string }
export interface RuleTestResponse { allowed: boolean; matched_rule: RuleRecord | null; reason?: string }

export interface QuarantineQuery { limit?: number; ecosystem?: string; action?: string; package?: string }

export interface DashboardWindow {
  total_requests: number
  hit_count?: number
  hit_rate: number
  bytes_served: number
  avg_latency_ms: number
}
export interface DashboardTopPackage { name: string; hit_count: number }
export interface DashboardUpstream { id: number; name: string; adapter: string; healthy: boolean; avg_latency_ms: number; success_rate: number }
export interface DashboardResponse {
  last_24h: DashboardWindow
  prev_24h: DashboardWindow
  daily_stats: Array<{ date: string; adapter_type: string; count: number }>
  upstreams: DashboardUpstream[]
  top_packages: { pypi: DashboardTopPackage[]; apt: DashboardTopPackage[] }
  cache_usage_percent?: number
}
export interface DashboardTrendsResponse { points: Array<{ bucket: number; date: string; requests: number; hits: number; misses: number; hit_rate: number; bytes_served: number; bytes_hit: number; bytes_miss: number; sum_latency_ms: number; avg_latency_ms: number; errors: number }> }

export interface BandwidthSummary {
  total_bytes: number
  hit_bytes: number
  miss_bytes: number
  savings_rate: number
  total_requests: number
  hit_requests: number
  miss_requests: number
  time_saved_ms: number
  avg_hit_latency: number
  avg_miss_latency: number
}
export interface BandwidthDaily { date: string; hit_bytes: number; miss_bytes: number; hit_count: number; miss_count: number }
export interface BandwidthEcosystem { ecosystem: string; hit_bytes: number; miss_bytes: number; hit_count: number; miss_count: number; avg_hit_latency_ms: number; avg_miss_latency_ms: number }
export interface BandwidthPackage { package_name: string; ecosystem: string; total_bytes: number; hit_bytes: number; request_count: number }
export interface BandwidthUpstream { upstream: string; miss_bytes: number; request_count: number; avg_latency_ms: number }
export interface BandwidthReportResponse {
  range: { start: string; end: string }
  summary: BandwidthSummary
  daily: BandwidthDaily[]
  by_ecosystem: BandwidthEcosystem[]
  top_packages: BandwidthPackage[]
  by_upstream: BandwidthUpstream[]
}

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

export interface AdminSettingsSnapshot {
  server: { host: string; port: number; log_level: 'debug' | 'info' | 'warn' | 'error' }
  database: { driver: string }
  storage: { type: string; path: string }
  cache: { max_size_gb: number; ttl_index: string; ttl_blob: string; lru_threshold: number }
  auth: { token_ttl: string }
}
export type SettingPath =
  | 'server.host' | 'server.port' | 'server.log_level'
  | 'database.driver' | 'storage.type' | 'storage.path'
  | 'cache.max_size_gb' | 'cache.ttl_index' | 'cache.ttl_blob'
  | 'cache.lru_threshold' | 'auth.token_ttl'
export type EditableSettingPath =
  | 'server.log_level' | 'cache.max_size_gb' | 'cache.ttl_index'
  | 'cache.ttl_blob' | 'cache.lru_threshold' | 'auth.token_ttl'
export type SettingSource = 'default' | 'file' | 'env'
export interface AdminSettingsResponse {
  configured: AdminSettingsSnapshot
  effective: AdminSettingsSnapshot
  pending_restart: EditableSettingPath[]
  overrides: Partial<Record<SettingPath, string>>
  sources: Record<SettingPath, SettingSource>
  editable: EditableSettingPath[]
  config_writable: boolean
}
export interface UpdateAdminSettingsRequest {
  server?: { log_level?: AdminSettingsSnapshot['server']['log_level'] }
  cache?: { max_size_gb?: number; ttl_index?: string; ttl_blob?: string; lru_threshold?: number }
  auth?: { token_ttl?: string }
}
export interface UpdateAdminSettingsResponse extends AdminSettingsResponse {
  changed: EditableSettingPath[]
  applied_now: EditableSettingPath[]
  restart_required: EditableSettingPath[]
  blocked_by_override: EditableSettingPath[]
}

export interface UpstreamMutationRequest {
  adapter_type: string
  name: string
  url: string
  proxy: string
  priority: number
  probe_mode: 'active' | 'passive'
  probe_interval: string
}

export interface AdminUpstream extends UpstreamMutationRequest {
  id: number
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
  last_checked_at: string | null
  worker_running: boolean
  created_at: string
  updated_at: string
}

export interface AdminUpstreamLatencyPoint {
  time: string
  latency_ms: number
  healthy: boolean
}

export interface AdminUpstreamLatencySeries {
  upstream_id: number
  points: AdminUpstreamLatencyPoint[]
}

export interface AdminUpstreamLatenciesResponse {
  series: AdminUpstreamLatencySeries[]
}

export type UpstreamUpdateResult = 'updated' | 'unchanged' | 'error'

export interface AdminUpstreamUpdateEvent {
  id: number
  cache_entry_id?: number
  ecosystem: string
  upstream: string
  package: string
  result: UpstreamUpdateResult
  detail: string
  latency_ms: number
  occurrence_count?: number
  first_seen_at?: string
  last_seen_at?: string
  created_at: string
}

export interface AdminUpstreamUpdateListResponse {
  items: AdminUpstreamUpdateEvent[]
  total: number
  next_cursor?: string | null
}

export interface AdminUpstreamUpdateQuery {
  limit?: number
  offset?: number
  cursor?: string
  ecosystem?: string
  upstream?: string
  result?: UpstreamUpdateResult
  package?: string
}

export interface SetupRequest {
  server: { port: number }
  storage: { path: string }
  admin: { username: string; password: string }
  ecosystems: Record<string, { enabled: boolean; upstreams: Array<{ name: string; url: string; priority: number }> }>
}

export interface SetupStatusResponse {
  needs_setup: boolean
  token_required: boolean
}

export interface SetupCompleteResponse {
  status: 'ok'
  message: string
  reconnect_url: string
  restart_strategy: 'exec' | 'supervisor_required'
}

export interface AdminUpstreamListResponse { items: AdminUpstream[]; total: number }
export interface DeleteUpstreamResponse { deleted_id: number; adapter_type: string }
export interface UpstreamCheckResult { healthy: boolean; latency_ms: number; checked_at: string; error: string | null }
export interface CheckUpstreamResponse { upstream: AdminUpstream; check: UpstreamCheckResult }
