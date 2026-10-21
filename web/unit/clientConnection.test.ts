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
    expect(connection?.verifyConfig?.body).toBe('python -m pip config debug')
  })

  it('uses a one-command source without exposing a persistent configuration step', () => {
    for (const shell of ['posix', 'powershell'] as const) {
      for (const origin of ['https://depsilo.example', 'http://10.0.0.2:23333']) {
        const connection = getClientConnection('python', 'pip', origin, { pipScope: 'temporary', shell })
        expect(connection?.configuration).toBeUndefined()
        expect(connection?.scope.id).toBe('temporary')
        expect(connection?.verifyConfig?.body).toBe('python -m pip config debug')
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
      expect(body).toContain(shell === 'posix' ? '&&\npython -m pip' : 'if ($LASTEXITCODE -eq 0)')
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
      expect(pip?.command?.body).toContain('pip download')
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
  for (const name of ['python', 'npm']) writeFileSync(join(bin, name), stub, { mode: 0o755 })
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
