import type { LDAPAuthenticationPatch, LDAPAuthenticationSettings, LDAPAuthenticationSourceState } from './adminApi.types'

export function newLDAPAuthenticationSource(id: string): LDAPAuthenticationSourceState {
  return { id, protocol: 'ldap', legacy: false, pending_restart: false, config_writable: true, overrides: {}, effective: null, configured: {
    enabled: false, display_name: '', admin_groups: [], readonly_groups: [], session_ttl: '30m',
    ldap: { url: '', bind_dn: '', bind_password_set: false, user_base_dn: '', user_filter: '(uid={username})', subject_attribute: 'entryUUID', display_name_attribute: 'displayName', groups_attribute: 'memberOf', ca_cert_pem: '' },
  } }
}

export function ldapDraft(settings: LDAPAuthenticationSettings) {
  return { ...settings, admin_groups: settings.admin_groups.join('\n'), readonly_groups: settings.readonly_groups.join('\n'), ldap: { ...settings.ldap, bind_password: '' } }
}
export function ldapFormSettings(source: LDAPAuthenticationSourceState): LDAPAuthenticationSettings {
  const configured = source.configured!
  const effective = source.effective
  const result = { ...configured, ldap: { ...configured.ldap } }
  if (!effective) return result
  for (const field of Object.keys(source.overrides)) {
    if (field === 'ldap.bind_password') result.ldap.bind_password_set = effective.ldap.bind_password_set
    else if (field.startsWith('ldap.')) {
      const key = field.slice(5)
      if (key in result.ldap) Object.assign(result.ldap, { [key]: effective.ldap[key as keyof typeof effective.ldap] })
    } else if (field in result) Object.assign(result, { [field]: effective[field as keyof typeof effective] })
  }
  return result
}
export type LDAPDraft = ReturnType<typeof ldapDraft>
export function ldapPatch(draft: LDAPDraft, saved: LDAPAuthenticationSettings, creating = false, overrides: Record<string, string> = {}): LDAPAuthenticationPatch {
  const patch: LDAPAuthenticationPatch = {}
  for (const field of ['display_name', 'session_ttl'] as const) if (creating || draft[field] !== saved[field]) patch[field] = draft[field].trim()
  if (creating || draft.enabled !== saved.enabled) patch.enabled = draft.enabled
  for (const field of ['admin_groups', 'readonly_groups'] as const) {
    const groups = draft[field].split('\n').map(value => value.trim()).filter(Boolean)
    if (creating || JSON.stringify(groups) !== JSON.stringify(saved[field])) patch[field] = groups
  }
  const directory: NonNullable<LDAPAuthenticationPatch['ldap']> = {}
  for (const field of ['url', 'bind_dn', 'user_base_dn', 'user_filter', 'subject_attribute', 'display_name_attribute', 'groups_attribute', 'ca_cert_pem'] as const) {
    if (creating || draft.ldap[field] !== saved.ldap[field]) directory[field] = draft.ldap[field]
  }
  if (draft.ldap.bind_password) directory.bind_password = draft.ldap.bind_password
  for (const field of Object.keys(overrides)) {
    if (field.startsWith('ldap.')) delete directory[field.slice(5) as keyof typeof directory]
    else delete patch[field as keyof typeof patch]
  }
  if (Object.keys(directory).length && !overrides.ldap) patch.ldap = directory
  return patch
}

export function rebaseLDAPDraft(draft: LDAPDraft, before: LDAPAuthenticationSettings, after: LDAPAuthenticationSettings, overrides: Record<string, string> = {}): LDAPDraft {
  const edits = ldapPatch(draft, before, false, overrides)
  const next = ldapDraft(after)
  for (const field of ['enabled', 'display_name', 'session_ttl', 'admin_groups', 'readonly_groups'] as const) {
    if (field in edits) Object.assign(next, { [field]: draft[field] })
  }
  if (edits.ldap) next.ldap = { ...next.ldap, ...edits.ldap }
  return next
}

export function ldapConfiguredStatus(source: LDAPAuthenticationSourceState) {
  const config = source.configured
  if (!config) return 'removing'
  if (config.enabled) return 'enabled'
  if (!config.ldap.url || !config.ldap.bind_dn || !config.ldap.bind_password_set || !config.ldap.user_base_dn || (!config.admin_groups.length && !config.readonly_groups.length)) return 'draft'
  return 'disabled'
}
