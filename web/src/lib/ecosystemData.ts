// web/src/lib/ecosystemData.ts

import type { EcosystemType } from '@/lib/ecosystemTypes'

// ── Seeded PRNG for decorative sparklines ──────────────────────────

function seeded(seed: number): () => number {
  let s = seed
  return () => {
    s = (s * 9301 + 49297) % 233280
    return s / 233280
  }
}

export function genSeries(
  n: number,
  seed: number,
  opts: { base?: number; amp?: number; drift?: number; floor?: number; ceil?: number } = {}
): number[] {
  const { base = 0.5, amp = 0.3, drift = 0, floor = 0, ceil = 1 } = opts
  const r = seeded(seed)
  const out: number[] = []
  let v = base
  for (let i = 0; i < n; i++) {
    v += (r() - 0.5) * amp * 0.4 + drift / n
    v = Math.max(floor, Math.min(ceil, v))
    out.push(v)
  }
  return out
}

// ── KPI series (Monitor page) ──────────────────────────────────────

export const KPI_SERIES = {
  hitRate:  genSeries(60, 11, { base: 0.93, amp: 0.04, floor: 0.86, ceil: 0.99 }),
  requests: genSeries(60, 23, { base: 0.55, amp: 0.5,  floor: 0.15, ceil: 0.95 }),
  saved:    genSeries(60, 37, { base: 0.6,  amp: 0.3,  drift: 0.1,  floor: 0.3, ceil: 0.95 }),
  latency:  genSeries(60, 51, { base: 0.4,  amp: 0.3,  floor: 0.15, ceil: 0.7 }),
}

// ── Mirror series (Monitor page) ──────────────────────────────────

export type MirrorStatus = 'healthy' | 'degraded' | 'failed'

export interface MirrorDef {
  type: string
  p50: number
  hit: string
  status: MirrorStatus
  // url is intentionally absent — the Monitor page sources upstream URLs from the
  // live /api/v1/stats API response (Task 1), not from this static decorative data.
  series: number[]
}

const RAW_MIRRORS: Omit<MirrorDef, 'series'>[] = [
  { type: 'pypi',     p50: 38,  hit: '96.1%', status: 'healthy'  },
  { type: 'npm',      p50: 41,  hit: '94.7%', status: 'healthy'  },
  { type: 'maven',    p50: 62,  hit: '89.3%', status: 'healthy'  },
  { type: 'cargo',    p50: 35,  hit: '97.2%', status: 'healthy'  },
  { type: 'go',       p50: 51,  hit: '92.8%', status: 'healthy'  },
  { type: 'docker',   p50: 184, hit: '78.4%', status: 'degraded' },
  { type: 'helm',     p50: 47,  hit: '91.5%', status: 'healthy'  },
  { type: 'rubygems', p50: 44,  hit: '93.0%', status: 'healthy'  },
  { type: 'nuget',    p50: 58,  hit: '88.2%', status: 'healthy'  },
  { type: 'apt',      p50: 72,  hit: '85.6%', status: 'healthy'  },
  { type: 'conda',    p50: 0,   hit: '-',     status: 'failed'   },
  { type: 'cran',     p50: 53,  hit: '90.4%', status: 'healthy'  },
  { type: 'composer', p50: 49,  hit: '92.1%', status: 'healthy'  },
  { type: 'alpine',   p50: 40,  hit: '94.0%', status: 'healthy'  },
]

export const MIRROR_DEFS: MirrorDef[] = RAW_MIRRORS.map((m, idx) => {
  const seed = (idx + 1) * 7
  let series: number[]
  if (m.status === 'failed') {
    series = genSeries(40, seed, { base: 0.4, amp: 0.1 }).map((v, i) => (i > 26 ? 0 : v))
  } else if (m.status === 'degraded') {
    series = genSeries(40, seed, { base: 0.55, amp: 0.5, drift: 0.4, floor: 0.1, ceil: 1 })
  } else {
    series = genSeries(40, seed, { base: 0.5, amp: 0.25, floor: 0.2, ceil: 0.9 })
  }
  return { ...m, series }
})

// ── Language / manager data (QuickStart page) ─────────────────────

// NOTE: These snippet paths use the actual Depsilo backend routes (/pypi/simple/, /rubygems/),
// which differ from the design reference data.jsx that had incorrect paths (/pip/simple/, /gem/).
// Do not "correct" these back to the reference — the backend adapter paths are authoritative.

export interface ManagerPath {
  os: string
  path: string
}

export interface ManagerConfig {
  id: string
  name: string
  hint: string
  quick: { lang: string; body: string }
  methods?: { label: string; lang: string; body: string }[]
  persistent: { file: string; lang: string; body: string }
  verify: { lang: string; body: string }
  paths: ManagerPath[]
  tutorial: string[]
}

/**
 * Catalog grouping for the QuickStart ecosystem picker.
 *  os    — system-level package managers (apt, alpine)
 *  lang  — language runtime + library package managers
 *  data  — data-science and AI artifact registries
 *  infra — container images, orchestration charts
 *
 * Group section headers + per-language subtitles read from i18n
 * (`quickstart.group.*`, `quickstart.subtitle.*`) so the labels stay in
 * sync with the user's UI language.
 */
export type LanguageGroup = 'os' | 'lang' | 'data' | 'infra'

export interface Language {
  id: string
  name: string
  glyph: string
  iconAdapter: EcosystemType
  group: LanguageGroup
  /** i18n key under `quickstart.subtitle` — short, direct ("Ruby 包" / "Ruby packages"). */
  subtitleKey: string
  managers: ManagerConfig[]
}

