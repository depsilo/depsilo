import { LANGUAGES, type Language, type ManagerConfig } from './ecosystemData'
import { getIsolatedArtifactCommand } from './artifactTestCommands'
import type { EcosystemType } from './ecosystemTypes'
import { renderManagerTemplate, resolveServiceOrigin } from './packageManagerConfig'

export type PipScope = 'temporary' | 'user' | 'venv'
export type ConnectionShell = 'posix' | 'powershell'

export interface ClientSelection {
  languageId: string
  managerId: string
  ecosystem: EcosystemType
}

export interface ConnectionCommand {
  lang: string
  body: string
}

export interface ClientConnection {
  selection: ClientSelection
  configuration?: ConnectionCommand & { file: string; command?: ConnectionCommand }
  scope: { id: PipScope | 'catalog'; labelKey: string; hintKey: string }
  shell: ConnectionShell
  shellOptions: readonly ConnectionShell[]
  verifyConfig?: ConnectionCommand
  test: {
    purpose: 'artifact' | 'metadata' | 'manual'
    command?: ConnectionCommand
    targetPackage?: string
    targetVersion?: string
    hintKey: string
  }
}

/** A virtual environment is a pip configuration scope, not a package client. */
export const CLIENT_CONNECTION_LANGUAGES: Language[] = LANGUAGES.map(language => ({
  ...language,
  managers: language.managers.filter(manager => manager.id !== 'venv'),
}))

export function getClientSelection(languageId: string, managerId: string): ClientSelection | null {
  const language = CLIENT_CONNECTION_LANGUAGES.find(candidate => candidate.id === languageId)
  if (!language?.managers.some(manager => manager.id === managerId)) return null
  return {
    languageId,
    managerId,
    ecosystem: languageId === 'python' && managerId === 'conda' ? 'conda' : language.iconAdapter,
  }
}

// Only config inspection belongs here. Catalog `verify` also includes installs,
// cache deletion and project operations; never promote those to a config check.
const CONFIG_INSPECTION: Record<string, string> = {
  pdm: 'pdm config pypi.url',
  conda: 'conda config --show channels\nconda config --show repodata_use_zst',
  npm: 'npm config get registry',
  pnpm: 'pnpm config get registry',
  yarn: 'yarn config get npmRegistryServer',
  goenv: 'go env GOPROXY',
  shell: 'go env GOPROXY',
  gem: 'gem sources',
  bundler: 'bundle config get mirror.https://rubygems.org',
  cli: 'dotnet nuget list source',
  nuget: 'dotnet nuget list source',
}

const SHELL_OPTIONS: readonly ConnectionShell[] = ['posix', 'powershell']
const POSIX_ONLY: readonly ConnectionShell[] = ['posix']

