// Response bodies generated from internal/api/contract (`make generate`).
// Do not redeclare one of these names here: a local declaration would shadow it.
export type * from './generated/apiContract'
import type { CompileCachePermissions, CompileCacheStatus } from './generated/apiContract'
import type { CacheIndexStatus, UpstreamUpdateResult } from './generated/apiContract'
import type { AuditExporterKind, AuditLogCacheResult } from './generated/apiContract'
import type { PolicyStatus } from './generated/apiContract'

export type UserRole = 'admin' | 'readonly'
export type TokenPermissions = 'readonly' | 'readwrite'

const ADMIN_ECOSYSTEMS = [
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
  permission_source?: 'local' | 'oidc' | 'ldap'
  login_method?: 'password' | 'oidc' | 'ldap' | 'api_token'
  can_manage_credentials?: boolean
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
export interface ApproveSuggestionRequest { version: string; reason?: string }
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

/** Ecosystem filters accept several ids, sent comma-separated (`pypi,npm`). */
export interface AccessLogQuery { page?: number; page_size?: number; search?: string; adapter_type?: string; hit?: boolean; result?: 'hit' | 'miss' | 'unknown'; start?: string; end?: string }

export interface CacheQuery { page?: number; page_size?: number; search?: string; adapter_type?: string; cache_kind?: 'artifact' | 'metadata' }

/** The status body, narrowed: endpoints and stats are present exactly when enabled. */
export type CompileCacheStatusResponse =
  | (CompileCacheStatus & { enabled: false })
  | (Required<CompileCacheStatus> & { enabled: true })

export interface CreateCompileCacheCredentialRequest {
  name: string
  namespace: string
  permissions: CompileCachePermissions
  /** Zero creates a credential that does not expire. */
  ttl_days: number
}

export interface CacheIndexQuery {
  page?: number
  page_size?: number
  search?: string
  adapter_type?: string
  status?: CacheIndexStatus
}

export interface AuditLogQuery {
  page?: number
  page_size?: number
  ecosystem?: string
  package?: string
  ip?: string
  result?: AuditLogCacheResult
  start?: string
  end?: string
}

export interface AuditExporterRequest {
  name?: string
  kind?: AuditExporterKind
  url?: string
  token?: string
  events?: string
  enabled?: boolean
}

export interface RuleRequest { ecosystem: string; package_name: string; version: string; action: 'allow' | 'deny'; reason: string }
export interface RuleRecord extends RuleRequest { id: number; created_by: string; created_at: string; updated_at: string }
export type RuleListResponse = RuleRecord[] | { items: RuleRecord[]; total?: number }
export interface RuleTestRequest { ecosystem: string; package: string; version: string }
export interface RuleTestSpecificity {
  priority: number
  ecosystem: number
  package: number
  version: number
  action: number
  id: number
}

export type RuleTestEcosystemMatchLevel = 'exact' | 'wildcard'
export type RuleTestPackageMatchLevel = 'exact' | 'prefix' | 'wildcard'
export type RuleTestVersionMatchLevel = 'exact' | 'range' | 'wildcard'

export interface RuleTestMatchLevels {
  ecosystem: RuleTestEcosystemMatchLevel
  package: RuleTestPackageMatchLevel
  version: RuleTestVersionMatchLevel
}

/** A persisted rule that matched the test coordinate, with its ranking. */
export interface RuleTestCandidate {
  rule: RuleRecord
  specificity: RuleTestSpecificity
  match_levels: RuleTestMatchLevels
  matched: boolean
  selected: boolean
  explanation?: string
}

/** Result returned by the Admin "test policy" endpoint. */
export interface RuleTestResponse {
  allowed: boolean
  matched_rule: RuleRecord | null
  reason?: string
  winning_rule?: RuleRecord | null
  winner_reason?: string
  /** Stable tuple dimension that selected the winner, when candidates exist. */
  precedence_reason?: string
  /** Full selected candidate, useful to clients that need its ranking details. */
  winner?: RuleTestCandidate | null
  /** Always an array; no-match decisions return an empty array. */
  candidates: RuleTestCandidate[]
  /** Optional status metadata when evaluation used a stale/fallback snapshot. */
  policy_status?: PolicyStatus
}

export interface QuarantineQuery { limit?: number; offset?: number; ecosystem?: string; action?: string; package?: string }

export interface Project {
  id: number
  name: string
  slug: string
  description: string
  created_at: string
  updated_at: string
}

export interface ProjectSummary extends Project { package_count: number; last_activity_at: string | null }
export interface ProjectCapabilities {
  /** null means the active entitlement does not limit project count. */
  project_limit: number | null
  can_create: boolean
  can_export_sbom: boolean
}
export interface ProjectListResponse { items: ProjectSummary[]; total: number; capabilities: ProjectCapabilities }
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
export type ProjectSBOMPreset = 'technical-file'
export interface ProjectSBOMQuery {
  format: ProjectSBOMFormat
  ecosystem?: string
  /** CRA technical-file envelope with coverage manifest (implies CRA mode). */
  preset?: ProjectSBOMPreset
  /** Detached Ed25519 signature over the exact response body. */
  sign?: boolean
}

export interface ComplianceProfile {
  organization: string
  contact: string
  signing_configured: boolean
  component_annotation_count: number
}

export interface AdminUser { permission_source?: 'local' | 'oidc' | 'ldap'; id: number; username: string; role: UserRole; enabled: boolean; last_login_at: string | null; created_at: string; updated_at: string }
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
  /** Cascade peer name; empty/omitted means direct egress. */
  via?: string
}

