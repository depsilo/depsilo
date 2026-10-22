import type { AdminUser, APITokenSummary } from './adminApi.types'

/** A token's declared scope is bounded by its owner's current account state. */
export function tokenStatus(token: APITokenSummary, users: AdminUser[] | undefined, now: number) {
  if (token.expires_at && Date.parse(token.expires_at) <= now) return 'expired'
  if (!users) return 'unknown'
  const owner = users.find(user => user.id === token.user_id)
  if (!owner) return 'ownerMissing'
  if (!owner.enabled) return 'ownerDisabled'
  if (token.permissions === 'readwrite' && owner.role !== 'admin') return 'restricted'
  return 'active'
}
