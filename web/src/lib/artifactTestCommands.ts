import { resolveServiceOrigin } from './packageManagerConfig'

/** Use existing client configuration unless a one-command pip source is selected. */
export function getIsolatedArtifactCommand(
  manager: 'pip' | 'npm',
  shell: 'posix' | 'powershell' = 'posix',
  options: { pipIndexOrigin?: string } = {},
) {
  const endpoint = manager === 'pip' && options.pipIndexOrigin ? new URL(resolveServiceOrigin(options.pipIndexOrigin)) : null
  const quote = (value: string) => `'${value.replace(/'/g, shell === 'powershell' ? "''" : "'\\''")}'`
  const pipSource = endpoint
    ? ` --index-url ${quote(`${endpoint.origin}/pypi/simple/`)}${endpoint.protocol === 'http:' ? ` --trusted-host ${quote(endpoint.host)}` : ''}`
    : ''
  if (shell === 'powershell') {
    return {
      lang: 'powershell',
      body: [
        '& {',
        '  $depsiloTestDir = $null',
        '  $global:LASTEXITCODE = 1',
        '  try {',
        `    $depsiloTestDir = (New-Item -ItemType Directory -Path (Join-Path ([IO.Path]::GetTempPath()) ('depsilo-${manager}-' + [guid]::NewGuid().ToString('N'))) -ErrorAction Stop).FullName`,
        manager === 'pip'
          ? `    python -m pip download --disable-pip-version-check --no-deps --no-cache-dir --only-binary=:all:${pipSource} --dest "$depsiloTestDir" six==1.17.0`
          : '    npm pack is-number@7.0.0 --ignore-scripts --workspaces=false --update-notifier=false --cache "$depsiloTestDir/cache" --pack-destination "$depsiloTestDir"',
        '  } finally {',
        '    if ($null -ne $depsiloTestDir) {',
        '      Remove-Item -LiteralPath $depsiloTestDir -Recurse -Force -ErrorAction Continue',
        '    }',
        '  }',
        '}',
      ].join('\n'),
    }
  }

  // A subshell keeps the caller's variables and signal handlers intact. All
  // interruptions return 130; plain `exit` in a signal trap can return success
  // when only the parent shell was signalled and the client finished with 0.
  return {
    lang: 'sh',
    body: [
      '(',
      '  depsilo_tmp="$(mktemp -d)" || exit',
      '  trap \'rm -rf -- "$depsilo_tmp"\' 0',
      '  trap \'exit 130\' HUP INT TERM',
      ...(manager === 'pip'
        ? [
            '  python -m pip download --no-deps --no-cache-dir --only-binary=:all: \\',
            ...(pipSource
              ? ['    --disable-pip-version-check -d "$depsilo_tmp" \\', `   ${pipSource} six==1.17.0`]
              : ['    --disable-pip-version-check -d "$depsilo_tmp" six==1.17.0']),
          ]
        : [
            '  npm pack is-number@7.0.0 --ignore-scripts \\',
            '    --workspaces=false --update-notifier=false \\',
            '    --cache "$depsilo_tmp/cache" --pack-destination "$depsilo_tmp"',
          ]),
      ')',
    ].join('\n'),
  }
}
