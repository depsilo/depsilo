import { getAdminRouteHref } from '../routes'
import { supportsPackageRules, supportsPackageRuleVersions } from '../operatorEcosystems'

/** Advisory references are data, never executable markup or arbitrary URL schemes. */
export function advisoryReferences(raw: string): string[] {
  let values: unknown[]
  try {
    const parsed: unknown = JSON.parse(raw)
    values = Array.isArray(parsed) ? parsed : [parsed]
  } catch {
    values = raw.split(/[\n\s,]+/)
  }
  const urls = values.flatMap(value => {
    const candidate = typeof value === 'string' ? value
      : value && typeof value === 'object' && 'url' in value ? value.url : undefined
    if (typeof candidate !== 'string') return []
    try {
      const url = new URL(candidate)
      return (url.protocol === 'https:' || url.protocol === 'http:') && !url.username && !url.password ? [url.href] : []
    } catch { return [] }
  })
  return [...new Set(urls)]
}

export function manualRuleDraftHref(ecosystem: string, packageName: string, advisory: string): string | null {
  if (!supportsPackageRules(ecosystem)) return null
  const params = new URLSearchParams({ draft: '1', ecosystem, package: packageName, source: advisory })
  // OSV affected sets do not have a lossless Package Rule translation. Never prefill a version.
  return `${getAdminRouteHref('rules')}?${params}`
}

export function readManualRuleDraft(params: URLSearchParams) {
  const ecosystem = params.get('ecosystem') ?? ''
  const packageName = params.get('package')?.trim() ?? ''
  if (params.get('draft') !== '1' || !supportsPackageRules(ecosystem) || !packageName) return null
  return {
    ecosystem,
    package_name: packageName,
    version: supportsPackageRuleVersions(ecosystem) ? '' : '*',
    action: 'deny' as const,
    reason: params.get('source')?.slice(0, 256) ?? '',
  }
}
