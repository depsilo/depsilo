import type { Principal } from './adminApi.types'

export function canManageCredentials(principal: Principal | undefined): boolean {
  if (!principal?.can_write || (principal.permission_source && principal.permission_source !== 'local') || (principal.login_method && !['password', 'api_token'].includes(principal.login_method))) return false
  // Older servers/fixtures predate the explicit field; External sessions always fail closed.
  return principal.can_manage_credentials ?? principal.can_write
}
