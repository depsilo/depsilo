import type { AdminCascadeConfigPatch, AdminCascadeConfigState } from '@/lib/adminApi.types'

export const CASCADE_MAX_HOPS = 16
export const CASCADE_MIN_TOKEN_LENGTH = 16

const goDurationPattern = /^(?:0|[+-]?(?:(?:\d+(?:\.\d*)?|\.\d+)(?:ns|us|µs|μs|ms|s|m|h))+)$/
const peerNamePattern = /^[a-z0-9][a-z0-9-]{0,63}$/

export interface CascadePeerDraft {
  key: string
  /** Name read from config.toml; used to keep an untouched token override. */
  originalName: string
  name: string
  url: string
  /** New write-only override typed in this dialog. */
  token: string
  /** An override already exists in config.toml. */
  tokenSet: boolean
  /** The operator asked to remove the stored override (back to inheritance). */
  clearToken: boolean
  forwardCredentials: boolean
}

export interface CascadeConfigDraft {
  enabled: boolean
  maxHops: string
  maxTTL: string
  allowInsecureHTTP: boolean
  /** New write-only shared secret typed in this dialog. */
  sharedToken: string
  /** A shared secret already exists in config.toml. */
  sharedTokenSet: boolean
  /** The operator asked to remove the stored shared secret. */
  clearSharedToken: boolean
  peers: CascadePeerDraft[]
}

export type CascadeValidationMessage =
  | 'cascade.validation.tokenRequired'
  | 'cascade.validation.tokenTooShort'
  | 'cascade.validation.maxHops'
  | 'cascade.validation.ttl'
  | 'cascade.validation.peerName'
  | 'cascade.validation.peerNameDuplicate'
  | 'cascade.validation.peerUrl'
  | 'cascade.validation.peerHttp'

export interface CascadePeerErrors {
  name?: CascadeValidationMessage
  url?: CascadeValidationMessage
  token?: CascadeValidationMessage
}

export interface CascadeDraftErrors {
  sharedToken?: CascadeValidationMessage
  maxHops?: CascadeValidationMessage
  maxTTL?: CascadeValidationMessage
  peers: Record<string, CascadePeerErrors>
}

let peerKeySeed = 0

function nextPeerKey(): string {
  peerKeySeed += 1
  return `cascade-peer-${peerKeySeed}`
}

export function draftFromState(state: AdminCascadeConfigState): CascadeConfigDraft {
  return {
    enabled: state.enabled,
    maxHops: String(state.max_hops),
    maxTTL: state.max_ttl,
    allowInsecureHTTP: state.allow_insecure_http,
    sharedToken: '',
    sharedTokenSet: state.token_set,
    clearSharedToken: false,
    peers: state.peers.map(peer => ({
      key: nextPeerKey(),
      originalName: peer.name,
      name: peer.name,
      url: peer.url,
      token: '',
      tokenSet: peer.token_set,
      clearToken: false,
      forwardCredentials: peer.forward_credentials,
    })),
  }
}

export function newPeerDraft(): CascadePeerDraft {
  return {
    key: nextPeerKey(),
    originalName: '',
    name: '',
    url: '',
    token: '',
    tokenSet: false,
    clearToken: false,
    forwardCredentials: false,
  }
}

export function durationToMilliseconds(value: string): number | null {
  const trimmed = value.trim()
  if (!goDurationPattern.test(trimmed) || trimmed.startsWith('-') || trimmed.startsWith('+')) {
    return null
  }
  if (trimmed === '0') return 0
  const units: Record<string, number> = {
    ns: 1e-6,
    us: 1e-3,
    'µs': 1e-3,
    'μs': 1e-3,
    ms: 1,
    s: 1_000,
    m: 60_000,
    h: 3_600_000,
  }
  let total = 0
  const parts = trimmed.matchAll(/(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/g)
  for (const part of parts) {
    const amount = Number(part[1])
    const unit = units[part[2]]
    if (!Number.isFinite(amount) || unit === undefined) return null
    total += amount * unit
  }
  return Number.isFinite(total) ? total : null
}

export function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase().replace(/^\[|\]$/g, '')
  return normalized === 'localhost' ||
    normalized.endsWith('.localhost') ||
    normalized === '::1' ||
    /^127(?:\.\d{1,3}){3}$/.test(normalized)
}

