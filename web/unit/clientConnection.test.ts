import { describe, expect, it } from 'vitest'
import { spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, realpathSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import {
  CLIENT_CONNECTION_LANGUAGES,
  getClientConnection,
  getClientSelection,
} from '../src/lib/clientConnection'
import { LANGUAGES } from '../src/lib/ecosystemData'
import en from '../src/i18n/en'
import zh from '../src/i18n/zh'

describe('client connection selection', () => {
  it('uses catalog managers while treating venv as a pip scope', () => {
    expect(CLIENT_CONNECTION_LANGUAGES.flatMap(language => language.managers).some(manager => manager.id === 'venv')).toBe(false)
    expect(LANGUAGES.find(language => language.id === 'python')?.managers.some(manager => manager.id === 'venv')).toBe(true)
    expect(getClientSelection('python', 'venv')).toBeNull()
    expect(getClientSelection('python', 'npm')).toBeNull()
    expect(getClientSelection('missing', 'pip')).toBeNull()
    expect(getClientConnection('missing', 'pip', 'https://depsilo.example')).toBeNull()
  })

  it('maps the selected client protocol, including Conda within Python', () => {
    expect(getClientSelection('python', 'conda')?.ecosystem).toBe('conda')
    expect(getClientSelection('python', 'pip')?.ecosystem).toBe('pypi')
    expect(getClientSelection('node', 'npm')?.ecosystem).toBe('npm')
    expect(getClientSelection('java', 'maven')?.ecosystem).toBe('maven')
    for (const language of CLIENT_CONNECTION_LANGUAGES) {
      for (const manager of language.managers) {
        expect(getClientSelection(language.id, manager.id)?.managerId).toBe(manager.id)
      }
    }
  })
})

describe('pip configuration scope', () => {
  it('explicitly writes user scope and exposes configuration precedence diagnostics', () => {
    const connection = getClientConnection('python', 'pip', 'http://10.0.0.2:23333')
    expect(connection?.scope.id).toBe('user')
    expect(connection?.configuration?.command?.body).toContain('config --user set global.index-url')
    expect(connection?.configuration?.command?.body).toContain("global.trusted-host '10.0.0.2:23333'")
    expect(connection?.verifyConfig?.body).toBe('python3 -m pip config debug')
  })

  it('uses a one-command source without exposing a persistent configuration step', () => {
    for (const shell of ['posix', 'powershell'] as const) {
      for (const origin of ['https://depsilo.example', 'http://10.0.0.2:23333']) {
        const connection = getClientConnection('python', 'pip', origin, { pipScope: 'temporary', shell })
        expect(connection?.configuration).toBeUndefined()
        expect(connection?.scope.id).toBe('temporary')
        expect(connection?.verifyConfig?.body).toBe(shell === 'posix' ? 'python3 -m pip config debug' : 'python -m pip config debug')
        expect(connection?.test).toMatchObject({ purpose: 'artifact', targetPackage: 'six', targetVersion: '1.17.0' })
        const command = connection?.test.command?.body ?? ''
        expect(command).toContain(`--index-url '${origin}/pypi/simple/'`)
        if (origin.startsWith('http:')) expect(command).toContain("--trusted-host '10.0.0.2:23333'")
        else expect(command).not.toContain('--trusted-host')
        expect(command).not.toMatch(/pip config|\binstall\b|PIP_INDEX_URL|PIP_CONFIG_FILE/)
      }
    }
  })

  it('guards virtual-environment configuration before each shell can write site scope', () => {
    for (const shell of ['posix', 'powershell'] as const) {
      const connection = getClientConnection('python', 'pip', 'https://depsilo.example', { pipScope: 'venv', shell })
      const body = connection?.configuration?.command?.body ?? ''
      expect(body).toContain('sys.prefix != sys.base_prefix')
      expect(body.indexOf('sys.prefix')).toBeLessThan(body.indexOf('config --site'))
      expect(body).toContain(shell === 'posix' ? '&&\npython3 -m pip' : 'if ($LASTEXITCODE -eq 0)')
      expect(body.startsWith(shell === 'posix' ? 'python3 -c ' : 'python -c ')).toBe(true)
      expect(body).not.toContain('--user')
      expect(body).not.toContain('trusted-host')
      expect(connection?.configuration?.body).not.toContain('trusted-host')
      expect(connection?.scope.id).toBe('venv')
    }
  })
})

describe('connection checks and artifact tests', () => {
  it('downloads fixed artifacts using configured sources and fresh isolated locations', () => {
    for (const shell of ['posix', 'powershell'] as const) {
      const pip = getClientConnection('python', 'pip', 'https://depsilo.example', { shell })?.test
      const npm = getClientConnection('node', 'npm', 'https://depsilo.example', { shell })?.test
      expect(pip).toMatchObject({ purpose: 'artifact', targetPackage: 'six', targetVersion: '1.17.0' })
      expect(npm).toMatchObject({ purpose: 'artifact', targetPackage: 'is-number', targetVersion: '7.0.0' })
      // POSIX systems may have no `python`; Windows may have no `python3`.
      expect(pip?.command?.body).toContain(shell === 'posix' ? 'python3 -m pip download' : 'python -m pip download')
      expect(pip?.command?.body).toContain('--no-cache-dir')
      expect(pip?.command?.body).toContain('--only-binary=:all:')
      expect(pip?.command?.body).toContain('--no-deps')
      expect(npm?.command?.body).toContain('npm pack is-number@7.0.0')
      expect(npm?.command?.body).toContain('--ignore-scripts')
      expect(npm?.command?.body).toContain('--cache ')
      expect(npm?.command?.body).toContain('--pack-destination ')
      for (const command of [pip?.command?.body ?? '', npm?.command?.body ?? '']) {
        expect(command).toContain(shell === 'posix' ? 'mktemp -d' : '[guid]::NewGuid()')
        expect(command).not.toMatch(/--(?:index-url|registry)|PIP_INDEX_URL|NPM_CONFIG_REGISTRY|depsilo\.example/)
        expect(command).not.toMatch(/\binstall\b|--dry-run|cache (?:clean|purge|rm)/)
      }
    }
  })

  it('keeps metadata checks distinct from downloads and avoids destructive verification fallbacks', () => {
    expect(getClientConnection('node', 'pnpm', 'https://depsilo.example')?.test).toMatchObject({ purpose: 'metadata', targetPackage: 'is-number' })
    expect(getClientConnection('python', 'uv', 'https://depsilo.example')?.test).toMatchObject({ purpose: 'metadata', targetPackage: 'six' })
    expect(getClientConnection('go', 'goenv', 'https://depsilo.example')?.test.command?.body).toBe('go list -m -versions rsc.io/quote')
    expect(getClientConnection('go', 'goenv', 'https://depsilo.example')?.test.targetPackage).toBe('rsc.io/quote')
    expect(getClientConnection('java', 'maven', 'https://depsilo.example')?.test).toMatchObject({
      purpose: 'artifact', targetPackage: 'org.slf4j:slf4j-api', targetVersion: '2.0.16',
    })
    expect(getClientConnection('python', 'pdm', 'https://depsilo.example')?.configuration?.command).toBeUndefined()
    // cargo info may create a Cargo.lock; bun's legacy verify clears cache.
    for (const [language, manager] of [['rust', 'cargo'], ['node', 'bun'], ['debian', 'apt']]) {
      const connection = getClientConnection(language, manager, 'https://depsilo.example')
      expect(connection?.test.purpose).toBe('manual')
      expect(connection?.test.command).toBeUndefined()
      expect(connection?.verifyConfig).toBeUndefined()
    }
  })

  it('tells every Node.js client what to do with a lockfile recorded against another registry', () => {
    const notes = (manager: string) => getClientConnection('node', manager, 'http://10.0.0.2:23333')?.notes ?? []
    // npm can keep its lockfile: rewrite it without resolved URLs, then install.
    expect(notes('npm')).toEqual([
      { text: 'quickstart.note.npmLockfile', lang: 'sh',
        body: 'npm install --package-lock-only --omit-lockfile-registry-resolved\nnpm ci' },
      { text: 'quickstart.note.npmLockfileSetting', file: './.npmrc', lang: 'ini',
        body: 'omit-lockfile-registry-resolved=true' },
    ])
    // The others have no such flag; their lockfile then records this instance.
    for (const manager of ['pnpm', 'yarn']) {
      expect(notes(manager).map(note => note.text).slice(0, 2), manager)
        .toEqual(['quickstart.note.lockfileRegenerate', 'quickstart.note.lockfileBound'])
    }
    // Bun, like npm, fails only on registry.npmjs.org URLs and bypasses Depsilo on others.
    expect(notes('bun').map(note => note.text))
      .toEqual(['quickstart.note.bunLockfile', 'quickstart.note.lockfileBound'])
    // pnpm 11 and later refuse the regenerated lockfile without this setting.
    expect(notes('pnpm')[2]).toEqual({ text: 'quickstart.note.pnpmLockfileMismatch',
      file: 'pnpm-workspace.yaml', lang: 'yaml', body: 'trustLockfile: true' })
    // Yarn 1 does not fail: it downloads from the recorded URLs instead.
    expect(notes('yarn-classic').map(note => note.text))
      .toEqual(['quickstart.note.yarnClassicLockfile', 'quickstart.note.lockfileBound'])
    expect(getClientConnection('python', 'pip', 'http://10.0.0.2:23333')?.notes).toEqual([])
  })

  it('tells uv, Poetry and Pipenv users that a lockfile written with this index records this instance', () => {
    const notes = (manager: string) => getClientConnection('python', manager, 'http://10.0.0.2:23333')?.notes
    // uv.lock names the index as the registry of every package, also when the
    // index comes from uv.toml or UV_INDEX_URL, so its sentence names no file.
    expect(notes('uv')).toEqual([{ text: 'quickstart.note.uvLockfileBound' }])
    // poetry.lock writes the url of the configured source under every
    // package it supplied; Pipfile.lock lists the Pipfile's sources.
    for (const manager of ['poetry', 'pipenv']) {
      expect(notes(manager), manager).toEqual([{ text: 'quickstart.note.pythonLockfileBound' }])
    }
    // pip writes no lockfile, and pdm.lock stores file names only.
    for (const manager of ['pip', 'pdm']) {
      expect(notes(manager), manager).toEqual([])
    }
    // The npm sentence names the npm upstream, so Python has its own.
    expect(en.translation.quickstart.note.uvLockfileBound)
      .toBe("A uv.lock generated while Depsilo is the index records this instance's address, however the index is set (pyproject.toml, uv.toml or an environment variable), so it depends on this address.")
    expect(zh.translation.quickstart.note.uvLockfileBound)
      .toBe('只要索引指向 Depsilo，不论写在 pyproject.toml、uv.toml 还是环境变量里，生成的 uv.lock 记录的都是本实例的地址，因此依赖这个服务地址。')
    expect(en.translation.quickstart.note.pythonLockfileBound)
      .toBe("A lockfile generated with Depsilo as the index in the project file records this instance's address, so it depends on this address.")
    expect(zh.translation.quickstart.note.pythonLockfileBound)
      .toBe('项目文件把索引指向 Depsilo 后生成的 lockfile 记录的是本实例的地址，因此依赖这个服务地址。')
  })

  it('says where Go checksum lookups go and names the reachable alternative', () => {
    for (const manager of ['goenv', 'shell']) {
      expect(getClientConnection('go', manager, 'http://10.0.0.2:23333')?.notes, manager).toEqual([
        { text: 'quickstart.note.goSumdb', lang: 'sh', body: 'go env -w GOSUMDB=sum.golang.google.cn' },
      ])
    }
    // Depsilo relays the checksum database to a Go upstream that mirrors it,
    // so the note must not send every reader to sum.golang.org.
    for (const text of [en.translation.quickstart.note.goSumdb, zh.translation.quickstart.note.goSumdb]) {
      for (const name of ['goproxy.cn', 'proxy.golang.org', 'sum.golang.org', 'GOSUMDB=sum.golang.google.cn']) {
        expect(text).toContain(name)
      }
    }
    expect(en.translation.quickstart.note.goSumdb).not.toContain('not through Depsilo')
    expect(zh.translation.quickstart.note.goSumdb).not.toContain('不经过 Depsilo')
  })

  it('lets the reviewer accept a lockfile change the user approved', () => {
    // The integration guide allows rewriting a package-lock.json recorded
    // against another registry once the user approves it in the plan.
    const { ai } = en.translation.quickstart
    for (const text of [ai.reviewDescription, ai.reviewScopedDescription]) {
      expect(text).toContain('no lockfile changed without your approval')
      expect(text).not.toContain('lockfiles, checksums, and signature checks are untouched')
    }
    const zhAi = zh.translation.quickstart.ai
    for (const text of [zhAi.reviewDescription, zhAi.reviewScopedDescription]) {
      expect(text).toContain('lockfile 没有未经你同意的改动')
    }
    // Quick Start says "lockfile" in Chinese too, as its notes do.
    expect(JSON.stringify(zh.translation.quickstart)).not.toContain('锁文件')
    // The setting exists so that later changes record no resolved URLs.
    expect(zh.translation.quickstart.note.npmLockfileSetting).toContain('这样以后的改动就不会把 resolved 地址写进 lockfile')
  })

  it('configures and inspects the setting each Yarn generation installs from', () => {
    const berry = getClientConnection('node', 'yarn', 'http://127.0.0.1:24526')
    expect(berry?.configuration?.file).toBe('./.yarnrc.yml')
    expect(berry?.configuration?.command).toBeUndefined()
    expect(berry?.configuration?.body).toContain('unsafeHttpWhitelist:\n  - "127.0.0.1"')
    expect(berry?.verifyConfig?.body).toBe('yarn config get npmRegistryServer')
    // Yarn 1 stores and echoes npmRegistryServer without installing from it.
    const classic = getClientConnection('node', 'yarn-classic', 'http://127.0.0.1:24526')
    expect(classic?.configuration?.command?.body).toBe('yarn config set registry http://127.0.0.1:24526/npm/')
    expect(classic?.verifyConfig?.body).toBe('yarn config get registry')
  })

  it('names the scope in every Bundler command and beside it', () => {
    // Without --local or --global, the file `bundle config` writes depends on
    // the Bundler version and on the directory it runs in.
    const local = 'bundle config set --local mirror.https://rubygems.org http://10.0.0.2:23333/rubygems/'
    const bundler = LANGUAGES.find(language => language.id === 'ruby')?.managers.find(manager => manager.id === 'bundler')
    const methods = Object.fromEntries((bundler?.methods ?? [])
      .map(method => [method.label, method.body.replace('{URL}', 'http://10.0.0.2:23333')]))
    expect(methods).toEqual({
      'quickstart.method.cmdlineProject': local,
      'quickstart.method.cmdlineUser': 'bundle config set --global mirror.https://rubygems.org http://10.0.0.2:23333/rubygems/',
    })
    expect(bundler?.quick.body.replace('{URL}', 'http://10.0.0.2:23333')).toBe(local)
    // The first path is the file shown with the instructions: the project's.
    expect(bundler?.persistent.file).toBe('.bundle/config')
    expect(bundler?.paths).toEqual([
      { os: 'Per-project', path: '.bundle/config' },
      { os: 'Per-user', path: '~/.bundle/config' },
    ])

    const connection = getClientConnection('ruby', 'bundler', 'http://10.0.0.2:23333')
    expect(connection?.configuration?.command?.body).toBe(local)
    expect(connection?.configuration?.file).toBe('.bundle/config')
    expect(connection?.scope).toEqual({
      id: 'catalog', labelKey: 'onboarding.scopeProject', hintKey: 'onboarding.scopeBundlerProjectHint',
    })
  })

  it('gives Bundler 1.x the forms it understands beside the set and get commands', () => {
    // Bundler 1.x has no `set` or `get` subcommand: it exits 0, stores the
    // arguments under a BUNDLE_SET key and leaves the mirror unset.
    const http = 'http://10.0.0.2:23333'
    const connection = getClientConnection('ruby', 'bundler', http)
    expect(connection?.configuration?.command?.body).toContain('bundle config set ')
    expect(connection?.verifyConfig?.body).toContain('bundle config get ')
    expect(connection?.notes).toEqual([{
      text: 'quickstart.note.bundlerLegacy', lang: 'sh',
      body: `bundle config --local mirror.https://rubygems.org ${http}/rubygems/\nbundle config mirror.https://rubygems.org`,
    }])
    for (const text of [en.translation.quickstart.note.bundlerLegacy, zh.translation.quickstart.note.bundlerLegacy]) {
      for (const name of ['Bundler 2.1', 'Bundler 1.x', '--global']) expect(text).toContain(name)
    }
  })

  it('adds the Gradle and NuGet plain-HTTP opt-ins only for an http address', () => {
    const http = 'http://10.0.0.2:23333'
    const https = 'https://depsilo.example'
    expect(getClientConnection('java', 'gradle', http)?.configuration?.body).toContain('allowInsecureProtocol = true')
    expect(getClientConnection('java', 'gradle', https)?.configuration?.body).not.toContain('allowInsecure')
    expect(getClientConnection('dotnet', 'nuget', http)?.configuration?.body).toContain('allowInsecureConnections="true"')
    expect(getClientConnection('dotnet', 'nuget', https)?.configuration?.body).not.toContain('allowInsecure')
    expect(getClientConnection('dotnet', 'cli', http)?.configuration?.command?.body)
      .toBe(`dotnet nuget add source ${http}/nuget/v3/index.json -n depsilo --allow-insecure-connections`)
    expect(getClientConnection('dotnet', 'cli', https)?.configuration?.command?.body)
      .toBe(`dotnet nuget add source ${https}/nuget/v3/index.json -n depsilo`)
  })

  it('says which .NET SDKs accept the plain-HTTP flag wherever the flag is generated', () => {
    const http = 'http://10.0.0.2:23333'
    for (const manager of ['cli', 'nuget']) {
      expect(getClientConnection('dotnet', manager, http)?.notes, manager).toEqual([{
        text: 'quickstart.note.dotnetInsecureFlag', plainHttpOnly: true, lang: 'sh',
        body: `dotnet nuget add source ${http}/nuget/v3/index.json -n depsilo`,
      }])
      expect(getClientConnection('dotnet', manager, 'https://depsilo.example')?.notes, manager).toEqual([])
    }
  })

  it('checks Docker mirror configuration without overriding the registry or widening the image target', () => {
    const connection = getClientConnection('container', 'docker', 'https://depsilo.example')
    expect(connection?.test).toMatchObject({
      purpose: 'artifact',
      targetPackage: 'library/alpine',
      targetVersion: '3.19',
      command: { lang: 'sh', body: 'docker pull alpine:3.19' },
      hintKey: 'onboarding.testDockerHint',
    })
  })

  it('renders the actual origin and exposes only supported shell variants', () => {
    const npm = getClientConnection('node', 'npm', 'http://0.0.0.0:23333', { shell: 'powershell' })
    expect(npm?.configuration?.command?.body).toContain("registry 'http://localhost:23333/npm/'")
    expect(npm?.shellOptions).toEqual(['posix', 'powershell'])
    expect(npm?.shell).toBe('powershell')
    const conda = getClientConnection('python', 'conda', 'https://depsilo.example', { shell: 'powershell' })
    expect(conda?.shellOptions).toEqual(['posix'])
    expect(conda?.shell).toBe('posix')
    expect(conda?.configuration?.body).toContain('https://depsilo.example/conda/pkgs/main')
  })
})

function artifactTestCommand(manager: 'pip' | 'npm', shell: 'posix' | 'powershell' = 'posix') {
  const command = getClientConnection(manager === 'pip' ? 'python' : 'node', manager, 'https://depsilo.example', { shell })?.test.command
  if (!command) throw new Error(`Missing ${manager} ${shell} download command`)
  return command.body
}

/** Execute the displayed command without a network or an installed package manager. */
function withDownloadFixture(run: (fixture: {
  root: string
  project: string
  temporary: string
  existing: string
  bin: string
  configuration: string
  env: NodeJS.ProcessEnv
}) => void) {
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'depsilo command test ')))
  const project = join(root, 'existing project')
  const temporary = join(root, 'temporary downloads')
  const existing = join(root, 'existing directory')
  const bin = join(root, 'bin')
  const configuration = join(root, 'pip.conf')
  for (const directory of [project, temporary, existing, bin]) mkdirSync(directory)
  writeFileSync(join(project, 'keep.txt'), 'project contents')
  writeFileSync(join(existing, 'keep.txt'), 'unrelated contents')
  writeFileSync(configuration, '[global]\nindex-url = https://existing.example/simple/\n')
  const stub = `#!/bin/sh
printf '%s\\n' invocation >> "$DEPSILO_STUB_RECORD/invocations"
printf '%s\\n' "$@" > "$DEPSILO_STUB_RECORD/arguments"
printf '%s' "$PWD" > "$DEPSILO_STUB_RECORD/working-directory"
while [ "$#" -gt 0 ]; do
  case "$1" in
    -d|--dest|--pack-destination) shift; destination="$1" ;;
    --cache) shift; cache="$1" ;;
  esac
  shift
done
[ -n "$destination" ] || exit 98
printf '%s' "$destination" > "$DEPSILO_STUB_RECORD/destination"
printf 'downloaded artifact' > "$destination/artifact.whl"
if [ -n "$cache" ]; then
  mkdir -p "$cache"
  printf 'test cache entry' > "$cache/entry"
fi
printf 'package manager output\\n'
printf 'package manager diagnostic\\n' >&2
if [ -n "$DEPSILO_STUB_SIGNAL" ]; then kill -"$DEPSILO_STUB_SIGNAL" "$PPID"; fi
exit "$DEPSILO_STUB_STATUS"
`
  for (const name of ['python', 'python3', 'npm']) writeFileSync(join(bin, name), stub, { mode: 0o755 })
  // macOS's no-template mktemp can ignore TMPDIR; still create a real unique
  // directory, with a template that keeps every test's files inside its fixture.
  writeFileSync(join(bin, 'mktemp'), '#!/bin/sh\nexec /usr/bin/mktemp "$@" "$DEPSILO_STUB_TEMPORARY/download.XXXXXXXX"\n', { mode: 0o755 })
  try {
    run({
      root, project, temporary, existing, bin, configuration,
      env: {
        ...process.env,
        PATH: `${bin}:${process.env.PATH}`,
        TMPDIR: `${temporary}/`,
        PIP_CONFIG_FILE: configuration,
        DEPSILO_STUB_RECORD: root,
        DEPSILO_STUB_TEMPORARY: temporary,
        DEPSILO_STUB_STATUS: '0',
        DEPSILO_EXISTING_DIRECTORY: existing,
      },
    })
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
}