export interface AdminCascadeConfigPeerPatch {
  name: string
  url: string
  /** Omitted keeps the stored override; "" removes it (inherit shared token). */
  token?: string
  forward_credentials: boolean
}

export interface AdminCascadeConfigPatch {
  enabled?: boolean
  max_hops?: number
  max_ttl?: string
  allow_insecure_http?: boolean
  /** Omitted keeps the stored secret; "" clears it. */
  shared_token?: string
  peers?: AdminCascadeConfigPeerPatch[]
}

export interface AdminUpstreamUpdateQuery {
  limit?: number
  offset?: number
  cursor?: string
  ecosystem?: string
  upstream?: string
  result?: UpstreamUpdateResult
  package?: string
  start?: string
  end?: string
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
  /** The port the server listens on. Sent only while setup is pending. */
  listen_port?: number
  /**
   * The command that prints the bootstrap token on this server, with the
   * --port and --host it needs. Sent only while setup is pending.
   */
  token_command?: string
  /** Whether a configuration file already exists. Sent only while setup is pending. */
  config_exists?: boolean
}

export interface SetupCompleteResponse {
  status: 'ok'
  message: string
  reconnect_url: string
  restart_strategy: 'exec' | 'supervisor_required'
}

export type OnboardingStatus = 'not_started' | 'completed' | 'skipped'
export type OnboardingTerminalStatus = Exclude<OnboardingStatus, 'not_started'>

export interface OnboardingEvent {
  id: number
  request_id?: string
  action?: string
  ecosystem: string
  package_name: string
  version: string
  outcome: 'hit' | 'miss' | 'blocked' | 'error' | string
  status_code: number
  created_at: string
}

export interface OnboardingDeliveryEvent extends OnboardingEvent {
  request_id: string
  cache_key: string
  method: string
  dependency_kind: string
  delivery_result: string
  bytes_sent: number
  successful_artifact_download: boolean
}

export interface OnboardingStatusResponse {
  status: OnboardingStatus
  started_at: string
  next_after_id: number
  events: OnboardingEvent[]
  has_more: boolean
  /** Optional for compatibility with servers predating delivery evidence. */
  next_delivery_after_id?: string
  delivery_events?: OnboardingDeliveryEvent[]
  deliveries_has_more?: boolean
}

export interface OnboardingStatusQuery {
  after_id?: number
  started_at?: string
  delivery_after_id?: string
}




