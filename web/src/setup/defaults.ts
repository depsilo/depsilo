import { setupUpstreamDefaults } from '@/lib/generated/setupUpstreamDefaults'

export interface UpstreamDefault {
  name: string
  url: string
  priority: number
  proxy?: string
}

export interface EcosystemDefault {
  key: string
  label: string
  upstreams: UpstreamDefault[]
}

// The Upstreams come from internal/ecosystem/defaults.go, which an instance
// without a config file also serves from; only the labels live here.
const labels: Record<(typeof setupUpstreamDefaults)[number]['key'], string> = {
  pypi: 'PyPI (pip)',
  apt: 'APT (Debian/Ubuntu)',
  npm: 'npm (Node.js)',
  go: 'Go Modules',
  cargo: 'Cargo (Rust)',
  maven: 'Maven (Java)',
  rubygems: 'RubyGems',
  composer: 'Composer (PHP)',
  nuget: 'NuGet (.NET)',
  conda: 'Conda',
  cran: 'CRAN (R)',
  alpine: 'Alpine Linux (apk)',
  helm: 'Helm (Kubernetes)',
}

export const ecosystemDefaults: EcosystemDefault[] = setupUpstreamDefaults.map(({ key, upstreams }) => ({
  key,
  label: labels[key],
  upstreams: upstreams.map(upstream => ({ ...upstream })),
}))
