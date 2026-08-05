import { describe, expect, it } from 'vitest'

import {
  operatorEcosystems,
  securityEcosystems,
  standardUpstreamEcosystems,
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
})