export interface IdentityProvider { id: string; name: string; protocol?: 'oidc' | 'ldap' }
export interface AuthenticationSettings {
  enabled: boolean
  display_name: string
  issuer: string
  client_id: string
  client_secret_set: boolean
  redirect_url: string
  admin_groups: string[]
  readonly_groups: string[]
  groups_claim: string
  session_ttl: string
  allow_insecure_loopback: boolean
}
export type AuthenticationField = Exclude<keyof AuthenticationSettings, 'client_secret_set'> | 'client_secret'
export type AuthenticationPatch = Partial<Omit<AuthenticationSettings, 'client_secret_set'> & { client_secret: string }>
export interface AuthenticationSettingsResponse {
  configured: AuthenticationSettings
  effective: AuthenticationSettings
  overrides: Partial<Record<AuthenticationField, string>>
  pending_restart: boolean
  config_writable: boolean
}
export interface OIDCAuthenticationSourceState {
  sessions_will_reset?: boolean
  can_restore?: boolean
  id: string
  protocol: 'oidc'
  configured: AuthenticationSettings | null
  effective: AuthenticationSettings | null
  overrides: Partial<Record<AuthenticationField, string>>
  pending_restart: boolean
  config_writable: boolean
}
export interface LDAPDirectorySettings {
  url: string
  bind_dn: string
  bind_password_set: boolean
  user_base_dn: string
  user_filter: string
  subject_attribute: string
  display_name_attribute: string
  groups_attribute: string
  ca_cert_pem: string
}
export interface LDAPAuthenticationSettings {
  enabled: boolean
  display_name: string
  admin_groups: string[]
  readonly_groups: string[]
  session_ttl: string
  ldap: LDAPDirectorySettings
}
export type LDAPAuthenticationPatch = Partial<Omit<LDAPAuthenticationSettings, 'ldap'>> & { ldap?: Partial<Omit<LDAPDirectorySettings, 'bind_password_set'> & { bind_password: string }> }
export interface LDAPAuthenticationSourceState {
  id: string
  protocol: 'ldap'
  configured: LDAPAuthenticationSettings | null
  effective: LDAPAuthenticationSettings | null
  overrides: Record<string, string>
  pending_restart: boolean
  config_writable: boolean
  can_restore?: boolean
  sessions_will_reset?: boolean
}
export type AuthenticationSourceState = OIDCAuthenticationSourceState | LDAPAuthenticationSourceState
export interface LDAPPreviewRequest { id: string; protocol: 'ldap'; create: boolean; settings: LDAPAuthenticationPatch }
export interface LDAPConfigurationCheck { checked_at: string; configuration_valid: boolean; tls_valid: boolean; bind_valid: boolean; directory_readable: boolean }
export interface AuthenticationSourcesResponse {
  sources: AuthenticationSourceState[]
  config_writable: boolean
}
export type CreateAuthenticationSource = (AuthenticationPatch & { id: string; protocol: 'oidc'; display_name: string }) | (LDAPAuthenticationPatch & { id: string; protocol: 'ldap'; display_name: string })
export interface LoginIdentity {
  id: number
  provider: string
  provider_name: string
  protocol?: 'oidc' | 'ldap'
  provider_matches?: boolean
  login_available?: boolean
  display_name: string
  created_at: string
  last_login_at: string | null
  can_unlink: boolean
}
export interface LoginIdentitiesResponse {
  permission_source: 'local' | 'oidc' | 'ldap'
  password_enabled: boolean
  can_link: boolean
  current_identity_id?: number | null
  current_provider_id?: string | null
  identities: LoginIdentity[]
  providers: (IdentityProvider & { enabled: boolean; can_link: boolean; session_ttl_seconds?: number })[]
}
export type OIDCCompleteResponse =
  | { mode: 'login'; token: string; expires_at: number; return_to: string }
  | { mode: 'link'; return_to: string; result: 'linked' }
  | { mode: 'choice'; provider: IdentityProvider; display_name: string; role: 'admin' | 'readonly'; return_to: string; expires_at: number }
  | { mode: 'test'; provider: IdentityProvider; outcome: 'success' | 'denied' | 'failed'; authenticated: boolean; mapped_role: 'admin' | 'readonly' | null; display_name?: string; code?: string; checked_at: string; return_to: string }

export interface AuthenticationPreviewRequest {
  id: string
  protocol: 'oidc'
  create: boolean
  settings: AuthenticationPatch
}
export interface AuthenticationCheck {
  checked_at: string
  configuration_valid: boolean
  discovery_valid: boolean
  jwks_readable: boolean
}
export interface AuthenticationImpact {
  source_id: string
  bound_user_count: number
  dependent_managed_user_count: number
  active_session_count: number
  observed_at: string
  users: { id: number; username: string; permission_source: 'local' | 'oidc' | 'ldap'; enabled: boolean }[]
  users_truncated: boolean
}
