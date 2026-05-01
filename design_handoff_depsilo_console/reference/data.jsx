/* ============================================================
   Depsilo — synthetic data + variant snippets
   ============================================================ */

function seeded(seed) {
  let s = seed;
  return () => { s = (s * 9301 + 49297) % 233280; return s / 233280; };
}
function genSeries(n, seed, { base = 0.5, amp = 0.3, drift = 0, floor = 0, ceil = 1 } = {}) {
  const r = seeded(seed);
  const out = []; let v = base;
  for (let i = 0; i < n; i++) {
    v += (r() - 0.5) * amp * 0.4 + drift / n;
    v = Math.max(floor, Math.min(ceil, v));
    out.push(v);
  }
  return out;
}

const HIT_RATE_SERIES = genSeries(60, 11, { base: 0.93, amp: 0.04, floor: 0.86, ceil: 0.99 });
const REQ_SERIES      = genSeries(60, 23, { base: 0.55, amp: 0.5, floor: 0.15, ceil: 0.95 });
const SAVED_SERIES    = genSeries(60, 37, { base: 0.6,  amp: 0.3, drift: 0.1, floor: 0.3, ceil: 0.95 });
const LATENCY_SERIES  = genSeries(60, 51, { base: 0.4,  amp: 0.3, floor: 0.15, ceil: 0.7 });

const KPIS = [
  { id: 'hit',   label: 'Cache hit rate',   value: '94.2', unit: '%',    delta: '+1.4',  deltaTone: 'ok', series: HIT_RATE_SERIES, tone: 'ok' },
  { id: 'req',   label: 'Total requests',   value: '1.28', unit: 'M / 24h', delta: '+12%', deltaTone: 'ok', series: REQ_SERIES, tone: 'brand' },
  { id: 'saved', label: 'Bandwidth saved',  value: '847',  unit: 'GB',   delta: '+96 GB', deltaTone: 'ok', series: SAVED_SERIES, tone: 'brand' },
  { id: 'p50',   label: 'P50 latency',      value: '42',   unit: 'ms',   delta: '−3 ms', deltaTone: 'ok', series: LATENCY_SERIES, tone: 'neutral', invert: true },
];

const MIRRORS = [
  { type: 'pip',      url: 'https://pypi.org/simple',                p50: 38,  hit: '96.1%', status: 'healthy',  seed: 1 },
  { type: 'npm',      url: 'https://registry.npmjs.org',             p50: 41,  hit: '94.7%', status: 'healthy',  seed: 2 },
  { type: 'maven',    url: 'https://repo1.maven.org/maven2',         p50: 62,  hit: '89.3%', status: 'healthy',  seed: 3 },
  { type: 'cargo',    url: 'https://index.crates.io',                p50: 35,  hit: '97.2%', status: 'healthy',  seed: 4 },
  { type: 'go',       url: 'https://proxy.golang.org',               p50: 51,  hit: '92.8%', status: 'healthy',  seed: 5 },
  { type: 'docker',   url: 'https://registry-1.docker.io',           p50: 184, hit: '78.4%', status: 'degraded', seed: 6 },
  { type: 'helm',     url: 'https://charts.bitnami.com/bitnami',     p50: 47,  hit: '91.5%', status: 'healthy',  seed: 7 },
  { type: 'gem',      url: 'https://rubygems.org',                   p50: 44,  hit: '93.0%', status: 'healthy',  seed: 8 },
  { type: 'nuget',    url: 'https://api.nuget.org/v3/index.json',    p50: 58,  hit: '88.2%', status: 'healthy',  seed: 9 },
  { type: 'apt',      url: 'http://archive.ubuntu.com/ubuntu',       p50: 72,  hit: '85.6%', status: 'healthy',  seed: 10 },
  { type: 'conda',    url: 'https://repo.anaconda.com/pkgs/main',    p50: 0,   hit: '—',     status: 'failed',   seed: 11 },
  { type: 'pub',      url: 'https://pub.dev',                        p50: 53,  hit: '90.4%', status: 'healthy',  seed: 12 },
  { type: 'composer', url: 'https://packagist.org',                  p50: 49,  hit: '92.1%', status: 'healthy',  seed: 13 },
];
MIRRORS.forEach(m => {
  if (m.status === 'failed')      m.series = genSeries(40, m.seed * 7, { base: 0.4, amp: 0.1 }).map((v, i) => i > 26 ? 0 : v);
  else if (m.status === 'degraded') m.series = genSeries(40, m.seed * 7, { base: 0.55, amp: 0.5, drift: 0.4, floor: 0.1, ceil: 1 });
  else                              m.series = genSeries(40, m.seed * 7, { base: 0.5, amp: 0.25, floor: 0.2, ceil: 0.9 });
});

