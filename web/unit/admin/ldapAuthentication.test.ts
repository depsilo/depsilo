import { describe, expect, it } from 'vitest'
import { ldapDraft, ldapFormSettings, ldapPatch, newLDAPAuthenticationSource, rebaseLDAPDraft } from '../../src/lib/ldapAuthentication'

describe('LDAP configuration edits', () => {
  it('retains saved directory credentials and emits only the changed nested field', () => {
    const saved = newLDAPAuthenticationSource('directory').configured!
    saved.ldap.bind_password_set = true
    const draft = ldapDraft(saved)
    expect(ldapPatch(draft, saved)).toEqual({})
    draft.ldap.url = 'ldaps://directory.example:636'
    expect(ldapPatch(draft, saved)).toEqual({ ldap: { url: 'ldaps://directory.example:636' } })
    draft.ldap.bind_password = 'replacement'
    expect(ldapPatch(draft, saved)).toEqual({ ldap: { url: 'ldaps://directory.example:636', bind_password: 'replacement' } })
    expect(ldapPatch(draft, saved, true).ldap).not.toHaveProperty('bind_password_set')
  })
  it('preserves commas in directory group DNs and keeps local edits across refresh', () => {
    const saved = newLDAPAuthenticationSource('directory').configured!
    const draft = ldapDraft(saved)
    draft.admin_groups = ' cn=operators,ou=groups,dc=example,dc=org \n'
    draft.ldap.user_filter = '(sAMAccountName={username})'
    const refreshed = { ...saved, session_ttl: '15m', ldap: { ...saved.ldap, display_name_attribute: 'cn' } }
    const merged = rebaseLDAPDraft(draft, saved, refreshed)
    expect(merged.session_ttl).toBe('15m')
    expect(merged.ldap.display_name_attribute).toBe('cn')
    expect(ldapPatch(merged, refreshed)).toEqual({ admin_groups: ['cn=operators,ou=groups,dc=example,dc=org'], ldap: { user_filter: '(sAMAccountName={username})' } })
  })
  it('shows effective environment fields and drops edits when the environment takes ownership', () => {
    const source = newLDAPAuthenticationSource('directory')
    const configured = source.configured!
    source.effective = { ...configured, ldap: { ...configured.ldap, url: 'ldaps://managed.example', bind_password_set: true } }
    source.overrides = { 'ldap.url': 'DEPSILO_DIRECTORY_URL', 'ldap.bind_password': 'DEPSILO_DIRECTORY_PASSWORD' }
    const draft = ldapDraft(ldapFormSettings(source))
    expect(draft.ldap.url).toBe('ldaps://managed.example')
    expect(draft.ldap.bind_password_set).toBe(true)
    draft.ldap.bind_password = 'must-not-write'
    expect(ldapPatch(draft, configured, false, source.overrides)).toEqual({})
    const rebased = rebaseLDAPDraft(draft, configured, ldapFormSettings(source), source.overrides)
    expect(rebased.ldap.bind_password).toBe('')
    expect(rebased.ldap.url).toBe('ldaps://managed.example')
  })
})
