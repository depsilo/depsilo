import { describe, expect, it } from 'vitest'
import { advisoryReferences, manualRuleDraftHref, readManualRuleDraft } from '../src/admin/security/evidence'

describe('advisory evidence handoff', () => {
  it('accepts public http references but rejects executable schemes, credentials and duplicates', () => {
    expect(advisoryReferences(JSON.stringify([
      { type: 'ADVISORY', url: 'https://example.org/advisory' },
      'https://example.org/advisory',
      { url: 'javascript:alert(1)' },
      'data:text/html,<script>1</script>',
      'https://user:secret@example.org/private',
      { url: 123 },
      'http://example.org/fix',
    ]))).toEqual(['https://example.org/advisory', 'http://example.org/fix'])
    expect(advisoryReferences('not-a-url\nhttps://example.org/a')).toEqual(['https://example.org/a'])
  })

  it('prefills identity and source only, requiring an explicit reviewed version', () => {
    const href = manualRuleDraftHref('pypi', 'example-package', 'GHSA-example')!
    const params = new URL(href, 'https://depsilo.test').searchParams
    expect(params.has('version')).toBe(false)
    expect(readManualRuleDraft(params)).toEqual({
      ecosystem: 'pypi', package_name: 'example-package', version: '', action: 'deny', reason: 'GHSA-example',
    })
    params.set('version', '>=1.0 <2.0 || ==3.0')
    expect(readManualRuleDraft(params)?.version).toBe('')
  })

  it('does not offer unsupported ecosystems and retains package-only semantics', () => {
    expect(manualRuleDraftHref('docker', 'library/nginx', 'GHSA-example')).toBeNull()
    expect(readManualRuleDraft(new URLSearchParams('draft=1&ecosystem=docker&package=nginx'))).toBeNull()
    expect(readManualRuleDraft(new URLSearchParams('draft=1&ecosystem=pypi&package='))).toBeNull()
    expect(readManualRuleDraft(new URLSearchParams('draft=1&ecosystem=apt&package=openssl'))?.version).toBe('*')
  })
})