export const LANGUAGES: Language[] = [
  {
    id: 'python', name: 'Python', glyph: 'PY', iconAdapter: 'pypi',
    group: 'lang', subtitleKey: 'python',
    managers: [
      {
        id: 'pip', name: 'pip', hint: 'PyPA package installer',
        quick:      { lang: 'sh', body: 'pip install -i {URL}/pypi/simple/ requests' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'pip install -i {URL}/pypi/simple/ requests' },
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'PIP_INDEX_URL={URL}/pypi/simple/ pip install requests' },
        ],
        persistent: { file: '~/.config/pip/pip.conf', lang: 'ini',
          body: '[global]\nindex-url = {URL}/pypi/simple/\ntrusted-host = {HOST}' },
        verify:     { lang: 'sh', body: 'pip install -i {URL}/pypi/simple/ --dry-run six' },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/pip/pip.conf' },
          { os: 'Linux (legacy)', path: '~/.pip/pip.conf' },
          { os: 'Windows',       path: '%APPDATA%\\pip\\pip.ini' },
          { os: 'Per-project',   path: './pip.conf' },
        ],
        tutorial: [
          'Create the config directory if it does not exist: mkdir -p ~/.config/pip',
          'Open ~/.config/pip/pip.conf in your editor (or use the snippet above).',
          'pip honors PIP_INDEX_URL in CI; set PIP_TRUSTED_HOST only for plain HTTP and omit it for HTTPS.',
          'Run pip config list to verify the index-url is picked up.',
        ],
      },
      {
        id: 'uv', name: 'uv', hint: 'astral.sh fast resolver',
        quick:      { lang: 'sh', body: 'UV_INDEX_URL={URL}/pypi/simple/ uv pip install requests' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'uv pip install --index-url {URL}/pypi/simple/ requests' },
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'UV_INDEX_URL={URL}/pypi/simple/ uv pip install requests' },
        ],
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: '[[tool.uv.index]]\nname = "depsilo"\nurl = "{URL}/pypi/simple/"\ndefault = true' },
        verify:     { lang: 'sh', body: 'uv pip compile --index-url {URL}/pypi/simple/ - <<< "six"' },
        paths: [
          { os: 'Per-project', path: './pyproject.toml' },
          { os: 'User config', path: '~/.config/uv/uv.toml' },
          { os: 'Env (CI)',    path: 'UV_INDEX_URL / UV_DEFAULT_INDEX' },
        ],
        tutorial: [
          'Add the [[tool.uv.index]] block to your pyproject.toml.',
          'Set default = true so uv resolves all packages through Depsilo first.',
          'For shell-wide setup, export UV_INDEX_URL in ~/.zshrc.',
          'uv lock will rewrite uv.lock to point at the new index.',
        ],
      },
      {
        id: 'venv', name: 'venv', hint: 'Project-local virtualenv',
        quick:      { lang: 'sh', body: 'python -m venv .venv && source .venv/bin/activate && pip install -i {URL}/pypi/simple/ requests' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'python -m venv .venv && source .venv/bin/activate\npip install -i {URL}/pypi/simple/ requests' },
        ],
        persistent: { file: '.venv/pip.conf', lang: 'ini',
          body: '[global]\nindex-url = {URL}/pypi/simple/\ntrusted-host = {HOST}' },
        verify:     { lang: 'sh', body: 'pip install -i {URL}/pypi/simple/ -r requirements.txt' },
        paths: [
          { os: 'venv-local', path: '.venv/pip.conf' },
          { os: 'venv-local', path: '.venv/Lib/pip.ini (Windows)' },
        ],
        tutorial: [
          'Create the venv: python -m venv .venv',
          'Activate it: source .venv/bin/activate',
          'Drop pip.conf inside .venv so the config is scoped to the venv only.',
          'Reinstall: pip install -r requirements.txt',
        ],
      },
      {
        id: 'poetry', name: 'Poetry', hint: 'Dependency manager',
        quick:      { lang: 'sh', body: 'poetry source add --priority=primary depsilo {URL}/pypi/simple/' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'poetry source add --priority=primary depsilo {URL}/pypi/simple/' },
        ],
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: '[[tool.poetry.source]]\nname = "depsilo"\nurl = "{URL}/pypi/simple/"\npriority = "primary"' },
        verify:     { lang: 'sh', body: 'poetry install --dry-run' },
        paths: [
          { os: 'Per-project', path: './pyproject.toml' },
          { os: 'User auth',   path: '~/.config/pypoetry/auth.toml' },
        ],
        tutorial: [
          'Run poetry source add to register Depsilo as the primary index.',
          'priority = "primary" means it is consulted before PyPI.',
          'Lock new deps with poetry lock --no-update.',
          'Verify resolution with poetry install --dry-run.',
        ],
      },
      {
        id: 'pipenv', name: 'Pipenv', hint: 'Pipfile + virtualenv',
        quick:      { lang: 'sh', body: 'PIPENV_PYPI_MIRROR={URL}/pypi/simple/ pipenv install requests' },
        methods: [
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'PIPENV_PYPI_MIRROR={URL}/pypi/simple/ pipenv install requests' },
        ],
        persistent: { file: 'Pipfile', lang: 'toml',
          body: '[[source]]\nname = "depsilo"\nurl = "{URL}/pypi/simple/"\nverify_ssl = false\n\n[packages]\nrequests = "*"' },
        verify:     { lang: 'sh', body: 'pipenv install --dev --dry-run' },
        paths: [
          { os: 'Per-project', path: './Pipfile' },
          { os: 'Env (CI)',    path: 'PIPENV_PYPI_MIRROR' },
        ],
        tutorial: [
          'Add the [[source]] block to your Pipfile to route all installs through Depsilo.',
          'PIPENV_PYPI_MIRROR overrides all sources without editing Pipfile — useful in CI.',
          'Run pipenv lock to regenerate Pipfile.lock with the new source.',
        ],
      },
      {
        id: 'pdm', name: 'PDM', hint: 'PEP 517 manager',
        quick:      { lang: 'sh', body: 'pdm config pypi.url {URL}/pypi/simple/' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'pdm config pypi.url {URL}/pypi/simple/' },
        ],
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: '[[tool.pdm.source]]\nname = "pypi"\nurl = "{URL}/pypi/simple/"\nverify_ssl = false' },
        verify:     { lang: 'sh', body: 'pdm config pypi.url' },
        paths: [
          { os: 'Per-project', path: './pyproject.toml' },
          { os: 'User config', path: '~/.config/pdm/config.toml' },
        ],
        tutorial: [
          'Using name = "pypi" overrides the built-in default PyPI source; any other name only appends an extra source and PDM may still resolve from pypi.org.',
          'pdm config pypi.url writes to user-level config, affecting all projects.',
          'For per-project setup, add [[tool.pdm.source]] to pyproject.toml.',
          'Run pdm lock to regenerate the lockfile with the new source.',
        ],
      },
      {
        id: 'conda', name: 'Conda', hint: 'Anaconda channels',
        quick:      { lang: 'sh', body: 'conda config --set repodata_use_zst false\nconda install --override-channels -c {URL}/conda/pkgs/main numpy' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'conda config --set repodata_use_zst false\nconda install --override-channels -c {URL}/conda/pkgs/main numpy' },
        ],
        persistent: { file: '~/.condarc', lang: 'yaml',
          body: 'channels:\n  - {URL}/conda/pkgs/main\n  - {URL}/conda/pkgs/r\ndefault_channels: []\nrepodata_use_zst: false' },
        verify:     { lang: 'sh', body: 'conda config --show channels && conda config --show repodata_use_zst' },
        paths: [
          { os: 'macOS / Linux', path: '~/.condarc' },
          { os: 'Windows',       path: '%USERPROFILE%\\.condarc' },
          { os: 'Per-env',       path: '$CONDA_PREFIX/.condarc' },
        ],
        tutorial: [
          'Anaconda upstreams expose channels under /pkgs/<name>/ (e.g. /pkgs/main, /pkgs/r); Depsilo passes the path through verbatim, so the channel URL must include pkgs/.',
          'repodata_use_zst = false disables the zstd-compressed repodata fetch — many mirrors do not publish the .zst variant and conda will otherwise hit 404.',
          'For conda-forge, configure a separate conda upstream pointing at https://conda.anaconda.org and use channel URL {URL}/conda/conda-forge.',
          'Replace the default_channels list (set to []) to fully route through Depsilo and prevent fallback to repo.anaconda.com.',
          '--override-channels in the install command bypasses .condarc entirely — useful for one-off tests.',
        ],
      },
    ],
  },
  {
    id: 'node', name: 'Node.js', glyph: 'JS', iconAdapter: 'npm',
    group: 'lang', subtitleKey: 'node',
    managers: [
      {
        id: 'npm', name: 'npm', hint: 'Default Node registry client',
        quick:      { lang: 'sh', body: 'npm install --registry={URL}/npm/ lodash' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'npm install --registry={URL}/npm/ lodash' },
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'NPM_CONFIG_REGISTRY={URL}/npm/ npm install lodash' },
        ],
        persistent: { file: '~/.npmrc', lang: 'ini',
          body: 'registry={URL}/npm/\n# scoped registries:\n@your-org:registry={URL}/npm/' },
        verify:     { lang: 'sh', body: 'npm config get registry' },
        paths: [
          { os: 'User',        path: '~/.npmrc' },
          { os: 'Per-project', path: './.npmrc' },
          { os: 'Global',      path: '/etc/npmrc' },
          { os: 'Env (CI)',    path: 'NPM_CONFIG_REGISTRY' },
        ],
        tutorial: [
          'Edit ~/.npmrc to set the default registry.',
          'For monorepos commit a project-level .npmrc so teammates inherit it.',
          'Use scoped overrides (@your-org:registry=…) when only some packages route through Depsilo.',
          'Verify with npm config get registry.',
        ],
      },
      {
        id: 'pnpm', name: 'pnpm', hint: 'Hard-linked store',
        quick:      { lang: 'sh', body: 'pnpm install --registry={URL}/npm/ lodash' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'pnpm install --registry={URL}/npm/ lodash' },
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'NPM_CONFIG_REGISTRY={URL}/npm/ pnpm install lodash' },
        ],
        persistent: { file: '~/.npmrc', lang: 'ini',
          body: 'registry={URL}/npm/\nstore-dir=~/.pnpm-store' },
        verify:     { lang: 'sh', body: 'pnpm config get registry' },
        paths: [
          { os: 'User',        path: '~/.npmrc' },
          { os: 'Per-project', path: './.npmrc' },
        ],
        tutorial: [
          'pnpm reads the same .npmrc as npm.',
          'Set store-dir to keep your hard-linked store in a known location.',
          'pnpm config set registry {URL}/npm/ writes to ~/.npmrc.',
        ],
      },
      {
        id: 'yarn', name: 'Yarn', hint: 'Berry & Classic',
        quick:      { lang: 'sh', body: 'yarn config set npmRegistryServer {URL}/npm/' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'yarn config set npmRegistryServer {URL}/npm/' },
        ],
        persistent: { file: './.yarnrc.yml', lang: 'yaml',
          body: 'npmRegistryServer: "{URL}/npm/"\nunsafeHttpWhitelist:\n  - {HOST}' },
        verify:     { lang: 'sh', body: 'yarn config get npmRegistryServer' },
        paths: [
          { os: 'Berry (Yarn 2+)', path: './.yarnrc.yml' },
          { os: 'Classic (Yarn 1)', path: '~/.yarnrc' },
        ],
        tutorial: [
          'Yarn 2+ (Berry) uses .yarnrc.yml — commit it to share with the team.',
          'Add Depsilo host to unsafeHttpWhitelist if not on HTTPS.',
          'Yarn Classic uses ~/.yarnrc with a different syntax (registry "{URL}/npm/").',
        ],
      },
      {
        id: 'bun', name: 'Bun', hint: 'JS runtime + manager',
        quick:      { lang: 'sh', body: 'bun install --registry={URL}/npm/ lodash' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'bun install --registry={URL}/npm/ lodash' },
        ],
        persistent: { file: 'bunfig.toml', lang: 'toml',
          body: '[install]\nregistry = "{URL}/npm/"' },
        verify:     { lang: 'sh', body: 'bun pm cache rm && bun install' },
        paths: [
          { os: 'Per-project', path: './bunfig.toml' },
          { os: 'User',        path: '~/.bunfig.toml' },
        ],
        tutorial: [
          'Add [install] registry = "{URL}/npm/" to bunfig.toml.',
          'For private registries, set a scoped key under [install.scopes].',
          'Clear the cache with bun pm cache rm to force re-fetch.',
        ],
      },
    ],
  },
  {
    id: 'java', name: 'Java', glyph: 'JV', iconAdapter: 'maven',
    group: 'lang', subtitleKey: 'java',
    managers: [
      {
        id: 'maven', name: 'Maven', hint: 'Apache Maven',
        quick:      { lang: 'sh', body: 'mvn -B dependency:get -Dartifact=com.google.guava:guava:32.1.3-jre' },
        methods: [],
        persistent: { file: '~/.m2/settings.xml', lang: 'xml',
          body: '<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <url>{URL}/maven/</url>\n      <mirrorOf>*</mirrorOf>\n    </mirror>\n  </mirrors>\n</settings>' },
        verify:     { lang: 'sh', body: 'mvn help:effective-settings | grep depsilo' },
        paths: [
          { os: 'User',   path: '~/.m2/settings.xml' },
          { os: 'Global', path: '$M2_HOME/conf/settings.xml' },
        ],
        tutorial: [
          'Maven has no built-in command-line flag to override the remote repository — mirrors must be configured in settings.xml.',
          'mirrorOf="*" makes Depsilo the mirror for every declared repository.',
          'Use !central in mirrorOf to exclude specific repos.',
          'Verify with mvn help:effective-settings.',
        ],
      },
      {
        id: 'gradle', name: 'Gradle', hint: 'Build tool',
        quick:      { lang: 'sh', body: 'gradle build --refresh-dependencies' },
        methods: [],
        persistent: { file: '~/.gradle/init.gradle', lang: 'groovy',
          body: 'allprojects {\n  repositories {\n    maven { url "{URL}/maven/" }\n  }\n}' },
        verify:     { lang: 'sh', body: 'gradle dependencies --refresh-dependencies' },
        paths: [
          { os: 'User',        path: '~/.gradle/init.gradle' },
          { os: 'Per-project', path: 'build.gradle.kts (repositories block)' },
        ],
        tutorial: [
          'Gradle has no built-in CLI flag to override repository URLs — use an init.gradle script instead.',
          'init.gradle is applied to every project on this machine.',
          'For per-project setup, add maven { url "{URL}/maven/" } to the repositories block in build.gradle(.kts).',
          'Use --refresh-dependencies to bypass the local cache for verification.',
        ],
      },
      {
        id: 'sbt', name: 'sbt', hint: 'Scala build tool',
        quick:      { lang: 'sh', body: 'sbt -Dsbt.override.build.repos=true compile' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'sbt -Dsbt.override.build.repos=true compile' },
        ],
        persistent: { file: '~/.sbt/repositories', lang: 'ini',
          body: '[repositories]\n  depsilo: {URL}/maven/\n  local' },
        verify:     { lang: 'sh', body: "sbt 'show fullResolvers'" },
        paths: [
          { os: 'User', path: '~/.sbt/repositories' },
        ],
        tutorial: [
          'Set sbt.override.build.repos=true so user-level repos win.',
          'Order matters — local first speeds up incremental builds.',
        ],
      },
    ],
  },
  {
    id: 'rust', name: 'Rust', glyph: 'RS', iconAdapter: 'cargo',
    group: 'lang', subtitleKey: 'rust',
    managers: [
      {
        id: 'cargo', name: 'Cargo', hint: 'Rust package manager',
        quick:      { lang: 'sh', body: 'cargo install --index sparse+{URL}/crates/ ripgrep' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'cargo install --index sparse+{URL}/crates/ ripgrep' },
        ],
        persistent: { file: '~/.cargo/config.toml', lang: 'toml',
          body: '[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "sparse+{URL}/crates/"' },
        verify:     { lang: 'sh', body: 'cargo fetch' },
        paths: [
          { os: 'User',        path: '~/.cargo/config.toml' },
          { os: 'Per-project', path: '.cargo/config.toml' },
        ],
        tutorial: [
          'replace-with = "depsilo" overrides crates.io for every fetch.',
          'The sparse+ prefix tells Cargo to use the HTTP sparse protocol (Cargo 1.68+ default); without it Cargo falls back to git and the proxy will not work.',
          'Depsilo serves the sparse index at /crates/ (not /cargo/).',
          'Run cargo fetch to populate the local cache without building.',
        ],
      },
    ],
  },
  {
    id: 'go', name: 'Go', glyph: 'GO', iconAdapter: 'go',
    group: 'lang', subtitleKey: 'go',
    managers: [
      {
        id: 'goenv', name: 'go env', hint: 'Recommended (persisted)',
        quick:      { lang: 'sh', body: 'GOPROXY={URL}/go/,direct go install golang.org/x/tools/cmd/godoc@latest' },
        methods: [
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'GOPROXY={URL}/go/,direct go install golang.org/x/tools/cmd/godoc@latest' },
        ],
        persistent: { file: 'go env -w', lang: 'sh',
          body: 'go env -w GOPROXY={URL}/go/,direct' },
        verify:     { lang: 'sh', body: 'go env GOPROXY' },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/go/env' },
          { os: 'Windows',       path: '%LOCALAPPDATA%\\go-build\\env' },
        ],
        tutorial: [
          'go env -w writes to ~/.config/go/env, persisting across shells.',
          ',direct is used only after a 404/410 response; it is not outage failover.',
          'Keep GOSUMDB enabled so Go continues verifying public module checksums.',
        ],
      },
      {
        id: 'shell', name: 'GOPROXY env', hint: 'Shell-level',
        quick:      { lang: 'sh', body: 'GOPROXY={URL}/go/,direct go build ./...' },
        methods: [
          { label: 'quickstart.method.envvar', lang: 'sh', body: 'GOPROXY={URL}/go/,direct go build ./...' },
        ],
        persistent: { file: '~/.zshrc · ~/.bashrc', lang: 'sh',
          body: 'export GOPROXY={URL}/go/,direct' },
        verify:     { lang: 'sh', body: 'echo $GOPROXY' },
        paths: [
          { os: 'zsh',  path: '~/.zshrc' },
          { os: 'bash', path: '~/.bashrc' },
          { os: 'fish', path: '~/.config/fish/config.fish' },
        ],
        tutorial: [
          'Shell exports take precedence over go env values in the current session.',
          'Reload with source ~/.zshrc after editing.',
        ],
      },
    ],
  },
  {
    id: 'container', name: 'Container', glyph: 'CT', iconAdapter: 'docker',
    group: 'infra', subtitleKey: 'container',
    managers: [
      {
        id: 'docker', name: 'Docker', hint: 'Daemon registry mirror',
        quick:      { lang: 'sh', body: 'docker pull {HOST}/library/alpine:3.19' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'docker pull {HOST}/library/alpine:3.19' },
        ],
        persistent: { file: '/etc/docker/daemon.json', lang: 'json',
          body: '{\n  "registry-mirrors": ["{URL}"],\n  "insecure-registries": ["{HOST}"]\n}' },
        verify:     { lang: 'sh', body: 'docker info | grep -A1 "Registry Mirrors"' },
        paths: [
          { os: 'Linux',          path: '/etc/docker/daemon.json' },
          { os: 'Docker Desktop', path: '~/.docker/daemon.json (via Settings → Docker Engine)' },
        ],
        tutorial: [
          'Depsilo exposes the OCI Distribution v2 API at the service root (/v2/), so the mirror URL is just {URL} — no /docker/ suffix.',
          'insecure-registries is required when Depsilo is served over plain HTTP (typical for LAN deployments).',
          'After editing daemon.json, restart Docker: sudo systemctl restart docker.',
          'On Docker Desktop, paste the JSON into Settings → Docker Engine and click Apply & Restart.',
          'Mirrors only apply to docker.io — explicit pulls from {HOST}/… work without daemon changes.',
        ],
      },
      {
        id: 'containerd', name: 'containerd', hint: 'CRI mirror',
        quick:      { lang: 'sh', body: 'crictl pull {HOST}/library/alpine:3.19' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'crictl pull {HOST}/library/alpine:3.19' },
        ],
        persistent: { file: '/etc/containerd/config.toml', lang: 'toml',
          body: '[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]\n  endpoint = ["{URL}"]' },
        verify:     { lang: 'sh', body: 'ctr -n k8s.io image pull {HOST}/library/alpine:3.19' },
        paths: [
          { os: 'containerd', path: '/etc/containerd/config.toml' },
          { os: 'k3s',        path: '/etc/rancher/k3s/registries.yaml' },
        ],
        tutorial: [
          'endpoint is the Depsilo service root ({URL}) — containerd appends /v2/ automatically.',
          'Restart containerd after editing: systemctl restart containerd.',
          'For k3s, write registries.yaml instead — k3s consumes that on start.',
        ],
      },
      {
        id: 'podman', name: 'Podman', hint: 'Daemonless containers',
        quick:      { lang: 'sh', body: 'podman pull {HOST}/library/alpine:3.19' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'podman pull {HOST}/library/alpine:3.19' },
        ],
        persistent: { file: '/etc/containers/registries.conf', lang: 'toml',
          body: '[[registry]]\nlocation = "docker.io"\n[[registry.mirror]]\nlocation = "{HOST}"\ninsecure = true' },
        verify:     { lang: 'sh', body: "podman info --format '{{.Registries}}'" },
        paths: [
          { os: 'System', path: '/etc/containers/registries.conf' },
          { os: 'User',   path: '~/.config/containers/registries.conf' },
        ],
        tutorial: [
          'Mirror location is the host:port only (no path) — Podman talks the v2 API at the root.',
          'insecure = true is required when Depsilo is served over plain HTTP.',
          'Mirrors are tried in order, then the original location is used as fallback.',
        ],
      },
    ],
  },
  {
    id: 'kubernetes', name: 'Kubernetes', glyph: 'K8', iconAdapter: 'helm',
    group: 'infra', subtitleKey: 'kubernetes',
    managers: [
      {
        id: 'helm', name: 'Helm', hint: 'Chart registry',
        quick:      { lang: 'sh', body: 'helm repo add depsilo {URL}/helm/\nhelm repo update\nhelm search repo depsilo | head' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'helm repo add depsilo {URL}/helm/\nhelm repo update' },
        ],
        persistent: { file: 'helm repo add', lang: 'sh',
          body: 'helm repo add depsilo {URL}/helm/\nhelm repo update' },
        verify:     { lang: 'sh', body: 'helm repo update && helm search repo depsilo | head' },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/helm/repositories.yaml' },
          { os: 'Windows',       path: '%APPDATA%\\helm\\repositories.yaml' },
        ],
        tutorial: [
          'The repo URL is the Depsilo /helm/ root — Depsilo proxies to the single helm upstream configured on the server (e.g. https://charts.bitnami.com/bitnami).',
          'Configure one Depsilo helm upstream per chart repository (Bitnami, Jetstack, Prometheus Community, …) — the chart repo URL is determined by the upstream config, not by a path suffix.',
          'Always run helm repo update after adding a new repo.',
        ],
      },
    ],
  },
  {
    id: 'ruby', name: 'Ruby', glyph: 'RB', iconAdapter: 'rubygems',
    group: 'lang', subtitleKey: 'ruby',
    managers: [
      {
        id: 'gem', name: 'RubyGems', hint: 'gem CLI',
        quick:      { lang: 'sh', body: 'gem install --source {URL}/rubygems/ rails' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'gem install --source {URL}/rubygems/ rails' },
        ],
        persistent: { file: '~/.gemrc', lang: 'yaml',
          body: '---\n:sources:\n  - {URL}/rubygems/\ngem: --no-document' },
        verify:     { lang: 'sh', body: 'gem sources' },
        paths: [
          { os: 'User',   path: '~/.gemrc' },
          { os: 'System', path: '/etc/gemrc' },
        ],
        tutorial: [
          'gem sources --add {URL}/rubygems/ also writes to .gemrc.',
          '--no-document skips RDoc/RI generation, speeding up installs.',
        ],
      },
      {
        id: 'bundler', name: 'Bundler', hint: 'bundle config',
        quick:      { lang: 'sh', body: 'bundle config mirror.https://rubygems.org {URL}/rubygems/' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'bundle config mirror.https://rubygems.org {URL}/rubygems/' },
        ],
        persistent: { file: '.bundle/config', lang: 'yaml',
          body: 'BUNDLE_MIRROR__HTTPS://RUBYGEMS__ORG/: "{URL}/rubygems/"' },
        verify:     { lang: 'sh', body: 'bundle config get mirror.https://rubygems.org' },
        paths: [
          { os: 'Per-project', path: '.bundle/config' },
          { os: 'Per-user',    path: '~/.bundle/config' },
        ],
        tutorial: [
          'bundle config mirror writes to .bundle/config in the current directory.',
          'Add --global to write to ~/.bundle/config instead.',
        ],
      },
    ],
  },
  {
    id: 'dotnet', name: '.NET', glyph: 'NT', iconAdapter: 'nuget',
    group: 'lang', subtitleKey: 'dotnet',
    managers: [
      {
        id: 'cli', name: 'dotnet CLI', hint: 'nuget add source',
        quick:      { lang: 'sh', body: 'dotnet restore --source {URL}/nuget/v3/index.json' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'dotnet restore --source {URL}/nuget/v3/index.json' },
        ],
        persistent: { file: 'dotnet nuget add source', lang: 'sh',
          body: 'dotnet nuget add source {URL}/nuget/v3/index.json -n depsilo' },
        verify:     { lang: 'sh', body: 'dotnet nuget list source' },
        paths: [
          { os: 'User (macOS/Linux)', path: '~/.nuget/NuGet/NuGet.Config' },
          { os: 'User (Windows)',     path: '%APPDATA%\\NuGet\\NuGet.Config' },
          { os: 'Per-project',        path: './NuGet.Config' },
        ],
        tutorial: [
          'CLI add source writes to your user-level NuGet.Config.',
          'For repo-level setup, commit a NuGet.Config next to your .sln.',
        ],
      },
      {
        id: 'nuget', name: 'NuGet.Config', hint: 'Per-project',
        quick:      { lang: 'sh', body: 'dotnet restore --source {URL}/nuget/v3/index.json' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'dotnet nuget add source {URL}/nuget/v3/index.json -n depsilo' },
        ],
        persistent: { file: 'NuGet.Config', lang: 'xml',
          body: '<configuration>\n  <packageSources>\n    <add key="depsilo" value="{URL}/nuget/v3/index.json" />\n  </packageSources>\n</configuration>' },
        verify:     { lang: 'sh', body: 'dotnet restore --verbosity normal' },
        paths: [
          { os: 'Per-project',  path: './NuGet.Config' },
          { os: 'Per-solution', path: 'next to .sln' },
        ],
        tutorial: [
          'Place NuGet.Config next to your .sln for solution-wide effect.',
          'Use <clear /> inside <packageSources> to drop default nuget.org.',
        ],
      },
    ],
  },
  {
    id: 'debian', name: 'Debian', glyph: 'DB', iconAdapter: 'apt',
    group: 'os', subtitleKey: 'debian',
    managers: [
      {
        id: 'apt', name: 'apt', hint: 'Debian/Ubuntu packages',
        quick:      { lang: 'sh', body: "sudo sed -i 's|http://archive.ubuntu.com|{URL}/apt/tuna|g; s|http://security.ubuntu.com|{URL}/apt/tuna|g' /etc/apt/sources.list && sudo apt update" },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: "sudo sed -i 's|http://archive.ubuntu.com|{URL}/apt/tuna|g; s|http://security.ubuntu.com|{URL}/apt/tuna|g' /etc/apt/sources.list" },
        ],
        persistent: { file: '/etc/apt/sources.list', lang: 'conf',
          body: 'deb {URL}/apt/tuna/ubuntu jammy main restricted universe multiverse\ndeb {URL}/apt/tuna/ubuntu jammy-updates main restricted universe multiverse\ndeb {URL}/apt/tuna/ubuntu jammy-security main restricted universe multiverse' },
        verify:     { lang: 'sh', body: 'sudo apt update && apt-cache policy | head' },
        paths: [
          { os: 'Debian/Ubuntu', path: '/etc/apt/sources.list' },
          { os: 'Modern Ubuntu', path: '/etc/apt/sources.list.d/ubuntu.sources' },
        ],
        tutorial: [
          'Depsilo proxies APT as a reverse proxy — you must replace the host in sources.list, not set Acquire::http::Proxy (which is for forward proxies).',
          'The path segment after /apt/ (tuna in this example) is the upstream name configured on the Depsilo server. Run depsilo config show or check the admin UI to see your upstream names.',
          'After /apt/<upstream-name>/, the path mirrors the upstream layout (so /ubuntu/dists/... for an upstream pointed at https://mirrors.tuna.tsinghua.edu.cn).',
          'Depsilo passes the response body through untouched so GPG signature verification keeps working.',
          'Replace jammy with your release codename (focal, bookworm, etc) — check with lsb_release -cs.',
        ],
      },
    ],
  },
  {
    id: 'alpine', name: 'Alpine', glyph: 'AL', iconAdapter: 'alpine',
    group: 'os', subtitleKey: 'alpine',
    managers: [
      {
        id: 'apk', name: 'apk', hint: 'Alpine Linux package manager',
        quick:      { lang: 'sh', body: '(\n  release="v$(cut -d. -f1,2 /etc/alpine-release)"\n  repos="$(mktemp)"\n  trap \'rm -f "$repos"\' 0\n  printf "%s\\n" "{URL}/alpine/${release}/main" "{URL}/alpine/${release}/community" > "$repos"\n  apk --repositories-file "$repos" add curl\n)' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: '(\n  release="v$(cut -d. -f1,2 /etc/alpine-release)"\n  repos="$(mktemp)"\n  trap \'rm -f "$repos"\' 0\n  printf "%s\\n" "{URL}/alpine/${release}/main" "{URL}/alpine/${release}/community" > "$repos"\n  apk --repositories-file "$repos" add curl\n)' },
          { label: 'quickstart.method.dockerfile', lang: 'dockerfile',
            body: 'FROM alpine:3.19\n\nARG DEPSILO_URL={URL}\nRUN printf "%s\\n" \\\n  "${DEPSILO_URL}/alpine/v3.19/main" \\\n  "${DEPSILO_URL}/alpine/v3.19/community" \\\n  > /etc/apk/repositories \\\n  && apk add --no-cache curl ca-certificates' },
        ],
        persistent: { file: '/etc/apk/repositories', lang: 'conf',
          body: '{URL}/alpine/v<major.minor>/main\n{URL}/alpine/v<major.minor>/community' },
        verify:     { lang: 'sh', body: '(\n  release="v$(cut -d. -f1,2 /etc/alpine-release)"\n  repos="$(mktemp)"\n  trap \'rm -f "$repos"\' 0\n  printf "%s\\n" "{URL}/alpine/${release}/main" "{URL}/alpine/${release}/community" > "$repos"\n  apk --repositories-file "$repos" update\n)' },
        paths: [
          { os: 'System',    path: '/etc/apk/repositories' },
          { os: 'Per-call',  path: 'mktemp + apk --repositories-file …' },
        ],
        tutorial: [
          'Replace the lines in /etc/apk/repositories with the Depsilo URLs above (keep main + community).',
          'Replace v<major.minor> with the branch for your release — check /etc/alpine-release.',
          'Use --repositories-file for one-shot tests; --repository/-X is supplemental and would keep public repositories active.',
          'In Docker builds, override DEPSILO_URL when needed: localhost inside the build container is not your host machine.',
          'APKINDEX.tar.gz is signed; Depsilo passes it through untouched so signature verification still works.',
          'Run apk update to refresh the index, then apk add <pkg> to install through the cache.',
        ],
      },
    ],
  },
  {
    id: 'php', name: 'PHP', glyph: 'PH', iconAdapter: 'composer',
    group: 'lang', subtitleKey: 'php',
    managers: [
      {
        id: 'composer', name: 'Composer', hint: 'PHP package manager',
        quick:      { lang: 'sh', body: 'composer config -g secure-http false\ncomposer config -g repo.packagist composer {URL}/composer/' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'composer config -g secure-http false\ncomposer config -g repo.packagist composer {URL}/composer/' },
        ],
        persistent: { file: '~/.composer/config.json', lang: 'json',
          body: '{\n  "config": {\n    "secure-http": false\n  },\n  "repositories": {\n    "packagist": {\n      "type": "composer",\n      "url": "{URL}/composer/"\n    }\n  }\n}' },
        verify:     { lang: 'sh', body: 'composer diagnose' },
        paths: [
          { os: 'User',        path: '~/.composer/config.json' },
          { os: 'Per-project', path: './composer.json (repositories block)' },
        ],
        tutorial: [
          'secure-http: false is required when Depsilo is served over plain HTTP (typical for LAN deployments) — Composer refuses non-https sources by default.',
          'composer config writes to ~/.composer/config.json by default.',
          'Pass --no-plugins if your project uses plugins that block source switching.',
        ],
      },
    ],
  },
  {
    id: 'r', name: 'R', glyph: 'R', iconAdapter: 'cran',
    group: 'data', subtitleKey: 'r',
    managers: [
      {
        id: 'rscript', name: 'R', hint: 'Base R CRAN mirror',
        quick:      { lang: 'r', body: 'install.packages("jsonlite", repos="{URL}/cran/")' },
        methods: [
          { label: 'quickstart.method.cmdline', lang: 'r', body: 'install.packages("jsonlite", repos="{URL}/cran/")' },
        ],
        persistent: { file: '~/.Rprofile', lang: 'r',
          body: 'options(repos = c(CRAN = "{URL}/cran/"))' },
        verify:     { lang: 'r', body: 'options(repos = c(CRAN = "{URL}/cran/"))\ninstall.packages("jsonlite")\nlibrary(jsonlite)' },
        paths: [
          { os: 'User',        path: '~/.Rprofile' },
          { os: 'Per-project', path: './.Rprofile' },
          { os: 'Global',      path: 'file.path(R.home("etc"), "Rprofile.site")' },
        ],
        tutorial: [
          'Add options(repos = ...) to ~/.Rprofile to permanently configure the CRAN mirror.',
          'Verify with getOption("repos") in an R console.',
          'For renv-managed projects, set RENV_CONFIG_REPOS_OVERRIDE="{URL}/cran/" in .Renviron.',
        ],
      },
    ],
  },
  {
    id: 'huggingface', name: 'Hugging Face', glyph: 'HF', iconAdapter: 'huggingface',
    group: 'data', subtitleKey: 'huggingface',
    managers: [
      {
        id: 'hf-cli', name: 'hf', hint: 'Official CLI',
        quick:      { lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface\nhf download bert-base-uncased --local-dir ./bert' },
        methods: [
          { label: 'quickstart.method.envvar',  lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
          { label: 'quickstart.method.cmdline', lang: 'sh', body: 'HF_ENDPOINT={URL}/huggingface hf download bert-base-uncased --local-dir ./bert' },
        ],
        persistent: { file: '~/.bashrc', lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
        verify:     { lang: 'sh', body: 'hf download prajjwal1/bert-tiny --local-dir /tmp/bert-tiny && ls /tmp/bert-tiny' },
        paths: [
          { os: 'POSIX shells', path: '~/.bashrc, ~/.zshrc, ~/.profile' },
          { os: 'fish',         path: 'set -Ux HF_ENDPOINT {URL}/huggingface' },
        ],
        tutorial: [
          'hf, transformers, datasets — every official tool respects HF_ENDPOINT.',
          'Set it once in your shell rc and forget about it; every download routes through Depsilo automatically.',
          'Gated models: pass your usual HF_TOKEN — Depsilo forwards it to upstream verbatim but does NOT cache the response.',
          'Current cache support uses regular HTTP downloads (about 50 GB or less per file); Xet-native larger files are not yet supported.',
        ],
      },
      {
        id: 'transformers', name: 'transformers', hint: 'Python: AutoModel.from_pretrained()',
        quick:      { lang: 'py', body: 'import os; os.environ["HF_ENDPOINT"] = "{URL}/huggingface"\nfrom transformers import AutoModel\nm = AutoModel.from_pretrained("bert-base-uncased")' },
        methods: [
          { label: 'quickstart.method.envvar',  lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
          { label: 'quickstart.method.inline',  lang: 'py', body: 'import os; os.environ["HF_ENDPOINT"] = "{URL}/huggingface"\nfrom transformers import AutoModel\nm = AutoModel.from_pretrained("bert-base-uncased")' },
        ],
        persistent: { file: '~/.bashrc', lang: 'sh', body: 'export HF_ENDPOINT={URL}/huggingface' },
        verify:     { lang: 'py', body: 'from transformers import AutoModel\nm = AutoModel.from_pretrained("prajjwal1/bert-tiny")\nprint(m.config.model_type)' },
        paths: [
          { os: 'POSIX shells', path: '~/.bashrc, ~/.zshrc, ~/.profile' },
          { os: 'In-script',    path: 'os.environ before importing transformers' },
        ],
        tutorial: [
          'Set HF_ENDPOINT before importing transformers (or any HF library). The env var must be set at import time, not later.',
          'Works for transformers, datasets, evaluate, peft, accelerate — anything that uses huggingface_hub under the hood.',
        ],
      },
    ],
  },
]
