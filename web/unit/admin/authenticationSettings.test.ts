import { describe, expect, it } from 'vitest'
import { authenticationDraft, authenticationMayResetSessions, authenticationPatch, rebaseAuthenticationDraft, authenticationRuntimeStatus, authenticationConfiguredStatus, newAuthenticationSource, authenticationCreateSettings, authenticationValidationKey } from '../../src/lib/authenticationSettings'
import type { AuthenticationSettingsResponse } from '../../src/lib/adminApi.types'

const config = { enabled: false, display_name: 'Company login', issuer: '', client_id: '', client_secret_set: true, redirect_url: '', admin_groups: [], readonly_groups: [], groups_claim: 'groups', session_ttl: '30m', allow_insecure_loopback: false }
const state: AuthenticationSettingsResponse = { configured: config, effective: config, overrides: {}, pending_restart: false, config_writable: true }

describe('authentication editor request', () => {
  it('preserves saved secrets and sends only changed settings', () => {
    const draft = authenticationDraft(state)
    expect(draft.client_secret).toBe('')
    expect(authenticationPatch(draft, state)).toEqual({})
    expect(authenticationPatch({ ...draft, display_name: 'Team login' }, state)).toEqual({ display_name: 'Team login' })
    expect(authenticationPatch({ ...draft, client_secret: 'replacement' }, state)).toEqual({ client_secret: 'replacement' })
  })
  it('shows effective env settings without sending them back to the config file', () => {
    const overridden: AuthenticationSettingsResponse = { ...state, effective: { ...config, issuer: 'https://identity.example', admin_groups: ['operators'] }, overrides: { issuer: 'DEPSILO_AUTH_OIDC_ISSUER', admin_groups: 'DEPSILO_AUTH_OIDC_ADMIN_GROUPS', client_secret: 'DEPSILO_AUTH_OIDC_CLIENT_SECRET' } }
    const draft = authenticationDraft(overridden)
    expect(draft.issuer).toBe('https://identity.example')
    expect(draft.admin_groups).toBe('operators')
    expect(authenticationPatch({ ...draft, issuer: 'https://override.example', admin_groups: 'changed', client_secret: 'do-not-write', enabled: true }, overridden)).toEqual({ enabled: true })
  })
  it('normalizes line endings and trims group entries without splitting names at spaces or commas', () => {
    const draft = authenticationDraft(state)
    expect(authenticationPatch({ ...draft, admin_groups: ' Team operators \r\n\r\nteam,west\n', readonly_groups: 'readers' }, state)).toEqual({ admin_groups: ['Team operators', 'team,west'], readonly_groups: ['readers'] })
  })
  it('keeps user edits without reverting untouched fields changed by another administrator', () => {
    const draft = { ...authenticationDraft(state), display_name: 'My edit' }
    const refreshed = { ...state, configured: { ...config, admin_groups: ['new-admin-group'], session_ttl: '15m' } }
    const rebased = rebaseAuthenticationDraft(draft, state, refreshed)
    expect(rebased.admin_groups).toBe('new-admin-group')
    expect(rebased.session_ttl).toBe('15m')
    expect(authenticationPatch(rebased, refreshed)).toEqual({ display_name: 'My edit' })
  })
  it('drops edits when a refreshed field becomes environment-owned', () => {
    const draft = { ...authenticationDraft(state), client_secret: 'replacement', issuer: 'https://my-edit.example' }
    const refreshed = { ...state, effective: { ...config, issuer: 'https://managed.example' }, overrides: { client_secret: 'DEPSILO_AUTH_OIDC_CLIENT_SECRET', issuer: 'DEPSILO_AUTH_OIDC_ISSUER' } }
    const rebased = rebaseAuthenticationDraft(draft, state, refreshed)
    expect(rebased.client_secret).toBe('')
    expect(rebased.issuer).toBe('https://managed.example')
    expect(authenticationPatch(rebased, refreshed)).toEqual({})
  })
})


describe('source lifecycle and preview', () => {
  it('keeps pending saved settings separate from running status', () => {
    const source = { ...newAuthenticationSource('company', 'https://depsilo.example'), configured: { ...config, enabled: true }, effective: { ...config, enabled: false }, pending_restart: true }
    expect(authenticationRuntimeStatus(source)).toBe('disabled')
    expect(authenticationConfiguredStatus(source)).toBe('enabled')
    expect(authenticationRuntimeStatus({ ...source, effective: null })).toBe('notLoaded')
    expect(authenticationConfiguredStatus({ ...source, configured: null })).toBe('removing')
    expect(authenticationConfiguredStatus({ ...source, configured: config })).toBe('draft')
  })
  it('creates an origin-derived, disabled in-memory draft and a write-only payload', () => {
    const source = newAuthenticationSource('company', 'https://depsilo.example')
    expect(source.effective).toBeNull()
    expect(source.configured.enabled).toBe(false)
    expect(source.configured.redirect_url).toBe('https://depsilo.example/api/v1/auth/oidc/company/callback')
    const draft = authenticationDraft({ ...source, effective: source.configured })
    const payload = authenticationCreateSettings({ ...draft, client_secret: 'one-time-input', readonly_groups: 'Readers\r\n Team west \n' })
    expect(payload).not.toHaveProperty('client_secret_set')
    expect(payload.client_secret).toBe('one-time-input')
    expect(payload.readonly_groups).toEqual(['Readers', 'Team west'])
  })
  it('does not render arbitrary server validation strings', () => {
    expect(authenticationValidationKey('GROUPS_REQUIRED')).toBe('authentication.validation.GROUPS_REQUIRED')
    expect(authenticationValidationKey('secret-value-from-a-proxy')).toBe('authentication.validation.INVALID_VALUE')
  })
})


describe('source session impact', () => {
  const source = { ...newAuthenticationSource('company', 'https://depsilo.example'), effective: { ...config, enabled: true }, sessions_will_reset: false }
  it('preserves label-only sessions, retaining pending and new security impacts', () => {
    expect(authenticationMayResetSessions(source, { display_name: 'New label' })).toBe(false)
    expect(authenticationMayResetSessions(source, { client_secret: 'replacement' })).toBe(true)
    expect(authenticationMayResetSessions(source, { session_ttl: '15m' })).toBe(true)
    expect(authenticationMayResetSessions({ ...source, sessions_will_reset: true }, { display_name: 'New label' })).toBe(true)
    expect(authenticationMayResetSessions({ ...source, sessions_will_reset: undefined }, { display_name: 'New label' })).toBe(true)
    expect(authenticationMayResetSessions({ ...source, effective: null }, { enabled: true })).toBe(false)
  })
})