export function getClientConnection(
  languageId: string,
  managerId: string,
  origin: string,
  options: { pipScope?: PipScope; shell?: ConnectionShell } = {},
): ClientConnection | null {
  const selection = getClientSelection(languageId, managerId)
  const manager = CLIENT_CONNECTION_LANGUAGES
    .find(language => language.id === languageId)?.managers.find(candidate => candidate.id === managerId)
  if (!selection || !manager) return null
  const endpoint = resolveServiceOrigin(origin)
  const shell = managerId === 'pip' || managerId === 'npm' ? options.shell ?? 'posix' : 'posix'
  const command = (source: ConnectionCommand): ConnectionCommand => ({
    ...source,
    body: renderManagerTemplate(source.body, endpoint),
  })
  const configuration = {
    ...manager.persistent,
    body: renderManagerTemplate(manager.persistent.body, endpoint),
    // PDM's catalog command writes user settings, but this file is scoped to
    // the project. Present the file rather than mixing two scopes.
    command: manager.configure && managerId !== 'pdm' ? command(manager.configure) : undefined,
  }
  const connection: ClientConnection = {
    selection,
    configuration,
    scope: {
      id: 'catalog',
      labelKey: 'onboarding.scopeFile',
      hintKey: 'onboarding.scopeFileHint',
    },
    shell,
    shellOptions: POSIX_ONLY,
    verifyConfig: CONFIG_INSPECTION[managerId]
      ? { lang: 'sh', body: CONFIG_INSPECTION[managerId] }
      : undefined,
    test: connectionTest(manager, endpoint),
  }

  if (managerId === 'pip') {
    const scope = options.pipScope ?? 'user'
    connection.verifyConfig = { lang: 'sh', body: 'python -m pip config debug' }
    connection.shellOptions = SHELL_OPTIONS
    connection.test.hintKey = 'onboarding.testIsolatedArtifactHint'
    connection.test.command = getIsolatedArtifactCommand('pip', shell, {
      pipIndexOrigin: scope === 'temporary' ? endpoint : undefined,
    })
    if (scope === 'temporary') {
      // No separate configuration command: the download carries its own source.
      connection.configuration = undefined
      connection.scope = { id: scope, labelKey: 'onboarding.scopeTemporary', hintKey: 'onboarding.scopePipTemporaryHint' }
      return connection
    }
    const scopeFlag = scope === 'venv' ? '--site' : '--user'
    const statements = [`python -m pip config ${scopeFlag} set global.index-url '${endpoint}/pypi/simple/'`]
    if (new URL(endpoint).protocol === 'http:') {
      statements.push(`python -m pip config ${scopeFlag} set global.trusted-host '${new URL(endpoint).host}'`)
    }
    // `pip config` deliberately ignores --require-virtualenv. Without this
    // check --site could write to the base Python installation instead.
    const guard = 'python -c "import sys; sys.exit(0 if sys.prefix != sys.base_prefix else \'Activate a virtual environment first.\')"'
    const configure = scope === 'venv'
      ? shell === 'powershell'
        ? `${guard}\nif ($LASTEXITCODE -eq 0) {\n  ${statements.join('\n  ')}\n}`
        : [guard, ...statements].join(' &&\n')
      : statements.join('\n')
    connection.configuration = {
      file: scope === 'venv'
        ? shell === 'powershell' ? '$env:VIRTUAL_ENV\\pip.ini' : '$VIRTUAL_ENV/pip.conf'
        : 'pip.conf / pip.ini',
      lang: 'ini',
      body: renderManagerTemplate(manager.persistent.body, endpoint),
      command: { lang: shell === 'powershell' ? 'powershell' : 'sh', body: configure },
    }
    connection.scope = {
      id: scope,
      labelKey: scope === 'venv' ? 'onboarding.scopeVenv' : 'onboarding.scopeUser',
      hintKey: scope === 'venv' ? 'onboarding.scopePipVenvHint' : 'onboarding.scopePipUserHint',
    }
  } else if (managerId === 'npm') {
    connection.configuration = {
      ...configuration,
      command: {
        lang: shell === 'powershell' ? 'powershell' : 'sh',
        body: `npm config set --location=user registry '${endpoint}/npm/'`,
      },
    }
    connection.scope = { id: 'user', labelKey: 'onboarding.scopeUser', hintKey: 'onboarding.scopeNpmUserHint' }
    connection.shellOptions = SHELL_OPTIONS
    connection.test.hintKey = 'onboarding.testIsolatedArtifactHint'
    connection.test.command = getIsolatedArtifactCommand('npm', shell)
  } else if (managerId === 'docker') {
    // Manifest events have image/tag identity; shared blob keys do not.
    // Keep the strict target so an unrelated Docker pull cannot satisfy it.
    connection.test.hintKey = 'onboarding.testDockerHint'
  }
  return connection
}

function connectionTest(manager: ManagerConfig, endpoint: string): ClientConnection['test'] {
  if (!manager.test) return { purpose: 'manual', hintKey: 'onboarding.testManualHint' }
  return {
    purpose: manager.test.purpose,
    command: { lang: manager.test.lang, body: renderManagerTemplate(manager.test.body, endpoint) },
    targetPackage: manager.test.targetPackage,
    targetVersion: manager.test.targetVersion,
    hintKey: manager.test.purpose === 'artifact' ? 'onboarding.testArtifactHint' : 'onboarding.testMetadataHint',
  }
}
