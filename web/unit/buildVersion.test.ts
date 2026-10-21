import { describe, expect, it } from 'vitest'
import { buildVersionLabel, providedBuildValue } from '../src/lib/buildVersion'

describe('buildVersionLabel', () => {
  it.each([
    ['0.9.1', 'v0.9.1'],
    ['v1.2.3', 'v1.2.3'],
    ['1.2.3-rc.2', 'v1.2.3-rc.2'],
    ['1.2.3+package.42', 'v1.2.3+package.42'],
  ])('keeps the complete release identifier %s', (version, expected) => {
    expect(buildVersionLabel({ version, commit: '1234567890abcdef' })).toBe(expected)
  })

  it.each([
    [{ version: '0.9.1-26-g43ca7fe-dirty' }, 'dev·43ca7fe'],
    [{ version: '0.9.1-dirty', commit: '1234567890abcdef' }, 'dev·1234567'],
    [{ version: 'dev', commit: '1234567890abcdef' }, 'dev·1234567'],
    [{ commit: '1234567890abcdef1234567890abcdef12345678-dirty', build_date: '2026-10-20T08:12:45Z' }, 'dev·1234567'],
    [{ version: '1234567890abcdef-dirty' }, 'dev·1234567'],
    [{ version: 'custom-build-name', commit: 'not-a-git-object', build_date: '2026-10-20T08:12:45Z' }, '2026-10-20'],
    [{ version: 'custom-build-name', build_date: 'not-a-date' }, 'build'],
    [{ version: 'dev', commit: 'unknown', build_date: 'unknown' }, 'dev'],
    [{ version: 'unknown' }, '—'],
    [{}, '—'],
  ])('uses available build identity without inventing a release: %j', (build, expected) => {
    expect(buildVersionLabel(build)).toBe(expected)
  })

  it('does not treat placeholders as known build fields', () => {
    expect(providedBuildValue(' unknown ')).toBeUndefined()
    expect(providedBuildValue('')).toBeUndefined()
    expect(providedBuildValue(' 0.9.1 ')).toBe('0.9.1')
  })
})
