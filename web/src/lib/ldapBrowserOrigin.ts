// Check before collecting or sending a directory password: a server-side
// rejection cannot undo transmission over an unencrypted browser connection.
export function ldapBrowserCredentialsAllowed(origin: string): boolean {
  try {
    const url = new URL(origin)
    if (url.username || url.password || url.pathname !== '/' || url.search || url.hash) return false
    if (url.protocol === 'https:') return true
    if (url.protocol !== 'http:') return false
    const host = url.hostname.toLowerCase()
    return host === 'localhost' || host === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(host)
  } catch {
    return false
  }
}
