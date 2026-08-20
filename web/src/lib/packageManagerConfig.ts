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
    if (/^\s*unsafeHttpWhitelist:\s*$/.test(line)) {
      while (index + 1 < lines.length && /^\s+-\s+/.test(lines[index + 1])) index += 1
      continue
    }
    safe.push(line
      .replace(/,?\s*"insecure-registries"\s*:\s*\[[^\]]*\],?/, '')
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
    .replace(/\{HOST\}/g, parsed.host)

  return parsed.protocol === 'https:' ? removeHTTPSOnlyExceptions(rendered) : rendered
}
