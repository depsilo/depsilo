import { describe, expect, it } from 'vitest'

import {
  operatorEcosystems,
  maliciousBlocklistEcosystems,
  packageRuleCapabilityFor,
  packageRuleEcosystems,
  securityEcosystems,
  standardUpstreamEcosystems,
  supportsPackageRuleRanges,
  supportsPackageRules,
  supportsPackageRuleVersions,
  supportsVulnerabilityAutoBlock,
} from '../../src/admin/operatorEcosystems'

describe('Operator ecosystem catalogs', () => {
  it('exposes product surfaces without internal adapter aliases', () => {
    const operatorIds = operatorEcosystems.map(ecosystem => ecosystem.id)
    expect(operatorIds).toEqual([
      'pypi',
      'apt',
      'npm',
      'go',
      'cargo',
      'maven',
      'rubygems',
      'composer',
      'nuget',
      'conda',
      'cran',
      'helm',
      'alpine',
      'docker',
      'huggingface',
    ])
    expect(operatorIds).toHaveLength(15)
    for (const internalAlias of ['pip', 'goproxy', 'crates']) {
      expect(operatorIds).not.toContain(internalAlias)
    }

    expect(standardUpstreamEcosystems.map(ecosystem => ecosystem.id)).toEqual([
      'pypi',
      'apt',
      'npm',
      'go',
      'cargo',
      'maven',
      'rubygems',
      'composer',
      'nuget',
      'conda',
      'cran',
      'helm',
      'alpine',
      'huggingface',
    ])
  })

  it('matches the security scanner capability surface exactly', () => {
    expect(securityEcosystems.map(ecosystem => ecosystem.id)).toEqual([
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
    expect(securityEcosystems).toHaveLength(10)
  })

  it('exposes only ecosystems with end-to-end malicious dataset enforcement', () => {
    expect(maliciousBlocklistEcosystems.map(ecosystem => ecosystem.id)).toEqual([
      'npm', 'cargo', 'composer', 'nuget', 'go', 'maven',
    ])
  })

  it('exposes the complete Package Rule capability matrix used by the rules UI', () => {
    expect(packageRuleEcosystems.map(ecosystem => [ecosystem.id, packageRuleCapabilityFor(ecosystem.id)])).toEqual([
      ['pypi', 'range'],
      ['apt', 'package-only'],
      ['npm', 'range'],
      ['go', 'exact'],
      ['cargo', 'range'],
      ['maven', 'exact'],
      ['composer', 'package-only'],
      ['nuget', 'exact'],
      ['conda', 'exact'],
      ['cran', 'exact'],
      ['alpine', 'exact'],
    ])

    for (const ecosystem of ['rubygems', 'helm', 'docker', 'huggingface', 'unknown']) {
      expect(packageRuleCapabilityFor(ecosystem)).toBeNull()
      expect(supportsPackageRules(ecosystem)).toBe(false)
      expect(supportsPackageRuleVersions(ecosystem)).toBe(false)
      expect(supportsPackageRuleRanges(ecosystem)).toBe(false)
    }
  })

  it('exposes request-path version and auto-block capability', () => {
    expect(
      securityEcosystems
        .filter(ecosystem => supportsVulnerabilityAutoBlock(ecosystem.id))
        .map(ecosystem => ecosystem.id),
    ).toEqual([])
    expect(supportsPackageRuleVersions('apt')).toBe(false)
    expect(supportsPackageRuleVersions('npm')).toBe(true)
    expect(supportsPackageRuleVersions('composer')).toBe(false)
    expect(supportsPackageRuleVersions('go')).toBe(true)
    expect(supportsPackageRuleVersions('alpine')).toBe(true)
    expect(supportsPackageRuleRanges('apt')).toBe(false)
    expect(supportsPackageRuleRanges('npm')).toBe(true)
    expect(supportsVulnerabilityAutoBlock('pypi')).toBe(false)
    expect(supportsVulnerabilityAutoBlock('cargo')).toBe(false)
    expect(supportsVulnerabilityAutoBlock('apt')).toBe(false)
    expect(supportsVulnerabilityAutoBlock('npm')).toBe(false)
  })
})
