export const ECOSYSTEM_TYPES = [
  'pip',
  'pypi',
  'apt',
  'npm',
  'go',
  'goproxy',
  'cargo',
  'crates',
  'maven',
  'rubygems',
  'composer',
  'nuget',
  'conda',
  'cran',
  'helm',
  'docker',
  'huggingface',
  'alpine',
] as const

export type EcosystemType = (typeof ECOSYSTEM_TYPES)[number]

const ecosystemTypes = new Set<string>(ECOSYSTEM_TYPES)

export function isEcosystemType(value: string): value is EcosystemType {
  return ecosystemTypes.has(value)
}