export function validateCascadeDraft(draft: CascadeConfigDraft): CascadeDraftErrors {
  const errors: CascadeDraftErrors = { peers: {} }
  const sharedToken = draft.sharedToken.trim()
  if (sharedToken !== '' && sharedToken.length < CASCADE_MIN_TOKEN_LENGTH) {
    errors.sharedToken = 'cascade.validation.tokenTooShort'
  } else if (draft.enabled && sharedToken === '' && (!draft.sharedTokenSet || draft.clearSharedToken)) {
    errors.sharedToken = 'cascade.validation.tokenRequired'
  }

  const maxHops = Number(draft.maxHops)
  if (!Number.isInteger(maxHops) || maxHops < 1 || maxHops > CASCADE_MAX_HOPS) {
    errors.maxHops = 'cascade.validation.maxHops'
  }
  const maxTTL = durationToMilliseconds(draft.maxTTL)
  if (maxTTL === null || maxTTL < 60_000 || maxTTL > 720 * 3_600_000) {
    errors.maxTTL = 'cascade.validation.ttl'
  }

  const seen = new Set<string>()
  for (const peer of draft.peers) {
    const peerErrors: CascadePeerErrors = {}
    const name = peer.name.trim()
    if (!peerNamePattern.test(name)) {
      peerErrors.name = 'cascade.validation.peerName'
    } else if (seen.has(name)) {
      peerErrors.name = 'cascade.validation.peerNameDuplicate'
    } else {
      seen.add(name)
    }
    try {
      const parsed = new URL(peer.url.trim())
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        peerErrors.url = 'cascade.validation.peerUrl'
      } else if (parsed.username || parsed.password || parsed.search || parsed.hash) {
        peerErrors.url = 'cascade.validation.peerUrl'
      } else if (parsed.protocol === 'http:' && !draft.allowInsecureHTTP && !isLoopbackHostname(parsed.hostname)) {
        peerErrors.url = 'cascade.validation.peerHttp'
      }
    } catch {
      peerErrors.url = 'cascade.validation.peerUrl'
    }
    const token = peer.token.trim()
    if (token !== '' && token.length < CASCADE_MIN_TOKEN_LENGTH) {
      peerErrors.token = 'cascade.validation.tokenTooShort'
    }
    if (peerErrors.name || peerErrors.url || peerErrors.token) {
      errors.peers[peer.key] = peerErrors
    }
  }
  return errors
}

export function cascadeDraftHasErrors(errors: CascadeDraftErrors): boolean {
  return Boolean(errors.sharedToken || errors.maxHops || errors.maxTTL) || Object.keys(errors.peers).length > 0
}

function peerTokenPatch(peer: CascadePeerDraft): string | undefined {
  if (peer.clearToken) return ''
  const token = peer.token.trim()
  return token === '' ? undefined : token
}

export function buildCascadePatch(draft: CascadeConfigDraft): AdminCascadeConfigPatch {
  const sharedToken = draft.clearSharedToken
    ? ''
    : (draft.sharedToken.trim() === '' ? undefined : draft.sharedToken.trim())
  return {
    enabled: draft.enabled,
    max_hops: Number(draft.maxHops),
    max_ttl: draft.maxTTL.trim(),
    allow_insecure_http: draft.allowInsecureHTTP,
    ...(sharedToken !== undefined ? { shared_token: sharedToken } : {}),
    peers: draft.peers.map(peer => ({
      name: peer.name.trim(),
      url: peer.url.trim(),
      forward_credentials: peer.forwardCredentials,
      ...(peerTokenPatch(peer) !== undefined ? { token: peerTokenPatch(peer) } : {}),
    })),
  }
}
