import { describe, expect, it } from 'vitest'

import {
  isLoopbackServiceOrigin, renderManagerNotes, renderManagerTemplate, resolveServiceOrigin,
} from '../src/lib/packageManagerConfig'
import { LANGUAGES } from '../src/lib/ecosystemData'

function manager(languageId: string, managerId: string) {
  const found = LANGUAGES.find(language => language.id === languageId)?.managers.find(candidate => candidate.id === managerId)
  if (!found) throw new Error(`No ${languageId}/${managerId} in the catalog`)
  return found
}

describe('package manager endpoint generation', () => {
  it('uses the browser-visible LAN and HTTPS origins unchanged', () => {
    expect(resolveServiceOrigin('http://10.0.0.10:23333')).toBe('http://10.0.0.10:23333')
    expect(resolveServiceOrigin('https://depsilo.example.com')).toBe('https://depsilo.example.com')
  })

  it('never publishes a wildcard bind address', () => {
    expect(resolveServiceOrigin('http://0.0.0.0:23333')).toBe('http://localhost:23333')
    expect(resolveServiceOrigin('http://[::]:23333')).toBe('http://localhost:23333')
  })

  it('recognizes endpoints that only work on the machine running the browser', () => {
    for (const origin of [
      'http://localhost:23333', 'http://127.0.0.1:23333', 'http://127.8.9.10', 'http://[::1]:23333',
      'https://depsilo.localhost', 'http://LOCALHOST:23333',
      // Wildcard bind addresses are rewritten to localhost before they are shown.
      'http://0.0.0.0:23333', 'http://[::]:23333',
    ]) {
      expect(isLoopbackServiceOrigin(origin), origin).toBe(true)
    }
    for (const origin of [
      'http://10.0.0.10:23333', 'http://192.168.1.20:23333', 'https://depsilo.example.com',
      'http://localhost.example.com:23333', 'http://127.example.com', 'http://[fd00::1]:23333',
    ]) {
      expect(isLoopbackServiceOrigin(origin), origin).toBe(false)
    }
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

  it('lists only the host name in the Yarn 2+ plain-HTTP whitelist', () => {
    // Yarn rejects an unsafeHttpWhitelist entry that carries a port (YN0081).
    const yarn = manager('node', 'yarn')
    expect(renderManagerTemplate(yarn.persistent.body, 'http://127.0.0.1:24526')).toBe(
      'npmRegistryServer: "http://127.0.0.1:24526/npm/"\nunsafeHttpWhitelist:\n  - "127.0.0.1"',
    )
    expect(renderManagerTemplate(yarn.persistent.body, 'http://depsilo.internal:23333'))
      .toContain('unsafeHttpWhitelist:\n  - "depsilo.internal"')
    expect(renderManagerTemplate(yarn.persistent.body, 'https://depsilo.example.com'))
      .toBe('npmRegistryServer: "https://depsilo.example.com/npm/"')
  })

  it('whitelists the host in the Yarn 2+ commands for an http address only', () => {
    // Yarn 2+ fails an http registry with YN0081 unless the host is whitelisted.
    const yarn = manager('node', 'yarn')
    const commands = [yarn.quick.body, ...(yarn.methods ?? []).map(method => method.body)]
    expect(commands).toHaveLength(2)
    for (const command of commands) {
      expect(renderManagerTemplate(command, 'http://127.0.0.1:24526')).toBe([
        'yarn config set npmRegistryServer http://127.0.0.1:24526/npm/',
        'yarn config set unsafeHttpWhitelist --json \'["127.0.0.1"]\'',
      ].join('\n'))
      expect(renderManagerTemplate(command, 'https://depsilo.example.com'))
        .toBe('yarn config set npmRegistryServer https://depsilo.example.com/npm/')
    }
  })

  it('keeps the two Yarn generations on their own registry setting', () => {
    const instructions = (id: string) => {
      const { quick, persistent, verify, configure, methods } = manager('node', id)
      return [quick, persistent, verify, ...(configure ? [configure] : []), ...(methods ?? [])]
        .map(entry => renderManagerTemplate(entry.body, 'http://10.0.0.2:23333'))
    }
    // Yarn 1 accepts and echoes npmRegistryServer but installs from `registry`.
    const classic = manager('node', 'yarn-classic')
    expect(renderManagerTemplate(classic.persistent.body, 'http://10.0.0.2:23333'))
      .toBe('registry "http://10.0.0.2:23333/npm/"')
    expect(classic.persistent.file).toBe('~/.yarnrc')
    expect(classic.verify.body).toBe('yarn config get registry')
    for (const body of instructions('yarn-classic')) {
      expect(body).toMatch(/\bregistry\b/)
      expect(body).not.toMatch(/npmRegistryServer|unsafeHttpWhitelist/)
    }
    for (const body of instructions('yarn')) {
      expect(body).toContain('npmRegistryServer')
      expect(body).not.toMatch(/\bregistry\b/)
    }
    expect(manager('node', 'yarn').paths.every(path => path.path.endsWith('.yarnrc.yml'))).toBe(true)
  })

  it('names the registry variable each pnpm generation reads', () => {
    // pnpm 11 and later ignore NPM_CONFIG_REGISTRY and read PNPM_CONFIG_REGISTRY.
    const methods = Object.fromEntries((manager('node', 'pnpm').methods ?? [])
      .map(method => [method.label, renderManagerTemplate(method.body, 'http://10.0.0.2:23333')]))
    expect(methods['quickstart.method.envvarPnpm11']).toBe('PNPM_CONFIG_REGISTRY=http://10.0.0.2:23333/npm/ pnpm install lodash')
    expect(methods['quickstart.method.envvarPnpm10']).toBe('NPM_CONFIG_REGISTRY=http://10.0.0.2:23333/npm/ pnpm install lodash')
    expect(methods['quickstart.method.envvar']).toBeUndefined()
  })

  it('opts Gradle in to a plain HTTP repository and never to an HTTPS one', () => {
    // Gradle 7 and later refuse an http repository without the explicit opt-in.
    const script = manager('java', 'gradle').persistent.body
    expect(renderManagerTemplate(script, 'http://10.0.0.2:23333')).toBe([
      'allprojects {',
      '  repositories {',
      '    maven {',
      '      url "http://10.0.0.2:23333/maven/"',
      '      allowInsecureProtocol = true',
      '    }',
      '  }',
      '}',
    ].join('\n'))
    expect(renderManagerTemplate(script, 'https://depsilo.example.com')).toBe([
      'allprojects {',
      '  repositories {',
      '    maven {',
      '      url "https://depsilo.example.com/maven/"',
      '    }',
      '  }',
      '}',
    ].join('\n'))
  })

  it('opts NuGet sources in to a plain HTTP address and never to an HTTPS one', () => {
    // .NET SDK 9 and later fail an http source with NU1302 without the opt-in.
    const config = manager('dotnet', 'nuget').persistent.body
    expect(renderManagerTemplate(config, 'http://10.0.0.2:23333'))
      .toContain('<add key="depsilo" value="http://10.0.0.2:23333/nuget/v3/index.json" allowInsecureConnections="true" />')
    expect(renderManagerTemplate(config, 'https://depsilo.example.com'))
      .toContain('<add key="depsilo" value="https://depsilo.example.com/nuget/v3/index.json" />')

    const cli = manager('dotnet', 'cli')
    const addSource = [cli.persistent.body, cli.configure?.body, manager('dotnet', 'nuget').methods?.[0].body]
    for (const command of addSource) {
      expect(renderManagerTemplate(command ?? '', 'http://10.0.0.2:23333'))
        .toBe('dotnet nuget add source http://10.0.0.2:23333/nuget/v3/index.json -n depsilo --allow-insecure-connections')
      expect(renderManagerTemplate(command ?? '', 'https://depsilo.example.com'))
        .toBe('dotnet nuget add source https://depsilo.example.com/nuget/v3/index.json -n depsilo')
    }
  })

  it('gives the add-source command for SDKs older than the plain-HTTP flag, only for an http address', () => {
    // `dotnet nuget add source --allow-insecure-connections` exists since
    // .NET SDK 9; SDK 8 and earlier reject the option and only warn about http.
    for (const id of ['cli', 'nuget']) {
      expect(renderManagerNotes(manager('dotnet', id).notes, 'http://10.0.0.2:23333'), id).toEqual([{
        text: 'quickstart.note.dotnetInsecureFlag', plainHttpOnly: true, lang: 'sh',
        body: 'dotnet nuget add source http://10.0.0.2:23333/nuget/v3/index.json -n depsilo',
      }])
      // Over https no generated command carries the flag, so there is nothing to explain.
      expect(renderManagerNotes(manager('dotnet', id).notes, 'https://depsilo.example.com'), id).toEqual([])
    }
    // Other notes do not depend on the scheme.
    expect(renderManagerNotes(manager('go', 'goenv').notes, 'https://depsilo.example.com')).toHaveLength(1)
    expect(renderManagerNotes(undefined, 'http://10.0.0.2:23333')).toEqual([])
  })

  it('keeps only the registry line in the pnpm .npmrc', () => {
    // pnpm 11 and later read only registry and auth settings from .npmrc, so
    // any other line there, such as the former store-dir, would be ignored.
    expect(renderManagerTemplate(manager('node', 'pnpm').persistent.body, 'http://10.0.0.2:23333'))
      .toBe('registry=http://10.0.0.2:23333/npm/')
  })

  it('runs Python as python3 in POSIX snippets and tutorials', () => {
    // Homebrew Python and Debian or Ubuntu without python-is-python3 have no
    // `python`, so a pasted `python -m ...` fails there.
    const lines = LANGUAGES.flatMap(language => language.managers.flatMap(manager => [
      ...[manager.quick, manager.configure, ...(manager.methods ?? []), manager.persistent, manager.test, manager.verify]
        .filter(snippet => snippet?.lang === 'sh').map(snippet => snippet!.body),
      ...manager.tutorial,
    ]))
    expect(lines.filter(line => /\bpython -m\b/.test(line))).toEqual([])
    expect(manager('python', 'venv').quick.body).toMatch(/^python3 -m venv \.venv /)
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
        .not.toMatch(/trusted-host|PIP_TRUSTED_HOST|verify_ssl\s*=\s*false|unsafeHttpWhitelist|insecure\s*=\s*true|secure-http\s+false|"secure-http"\s*:\s*false|insecure-registries|allowInsecure|allow-insecure/)
    }
  })

  it('trusts a plain-HTTP host in every pip command, which pip otherwise ignores', () => {
    // pip skips an http index on any host but localhost unless the host is
    // trusted, so a command copied on a second machine must carry it.
    for (const managerId of ['pip', 'venv']) {
      const entry = manager('python', managerId)
      const commands = [entry.quick.body, entry.verify.body, ...(entry.methods?.map(method => method.body) ?? [])]
        .filter(body => body.includes('pip install'))
      expect(commands.length, managerId).toBeGreaterThan(0)
      for (const command of commands) {
        const rendered = renderManagerTemplate(command, 'http://192.168.1.10:23333')
        expect(rendered, `${managerId}: ${command}`).toMatch(/--trusted-host 192\.168\.1\.10:23333|PIP_TRUSTED_HOST=192\.168\.1\.10:23333/)
        expect(renderManagerTemplate(command, 'https://depsilo.example.com'), `${managerId}: ${command}`)
          .toMatch(/pip install(?:(?!trusted-host|PIP_TRUSTED_HOST).)*$/s)
      }
    }
  })
})
