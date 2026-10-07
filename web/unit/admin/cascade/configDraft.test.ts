import { describe, expect, it } from 'vitest'

import {
  buildCascadePatch,
  cascadeDraftHasErrors,
  draftFromState,
  durationToMilliseconds,
  newPeerDraft,
  validateCascadeDraft,
} from '../../../src/admin/cascade/configDraft'
import type { AdminCascadeConfigState } from '../../../src/lib/adminApi.types'

const state: AdminCascadeConfigState = {
  enabled: true,
  max_hops: 4,
  max_ttl: '168h',
  allow_insecure_http: false,
  token_set: true,
  peers: [
    { name: 'home', url: 'https://cache.example', token_set: true, forward_credentials: false },
    { name: 'lab', url: 'http://127.0.0.1:23333', token_set: false, forward_credentials: true },
  ],
  config_writable: true,
  pending_restart: false,
}

describe('cascade config draft', () => {
  it('masks stored tokens and validates an untouched form', () => {
    const draft = draftFromState(state)
    expect(draft.sharedToken).toBe('')
    expect(draft.peers.map(peer => peer.token)).toEqual(['', ''])
    expect(cascadeDraftHasErrors(validateCascadeDraft(draft))).toBe(false)
  })

  it('requires a shared secret when enabling without an existing one', () => {
    const draft = { ...draftFromState(state), enabled: true, sharedTokenSet: false }
    const errors = validateCascadeDraft(draft)
    expect(errors.sharedToken).toBe('cascade.validation.tokenRequired')
  })

  it('checks token length, hop range, and TTL bounds', () => {
    const draft = {
      ...draftFromState(state),
      sharedToken: 'short',
      maxHops: '99',
      maxTTL: '10s',
    }
    const errors = validateCascadeDraft(draft)
    expect(errors.sharedToken).toBe('cascade.validation.tokenTooShort')
    expect(errors.maxHops).toBe('cascade.validation.maxHops')
    expect(errors.maxTTL).toBe('cascade.validation.ttl')
  })

  it('rejects duplicate names and insecure peer URLs', () => {
    const duplicate = newPeerDraft()
    duplicate.name = 'home'
    duplicate.url = 'http://cache.internal:23333'
    const draft = { ...draftFromState(state), peers: [...draftFromState(state).peers, duplicate] }
    const errors = validateCascadeDraft(draft)
    expect(errors.peers[duplicate.key]?.name).toBe('cascade.validation.peerNameDuplicate')
    expect(errors.peers[duplicate.key]?.url).toBe('cascade.validation.peerHttp')
  })

  it('keeps untouched tokens, sends typed tokens, and clears on request', () => {
    const draft = draftFromState(state)
    draft.peers[0].token = 'new-peer-token-0123456789'
    draft.peers[1].tokenSet = true
    draft.peers[1].clearToken = true
    draft.sharedToken = 'new-shared-token-0123456789'
    const patch = buildCascadePatch(draft)
    expect(patch.shared_token).toBe('new-shared-token-0123456789')
    expect(patch.peers?.[0].token).toBe('new-peer-token-0123456789')
    expect(patch.peers?.[1].token).toBe('')
  })

  it('omits unchanged secrets from the patch', () => {
    const patch = buildCascadePatch(draftFromState(state))
    expect(patch.shared_token).toBeUndefined()
    expect(patch.peers?.[0].token).toBeUndefined()
    expect(patch.peers?.[1].token).toBeUndefined()
  })
})

describe('duration parsing', () => {
  it('parses Go-style compound durations', () => {
    expect(durationToMilliseconds('1h30m')).toBe(5_400_000)
    expect(durationToMilliseconds('720h')).toBe(2_592_000_000)
    expect(durationToMilliseconds('-1h')).toBeNull()
    expect(durationToMilliseconds('soon')).toBeNull()
  })
})
