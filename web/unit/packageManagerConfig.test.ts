import { describe, expect, it } from 'vitest'

import { renderManagerTemplate, resolveServiceOrigin } from '../src/lib/packageManagerConfig'
import { LANGUAGES } from '../src/lib/ecosystemData'

describe('package manager endpoint generation', () => {
  it('uses the browser-visible LAN and HTTPS origins unchanged', () => {
    expect(resolveServiceOrigin('http://10.0.0.10:23333')).toBe('http://10.0.0.10:23333')
    expect(resolveServiceOrigin('https://depsilo.example.com')).toBe('https://depsilo.example.com')
  })

  it('never publishes a wildcard bind address', () => {
    expect(resolveServiceOrigin('http://0.0.0.0:23333')).toBe('http://localhost:23333')
    expect(resolveServiceOrigin('http://[::]:23333')).toBe('http://localhost:23333')
  })

  it('removes every plain-HTTP exception from HTTPS configuration', () => {
    const template = [
      'url = "{URL}"',
      'trusted-host = {HOST}',
      'pip config set global.trusted-host {HOST}',
      'verify_ssl = false',
      'unsafeHttpWhitelist:',
      '  - {HOST}',
      'insecure = true',
      'composer config -g secure-http false',
      '"secure-http": false',
      '"insecure-registries": ["{HOST}"]',
    ].join('\n')
    const result = renderManagerTemplate(template, 'https://depsilo.example.com')

    expect(result).toContain('https://depsilo.example.com')
    expect(result).toContain('"secure-http": true')
    expect(result).not.toMatch(/trusted-host|verify_ssl|unsafeHttpWhitelist|insecure\s*=\s*true|secure-http false|insecure-registries/)
  })

  it('keeps required exceptions for a plain HTTP endpoint', () => {
    expect(renderManagerTemplate('trusted-host = {HOST}', 'http://localhost:23333'))
      .toBe('trusted-host = localhost:23333')
  })

  it('renders every canonical HTTPS template without an insecure opt-out', () => {
    const templates = LANGUAGES.flatMap(language => language.managers.flatMap(manager => [
      manager.quick.body,
      manager.persistent.body,
      manager.verify.body,
      ...(manager.configure ? [manager.configure.body] : []),
      ...(manager.methods?.map(method => method.body) ?? []),
      ...(manager.test ? [manager.test.body] : []),
    ]))

    for (const template of templates) {
      expect(renderManagerTemplate(template, 'https://depsilo.example.com'))
        .not.toMatch(/trusted-host|verify_ssl\s*=\s*false|unsafeHttpWhitelist|insecure\s*=\s*true|secure-http\s+false|"secure-http"\s*:\s*false|insecure-registries/)
    }
  })
})