describe('displayed artifact download command lifecycle', () => {
  it.each([
    ['pip', 0], ['pip', 29], ['npm', 0], ['npm', 29],
  ] as const)('%s cleans its download and cache on exit %i without changing the caller', (manager, status) => {
    withDownloadFixture(fixture => {
      const result = spawnSync('/bin/sh', ['-c', `
trap 'printf "caller exit trap" > "$DEPSILO_STUB_RECORD/caller-trap"' 0
depsilo_tmp="$DEPSILO_EXISTING_DIRECTORY"
${artifactTestCommand(manager)}
download_status=$?
printf '%s' "$PWD" > "$DEPSILO_STUB_RECORD/caller-directory"
printf '%s' "$depsilo_tmp" > "$DEPSILO_STUB_RECORD/caller-variable"
exit "$download_status"
`], {
        cwd: fixture.project,
        env: { ...fixture.env, DEPSILO_STUB_STATUS: String(status) },
        encoding: 'utf8',
        timeout: 5000,
      })
      expect(result.error).toBeUndefined()
      expect(result.status).toBe(status)
      expect(result.stdout).toContain('package manager output')
      expect(result.stderr).toContain('package manager diagnostic')
      const destination = readFileSync(join(fixture.root, 'destination'), 'utf8')
      expect(destination.startsWith(`${fixture.temporary}/`)).toBe(true)
      expect(existsSync(destination)).toBe(false)
      expect(readdirSync(fixture.temporary)).toEqual([])
      expect(readFileSync(join(fixture.root, 'caller-directory'), 'utf8')).toBe(fixture.project)
      expect(readFileSync(join(fixture.root, 'caller-variable'), 'utf8')).toBe(fixture.existing)
      expect(readFileSync(join(fixture.root, 'caller-trap'), 'utf8')).toBe('caller exit trap')
      expect(readFileSync(join(fixture.existing, 'keep.txt'), 'utf8')).toBe('unrelated contents')
      expect(readdirSync(fixture.project)).toEqual(['keep.txt'])
    })
  })

  it.each([
    ['pip', 'HUP'], ['pip', 'INT'], ['pip', 'TERM'],
    ['npm', 'HUP'], ['npm', 'INT'], ['npm', 'TERM'],
  ] as const)('%s cleans a %s-interrupted download and does not report success', (manager, signal) => {
    withDownloadFixture(fixture => {
      const result = spawnSync('/bin/sh', ['-c', artifactTestCommand(manager)], {
        cwd: fixture.project,
        // The client returns zero after signaling its parent, so cancellation
        // must not inherit that status and falsely report a successful download.
        env: { ...fixture.env, DEPSILO_STUB_SIGNAL: signal },
        encoding: 'utf8', timeout: 5000,
      })
      expect(result.error).toBeUndefined()
      expect(result.status).toBe(130)
      expect(result.stderr).toContain('package manager diagnostic')
      expect(existsSync(readFileSync(join(fixture.root, 'destination'), 'utf8'))).toBe(false)
      expect(readdirSync(fixture.temporary)).toEqual([])
      expect(readFileSync(join(fixture.existing, 'keep.txt'), 'utf8')).toBe('unrelated contents')
    })
  })

  it('does not download or delete existing files when temporary directory creation fails', () => {
    withDownloadFixture(fixture => {
      // Even misleading stdout from a failed creator must not become a cleanup target.
      writeFileSync(join(fixture.bin, 'mktemp'), `#!/bin/sh
printf '%s\\n' "$DEPSILO_EXISTING_DIRECTORY"
printf 'cannot create temporary directory\\n' >&2
exit 37
`, { mode: 0o755 })
      const result = spawnSync('/bin/sh', ['-c', artifactTestCommand('pip')], {
        cwd: fixture.project, env: fixture.env, encoding: 'utf8', timeout: 5000,
      })
      expect(result.error).toBeUndefined()
      expect(result.status).toBe(37)
      expect(result.stderr).toContain('cannot create temporary directory')
      expect(existsSync(join(fixture.root, 'arguments'))).toBe(false)
      expect(readFileSync(join(fixture.existing, 'keep.txt'), 'utf8')).toBe('unrelated contents')
      expect(readdirSync(fixture.temporary)).toEqual([])
      expect(readdirSync(fixture.project)).toEqual(['keep.txt'])
    })
  })

  const hasPowerShell = spawnSync('pwsh', ['-NoProfile', '-Command', '$PSVersionTable.PSVersion.ToString()'], { timeout: 5000 }).status === 0
  for (const shell of ['posix', 'powershell'] as const) {
    it.skipIf(shell === 'powershell' && !hasPowerShell).each([
      ["https://a'b.example", 0],
      ["http://a'b.example:23333", 29],
    ] as const)(`${shell} passes a quoted temporary source %s and cleans up on exit %i`, (origin, status) => {
      withDownloadFixture(fixture => {
        const connection = getClientConnection('python', 'pip', origin, { pipScope: 'temporary', shell })
        const command = connection?.test.command?.body
        if (!command) throw new Error('Missing temporary pip command')
        const before = readFileSync(fixture.configuration, 'utf8')
        const result = spawnSync(shell === 'powershell' ? 'pwsh' : '/bin/sh',
          shell === 'powershell'
            ? ['-NoProfile', '-Command', `${command}\nexit $LASTEXITCODE`]
            : ['-c', command], {
            cwd: fixture.project,
            env: { ...fixture.env, PIP_INDEX_URL: 'https://existing.example/simple/', DEPSILO_STUB_STATUS: String(status) },
            encoding: 'utf8', timeout: 10000,
          })
        expect(result.error).toBeUndefined()
        expect(result.status).toBe(status)
        const destination = readFileSync(join(fixture.root, 'destination'), 'utf8')
        const args = readFileSync(join(fixture.root, 'arguments'), 'utf8').trimEnd().split('\n')
        const sourceArgs = [
          '--index-url', `${origin}/pypi/simple/`,
          ...(origin.startsWith('http:') ? ['--trusted-host', new URL(origin).host] : []),
        ]
        expect(args).toEqual(shell === 'posix' ? [
          '-m', 'pip', 'download', '--no-deps', '--no-cache-dir', '--only-binary=:all:',
          '--disable-pip-version-check', '-d', destination, ...sourceArgs, 'six==1.17.0',
        ] : [
          '-m', 'pip', 'download', '--disable-pip-version-check', '--no-deps', '--no-cache-dir', '--only-binary=:all:',
          ...sourceArgs, '--dest', destination, 'six==1.17.0',
        ])
        expect(readFileSync(join(fixture.root, 'invocations'), 'utf8')).toBe('invocation\n')
        expect(readFileSync(fixture.configuration, 'utf8')).toBe(before)
        expect(existsSync(destination)).toBe(false)
        expect(readdirSync(fixture.temporary)).toEqual([])
        expect(readdirSync(fixture.project)).toEqual(['keep.txt'])
        expect(readFileSync(join(fixture.existing, 'keep.txt'), 'utf8')).toBe('unrelated contents')
      })
    })
  }

  it.skipIf(!hasPowerShell).each([['pip', 0], ['npm', 29]] as const)(
    'PowerShell %s cleans files and preserves native exit %i without closing the caller', (manager, status) => {
      withDownloadFixture(fixture => {
        const result = spawnSync('pwsh', ['-NoProfile', '-Command', `
${artifactTestCommand(manager, 'powershell')}
$downloadStatus = $LASTEXITCODE
[Console]::Out.WriteLine('caller continues')
exit $downloadStatus
`], {
          cwd: fixture.project,
          env: { ...fixture.env, DEPSILO_STUB_STATUS: String(status) },
          encoding: 'utf8', timeout: 10000,
        })
        expect(result.error).toBeUndefined()
        expect(result.status).toBe(status)
        expect(result.stdout).toContain('caller continues')
        expect(result.stderr).toContain('package manager diagnostic')
        expect(existsSync(readFileSync(join(fixture.root, 'destination'), 'utf8'))).toBe(false)
        expect(readdirSync(fixture.temporary)).toEqual([])
        expect(readFileSync(join(fixture.existing, 'keep.txt'), 'utf8')).toBe('unrelated contents')
        expect(readdirSync(fixture.project)).toEqual(['keep.txt'])
      })
    },
  )
})
