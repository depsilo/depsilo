export interface BuildVersion {
  version?: string | null
  commit?: string | null
  build_date?: string | null
}

export function providedBuildValue(value?: string | null): string | undefined {
  const normalized = value?.trim()
  return normalized && normalized !== 'unknown' ? normalized : undefined
}

/**
 * Keep release identifiers intact. Only Git object IDs have a defined short
 * form; arbitrary build identifiers stay available in the build details.
 */
export function buildVersionLabel(build: BuildVersion): string {
  const version = providedBuildValue(build.version)
  const describedCommit = version?.match(/-\d+-g([a-f\d]{7,40})(?:-dirty)?$/i)?.[1]
  const dirty = version?.endsWith('-dirty')
  const release = version?.match(/^v?(\d+\.\d+\.\d+(?:-[\da-z.-]+)?(?:\+[\da-z.-]+)?)$/i)?.[1]
  if (release && !describedCommit && !dirty) return `v${release}`

  const commit = [build.commit, describedCommit, version]
    .map(value => providedBuildValue(value)?.replace(/-dirty$/, ''))
    .find(value => value && /^[a-f\d]{7,40}$/i.test(value))
  if (commit) return `dev·${commit.slice(0, 7)}`

  const date = providedBuildValue(build.build_date)
  if (date && /^\d{4}-\d{2}-\d{2}T/.test(date) && Number.isFinite(Date.parse(date))) {
    return new Date(date).toISOString().slice(0, 10)
  }

  if (!version) return '—'
  return version === 'dev' ? 'dev' : 'build'
}
