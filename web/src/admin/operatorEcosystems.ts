/**
 * Canonical Ecosystems shown to Operators in Admin filters and forms.
 *
 * These are the 14 standard install surfaces plus Docker's separate OCI
 * surface. Internal adapter aliases such as `pip`, `goproxy`, and `crates`
 * intentionally do not cross this product-language seam.
 */
const operatorEcosystemDefinitions = [
  { id: 'pypi', label: 'PyPI' },
  { id: 'apt', label: 'APT' },
  { id: 'npm', label: 'npm' },
  { id: 'go', label: 'Go' },
  { id: 'cargo', label: 'Cargo' },
  { id: 'maven', label: 'Maven' },
  { id: 'rubygems', label: 'RubyGems' },
  { id: 'composer', label: 'Composer' },
  { id: 'nuget', label: 'NuGet' },
  { id: 'conda', label: 'Conda' },
  { id: 'cran', label: 'CRAN' },
  { id: 'helm', label: 'Helm' },
  { id: 'alpine', label: 'Alpine' },
  { id: 'docker', label: 'Docker' },
  { id: 'huggingface', label: 'Hugging Face' },
] as const

export type OperatorEcosystemId = (typeof operatorEcosystemDefinitions)[number]['id']

export const operatorEcosystems = Object.freeze(operatorEcosystemDefinitions)

export const standardUpstreamEcosystems = Object.freeze(
  operatorEcosystems.filter(ecosystem => ecosystem.id !== 'docker'),
)

const securityEcosystemIds: ReadonlySet<OperatorEcosystemId> = new Set([
  'pypi',
  'apt',
  'npm',
  'go',
  'cargo',
  'maven',
  'rubygems',
  'composer',
  'nuget',
  'cran',
])

/** Ecosystems backed by the advisory scanner's OSV capability catalog. */
export const securityEcosystems = Object.freeze(
  operatorEcosystems.filter(ecosystem => securityEcosystemIds.has(ecosystem.id)),
)

const maliciousBlocklistEcosystemIds = [
  'npm', 'cargo', 'composer', 'nuget', 'go', 'maven',
] as const satisfies readonly OperatorEcosystemId[]

/** Ecosystems with end-to-end MAL dataset enforcement on artifact requests. */
export const maliciousBlocklistEcosystems = Object.freeze(
  maliciousBlocklistEcosystemIds.map(id => operatorEcosystems.find(ecosystem => ecosystem.id === id)!),
)

export type PackageRuleCapability = 'package-only' | 'exact' | 'range'

const packageRuleCapabilities: Readonly<Partial<Record<OperatorEcosystemId, PackageRuleCapability>>> = Object.freeze({
  pypi: 'range',
  apt: 'package-only',
  npm: 'range',
  go: 'exact',
  cargo: 'range',
  maven: 'exact',
  composer: 'package-only',
  nuget: 'exact',
  conda: 'exact',
  cran: 'exact',
  alpine: 'exact',
})

/** Ecosystems whose proxy request identity is safe enough for Package Rules. */
export const packageRuleEcosystems = Object.freeze(
  operatorEcosystems.filter(ecosystem => packageRuleCapabilities[ecosystem.id] !== undefined),
)

export function packageRuleCapabilityFor(ecosystem: string): PackageRuleCapability | null {
  return packageRuleCapabilities[ecosystem as OperatorEcosystemId] ?? null
}

export function supportsPackageRules(ecosystem: string): boolean {
  return packageRuleCapabilityFor(ecosystem) !== null
}

/** Whether real proxy requests expose a complete version for package rules. */
export function supportsPackageRuleVersions(ecosystem: string): boolean {
  const capability = packageRuleCapabilityFor(ecosystem)
  return capability === 'exact' || capability === 'range'
}

/** Whether package rules can safely express an ordered version comparison. */
export function supportsPackageRuleRanges(ecosystem: string): boolean {
  return packageRuleCapabilityFor(ecosystem) === 'range'
}

const vulnerabilityAutoBlockEcosystemIds: ReadonlySet<string> = new Set()

/** Whether complete OSV affected sets can be projected into Package Rules. */
export function supportsVulnerabilityAutoBlock(ecosystem: string): boolean {
  return vulnerabilityAutoBlockEcosystemIds.has(ecosystem)
}
