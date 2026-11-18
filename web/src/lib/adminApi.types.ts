// Response bodies generated from internal/api/contract (`make generate`).
// Do not redeclare one of these names here: a local declaration would shadow it.
export type * from './generated/apiContract'
import type { CompileCachePermissions, CompileCacheStatus } from './generated/apiContract'
import type { CacheIndexStatus, UpstreamUpdateResult } from './generated/apiContract'
import type { AuditExporterKind, AuditLogCacheResult } from './generated/apiContract'
import type { RuleAction, SecuritySeverity } from './generated/apiContract'
import type { BlocklistDisabled, BlocklistOverrideListResponse, BlocklistStatus } from './generated/apiContract'
import type { AdminSettingsLogLevel } from './generated/apiContract'
import type { AuthenticationSource, AuthenticationSourceList, AuthenticationSourceSettings, LDAPDirectorySettings, OnboardingStatus } from './generated/apiContract'
import type { IdentityChoiceResult, IdentityLinkResult, IdentityLoginResult, IdentityTrialResult } from './generated/apiContract'

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

export interface SecurityBaseQuery { page?: number; per_page?: number; ecosystem?: string }
export interface SecurityQuery extends SecurityBaseQuery { severity?: SecuritySeverity; package?: string }
export interface UpdateSecurityPolicyRequest { auto_block_enabled: boolean; min_cvss_score: number }
export interface ApproveSuggestionRequest { version: string; reason?: string }

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

export interface RuleRequest { ecosystem: string; package_name: string; version: string; action: RuleAction; reason: string }
export interface RuleTestRequest { ecosystem: string; package: string; version: string }

export interface QuarantineQuery { limit?: number; offset?: number; ecosystem?: string; action?: string; package?: string }

/** The blocklist read bodies, narrowed: a disabled blocklist answers only `{ enabled: false }`. */
type BlocklistDisabledResponse = BlocklistDisabled & { enabled: false }
export type BlocklistStatusResponse = (BlocklistStatus & { enabled: true }) | BlocklistDisabledResponse
export type BlocklistOverridesResponse = BlocklistOverrideListResponse | BlocklistDisabledResponse

export interface CreateProjectRequest { name: string; description: string }
export interface UpdateProjectRequest { name?: string; description?: string }
export interface ProjectPackageQuery { page?: number; per_page?: number; ecosystem?: string; search?: string }
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

export interface CreateUserRequest { username: string; password: string; role: UserRole }
export interface UpdateUserRequest { password?: string; role?: UserRole; enabled?: boolean }
export interface CreateAPITokenRequest { name: string; permissions: TokenPermissions; ttl: '7d' | '30d' | '90d' | 'never' }

export interface UpdateAdminSettingsRequest {
  server?: { log_level?: AdminSettingsLogLevel }
  cache?: { max_size_gb?: number; ttl_index?: string; ttl_blob?: string; lru_threshold?: number }
  auth?: { token_ttl?: string }
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

export type OnboardingTerminalStatus = Exclude<OnboardingStatus, 'not_started'>

export interface OnboardingStatusQuery {
  after_id?: number
  started_at?: string
  delivery_after_id?: string
}




/** An OIDC source's settings: every source also carries these fields, and only LDAP adds `ldap`. */
export type AuthenticationSettings = Omit<AuthenticationSourceSettings, 'ldap'>
export type AuthenticationField = Exclude<keyof AuthenticationSettings, 'client_secret_set'> | 'client_secret'
export type AuthenticationPatch = Partial<Omit<AuthenticationSettings, 'client_secret_set'> & { client_secret: string }>
export interface AuthenticationSettingsResponse {
  configured: AuthenticationSettings
  effective: AuthenticationSettings
  overrides: Partial<Record<AuthenticationField, string>>
  pending_restart: boolean
  config_writable: boolean
}
/** A source state narrowed by protocol: an OIDC source's settings carry no `ldap` block. */
export type OIDCAuthenticationSourceState = Omit<AuthenticationSource, 'protocol' | 'configured' | 'effective' | 'overrides'> & {
  protocol: 'oidc'
  configured: AuthenticationSettings | null
  effective: AuthenticationSettings | null
  overrides: Partial<Record<AuthenticationField, string>>
}
/** The fields an LDAP source uses; the server also sends the empty OIDC fields. */
export type LDAPAuthenticationSettings = Pick<AuthenticationSourceSettings, 'enabled' | 'display_name' | 'admin_groups' | 'readonly_groups' | 'session_ttl'> & { ldap: LDAPDirectorySettings }
export type LDAPAuthenticationPatch = Partial<Omit<LDAPAuthenticationSettings, 'ldap'>> & { ldap?: Partial<Omit<LDAPDirectorySettings, 'bind_password_set'> & { bind_password: string }> }
/** A source state narrowed by protocol: an LDAP source's settings always carry `ldap`. */
export type LDAPAuthenticationSourceState = Omit<AuthenticationSource, 'protocol' | 'configured' | 'effective'> & {
  protocol: 'ldap'
  configured: LDAPAuthenticationSettings | null
  effective: LDAPAuthenticationSettings | null
}
export type AuthenticationSourceState = OIDCAuthenticationSourceState | LDAPAuthenticationSourceState
export interface LDAPPreviewRequest { id: string; protocol: 'ldap'; create: boolean; settings: LDAPAuthenticationPatch }
/** The source list with each source narrowed by protocol. */
export type AuthenticationSourcesResponse = Omit<AuthenticationSourceList, 'sources'> & { sources: AuthenticationSourceState[] }
export type CreateAuthenticationSource = (AuthenticationPatch & { id: string; protocol: 'oidc'; display_name: string }) | (LDAPAuthenticationPatch & { id: string; protocol: 'ldap'; display_name: string })
/** Identity completion bodies, discriminated by mode. */
export type OIDCCompleteResponse =
  | (IdentityLoginResult & { mode: 'login' })
  | (IdentityLinkResult & { mode: 'link' })
  | (IdentityChoiceResult & { mode: 'choice' })
  | (IdentityTrialResult & { mode: 'test' })

export interface AuthenticationPreviewRequest {
  id: string
  protocol: 'oidc'
  create: boolean
  settings: AuthenticationPatch
}
