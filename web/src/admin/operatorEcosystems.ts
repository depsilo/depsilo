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
