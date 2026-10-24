export function safeAdminDestination(candidate: unknown, origin = window.location.origin): string | null {
  if (typeof candidate !== 'string' || !candidate.startsWith('/admin') || candidate.startsWith('//')) return null
  try {
    const parsed = new URL(candidate, origin)
    const pathname = parsed.pathname.toLowerCase().replace(/\/+$/, '')
    if (parsed.origin !== origin ||
        (parsed.pathname !== '/admin' && !parsed.pathname.startsWith('/admin/')) ||
        (pathname === '/admin/login' || pathname.startsWith('/admin/auth/'))) return null
    return `${parsed.pathname}${parsed.search}${parsed.hash}`
  } catch {
    return null
  }
}

export function loginDestination(state: unknown, search: string, origin = window.location.origin) {
  const queryDestination = safeAdminDestination(new URLSearchParams(search).get('next'), origin)
  if (!state || typeof state !== 'object' || !('from' in state)) return queryDestination ?? '/admin'
  const from = (state as { from?: unknown }).from
  if (!from || typeof from !== 'object') return queryDestination ?? '/admin'
  const candidate = from as { pathname?: unknown; search?: unknown; hash?: unknown }
  const fromSearch = typeof candidate.search === 'string' ? candidate.search : ''
  const hash = typeof candidate.hash === 'string' ? candidate.hash : ''
  return safeAdminDestination(`${String(candidate.pathname ?? '')}${fromSearch}${hash}`, origin) ?? queryDestination ?? '/admin'
}

export function adminLoginURL(target: string) {
  const login = new URL('/admin/login', target)
  // Enter through the Admin root so the authenticated durable gate decides
  // between Dashboard and first-run onboarding after a cross-origin restart.
  login.searchParams.set('next', '/admin')
  return login.toString()
}
