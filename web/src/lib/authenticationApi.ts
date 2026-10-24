import type { LDAPAuthenticationPatch, LDAPPreviewRequest, LDAPConfigurationCheck } from './adminApi.types'
import api, { type ApiGetOptions } from './api'
import type { AuthenticationPatch, AuthenticationSettingsResponse, IdentityProvider, LoginIdentitiesResponse, OIDCCompleteResponse } from './adminApi.types'
import type { AuthenticationSourcesResponse, AuthenticationSourceState, CreateAuthenticationSource, AuthenticationPreviewRequest, AuthenticationCheck, AuthenticationImpact } from './adminApi.types'

/** Identity transport stays with the lazily loaded login/account/system pages. */
export const authenticationApi = {
  providers: (options: ApiGetOptions = {}) => api.get<{ providers: IdentityProvider[] }>('/auth/providers', options),
  startOIDC: (returnTo: string, providerId: string, options: ApiGetOptions = {}) => api.post<{ authorization_url: string }>('/auth/oidc/start', { return_to: returnTo, provider_id: providerId }, options),
  completeOIDC: (currentToken: string | null) => api.post<OIDCCompleteResponse>('/auth/identity/complete', {}, currentToken ? { headers: { Authorization: `Bearer ${currentToken}` } } : undefined),
  resolveOIDC: (data: { action: 'create' } | { action: 'link'; username: string; password: string }, options: ApiGetOptions = {}) => api.post<Extract<OIDCCompleteResponse, { mode: 'login' }>>('/auth/identity/resolve', data, options),
  cancelOIDC: (options: ApiGetOptions = {}) => api.post<void>('/auth/identity/cancel', {}, options),
  loginLDAP: (data: { provider_id: string; username: string; password: string; return_to: string }, options: ApiGetOptions = {}) => api.post<Extract<OIDCCompleteResponse, { mode: 'login' | 'choice' }>>('/auth/ldap/login', data, options),
  linkLDAP: (data: { provider_id: string; password: string; username: string; directory_password: string }, options: ApiGetOptions = {}) => api.post<Extract<OIDCCompleteResponse, { mode: 'link' }>>('/auth/identities/ldap/link', data, options),
  checkLDAP: (data: LDAPPreviewRequest, options: ApiGetOptions = {}) => api.post<LDAPConfigurationCheck>('/admin/authentication/ldap/check', data, options),
  trialLDAP: (data: LDAPPreviewRequest & { username: string; password: string }, options: ApiGetOptions = {}) => api.post<Extract<OIDCCompleteResponse, { mode: 'test' }>>('/admin/authentication/ldap/trial', data, options),
  sources: (options: ApiGetOptions = {}) => api.get<AuthenticationSourcesResponse>('/admin/authentication/sources', options),
  createSource: (data: CreateAuthenticationSource, options: ApiGetOptions = {}) => api.post<AuthenticationSourceState>('/admin/authentication/sources', data, options),
  updateSource: (id: string, data: AuthenticationPatch | LDAPAuthenticationPatch, options: ApiGetOptions = {}) => api.put<AuthenticationSourceState>(`/admin/authentication/sources/${encodeURIComponent(id)}`, data, options),
  deleteSource: (id: string, options: ApiGetOptions = {}) => api.delete<void>(`/admin/authentication/sources/${encodeURIComponent(id)}`, options),
  checkSource: (data: AuthenticationPreviewRequest, options: ApiGetOptions = {}) => api.post<AuthenticationCheck>('/admin/authentication/check', data, options),
  trialSource: (data: AuthenticationPreviewRequest, options: ApiGetOptions = {}) => api.post<{ authorization_url: string }>('/admin/authentication/trial', data, options),
  sourceImpact: (id: string, options: ApiGetOptions = {}) => api.get<AuthenticationImpact>(`/admin/authentication/sources/${encodeURIComponent(id)}/impact`, options),
  restoreSource: (id: string, options: ApiGetOptions = {}) => api.post<AuthenticationSourceState>(`/admin/authentication/sources/${encodeURIComponent(id)}/restore`, {}, options),
  settings: (options: ApiGetOptions = {}) => api.get<AuthenticationSettingsResponse>('/admin/authentication', options),
  updateSettings: (data: AuthenticationPatch, options: ApiGetOptions = {}) => api.put<AuthenticationSettingsResponse>('/admin/authentication', data, options),
  identities: (options: ApiGetOptions = {}) => api.get<LoginIdentitiesResponse>('/auth/identities', options),
  linkIdentity: (password: string, providerId: string, options: ApiGetOptions = {}) => api.post<{ authorization_url: string }>('/auth/identities/link', { password, provider_id: providerId }, options),
  unlinkIdentity: (id: number, password: string, options: ApiGetOptions = {}) => api.post<void>(`/auth/identities/${id}/unlink`, { password }, options),
}
