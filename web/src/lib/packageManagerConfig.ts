import type { ManagerNote } from './ecosystemData'

function browserOrigin(): string {
  if (typeof window === 'undefined') return 'http://localhost:23333'
  return window.location.origin
}

/**
 * Turn a browser-visible origin into a package-manager endpoint. Bind-only
 * wildcard hosts are never useful to a client, so use localhost while
 * preserving the browser's scheme and port.
 */
export function resolveServiceOrigin(origin = browserOrigin()): string {
  try {
    const url = new URL(origin)
    if (url.hostname === '0.0.0.0' || url.hostname === '[::]' || url.hostname === '::') {
      url.hostname = 'localhost'
    }
    url.pathname = ''
    url.search = ''
    url.hash = ''
    return url.origin
  } catch {
    return resolveServiceOrigin()
  }
}

/**
 * Whether the endpoint written into client configuration is a loopback
 * address. Such configuration only works on the machine the browser runs on.
 */
export function isLoopbackServiceOrigin(origin = browserOrigin()): boolean {
  const hostname = new URL(resolveServiceOrigin(origin)).hostname
  return hostname === 'localhost' || hostname.endsWith('.localhost')
    || hostname === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(hostname)
}

function removeHTTPSOnlyExceptions(source: string): string {
  const lines = source.split('\n')
  const safe: string[] = []

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index]
    if (/^\s*trusted-host\s*=/.test(line)) continue
    if (/^\s*pip config set global\.trusted-host\b/.test(line)) continue
    if (/^\s*verify_ssl\s*=\s*false\s*$/.test(line)) continue
    if (/^\s*insecure\s*=\s*true\s*$/.test(line)) continue
    if (/^\s*composer\s+config\s+-g\s+secure-http\s+false\s*$/.test(line)) continue
    if (/^\s*allowInsecureProtocol\s*=\s*true\s*$/.test(line)) continue
    if (/^\s*yarn config set unsafeHttpWhitelist\b/.test(line)) continue
    if (/^\s*unsafeHttpWhitelist:\s*$/.test(line)) {
      while (index + 1 < lines.length && /^\s+-\s+/.test(lines[index + 1])) index += 1
      continue
    }
    safe.push(line
      .replace(/,?\s*"insecure-registries"\s*:\s*\[[^\]]*\],?/, '')
      .replace(/\s+allowInsecureConnections="true"/g, '')
      .replace(/\s+--allow-insecure-connections\b/g, '')
      .replace(/\s+--trusted-host\s+\S+/g, '')
      .replace(/\s+PIP_TRUSTED_HOST=\S+/g, '')
      .replace(/"secure-http"\s*:\s*false/g, '"secure-http": true'))
  }

  return safe
    .join('\n')
    .replace(/\{\s*\n\s*,/g, '{\n')
    .replace(/,\s*\n\s*}/g, '\n}')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

/** Fill a catalog template for the current instance without leaking bind-host settings. */
export function renderManagerTemplate(source: string, rawOrigin = browserOrigin()): string {
  const endpoint = resolveServiceOrigin(rawOrigin)
  const parsed = new URL(endpoint)
  const rendered = source
    .replace(/\{URL\}/g, endpoint)
    // {HOST} keeps the port; {HOSTNAME} is for settings that match the name alone.
    .replace(/\{HOSTNAME\}/g, parsed.hostname)
    .replace(/\{HOST\}/g, parsed.host)

  return parsed.protocol === 'https:' ? removeHTTPSOnlyExceptions(rendered) : rendered
}

/**
 * A manager's caveats for this instance: commands filled in, and the notes
 * about a plain-HTTP opt-in left out when the address is https.
 */
export function renderManagerNotes(notes: readonly ManagerNote[] = [], rawOrigin = browserOrigin()): ManagerNote[] {
  const endpoint = resolveServiceOrigin(rawOrigin)
  const https = endpoint.startsWith('https:')
  return notes
    .filter(note => !(https && note.plainHttpOnly))
    .map(note => (note.body ? { ...note, body: renderManagerTemplate(note.body, endpoint) } : note))
}
