import { getApiError } from './apiError'

export function identityErrorKey(error: unknown): string {
  const { code, status } = getApiError(error)
  switch (code) {
    case 'LDAP_UNAVAILABLE': return 'ldap.errors.unavailable'
    case 'LDAP_AUTHENTICATION_FAILED': return 'ldap.errors.credentials'
    case 'HTTPS_REQUIRED': return 'ldap.errors.https'
    case 'REAUTHENTICATION_FAILED': return 'account.errors.password'
    case 'ACCESS_DENIED': return 'account.errors.denied'
    case 'IDENTITY_ALREADY_BOUND': return 'account.errors.conflict'
    case 'LOGIN_EXPIRED': return 'account.errors.expired'
    case 'OIDC_UNAVAILABLE': return 'account.errors.unavailable'
    case 'RATE_LIMITED': return 'account.errors.rateLimited'
    default: return status === 429 ? 'account.errors.rateLimited' : 'account.errors.failed'
  }
}

/** Only the server chooses a provider URL; never accept a destination from query parameters. */
export function identityAuthorizationURL(value: string): string {
  const url = new URL(value)
  if (!['https:', 'http:'].includes(url.protocol) || url.username || url.password) {
    throw new TypeError('Invalid authorization URL')
  }
  return url.href
}
