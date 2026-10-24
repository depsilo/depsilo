import type { AuthenticationPatch, AuthenticationSettingsResponse, AuthenticationSourceState, OIDCAuthenticationSourceState } from './adminApi.types'

/** The editor shows effective values for environment-owned fields, which cannot be saved. */
export function authenticationDraft(state: AuthenticationSettingsResponse) {
  const config = { ...state.configured }
  for (const key of Object.keys(state.overrides)) {
    if (key === 'client_secret') config.client_secret_set = state.effective.client_secret_set
    else if (key in config) Object.assign(config, { [key]: state.effective[key as keyof typeof config] })
  }
  return { ...config, admin_groups: config.admin_groups.join('\n'), readonly_groups: config.readonly_groups.join('\n'), client_secret: '' }
}

export type AuthenticationDraft = ReturnType<typeof authenticationDraft>

/** Keep only actual edits when refreshed configuration changes beneath the form. */
export function rebaseAuthenticationDraft(draft: AuthenticationDraft, before: AuthenticationSettingsResponse, after: AuthenticationSettingsResponse): AuthenticationDraft {
  const edits = authenticationPatch(draft, before)
  const next = authenticationDraft(after)
  for (const field of Object.keys(edits) as (keyof AuthenticationPatch)[]) {
    if (!after.overrides[field]) Object.assign(next, { [field]: draft[field] })
  }
  return next
}

export function authenticationPatch(draft: AuthenticationDraft, state: AuthenticationSettingsResponse): AuthenticationPatch {
  const base = authenticationDraft(state)
  const patch: AuthenticationPatch = {}
  for (const key of ['enabled', 'allow_insecure_loopback'] as const) {
    if (!state.overrides[key] && draft[key] !== base[key]) patch[key] = draft[key]
  }
  for (const key of ['display_name', 'issuer', 'client_id', 'redirect_url', 'groups_claim', 'session_ttl'] as const) {
    if (!state.overrides[key] && draft[key] !== base[key]) patch[key] = draft[key]
  }
  for (const key of ['admin_groups', 'readonly_groups'] as const) {
    const groups = draft[key].split('\n').map(value => value.trim()).filter(Boolean)
    if (!state.overrides[key] && JSON.stringify(groups) !== JSON.stringify(state.configured[key])) patch[key] = groups
  }
  // Empty means keep the saved secret; the response and Query cache never contain it.
  if (!state.overrides.client_secret && draft.client_secret) patch.client_secret = draft.client_secret
  return patch
}

/** A new source exists only in the editor until its first explicit save. */
export function newAuthenticationSource(id: string, origin: string) {
  const configured = {
    enabled: false, display_name: '', issuer: '', client_id: '', client_secret_set: false,
    redirect_url: `${origin}/api/v1/auth/oidc/${id}/callback`,
    admin_groups: [], readonly_groups: [], groups_claim: 'groups', session_ttl: '30m', allow_insecure_loopback: false,
  }
  return { id, protocol: 'oidc' as const, legacy: false, configured, effective: null, overrides: {}, pending_restart: false, config_writable: true }
}

/** Running labels always describe the effective config, never a pending draft. */
export function authenticationRuntimeStatus(source: AuthenticationSourceState) {
  if (!source.effective) return 'notLoaded'
  return source.effective.enabled ? 'enabled' : 'disabled'
}

export function authenticationConfiguredStatus(source: OIDCAuthenticationSourceState) {
  const config = source.configured
  if (!config) return 'removing'
  if (config.enabled) return 'enabled'
  if (!config.issuer || !config.client_id || !config.redirect_url || !config.client_secret_set || (!config.admin_groups.length && !config.readonly_groups.length)) return 'draft'
  return 'disabled'
}

/** A saved security change still matters when the only new edit is a label.
 * Older servers omit the impact flag; retain their conservative warning. */
export function authenticationMayResetSessions(source: AuthenticationSourceState, patch: AuthenticationPatch) {
  if (!source.effective?.enabled) return false
  return source.sessions_will_reset !== false || Object.keys(patch).some(field => field !== 'display_name')
}

export function authenticationCreateSettings(draft: AuthenticationDraft): AuthenticationPatch {
  // The write DTO contains no read-only client_secret_set flag and no plaintext placeholder.
  const { client_secret_set, ...settings } = draft
  void client_secret_set
  return {
    ...settings, display_name: settings.display_name.trim(),
    admin_groups: draft.admin_groups.split('\n').map(value => value.trim()).filter(Boolean),
    readonly_groups: draft.readonly_groups.split('\n').map(value => value.trim()).filter(Boolean),
  }
}

const validationCodes = new Set(['REQUIRED', 'INVALID_VALUE', 'INVALID_URL', 'CALLBACK_PATH', 'DUPLICATE_ISSUER', 'CALLBACK_ORIGIN', 'GROUPS_OVERLAP', 'ENV_OVERRIDE', 'GROUPS_REQUIRED', 'RESERVED_ID', 'UNSUPPORTED_PROTOCOL', 'SOURCE_EXISTS'])
/** Server errors are codes, not text: never render returned configuration values or secrets. */
export function authenticationValidationKey(code: string) {
  return `authentication.validation.${validationCodes.has(code) ? code : 'INVALID_VALUE'}`
}