const MANAGERS = [
  { id: 'pip',      name: 'pip',      lang: 'Python',     glyph: 'PIP' },
  { id: 'uv',       name: 'uv',       lang: 'Python',     glyph: 'UV'  },
  { id: 'poetry',   name: 'Poetry',   lang: 'Python',     glyph: 'PTR' },
  { id: 'npm',      name: 'npm',      lang: 'Node.js',    glyph: 'NPM' },
  { id: 'pnpm',     name: 'pnpm',     lang: 'Node.js',    glyph: 'PNM' },
  { id: 'yarn',     name: 'Yarn',     lang: 'Node.js',    glyph: 'YRN' },
  { id: 'maven',    name: 'Maven',    lang: 'Java',       glyph: 'MVN' },
  { id: 'gradle',   name: 'Gradle',   lang: 'Java',       glyph: 'GRD' },
  { id: 'cargo',    name: 'Cargo',    lang: 'Rust',       glyph: 'CRG' },
  { id: 'go',       name: 'Go',       lang: 'Go',         glyph: 'GO'  },
  { id: 'docker',   name: 'Docker',   lang: 'Container',  glyph: 'DKR' },
  { id: 'helm',     name: 'Helm',     lang: 'Kubernetes', glyph: 'HLM' },
  { id: 'gem',      name: 'RubyGems', lang: 'Ruby',       glyph: 'GEM' },
  { id: 'nuget',    name: 'NuGet',    lang: '.NET',       glyph: 'NUG' },
  { id: 'apt',      name: 'APT',      lang: 'Debian',     glyph: 'APT' },
  { id: 'conda',    name: 'Conda',    lang: 'Python',     glyph: 'CND' },
  { id: 'pub',      name: 'pub',      lang: 'Dart',       glyph: 'PUB' },
  { id: 'composer', name: 'Composer', lang: 'PHP',        glyph: 'CMP' },
];

// Languages → managers (canonical config per manager).
// Each manager: { id, name, hint, kind: 'cmd' | 'file', file?, lang, body }
const LANGUAGES = [
  {
    id: 'python', name: 'Python', glyph: 'PY',
    managers: [
      {
        id: 'pip', name: 'pip', hint: 'PyPA package installer',
        quick:      { lang: 'sh', body: `PIP_INDEX_URL={URL}/pip/simple/ pip install requests` },
        persistent: { file: '~/.config/pip/pip.conf', lang: 'ini',
          body: `[global]\nindex-url = {URL}/pip/simple/\ntrusted-host = {HOST}` },
        verify:     { lang: 'sh', body: `pip install -i {URL}/pip/simple/ --dry-run six` },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/pip/pip.conf' },
          { os: 'Linux (legacy)', path: '~/.pip/pip.conf' },
          { os: 'Windows',       path: '%APPDATA%\\pip\\pip.ini' },
          { os: 'Per-project',   path: './pip.conf' },
        ],
        tutorial: [
          'Create the config directory if it does not exist: mkdir -p ~/.config/pip',
          'Open ~/.config/pip/pip.conf in your editor (or use the snippet above).',
          'pip honors PIP_INDEX_URL and PIP_TRUSTED_HOST env vars too — useful in CI.',
          'Run pip config list to verify the index-url is picked up.',
        ],
      },
      {
        id: 'uv', name: 'uv', hint: 'astral.sh fast resolver',
        quick:      { lang: 'sh', body: `UV_INDEX_URL={URL}/pip/simple/ uv pip install requests` },
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: `[[tool.uv.index]]\nname = "depsilo"\nurl = "{URL}/pip/simple/"\ndefault = true` },
        verify:     { lang: 'sh', body: `uv pip install --index-url {URL}/pip/simple/ --dry-run six` },
        paths: [
          { os: 'Per-project',   path: './pyproject.toml' },
          { os: 'User config',   path: '~/.config/uv/uv.toml' },
          { os: 'Env (CI)',      path: 'UV_INDEX_URL / UV_DEFAULT_INDEX' },
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
        quick:      { lang: 'sh', body: `python -m venv .venv && source .venv/bin/activate && pip install -i {URL}/pip/simple/ requests` },
        persistent: { file: '.venv/pip.conf', lang: 'ini',
          body: `[global]\nindex-url = {URL}/pip/simple/\ntrusted-host = {HOST}` },
        verify:     { lang: 'sh', body: `pip install -i {URL}/pip/simple/ -r requirements.txt` },
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
        quick:      { lang: 'sh', body: `poetry source add --priority=primary depsilo {URL}/pip/simple/` },
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: `[[tool.poetry.source]]\nname = "depsilo"\nurl = "{URL}/pip/simple/"\npriority = "primary"` },
        verify:     { lang: 'sh', body: `poetry install --dry-run` },
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
        id: 'conda', name: 'Conda', hint: 'Anaconda channels',
        quick:      { lang: 'sh', body: `conda install -c {URL}/conda/main numpy` },
        persistent: { file: '~/.condarc', lang: 'yaml',
          body: `channels:\n  - {URL}/conda/main\n  - {URL}/conda/conda-forge\ndefault_channels: []` },
        verify:     { lang: 'sh', body: `conda config --show channels` },
        paths: [
          { os: 'macOS / Linux', path: '~/.condarc' },
          { os: 'Windows',       path: '%USERPROFILE%\\.condarc' },
          { os: 'Per-env',       path: '$CONDA_PREFIX/.condarc' },
        ],
        tutorial: [
          'Replace the default_channels list to fully route through Depsilo.',
          'Use conda config --add channels to merge instead of replace.',
          'Run conda info to confirm the active channel order.',
        ],
      },
    ],
  },
  {
    id: 'node', name: 'Node.js', glyph: 'JS',
    managers: [
      {
        id: 'npm', name: 'npm', hint: 'Default Node registry client',
        quick:      { lang: 'sh', body: `npm install --registry={URL}/npm/ lodash` },
        persistent: { file: '~/.npmrc', lang: 'ini',
          body: `registry={URL}/npm/\n# scoped registries:\n@your-org:registry={URL}/npm/` },
        verify:     { lang: 'sh', body: `npm config get registry` },
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
        quick:      { lang: 'sh', body: `pnpm install --registry={URL}/npm/ lodash` },
        persistent: { file: '~/.npmrc', lang: 'ini',
          body: `registry={URL}/npm/\nstore-dir=~/.pnpm-store` },
        verify:     { lang: 'sh', body: `pnpm config get registry` },
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
        quick:      { lang: 'sh', body: `yarn config set npmRegistryServer {URL}/npm/` },
        persistent: { file: './.yarnrc.yml', lang: 'yaml',
          body: `npmRegistryServer: "{URL}/npm/"\nunsafeHttpWhitelist:\n  - {HOST}` },
        verify:     { lang: 'sh', body: `yarn config get npmRegistryServer` },
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
        quick:      { lang: 'sh', body: `bun install --registry={URL}/npm/ lodash` },
        persistent: { file: 'bunfig.toml', lang: 'toml',
          body: `[install]\nregistry = "{URL}/npm/"` },
        verify:     { lang: 'sh', body: `bun pm cache rm && bun install` },
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
    id: 'java', name: 'Java', glyph: 'JV',
    managers: [
      {
        id: 'maven', name: 'Maven', hint: 'Apache Maven',
        quick:      { lang: 'sh', body: `mvn -Dmaven.repo.remote={URL}/maven/ install` },
        persistent: { file: '~/.m2/settings.xml', lang: 'xml',
          body: `<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <url>{URL}/maven/</url>\n      <mirrorOf>*</mirrorOf>\n    </mirror>\n  </mirrors>\n</settings>` },
        verify:     { lang: 'sh', body: `mvn help:effective-settings | grep depsilo` },
        paths: [
          { os: 'User',   path: '~/.m2/settings.xml' },
          { os: 'Global', path: '$M2_HOME/conf/settings.xml' },
        ],
        tutorial: [
          'mirrorOf="*" makes Depsilo the mirror for every declared repository.',
          'Use !central in mirrorOf to exclude specific repos (e.g. mirrorOf="*,!internal").',
          'Verify with mvn help:effective-settings.',
        ],
      },
      {
        id: 'gradle', name: 'Gradle', hint: 'Build tool',
        quick:      { lang: 'sh', body: `gradle build -Dorg.gradle.repositoryMirrorUrl={URL}/maven/` },
        persistent: { file: '~/.gradle/init.gradle', lang: 'groovy',
          body: `allprojects {\n  repositories {\n    maven { url "{URL}/maven/" }\n  }\n}` },
        verify:     { lang: 'sh', body: `gradle dependencies --refresh-dependencies` },
        paths: [
          { os: 'User',        path: '~/.gradle/init.gradle' },
          { os: 'Per-project', path: 'build.gradle.kts (repositories block)' },
        ],
        tutorial: [
          'init.gradle is applied to every project on this machine.',
          'For per-project setup, add maven(url = "{URL}/maven/") to repositories.',
          'Use --refresh-dependencies to bypass the local cache for verification.',
        ],
      },
      {
        id: 'sbt', name: 'sbt', hint: 'Scala build tool',
        quick:      { lang: 'sh', body: `sbt -Dsbt.override.build.repos=true compile` },
        persistent: { file: '~/.sbt/repositories', lang: 'ini',
          body: `[repositories]\n  depsilo: {URL}/maven/\n  local` },
        verify:     { lang: 'sh', body: `sbt 'show fullResolvers'` },
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
    id: 'rust', name: 'Rust', glyph: 'RS',
    managers: [
      {
        id: 'cargo', name: 'Cargo', hint: 'Rust package manager',
        quick:      { lang: 'sh', body: `cargo install --index {URL}/cargo/index ripgrep` },
        persistent: { file: '~/.cargo/config.toml', lang: 'toml',
          body: `[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "{URL}/cargo/index"` },
        verify:     { lang: 'sh', body: `cargo fetch` },
        paths: [
          { os: 'User',        path: '~/.cargo/config.toml' },
          { os: 'Per-project', path: '.cargo/config.toml' },
        ],
        tutorial: [
          'replace-with = "depsilo" overrides crates.io for every fetch.',
          'Cargo expects a sparse index by default; the URL above already points to the sparse format.',
          'Run cargo fetch to populate the local cache without building.',
        ],
      },
    ],
  },
  {
    id: 'go', name: 'Go', glyph: 'GO',
    managers: [
      {
        id: 'goenv', name: 'go env', hint: 'Recommended (persisted)',
        quick:      { lang: 'sh', body: `GOPROXY={URL}/go/,direct go install golang.org/x/tools/cmd/godoc@latest` },
        persistent: { file: 'go env -w', lang: 'sh',
          body: `go env -w GOPROXY={URL}/go/,direct\ngo env -w GOSUMDB=off` },
        verify:     { lang: 'sh', body: `go env GOPROXY` },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/go/env' },
          { os: 'Windows',       path: '%LOCALAPPDATA%\\go-build\\env' },
        ],
        tutorial: [
          'go env -w writes to ~/.config/go/env, persisting across shells.',
          'Use ,direct to fall back to the upstream when the cache misses.',
          'Disable GOSUMDB only on private networks — it weakens supply-chain checks.',
        ],
      },
      {
        id: 'shell', name: 'GOPROXY env', hint: 'Shell-level',
        quick:      { lang: 'sh', body: `GOPROXY={URL}/go/,direct go build ./...` },
        persistent: { file: '~/.zshrc · ~/.bashrc', lang: 'sh',
          body: `export GOPROXY={URL}/go/,direct\nexport GOSUMDB=off` },
        verify:     { lang: 'sh', body: `echo $GOPROXY` },
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
    id: 'container', name: 'Container', glyph: 'CT',
    managers: [
      {
        id: 'docker', name: 'Docker', hint: 'Daemon registry mirror',
        quick:      { lang: 'sh', body: `docker pull {HOST}/docker/library/alpine:3.19` },
        persistent: { file: '/etc/docker/daemon.json', lang: 'json',
          body: `{\n  "registry-mirrors": ["{URL}/docker/"]\n}` },
        verify:     { lang: 'sh', body: `docker info | grep -A1 "Registry Mirrors"` },
        paths: [
          { os: 'Linux',       path: '/etc/docker/daemon.json' },
          { os: 'Docker Desktop', path: '~/.docker/daemon.json (via Settings → Docker Engine)' },
        ],
        tutorial: [
          'After editing daemon.json, restart Docker: sudo systemctl restart docker.',
          'On Docker Desktop, paste the JSON into Settings → Docker Engine and click Apply & Restart.',
          'Mirrors only apply to docker.io — explicit pulls from {HOST}/docker/… work without daemon changes.',
        ],
      },
      {
        id: 'containerd', name: 'containerd', hint: 'CRI mirror',
        quick:      { lang: 'sh', body: `crictl pull {HOST}/docker/library/alpine:3.19` },
        persistent: { file: '/etc/containerd/config.toml', lang: 'toml',
          body: `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]\n  endpoint = ["{URL}/docker/"]` },
        verify:     { lang: 'sh', body: `ctr -n k8s.io image pull {HOST}/docker/library/alpine:3.19` },
        paths: [
          { os: 'containerd', path: '/etc/containerd/config.toml' },
          { os: 'k3s',        path: '/etc/rancher/k3s/registries.yaml' },
        ],
        tutorial: [
          'Restart containerd after editing: systemctl restart containerd.',
          'For k3s, write registries.yaml instead — k3s consumes that on start.',
        ],
      },
      {
        id: 'podman', name: 'Podman', hint: 'Daemonless containers',
        quick:      { lang: 'sh', body: `podman pull {HOST}/docker/library/alpine:3.19` },
        persistent: { file: '/etc/containers/registries.conf', lang: 'toml',
          body: `[[registry]]\nlocation = "docker.io"\n[[registry.mirror]]\nlocation = "{HOST}/docker"` },
        verify:     { lang: 'sh', body: `podman info --format '{{.Registries}}'` },
        paths: [
          { os: 'System', path: '/etc/containers/registries.conf' },
          { os: 'User',   path: '~/.config/containers/registries.conf' },
        ],
        tutorial: [
          'Podman supports the same TOML format system-wide and per-user.',
          'Mirrors are tried in order, then the original location is used as fallback.',
        ],
      },
    ],
  },
  {
    id: 'kubernetes', name: 'Kubernetes', glyph: 'K8',
    managers: [
      {
        id: 'helm', name: 'Helm', hint: 'Chart registry',
        quick:      { lang: 'sh', body: `helm install --repo {URL}/helm/bitnami nginx bitnami/nginx` },
        persistent: { file: 'helm repo add', lang: 'sh',
          body: `helm repo add bitnami {URL}/helm/bitnami\nhelm repo update` },
        verify:     { lang: 'sh', body: `helm repo list` },
        paths: [
          { os: 'macOS / Linux', path: '~/.config/helm/repositories.yaml' },
          { os: 'Windows',       path: '%APPDATA%\\helm\\repositories.yaml' },
        ],
        tutorial: [
          'helm repo add writes to repositories.yaml.',
          'Always run helm repo update after adding a new repo.',
        ],
      },
    ],
  },
  {
    id: 'ruby', name: 'Ruby', glyph: 'RB',
    managers: [
      {
        id: 'gem', name: 'RubyGems', hint: 'gem CLI',
        quick:      { lang: 'sh', body: `gem install --source {URL}/gem/ rails` },
        persistent: { file: '~/.gemrc', lang: 'yaml',
          body: `---\n:sources:\n  - {URL}/gem/\ngem: --no-document` },
        verify:     { lang: 'sh', body: `gem sources` },
        paths: [
          { os: 'User',   path: '~/.gemrc' },
          { os: 'System', path: '/etc/gemrc' },
        ],
        tutorial: [
          'gem sources --add {URL}/gem/ also writes to .gemrc.',
          '--no-document skips RDoc/RI generation, speeding up installs.',
        ],
      },
      {
        id: 'bundler', name: 'Bundler', hint: 'bundle config',
        quick:      { lang: 'sh', body: `bundle config mirror.https://rubygems.org {URL}/gem/` },
        persistent: { file: '.bundle/config', lang: 'yaml',
          body: `BUNDLE_MIRROR__HTTPS://RUBYGEMS__ORG/: "{URL}/gem/"` },
        verify:     { lang: 'sh', body: `bundle config get mirror.https://rubygems.org` },
        paths: [
          { os: 'Per-project', path: '.bundle/config' },
          { os: 'User',        path: '~/.bundle/config' },
        ],
        tutorial: [
          'bundle config mirror writes to .bundle/config in the current directory.',
          'Add --global to write to ~/.bundle/config instead.',
        ],
      },
    ],
  },
  {
    id: 'dotnet', name: '.NET', glyph: 'NT',
    managers: [
      {
        id: 'cli', name: 'dotnet CLI', hint: 'nuget add source',
        quick:      { lang: 'sh', body: `dotnet restore --source {URL}/nuget/v3/index.json` },
        persistent: { file: 'dotnet nuget add source', lang: 'sh',
          body: `dotnet nuget add source {URL}/nuget/v3/index.json -n depsilo` },
        verify:     { lang: 'sh', body: `dotnet nuget list source` },
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
        quick:      { lang: 'sh', body: `dotnet restore --source {URL}/nuget/v3/index.json` },
        persistent: { file: 'NuGet.Config', lang: 'xml',
          body: `<configuration>\n  <packageSources>\n    <add key="depsilo" value="{URL}/nuget/v3/index.json" />\n  </packageSources>\n</configuration>` },
        verify:     { lang: 'sh', body: `dotnet restore --verbosity normal` },
        paths: [
          { os: 'Per-project', path: './NuGet.Config' },
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
    id: 'debian', name: 'Debian', glyph: 'DB',
    managers: [
      {
        id: 'apt', name: 'apt', hint: 'Debian/Ubuntu packages',
        quick:      { lang: 'sh', body: `sudo apt -o Acquire::http::Proxy="{URL}/apt/" update` },
        persistent: { file: '/etc/apt/apt.conf.d/99-depsilo', lang: 'conf',
          body: `Acquire::http::Proxy "{URL}/apt/";\nAcquire::https::Proxy "{URL}/apt/";` },
        verify:     { lang: 'sh', body: `apt-config dump | grep Proxy` },
        paths: [
          { os: 'System', path: '/etc/apt/apt.conf.d/99-depsilo' },
          { os: 'Per-user (rare)', path: '~/.aptconfig' },
        ],
        tutorial: [
          'Files in apt.conf.d are merged in lexical order — 99- ensures yours wins.',
          'After editing, run sudo apt update to verify the proxy is consulted.',
        ],
      },
    ],
  },
  {
    id: 'dart', name: 'Dart', glyph: 'DT',
    managers: [
      {
        id: 'pub', name: 'pub', hint: 'Dart / Flutter packages',
        quick:      { lang: 'sh', body: `PUB_HOSTED_URL={URL}/pub/ dart pub get` },
        persistent: { file: '~/.zshrc · ~/.bashrc', lang: 'sh',
          body: `export PUB_HOSTED_URL={URL}/pub/\nexport FLUTTER_STORAGE_BASE_URL={URL}/pub/storage` },
        verify:     { lang: 'sh', body: `dart pub get --verbose` },
        paths: [
          { os: 'zsh',  path: '~/.zshrc' },
          { os: 'bash', path: '~/.bashrc' },
        ],
        tutorial: [
          'Set both PUB_HOSTED_URL (packages) and FLUTTER_STORAGE_BASE_URL (Flutter SDK).',
          'Reload with source ~/.zshrc.',
        ],
      },
    ],
  },
  {
    id: 'php', name: 'PHP', glyph: 'PH',
    managers: [
      {
        id: 'composer', name: 'Composer', hint: 'PHP package manager',
        quick:      { lang: 'sh', body: `composer config repositories.packagist composer {URL}/composer/` },
        persistent: { file: '~/.composer/config.json', lang: 'json',
          body: `{\n  "repositories": {\n    "packagist": {\n      "type": "composer",\n      "url": "{URL}/composer/"\n    }\n  }\n}` },
        verify:     { lang: 'sh', body: `composer diagnose` },
        paths: [
          { os: 'User',        path: '~/.composer/config.json' },
          { os: 'Per-project', path: './composer.json (repositories block)' },
        ],
        tutorial: [
          'composer config writes to ~/.composer/config.json by default.',
          'Pass --no-plugins if your project uses plugins that block source switching.',
        ],
      },
    ],
  },
];


// AI prompt — generated dynamically by app at copy time, depends on endpoint
function buildPrompt(endpoint, manager) {
  if (manager === 'all') {
    return `I have a local Depsilo cache proxy running at ${endpoint}. It mirrors all common package registries (pip, npm, Maven, Cargo, Go, Docker, Helm, etc).\n\nPlease detect every package manager used by my project and reconfigure each one to route through ${endpoint}. For each manager, edit the appropriate config file (~/.npmrc, ~/.cargo/config.toml, ~/.config/pip/pip.conf, ~/.m2/settings.xml, /etc/docker/daemon.json, etc) so all future installs use the cache. Show me each file you change, and run a verification install to confirm the cache is working.`;
  }
  // manager here is a language id (e.g. 'python', 'node')
  const lang = LANGUAGES.find(l => l.id === manager);
  const langName = lang?.name || manager;
  const mgrList = lang ? lang.managers.map(m => m.name).join(', ') : '';
  return `Configure my ${langName} project to use the local Depsilo cache proxy at ${endpoint}.\n\nDetect which package manager this project uses (${mgrList}) and edit its config file so installs route through ${endpoint}. Make the change persistent (not a one-shot env var). After editing, run a small test install to verify the cache is hit.`;
}

// One-click "all-in-one" shell script
function buildAllScript(endpoint) {
  const host = endpoint.replace(/^https?:\/\//, '');
  return `#!/usr/bin/env bash
# Depsilo — configure every package manager on this machine
set -euo pipefail
DEPSILO="${endpoint}"

# pip
mkdir -p ~/.config/pip
cat > ~/.config/pip/pip.conf <<EOF
[global]
index-url = $DEPSILO/pip/simple/
trusted-host = ${host}
EOF

# npm / pnpm / yarn
echo "registry=$DEPSILO/npm/" >> ~/.npmrc

# cargo
mkdir -p ~/.cargo
cat >> ~/.cargo/config.toml <<EOF
[source.crates-io]
replace-with = "depsilo"
[source.depsilo]
registry = "$DEPSILO/cargo/index"
EOF

# go
go env -w GOPROXY="$DEPSILO/go/,direct" GOSUMDB=off

# docker (requires sudo + restart)
echo '{ "registry-mirrors": ["'"$DEPSILO"'/docker/"] }' | sudo tee /etc/docker/daemon.json

echo "✓ All managers routed through $DEPSILO"`;
}

window.DEPSILO_DATA = { KPIS, MIRRORS, MANAGERS, LANGUAGES, genSeries, buildPrompt, buildAllScript };
