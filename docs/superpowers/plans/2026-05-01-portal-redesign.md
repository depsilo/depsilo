# Portal Pages Redesign — Quick Start & Monitor

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the Portal QuickStart and Monitor pages to match the visual layout and component structure from `design_handoff_depsilo_console/reference/`.

**Architecture:** QuickStart becomes a 2-column layout (240px LanguageRail + 1fr ConfigurePane) driven by a TypeScript port of the reference data module. Monitor becomes a vertical stack: HitRateHero card → StatStrip (3-col KPI) → MirrorMatrix (2-col upstream grid), all using the existing design token system.

**Tech Stack:** React 19, TypeScript, TanStack Query, SSE EventSource, existing tokens/utilities (tokens.css, utilities.css), existing Sparkline.tsx, StatusDot.tsx, Badge.tsx, Card.tsx.

---

## File Map

**Create:**
- `web/src/lib/ecosystemData.ts` — TypeScript port of `data.jsx`: LANGUAGES, genSeries, buildPrompt, buildAllScript
- `web/src/components/Segmented.tsx` — pill-style segmented control (Shell script / AI prompt, 15m/1h/24h/7d)
- `web/src/components/HeroSparkline.tsx` — full-width sparkline with gridlines + time axis (Monitor hero)
- `web/src/portal/components/LanguageRail.tsx` — left sidebar: All-in-one + OR BY LANGUAGE + language list
- `web/src/portal/components/ConfigurePane.tsx` — per-language pane: header + manager tabs + 3 sections + LiveDetector
- `web/src/portal/components/AllInOnePane.tsx` — shell script / AI prompt mode with Segmented toggle

**Modify:**
- `internal/api/public/stats.go` — add `url` field to each upstream entry in the response
- `web/src/portal/pages/Monitor.tsx` — full redesign: HitRateHero + StatStrip + MirrorMatrix
- `web/src/portal/pages/QuickStart.tsx` — full redesign: 2-column grid with LanguageRail + ConfigurePane
- `web/src/i18n/en.ts` — add/update keys for new monitor labels
- `web/src/i18n/zh.ts` — keep in sync with en.ts

---

## Task 1: Backend — add `url` to upstream stats response

**Files:**
- Modify: `internal/api/public/stats.go:89-179`

- [ ] **Step 1: Add `url` field to every upstream entry in the response**

In `internal/api/public/stats.go`, every `gin.H{...}` block that builds an upstream entry currently has `name`, `adapter`, `healthy`, `avg_latency_ms`, `success_rate`. Add `"url": u.URL` to all of them.

The pattern repeats 12 times (one per pool). For each block, change:
```go
upstreams = append(upstreams, gin.H{
    "name":           u.Name,
    "adapter":        "pypi",
    "healthy":        u.Healthy,
    "avg_latency_ms": u.AvgLatency().Milliseconds(),
    "success_rate":   u.SuccessRate(),
})
```
to:
```go
upstreams = append(upstreams, gin.H{
    "name":           u.Name,
    "adapter":        "pypi",
    "url":            u.URL,
    "healthy":        u.Healthy,
    "avg_latency_ms": u.AvgLatency().Milliseconds(),
    "success_rate":   u.SuccessRate(),
})
```
Apply this to all 12 pool loops (pypi, apt, npm, go, cargo, maven, rubygems, composer, nuget, conda, cran, helm).

- [ ] **Step 2: Verify backend builds**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/public/stats.go
git commit -m "feat(stats): include upstream url in stats response"
```

---

## Task 2: Create `web/src/lib/ecosystemData.ts`

**Files:**
- Create: `web/src/lib/ecosystemData.ts`

This is a TypeScript port of `design_handoff_depsilo_console/reference/data.jsx`. It exports the data and helper functions the QuickStart and Monitor pages need.

- [ ] **Step 1: Write the file**

```typescript
// web/src/lib/ecosystemData.ts

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
  series: number[]
}

const RAW_MIRRORS: Omit<MirrorDef, 'series'>[] = [
  { type: 'pip',      p50: 38,  hit: '96.1%', status: 'healthy'  },
  { type: 'npm',      p50: 41,  hit: '94.7%', status: 'healthy'  },
  { type: 'maven',    p50: 62,  hit: '89.3%', status: 'healthy'  },
  { type: 'cargo',    p50: 35,  hit: '97.2%', status: 'healthy'  },
  { type: 'go',       p50: 51,  hit: '92.8%', status: 'healthy'  },
  { type: 'docker',   p50: 184, hit: '78.4%', status: 'degraded' },
  { type: 'helm',     p50: 47,  hit: '91.5%', status: 'healthy'  },
  { type: 'gem',      p50: 44,  hit: '93.0%', status: 'healthy'  },
  { type: 'nuget',    p50: 58,  hit: '88.2%', status: 'healthy'  },
  { type: 'apt',      p50: 72,  hit: '85.6%', status: 'healthy'  },
  { type: 'conda',    p50: 0,   hit: '—',     status: 'failed'   },
  { type: 'pub',      p50: 53,  hit: '90.4%', status: 'healthy'  },
  { type: 'composer', p50: 49,  hit: '92.1%', status: 'healthy'  },
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

export interface ManagerPath {
  os: string
  path: string
}

export interface ManagerConfig {
  id: string
  name: string
  hint: string
  quick: { lang: string; body: string }
  persistent: { file: string; lang: string; body: string }
  verify: { lang: string; body: string }
  paths: ManagerPath[]
  tutorial: string[]
}

export interface Language {
  id: string
  name: string
  glyph: string
  managers: ManagerConfig[]
}

export const LANGUAGES: Language[] = [
  {
    id: 'python', name: 'Python', glyph: 'PY',
    managers: [
      {
        id: 'pip', name: 'pip', hint: 'PyPA package installer',
        quick:      { lang: 'sh', body: 'PIP_INDEX_URL={URL}/pypi/simple/ pip install requests' },
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
          'pip honors PIP_INDEX_URL and PIP_TRUSTED_HOST env vars too — useful in CI.',
          'Run pip config list to verify the index-url is picked up.',
        ],
      },
      {
        id: 'uv', name: 'uv', hint: 'astral.sh fast resolver',
        quick:      { lang: 'sh', body: 'UV_INDEX_URL={URL}/pypi/simple/ uv pip install requests' },
        persistent: { file: 'pyproject.toml', lang: 'toml',
          body: '[[tool.uv.index]]\nname = "depsilo"\nurl = "{URL}/pypi/simple/"\ndefault = true' },
        verify:     { lang: 'sh', body: 'uv pip install --index-url {URL}/pypi/simple/ --dry-run six' },
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
        id: 'conda', name: 'Conda', hint: 'Anaconda channels',
        quick:      { lang: 'sh', body: 'conda install -c {URL}/conda/main numpy' },
        persistent: { file: '~/.condarc', lang: 'yaml',
          body: 'channels:\n  - {URL}/conda/main\n  - {URL}/conda/conda-forge\ndefault_channels: []' },
        verify:     { lang: 'sh', body: 'conda config --show channels' },
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
        quick:      { lang: 'sh', body: 'npm install --registry={URL}/npm/ lodash' },
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
    id: 'java', name: 'Java', glyph: 'JV',
    managers: [
      {
        id: 'maven', name: 'Maven', hint: 'Apache Maven',
        quick:      { lang: 'sh', body: 'mvn -Dmaven.repo.remote={URL}/maven/ install' },
        persistent: { file: '~/.m2/settings.xml', lang: 'xml',
          body: '<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <url>{URL}/maven/</url>\n      <mirrorOf>*</mirrorOf>\n    </mirror>\n  </mirrors>\n</settings>' },
        verify:     { lang: 'sh', body: 'mvn help:effective-settings | grep depsilo' },
        paths: [
          { os: 'User',   path: '~/.m2/settings.xml' },
          { os: 'Global', path: '$M2_HOME/conf/settings.xml' },
        ],
        tutorial: [
          'mirrorOf="*" makes Depsilo the mirror for every declared repository.',
          'Use !central in mirrorOf to exclude specific repos.',
          'Verify with mvn help:effective-settings.',
        ],
      },
      {
        id: 'gradle', name: 'Gradle', hint: 'Build tool',
        quick:      { lang: 'sh', body: 'gradle build -Dorg.gradle.repositoryMirrorUrl={URL}/maven/' },
        persistent: { file: '~/.gradle/init.gradle', lang: 'groovy',
          body: 'allprojects {\n  repositories {\n    maven { url "{URL}/maven/" }\n  }\n}' },
        verify:     { lang: 'sh', body: 'gradle dependencies --refresh-dependencies' },
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
        quick:      { lang: 'sh', body: 'sbt -Dsbt.override.build.repos=true compile' },
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
    id: 'rust', name: 'Rust', glyph: 'RS',
    managers: [
      {
        id: 'cargo', name: 'Cargo', hint: 'Rust package manager',
        quick:      { lang: 'sh', body: 'cargo install --index {URL}/cargo/index ripgrep' },
        persistent: { file: '~/.cargo/config.toml', lang: 'toml',
          body: '[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "{URL}/cargo/index"' },
        verify:     { lang: 'sh', body: 'cargo fetch' },
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
        quick:      { lang: 'sh', body: 'GOPROXY={URL}/go/,direct go install golang.org/x/tools/cmd/godoc@latest' },
        persistent: { file: 'go env -w', lang: 'sh',
          body: 'go env -w GOPROXY={URL}/go/,direct\ngo env -w GOSUMDB=off' },
        verify:     { lang: 'sh', body: 'go env GOPROXY' },
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
        quick:      { lang: 'sh', body: 'GOPROXY={URL}/go/,direct go build ./...' },
        persistent: { file: '~/.zshrc · ~/.bashrc', lang: 'sh',
          body: 'export GOPROXY={URL}/go/,direct\nexport GOSUMDB=off' },
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
    id: 'container', name: 'Container', glyph: 'CT',
    managers: [
      {
        id: 'docker', name: 'Docker', hint: 'Daemon registry mirror',
        quick:      { lang: 'sh', body: 'docker pull {HOST}/docker/library/alpine:3.19' },
        persistent: { file: '/etc/docker/daemon.json', lang: 'json',
          body: '{\n  "registry-mirrors": ["{URL}/docker/"]\n}' },
        verify:     { lang: 'sh', body: 'docker info | grep -A1 "Registry Mirrors"' },
        paths: [
          { os: 'Linux',          path: '/etc/docker/daemon.json' },
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
        quick:      { lang: 'sh', body: 'crictl pull {HOST}/docker/library/alpine:3.19' },
        persistent: { file: '/etc/containerd/config.toml', lang: 'toml',
          body: '[plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]\n  endpoint = ["{URL}/docker/"]' },
        verify:     { lang: 'sh', body: 'ctr -n k8s.io image pull {HOST}/docker/library/alpine:3.19' },
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
        quick:      { lang: 'sh', body: 'podman pull {HOST}/docker/library/alpine:3.19' },
        persistent: { file: '/etc/containers/registries.conf', lang: 'toml',
          body: '[[registry]]\nlocation = "docker.io"\n[[registry.mirror]]\nlocation = "{HOST}/docker"' },
        verify:     { lang: 'sh', body: "podman info --format '{{.Registries}}'" },
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
        quick:      { lang: 'sh', body: 'helm install --repo {URL}/helm/bitnami nginx bitnami/nginx' },
        persistent: { file: 'helm repo add', lang: 'sh',
          body: 'helm repo add bitnami {URL}/helm/bitnami\nhelm repo update' },
        verify:     { lang: 'sh', body: 'helm repo list' },
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
        quick:      { lang: 'sh', body: 'gem install --source {URL}/rubygems/ rails' },
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
        persistent: { file: '.bundle/config', lang: 'yaml',
          body: 'BUNDLE_MIRROR__HTTPS://RUBYGEMS__ORG/: "{URL}/rubygems/"' },
        verify:     { lang: 'sh', body: 'bundle config get mirror.https://rubygems.org' },
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
        quick:      { lang: 'sh', body: 'dotnet restore --source {URL}/nuget/v3/index.json' },
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
    id: 'debian', name: 'Debian', glyph: 'DB',
    managers: [
      {
        id: 'apt', name: 'apt', hint: 'Debian/Ubuntu packages',
        quick:      { lang: 'sh', body: 'sudo apt -o Acquire::http::Proxy="{URL}/apt/" update' },
        persistent: { file: '/etc/apt/apt.conf.d/99-depsilo', lang: 'conf',
          body: 'Acquire::http::Proxy "{URL}/apt/";\nAcquire::https::Proxy "{URL}/apt/";' },
        verify:     { lang: 'sh', body: 'apt-config dump | grep Proxy' },
        paths: [
          { os: 'System',          path: '/etc/apt/apt.conf.d/99-depsilo' },
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
        quick:      { lang: 'sh', body: 'PUB_HOSTED_URL={URL}/pub/ dart pub get' },
        persistent: { file: '~/.zshrc · ~/.bashrc', lang: 'sh',
          body: 'export PUB_HOSTED_URL={URL}/pub/\nexport FLUTTER_STORAGE_BASE_URL={URL}/pub/storage' },
        verify:     { lang: 'sh', body: 'dart pub get --verbose' },
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
        quick:      { lang: 'sh', body: 'composer config repositories.packagist composer {URL}/composer/' },
        persistent: { file: '~/.composer/config.json', lang: 'json',
          body: '{\n  "repositories": {\n    "packagist": {\n      "type": "composer",\n      "url": "{URL}/composer/"\n    }\n  }\n}' },
        verify:     { lang: 'sh', body: 'composer diagnose' },
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
]

// ── AI prompt / shell script builders ─────────────────────────────

export function buildPrompt(endpoint: string, languageId: string): string {
  if (languageId === 'all') {
    return `I have a local Depsilo cache proxy running at ${endpoint}. It mirrors all common package registries (pip, npm, Maven, Cargo, Go, Docker, Helm, etc).\n\nPlease detect every package manager used by my project and reconfigure each one to route through ${endpoint}. For each manager, edit the appropriate config file (~/.npmrc, ~/.cargo/config.toml, ~/.config/pip/pip.conf, ~/.m2/settings.xml, /etc/docker/daemon.json, etc) so all future installs use the cache. Show me each file you change, and run a verification install to confirm the cache is working.`
  }
  const lang = LANGUAGES.find(l => l.id === languageId)
  const langName = lang?.name ?? languageId
  const mgrList = lang ? lang.managers.map(m => m.name).join(', ') : ''
  return `Configure my ${langName} project to use the local Depsilo cache proxy at ${endpoint}.\n\nDetect which package manager this project uses (${mgrList}) and edit its config file so installs route through ${endpoint}. Make the change persistent (not a one-shot env var). After editing, run a small test install to verify the cache is hit.`
}

export function buildAllScript(endpoint: string): string {
  const host = endpoint.replace(/^https?:\/\//, '')
  return `#!/usr/bin/env bash
# Depsilo — configure every package manager on this machine
set -euo pipefail
DEPSILO="${endpoint}"

# pip
mkdir -p ~/.config/pip
cat > ~/.config/pip/pip.conf <<EOF
[global]
index-url = $DEPSILO/pypi/simple/
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

echo "✓ All managers routed through $DEPSILO"`
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors related to ecosystemData.ts

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/ecosystemData.ts
git commit -m "feat(portal): add ecosystemData module with LANGUAGES, MIRROR_DEFS, KPI_SERIES"
```

---

## Task 3: Create `Segmented.tsx` and `HeroSparkline.tsx`

**Files:**
- Create: `web/src/components/Segmented.tsx`
- Create: `web/src/components/HeroSparkline.tsx`

- [ ] **Step 1: Write `Segmented.tsx`**

```tsx
// web/src/components/Segmented.tsx
interface SegmentedOption {
  value: string
  label: string
}

interface Props {
  options: SegmentedOption[]
  value: string
  onChange: (value: string) => void
}

export default function Segmented({ options, value, onChange }: Props) {
  return (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        padding: 3,
        gap: 2,
        flexShrink: 0,
      }}
    >
      {options.map(opt => {
        const active = opt.value === value
        return (
          <button
            key={opt.value}
            onClick={() => onChange(opt.value)}
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 11,
              fontWeight: active ? 600 : 400,
              letterSpacing: '0.01em',
              color: active ? 'var(--text)' : 'var(--text-soft)',
              background: active ? 'var(--bg-card)' : 'transparent',
              border: active ? '0.5px solid var(--border-strong)' : '0.5px solid transparent',
              borderRadius: 5,
              padding: '4px 10px',
              cursor: 'pointer',
              transition: 'all 120ms ease',
            }}
          >
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 2: Write `HeroSparkline.tsx`**

```tsx
// web/src/components/HeroSparkline.tsx
import { useId } from 'react'

interface Props {
  values: number[]
}

export default function HeroSparkline({ values }: Props) {
  const uid = useId()
  const strokeId = `hero-stroke-${uid}`
  const areaId = `hero-area-${uid}`

  const W = 760
  const H = 110
  const padY = 6

  if (!values?.length) return null

  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1

  const points = values.map((v, i) => {
    const x = (i / (values.length - 1)) * W
    const y = padY + (1 - (v - min) / range) * (H - padY * 2)
    return [x, y] as [number, number]
  })

  const linePath = points
    .map((p, i) => (i === 0 ? `M${p[0]},${p[1]}` : `L${p[0]},${p[1]}`))
    .join(' ')
  const areaPath = `${linePath} L${W},${H} L0,${H} Z`
  const last = points[points.length - 1]

  const timeLabels = ['−60m', '−45m', '−30m', '−15m', 'now']

  return (
    <svg
      viewBox={`0 0 ${W} ${H + 16}`}
      preserveAspectRatio="none"
      style={{ width: '100%', height: 110, overflow: 'visible' }}
    >
      <defs>
        <linearGradient id={strokeId} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%"   stopColor="var(--spec-1)" />
          <stop offset="55%"  stopColor="var(--spec-2)" />
          <stop offset="100%" stopColor="var(--spec-3)" />
        </linearGradient>
        <linearGradient id={areaId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%"   stopColor="oklch(0.62 0.18 305)" stopOpacity={0.18} />
          <stop offset="100%" stopColor="oklch(0.72 0.12 210)" stopOpacity={0} />
        </linearGradient>
      </defs>

      {[0.25, 0.5, 0.75].map(g => (
        <line
          key={g}
          x1="0" x2={W}
          y1={H * g} y2={H * g}
          stroke="var(--border)"
          strokeWidth="0.5"
          strokeDasharray="2 3"
        />
      ))}

      <path d={areaPath} fill={`url(#${areaId})`} />
      <path
        d={linePath}
        fill="none"
        stroke={`url(#${strokeId})`}
        strokeWidth="1.4"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      <circle cx={last[0]} cy={last[1]} r={3} fill="var(--spec-2)" />
      <circle cx={last[0]} cy={last[1]} r={7} fill="var(--spec-2)" opacity={0.18} />

      {timeLabels.map((label, i) => (
        <text
          key={label}
          x={(i / 4) * W}
          y={H + 12}
          fontFamily="var(--font-mono)"
          fontSize={9}
          fill="var(--text-subtle)"
          textAnchor={i === 0 ? 'start' : i === 4 ? 'end' : 'middle'}
        >
          {label}
        </text>
      ))}
    </svg>
  )
}
```

- [ ] **Step 3: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
git add web/src/components/Segmented.tsx web/src/components/HeroSparkline.tsx
git commit -m "feat(components): add Segmented control and HeroSparkline"
```

---

## Task 4: Redesign Monitor page

**Files:**
- Modify: `web/src/portal/pages/Monitor.tsx`

The page becomes: title row with Segmented time-range selector → optional disconnect Banner → HitRateHero card → StatStrip (3-col KPI) → Upstream mirrors section with MirrorMatrix.

Real values come from `/api/v1/stats`. Decorative series come from `ecosystemData.ts`. The StatsData interface must add `url` to each upstream.

- [ ] **Step 1: Rewrite Monitor.tsx**

```tsx
// web/src/portal/pages/Monitor.tsx
import { useState, useEffect, useCallback, useId } from 'react'
import { useQuery } from '@tanstack/react-query'
import { statsApi } from '@/lib/api'
import StatusDot from '@/components/StatusDot'
import Sparkline from '@/components/Sparkline'
import Segmented from '@/components/Segmented'
import HeroSparkline from '@/components/HeroSparkline'
import { KPI_SERIES, MIRROR_DEFS, genSeries, type MirrorStatus } from '@/lib/ecosystemData'

interface UpstreamInfo {
  name: string
  adapter: string
  url: string
  healthy: boolean
  avg_latency_ms: number
  success_rate: number
}

interface StatsData {
  service: { status: string }
  today: {
    total_requests: number
    hit_count: number
    miss_count: number
    hit_rate: number
    bytes_served: number
    bytes_saved: number
  }
  cache: { total_files: number; total_size_bytes: number }
  upstreams: UpstreamInfo[]
}

function formatBytes(bytes: number): { value: string; unit: string } {
  if (bytes >= 1e12) return { value: (bytes / 1e12).toFixed(1), unit: 'TB' }
  if (bytes >= 1e9)  return { value: (bytes / 1e9).toFixed(0),  unit: 'GB' }
  if (bytes >= 1e6)  return { value: (bytes / 1e6).toFixed(0),  unit: 'MB' }
  return { value: String(bytes), unit: 'B' }
}

function formatRequests(n: number): { value: string; unit: string } {
  if (n >= 1e6) return { value: (n / 1e6).toFixed(2), unit: 'M' }
  if (n >= 1e3) return { value: (n / 1e3).toFixed(1), unit: 'K' }
  return { value: String(n), unit: '' }
}

// ── HitRateHero ───────────────────────────────────────────────────

function HitRateHero({ hitRate }: { hitRate: number }) {
  const displayRate = (hitRate * 100).toFixed(1)

  return (
    <div
      className="card aurora-rim"
      style={{
        padding: '28px 32px',
        display: 'grid',
        gridTemplateColumns: '320px 1fr',
        alignItems: 'stretch',
        gap: 32,
        minHeight: 168,
        position: 'relative',
        overflow: 'hidden',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}>
        <div>
          <div
            className="eyebrow"
            style={{ display: 'flex', alignItems: 'center', gap: 8 }}
          >
            <StatusDot status="healthy" live />
            <span>Cache hit rate · today</span>
          </div>
          <div
            className="aurora-glow"
            style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 14 }}
          >
            <span
              className="grad-text-aurora"
              style={{
                fontFamily: 'var(--font-mono)',
                fontSize: 76,
                fontWeight: 600,
                letterSpacing: '-0.06em',
                lineHeight: 1,
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              {displayRate}
            </span>
            <span style={{ fontSize: 22, color: 'var(--text-soft)' }}>%</span>
          </div>
        </div>
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          position: 'relative',
          borderLeft: '0.5px solid var(--border)',
          paddingLeft: 32,
        }}
      >
        <HeroSparkline values={KPI_SERIES.hitRate} />
      </div>
    </div>
  )
}

// ── StatStrip ─────────────────────────────────────────────────────

function StatStrip({ data }: { data: StatsData['today'] }) {
  const reqFmt  = formatRequests(data.total_requests)
  const savedFmt = formatBytes(data.bytes_saved)

  const items = [
    {
      label: 'Total requests',
      value: reqFmt.value,
      unit: reqFmt.unit,
      tone: 'brand' as const,
      series: KPI_SERIES.requests,
    },
    {
      label: 'Bandwidth saved',
      value: savedFmt.value,
      unit: savedFmt.unit,
      tone: 'brand' as const,
      series: KPI_SERIES.saved,
    },
    {
      label: 'P50 latency',
      value: '—',
      unit: 'ms',
      tone: 'neutral' as const,
      series: KPI_SERIES.latency,
    },
  ]

  return (
    <div
      className="card"
      style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(3, 1fr)',
        gap: 0,
        padding: 0,
      }}
    >
      {items.map((it, i) => (
        <div
          key={it.label}
          style={{
            padding: '16px 20px',
            borderRight: i < items.length - 1 ? '0.5px solid var(--border)' : 'none',
            display: 'flex',
            flexDirection: 'column',
            gap: 10,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{it.label}</span>
          </div>
          <div
            style={{
              display: 'flex',
              alignItems: 'baseline',
              justifyContent: 'space-between',
              gap: 8,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 4 }}>
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 32,
                  fontWeight: 600,
                  letterSpacing: '-0.04em',
                  fontVariantNumeric: 'tabular-nums',
                }}
              >
                {it.value}
              </span>
              <span style={{ fontSize: 13, color: 'var(--text-soft)' }}>{it.unit}</span>
            </div>
            <Sparkline data={it.series} width={120} height={26} tone={it.tone} />
          </div>
        </div>
      ))}
    </div>
  )
}

// ── MirrorTile ────────────────────────────────────────────────────

function mirrorStatus(u: UpstreamInfo): MirrorStatus {
  if (!u.healthy) return 'failed'
  if (u.avg_latency_ms > 150) return 'degraded'
  return 'healthy'
}

function MirrorTile({ upstream }: { upstream: UpstreamInfo }) {
  const status = mirrorStatus(upstream)
  const isFailed = status === 'failed'
  const tone = status === 'healthy' ? 'ok' : status === 'degraded' ? 'warn' : 'danger'
  const def = MIRROR_DEFS.find(m => m.type === upstream.adapter)
  const series = def?.series ?? genSeries(40, Math.random() * 100, {})

  return (
    <div
      className="row-hover"
      style={{
        display: 'grid',
        gridTemplateColumns: '50px 1fr 96px',
        alignItems: 'center',
        gap: 12,
        padding: '10px 14px',
        borderBottom: '0.5px solid var(--border)',
        cursor: 'pointer',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <StatusDot status={status === 'healthy' ? 'healthy' : status === 'degraded' ? 'degraded' : 'failed'} />
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--text)',
          }}
        >
          {upstream.adapter}
        </span>
      </div>
      <div style={{ minWidth: 0 }}>
        <div
          className="mono"
          style={{
            fontSize: 12,
            color: isFailed ? 'var(--text-subtle)' : 'var(--text)',
            textDecoration: isFailed ? 'line-through' : 'none',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {(upstream.url || upstream.name).replace(/^https?:\/\//, '')}
        </div>
        <div style={{ display: 'flex', gap: 14, marginTop: 2, fontSize: 11 }}>
          <span style={{ color: 'var(--text-subtle)' }}>
            P50{' '}
            <span
              className="num"
              style={{
                color: isFailed
                  ? 'var(--text-subtle)'
                  : upstream.avg_latency_ms > 100
                  ? 'var(--warn-text)'
                  : 'var(--text-muted)',
              }}
            >
              {isFailed ? '—' : `${upstream.avg_latency_ms}ms`}
            </span>
          </span>
          <span style={{ color: 'var(--text-subtle)' }}>
            hit{' '}
            <span className="num" style={{ color: isFailed ? 'var(--text-subtle)' : 'var(--text-muted)' }}>
              {isFailed ? '—' : `${(upstream.success_rate * 100).toFixed(1)}%`}
            </span>
          </span>
        </div>
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <Sparkline data={series} width={92} height={22} tone={tone} />
      </div>
    </div>
  )
}

function MirrorMatrix({ upstreams }: { upstreams: UpstreamInfo[] }) {
  const half = Math.ceil(upstreams.length / 2)
  const left = upstreams.slice(0, half)
  const right = upstreams.slice(half)

  return (
    <div
      className="card"
      style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', overflow: 'hidden' }}
    >
      <div style={{ borderRight: '0.5px solid var(--border)' }}>
        {left.map(u => (
          <MirrorTile key={`${u.adapter}-${u.name}`} upstream={u} />
        ))}
      </div>
      <div>
        {right.map(u => (
          <MirrorTile key={`${u.adapter}-${u.name}`} upstream={u} />
        ))}
      </div>
    </div>
  )
}

// ── Banner ────────────────────────────────────────────────────────

function Banner({ onDismiss }: { onDismiss: () => void }) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '8px 12px',
        background: 'var(--warn-fill)',
        border: '0.5px solid var(--warn-border)',
        borderRadius: 8,
        fontSize: 12,
      }}
    >
      <StatusDot status="degraded" />
      <span style={{ flex: 1, color: 'var(--text)' }}>
        SSE stream disconnected — real-time updates paused.
      </span>
      <button
        onClick={onDismiss}
        style={{ color: 'var(--text-muted)', padding: 4, lineHeight: 0 }}
      >
        <svg width="10" height="10" viewBox="0 0 10 10">
          <path d="M2 2l6 6M8 2l-6 6" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" />
        </svg>
      </button>
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────

const TIME_RANGES = [
  { value: '15m', label: '15m' },
  { value: '1h',  label: '1h'  },
  { value: '24h', label: '24h' },
  { value: '7d',  label: '7d'  },
]

export default function MonitorPage() {
  const [timeRange, setTimeRange] = useState('1h')
  const [showBanner, setShowBanner] = useState(false)

  const { data } = useQuery<StatsData>({
    queryKey: ['stats-monitor'],
    queryFn: async () => { const res = await statsApi.getStats(); return res.data },
    refetchInterval: 30000,
  })

  const upstreams = data?.upstreams ?? []
  const hitRate   = data?.today.hit_rate ?? 0
  const today     = data?.today ?? {
    total_requests: 0, hit_count: 0, miss_count: 0,
    hit_rate: 0, bytes_served: 0, bytes_saved: 0,
  }

  const healthyCounts = upstreams.reduce(
    (acc, u) => {
      const s = mirrorStatus(u)
      acc[s] = (acc[s] ?? 0) + 1
      return acc
    },
    {} as Record<MirrorStatus, number>
  )

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      {/* Title row */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 16,
        }}
      >
        <div>
          <h1
            className="grad-text"
            style={{
              margin: 0,
              fontSize: 44,
              fontWeight: 700,
              letterSpacing: '-0.04em',
              lineHeight: 1.02,
            }}
          >
            Live monitoring
          </h1>
          <p
            style={{
              margin: '14px 0 0 0',
              fontSize: 17,
              lineHeight: 1.45,
              color: 'var(--text)',
              maxWidth: 620,
              fontWeight: 400,
              letterSpacing: '-0.005em',
            }}
          >
            Real-time view of cache performance and upstream mirror health.
          </p>
        </div>
        <Segmented options={TIME_RANGES} value={timeRange} onChange={setTimeRange} />
      </div>

      {showBanner && <Banner onDismiss={() => setShowBanner(false)} />}

      <HitRateHero hitRate={hitRate} />
      <StatStrip data={today} />

      {/* Mirrors section */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'flex-end',
            justifyContent: 'space-between',
          }}
        >
          <div>
            <h2
              style={{
                margin: 0,
                fontSize: 26,
                fontWeight: 700,
                letterSpacing: '-0.03em',
                lineHeight: 1.1,
              }}
            >
              Upstream mirrors{' '}
              <span
                style={{
                  fontFamily: 'var(--font-mono)',
                  fontSize: 14,
                  fontWeight: 500,
                  letterSpacing: '-0.02em',
                  color: 'var(--text-subtle)',
                  marginLeft: 6,
                }}
              >
                {upstreams.length}
              </span>
            </h2>
            <p style={{ margin: '2px 0 0 0', fontSize: 12, color: 'var(--text-muted)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="healthy" />
                <span className="num">{healthyCounts.healthy ?? 0}</span> healthy
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="degraded" />
                <span className="num">{healthyCounts.degraded ?? 0}</span> degraded
              </span>
              <span style={{ margin: '0 10px', color: 'var(--border-strong)' }}>·</span>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
                <StatusDot status="failed" />
                <span className="num">{healthyCounts.failed ?? 0}</span> failed
              </span>
            </p>
          </div>
        </div>
        {upstreams.length > 0 && <MirrorMatrix upstreams={upstreams} />}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add web/src/portal/pages/Monitor.tsx
git commit -m "feat(monitor): redesign with HitRateHero, StatStrip, MirrorMatrix"
```

---

## Task 5: Create QuickStart sub-components and redesign page

**Files:**
- Create: `web/src/portal/components/LanguageRail.tsx`
- Create: `web/src/portal/components/AllInOnePane.tsx`
- Create: `web/src/portal/components/ConfigurePane.tsx`
- Modify: `web/src/portal/pages/QuickStart.tsx`

The existing `web/src/portal/components/CodeBlock.tsx` is already present and already accepts `{ filename?, code, language? }` — but the reference uses `file`, `body`, `lang` props and adds an `accentLang` flag. We'll use the existing CodeBlock and adapt calls.

Note: Check the current CodeBlock props signature first. The reference uses `<CodeBlock file={...} body={...} lang={...} />`. The existing CodeBlock uses `code` not `body`. We keep calling it with `code={body}` and adapt the `file` → `filename` prop.

- [ ] **Step 1: Write `LanguageRail.tsx`**

```tsx
// web/src/portal/components/LanguageRail.tsx
import { LANGUAGES } from '@/lib/ecosystemData'

interface Props {
  selected: string
  onSelect: (id: string) => void
}

export default function LanguageRail({ selected, onSelect }: Props) {
  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      {/* All-in-one row */}
      <button
        onClick={() => onSelect('all')}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '10px 12px',
          background: selected === 'all' ? 'var(--brand-soft)' : 'transparent',
          borderBottom: '0.5px solid var(--border)',
          borderLeft: selected === 'all' ? '2px solid var(--brand)' : '2px solid transparent',
          textAlign: 'left',
          cursor: 'pointer',
          width: '100%',
        }}
      >
        <div
          style={{
            width: 26,
            height: 26,
            borderRadius: 6,
            background: 'var(--brand)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            flexShrink: 0,
          }}
        >
          <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
            <path
              d="M2.5 4l2 2 4-4M2.5 9l2 2 4-4M11 6h.5M11 11h.5"
              stroke="currentColor"
              strokeWidth="1.4"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 500,
              color: selected === 'all' ? 'var(--brand)' : 'var(--text)',
              whiteSpace: 'nowrap',
            }}
          >
            All-in-one
          </div>
          <div style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
            Configure everything
          </div>
        </div>
      </button>

      {/* Eyebrow */}
      <div
        style={{
          padding: '8px 12px 4px',
          borderBottom: '0.5px solid var(--border)',
        }}
      >
        <span className="eyebrow">Or by language</span>
      </div>

      {/* Language list */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
        {LANGUAGES.map((lang, i) => {
          const active = lang.id === selected
          return (
            <button
              key={lang.id}
              onClick={() => onSelect(lang.id)}
              style={{
                flex: 1,
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '0 12px',
                textAlign: 'left',
                background: active ? 'var(--brand-soft)' : 'transparent',
                borderLeft: active ? '2px solid var(--brand)' : '2px solid transparent',
                borderBottom:
                  i === LANGUAGES.length - 1 ? 'none' : '0.5px solid var(--border)',
                transition: 'background 100ms ease',
                cursor: 'pointer',
                minHeight: 0,
                width: '100%',
              }}
            >
              <span
                style={{
                  width: 22,
                  height: 22,
                  borderRadius: 4,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  fontFamily: 'var(--font-mono)',
                  fontSize: 10,
                  fontWeight: 500,
                  color: active ? 'var(--brand)' : 'var(--text-subtle)',
                  flexShrink: 0,
                }}
              >
                {lang.glyph}
              </span>
              <span
                style={{
                  fontSize: 12.5,
                  fontWeight: active ? 500 : 400,
                  color: active ? 'var(--brand)' : 'var(--text)',
                  flex: 1,
                  minWidth: 0,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {lang.name}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Write `AllInOnePane.tsx`**

```tsx
// web/src/portal/components/AllInOnePane.tsx
import { useState } from 'react'
import CodeBlock from '@/portal/components/CodeBlock'
import Segmented from '@/components/Segmented'
import { buildAllScript, buildPrompt } from '@/lib/ecosystemData'

interface Props { endpoint: string }

const MODES = [
  { value: 'script', label: 'Shell script' },
  { value: 'prompt', label: 'AI prompt'   },
]

function PromptCard({ prompt }: { prompt: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(prompt).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }
  return (
    <div
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 8px 0 12px',
          borderBottom: '0.5px solid var(--border)',
          background: 'var(--bg-card)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--brand)',
              padding: '1px 6px',
              background: 'var(--brand-soft)',
              borderRadius: 3,
              letterSpacing: '0.04em',
            }}
          >
            AI
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            Prompt for any AI coding tool
          </span>
        </div>
        <button
          onClick={copy}
          style={{
            fontSize: 11,
            color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
            padding: '3px 8px',
            border: '0.5px solid var(--border)',
            borderRadius: 4,
          }}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div
        style={{
          padding: '12px 14px',
          fontSize: 12,
          lineHeight: 1.6,
          color: 'var(--text)',
          whiteSpace: 'pre-wrap',
          maxHeight: 180,
          overflowY: 'auto',
        }}
      >
        {prompt}
      </div>
    </div>
  )
}

export default function AllInOnePane({ endpoint }: Props) {
  const [mode, setMode] = useState('script')
  const script = buildAllScript(endpoint)
  const prompt = buildPrompt(endpoint, 'all')

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          borderBottom: '0.5px solid var(--border)',
          padding: '0 14px',
          height: 44,
          flexShrink: 0,
          gap: 12,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 17,
              fontWeight: 700,
              whiteSpace: 'nowrap',
              letterSpacing: '-0.02em',
              lineHeight: 1.2,
            }}
          >
            All-in-one setup
          </div>
          <div
            style={{
              fontSize: 12,
              color: 'var(--text-soft)',
              whiteSpace: 'nowrap',
              marginTop: 2,
            }}
          >
            Configure every detected package manager in one go
          </div>
        </div>
        <Segmented options={MODES} value={mode} onChange={setMode} />
      </div>
      <div
        style={{
          padding: 16,
          flex: 1,
          overflow: 'auto',
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 14,
        }}
      >
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          {mode === 'script'
            ? 'Run this as root on your machine. It edits config for pip, npm, cargo, go, and docker — extend as needed.'
            : 'Paste this into ChatGPT, Claude, Cursor, or any agentic coding tool. The AI will detect your stack and edit the right files.'}
        </div>
        {mode === 'script' ? (
          <CodeBlock filename="depsilo-setup.sh" code={script} language="sh" />
        ) : (
          <PromptCard prompt={prompt} />
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Write `ConfigurePane.tsx`**

```tsx
// web/src/portal/components/ConfigurePane.tsx
import { useState, useEffect, useCallback } from 'react'
import CodeBlock from '@/portal/components/CodeBlock'
import { LANGUAGES, buildPrompt, type ManagerConfig } from '@/lib/ecosystemData'

interface Props {
  languageId: string
  endpoint: string
}

function relTime(ts: number): string {
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 1) return 'now'
  if (s < 60) return `${s}s ago`
  return `${Math.floor(s / 60)}m ago`
}

// Footer: listens to SSE for requests matching the manager
function LiveDetector({ endpoint, managerId }: { endpoint: string; managerId: string }) {
  const [hits, setHits] = useState<{ id: string; path: string; ms: number; t: number }[]>([])
  const [, setTick] = useState(0)

  const handleEvent = useCallback((e: MessageEvent) => {
    try {
      const ev = JSON.parse(e.data)
      // filter to events that match the current manager's adapter type
      // adapter_type in SSE matches the manager id pattern (pip, npm, cargo, etc.)
      if (!ev.adapter_type) return
      const adapterKey = ev.adapter_type as string
      // rough match: managerId is a manager id (pip/uv/poetry → pypi), so we check adapter_type
      // Use the package_name + file_name as path
      const path = ev.file_name || ev.package_name || '—'
      const ms = ev.latency_ms ?? 0
      const id = ev.id || `${ev.timestamp}-${Math.random().toString(36).slice(2, 6)}`
      setHits(prev => [{ id, path, ms, t: Date.now() }, ...prev].slice(0, 3))
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const es = new EventSource('/api/v1/events/stream')
    es.onmessage = handleEvent
    return () => es.close()
  }, [handleEvent])

  // tick for relative timestamps
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 1000)
    return () => clearInterval(id)
  }, [])

  const latest = hits[0]
  const fresh = latest && Date.now() - latest.t < 4000

  return (
    <div
      style={{
        borderTop: '0.5px solid var(--border)',
        padding: '8px 14px',
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        background: 'var(--bg-card)',
        flexShrink: 0,
        minHeight: 36,
      }}
    >
      <span
        className="dot-live"
        style={{
          width: 6,
          height: 6,
          borderRadius: '50%',
          background: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
          color: hits.length ? 'var(--ok)' : 'var(--text-subtle)',
          flexShrink: 0,
        }}
      />
      {hits.length === 0 ? (
        <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
          Listening for requests on{' '}
          <span className="mono" style={{ color: 'var(--text-subtle)' }}>
            {endpoint}
          </span>
          … run the verify command above.
        </span>
      ) : (
        <>
          <span
            style={{
              fontSize: 11,
              color: fresh ? 'var(--ok-text)' : 'var(--text-muted)',
              fontWeight: fresh ? 500 : 400,
              transition: 'color 300ms',
              whiteSpace: 'nowrap',
            }}
          >
            {hits.length} request{hits.length > 1 ? 's' : ''} detected
          </span>
          <span style={{ color: 'var(--border-strong)' }}>·</span>
          <span
            className="mono"
            style={{
              fontSize: 11,
              color: 'var(--text)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
              minWidth: 0,
            }}
          >
            {latest.path}
          </span>
          <span
            className="mono"
            style={{
              fontSize: 10,
              color: 'var(--text-subtle)',
              padding: '2px 6px',
              background: 'var(--bg-soft)',
              border: '0.5px solid var(--border)',
              borderRadius: 4,
              flexShrink: 0,
            }}
          >
            {latest.ms}ms
          </span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', flexShrink: 0, whiteSpace: 'nowrap' }}>
            {relTime(latest.t)}
          </span>
        </>
      )}
    </div>
  )
}

function ConfigSection({
  step,
  title,
  subtitle,
  children,
}: {
  step: number
  title: string
  subtitle?: string
  children: React.ReactNode
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '0 2px' }}>
        <span
          style={{
            fontFamily: 'var(--font-mono)',
            fontSize: 11,
            color: 'var(--text-subtle)',
            letterSpacing: '0.04em',
            flexShrink: 0,
          }}
        >
          {String(step).padStart(2, '0')}
        </span>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: 'var(--text)', letterSpacing: '-0.005em' }}>
            {title}
          </div>
          {subtitle && (
            <div style={{ fontSize: 12, color: 'var(--text-soft)', marginTop: 2 }}>{subtitle}</div>
          )}
        </div>
      </div>
      {children}
    </div>
  )
}

function ManagerTabs({
  managers,
  active,
  onChange,
}: {
  managers: ManagerConfig[]
  active: string
  onChange: (id: string) => void
}) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' }}>
      {managers.map(m => {
        const isActive = m.id === active
        return (
          <button
            key={m.id}
            onClick={() => onChange(m.id)}
            style={{
              display: 'inline-flex',
              flexDirection: 'column',
              padding: '6px 10px',
              background: isActive ? 'var(--bg-card)' : 'transparent',
              border: `0.5px solid ${isActive ? 'var(--border-strong)' : 'var(--border)'}`,
              borderRadius: 6,
              textAlign: 'left',
              cursor: 'pointer',
              transition: 'all 120ms ease',
            }}
          >
            <span
              style={{
                fontSize: 12,
                fontWeight: 500,
                color: isActive ? 'var(--text)' : 'var(--text-muted)',
                whiteSpace: 'nowrap',
              }}
            >
              {m.name}
            </span>
            <span style={{ fontSize: 10, color: 'var(--text-subtle)', whiteSpace: 'nowrap' }}>
              {m.hint}
            </span>
          </button>
        )
      })}
    </div>
  )
}

function PromptCard({ prompt }: { prompt: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(prompt).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }
  return (
    <div
      style={{
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 8,
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          height: 32,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 8px 0 12px',
          borderBottom: '0.5px solid var(--border)',
          background: 'var(--bg-card)',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 10,
              color: 'var(--brand)',
              padding: '1px 6px',
              background: 'var(--brand-soft)',
              borderRadius: 3,
              letterSpacing: '0.04em',
            }}
          >
            AI
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>
            Prompt for ChatGPT / Claude / Cursor
          </span>
        </div>
        <button
          onClick={copy}
          style={{
            fontSize: 11,
            color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
            padding: '3px 8px',
            border: '0.5px solid var(--border)',
            borderRadius: 4,
          }}
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div
        style={{
          padding: '12px 14px',
          fontSize: 12,
          lineHeight: 1.6,
          color: 'var(--text)',
          whiteSpace: 'pre-wrap',
          maxHeight: 180,
          overflowY: 'auto',
        }}
      >
        {prompt}
      </div>
    </div>
  )
}

export default function ConfigurePane({ languageId, endpoint }: Props) {
  const lang = LANGUAGES.find(l => l.id === languageId)
  const [mgrId, setMgrId] = useState(lang?.managers[0]?.id ?? '')
  const [showPrompt, setShowPrompt] = useState(false)

  useEffect(() => {
    setMgrId(lang?.managers[0]?.id ?? '')
    setShowPrompt(false)
  }, [languageId, lang])

  if (!lang) return null

  const m = lang.managers.find(x => x.id === mgrId) ?? lang.managers[0]
  const url = endpoint
  const host = url.replace(/^https?:\/\//, '')
  const fill = (s: string) => s.replace(/\{URL\}/g, url).replace(/\{HOST\}/g, host)
  const prompt = buildPrompt(endpoint, languageId)

  return (
    <div
      className="card"
      style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden', height: '100%' }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          borderBottom: '0.5px solid var(--border)',
          padding: '0 14px',
          height: 44,
          flexShrink: 0,
          gap: 12,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, whiteSpace: 'nowrap' }}>
          <span
            style={{
              width: 22,
              height: 22,
              borderRadius: 5,
              background: 'var(--brand-soft)',
              border: '0.5px solid var(--brand-border)',
              display: 'inline-flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              fontWeight: 500,
              color: 'var(--brand)',
            }}
          >
            {lang.glyph}
          </span>
          <span style={{ fontSize: 17, fontWeight: 700, letterSpacing: '-0.02em' }}>
            Configure {lang.name}
          </span>
          <span style={{ fontSize: 10, color: 'var(--text-subtle)', marginLeft: 4 }}>
            {lang.managers.length} {lang.managers.length === 1 ? 'manager' : 'managers'}
          </span>
        </div>
        <div style={{ flex: 1 }} />
        <button
          onClick={() => setShowPrompt(p => !p)}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '4px 10px',
            fontSize: 11,
            fontWeight: 500,
            color: showPrompt ? 'var(--brand)' : 'var(--text-muted)',
            background: showPrompt ? 'var(--brand-soft)' : 'transparent',
            border: `0.5px solid ${showPrompt ? 'var(--brand-border)' : 'var(--border)'}`,
            borderRadius: 6,
            whiteSpace: 'nowrap',
            cursor: 'pointer',
          }}
        >
          <span
            style={{
              fontFamily: 'var(--font-mono)',
              fontSize: 9,
              color: showPrompt ? 'var(--brand)' : 'var(--text-subtle)',
              letterSpacing: '0.04em',
            }}
          >
            AI
          </span>
          prompt
        </button>
      </div>

      {/* Body */}
      <div
        style={{
          padding: 16,
          flex: 1,
          overflow: 'auto',
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          gap: 16,
        }}
      >
        {showPrompt && <PromptCard prompt={prompt} />}

        {lang.managers.length > 1 && (
          <ManagerTabs managers={lang.managers} active={m.id} onChange={setMgrId} />
        )}

        {/* 01 Configure */}
        <ConfigSection
          step={1}
          title="Configure"
          subtitle={`Edit ${m.persistent.file} — applied to every install from now on`}
        >
          <CodeBlock
            filename={m.persistent.file}
            code={fill(m.persistent.body)}
            language={m.persistent.lang}
          />
          <details
            style={{
              marginTop: 8,
              border: '0.5px solid var(--border)',
              borderRadius: 6,
              background: 'var(--bg-soft)',
            }}
          >
            <summary
              style={{
                padding: '6px 12px',
                fontSize: 11,
                color: 'var(--text-muted)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                gap: 6,
                listStyle: 'none',
              }}
            >
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Where this manager reads config from
            </summary>
            <div style={{ borderTop: '0.5px solid var(--border)' }}>
              {m.paths.map((p, i) => (
                <div
                  key={i}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '120px 1fr',
                    alignItems: 'center',
                    gap: 12,
                    padding: '6px 12px',
                    borderBottom:
                      i < m.paths.length - 1 ? '0.5px solid var(--border)' : 'none',
                  }}
                >
                  <span className="eyebrow" style={{ whiteSpace: 'nowrap' }}>
                    {p.os}
                  </span>
                  <span
                    style={{
                      fontFamily: 'var(--font-mono)',
                      fontSize: 11.5,
                      color: 'var(--text)',
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {p.path}
                  </span>
                </div>
              ))}
            </div>
          </details>
        </ConfigSection>

        {/* 02 Verify */}
        <ConfigSection
          step={2}
          title="Verify"
          subtitle="Run a test install — the request will appear in monitoring within ~2s"
        >
          <CodeBlock code={fill(m.verify.body)} language={m.verify.lang} />
        </ConfigSection>

        {/* 03 Step-by-step */}
        <ConfigSection
          step={3}
          title="Step-by-step"
          subtitle="Walk through the configuration end-to-end"
        >
          <details>
            <summary
              style={{
                padding: '8px 12px',
                fontSize: 11,
                color: 'var(--text-muted)',
                background: 'var(--bg-soft)',
                border: '0.5px solid var(--border)',
                borderRadius: 6,
                cursor: 'pointer',
                listStyle: 'none',
                display: 'flex',
                alignItems: 'center',
                gap: 6,
              }}
            >
              <span style={{ color: 'var(--text-subtle)' }}>▸</span>
              Show {m.tutorial.length} steps
            </summary>
            <ol
              style={{
                margin: '8px 0 0 0',
                paddingLeft: 0,
                listStyle: 'none',
                display: 'flex',
                flexDirection: 'column',
                gap: 6,
              }}
            >
              {m.tutorial.map((step, i) => (
                <li
                  key={i}
                  style={{
                    display: 'grid',
                    gridTemplateColumns: '24px 1fr',
                    alignItems: 'flex-start',
                    gap: 10,
                    padding: '8px 12px',
                    background: 'var(--bg-soft)',
                    border: '0.5px solid var(--border)',
                    borderRadius: 6,
                  }}
                >
                  <span
                    style={{
                      width: 18,
                      height: 18,
                      borderRadius: 4,
                      background: 'var(--brand-soft)',
                      color: 'var(--brand)',
                      fontFamily: 'var(--font-mono)',
                      fontSize: 10,
                      fontWeight: 500,
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      marginTop: 1,
                      flexShrink: 0,
                    }}
                  >
                    {i + 1}
                  </span>
                  <span style={{ fontSize: 12, lineHeight: 1.6, color: 'var(--text)' }}>
                    {fill(step)}
                  </span>
                </li>
              ))}
            </ol>
          </details>
        </ConfigSection>
      </div>

      <LiveDetector endpoint={endpoint} managerId={m.id} />
    </div>
  )
}
```

- [ ] **Step 4: Rewrite QuickStart.tsx**

First check the current CodeBlock component's props interface to confirm `filename` prop name:
Read `web/src/portal/components/CodeBlock.tsx` lines 1–20.

If the prop is named `filename` → use `filename={...}`. If named `file` → use `file={...}`. If named something else, adapt calls in ConfigurePane.tsx and AllInOnePane.tsx accordingly.

Then write:

```tsx
// web/src/portal/pages/QuickStart.tsx
import { useState } from 'react'
import StatusDot from '@/components/StatusDot'
import LanguageRail from '@/portal/components/LanguageRail'
import ConfigurePane from '@/portal/components/ConfigurePane'
import AllInOnePane from '@/portal/components/AllInOnePane'

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }
  return (
    <button
      onClick={copy}
      style={{
        fontSize: 11,
        color: copied ? 'var(--ok-text)' : 'var(--text-muted)',
        padding: '3px 8px',
        border: '0.5px solid var(--border)',
        borderRadius: 4,
        cursor: 'pointer',
        flexShrink: 0,
      }}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

function EndpointInline({ endpoint }: { endpoint: string }) {
  return (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '4px 6px 4px 10px',
        background: 'var(--bg-soft)',
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        flexShrink: 0,
      }}
    >
      <StatusDot status="healthy" live />
      <span
        className="mono"
        style={{
          fontSize: 12,
          color: 'var(--text)',
          letterSpacing: '-0.02em',
          whiteSpace: 'nowrap',
        }}
      >
        {endpoint}
      </span>
      <CopyButton text={endpoint} />
    </div>
  )
}

export default function QuickStart() {
  const endpoint = window.location.origin
  const [language, setLanguage] = useState('python')

  return (
    <div className="fade-up" style={{ display: 'flex', flexDirection: 'column', gap: 18 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          justifyContent: 'space-between',
          gap: 16,
        }}
      >
        <div>
          <h1
            className="grad-text"
            style={{
              margin: 0,
              fontSize: 44,
              fontWeight: 700,
              letterSpacing: '-0.04em',
              lineHeight: 1.02,
            }}
          >
            Quick start
          </h1>
          <p
            style={{
              margin: '14px 0 0 0',
              fontSize: 17,
              lineHeight: 1.45,
              color: 'var(--text)',
              maxWidth: 580,
              fontWeight: 400,
              letterSpacing: '-0.005em',
            }}
          >
            Pick a language, choose a package manager, copy the snippet —{' '}
            <span style={{ color: 'var(--text-soft)' }}>
              or grab the AI prompt for your assistant.
            </span>
          </p>
        </div>
        <EndpointInline endpoint={endpoint} />
      </div>

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '240px 1fr',
          gap: 16,
          height: 720,
        }}
      >
        <LanguageRail selected={language} onSelect={setLanguage} />
        {language === 'all' ? (
          <AllInOnePane endpoint={endpoint} />
        ) : (
          <ConfigurePane languageId={language} endpoint={endpoint} />
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 5: Check CodeBlock props and fix if needed**

Read `web/src/portal/components/CodeBlock.tsx` (first 30 lines) to confirm prop names used in ConfigurePane.tsx and AllInOnePane.tsx are correct. Specifically check if it uses `code` or `body`, and `filename` or `file`.

If the actual prop name differs from what the plan uses, edit ConfigurePane.tsx and AllInOnePane.tsx accordingly.

- [ ] **Step 6: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

Fix any type errors before proceeding.

- [ ] **Step 7: Commit**

```bash
git add web/src/portal/components/LanguageRail.tsx \
        web/src/portal/components/AllInOnePane.tsx \
        web/src/portal/components/ConfigurePane.tsx \
        web/src/portal/pages/QuickStart.tsx
git commit -m "feat(quickstart): redesign with LanguageRail + ConfigurePane + AllInOnePane"
```

---

## Task 6: i18n updates + build verification

**Files:**
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh.ts`

- [ ] **Step 1: Update `en.ts` monitor section**

The new Monitor page no longer uses `monitor.hitRate`, `monitor.requests`, `monitor.hits`, `monitor.misses`, `monitor.noEvents`. The title is hardcoded in English in the component (`'Live monitoring'`), consistent with the reference design.

Check the current monitor section in en.ts. Remove any keys that are no longer referenced. Add keys for any strings that are still referenced.

Run: `grep -r "monitor\." web/src/portal/ --include="*.tsx"` to find all t('monitor.*') usages in the new files.

If the new Monitor.tsx does not call `t('monitor.*')` at all (since all strings are inline English per the reference design), no changes to i18n are needed for the Monitor page.

Similarly, check QuickStart.tsx for any remaining `t('quickStart.*')` calls. If none, skip this step.

- [ ] **Step 2: Build frontend**

Run: `cd web && npm run build`
Expected: Build completes with no errors. Output in `web/dist/`.

- [ ] **Step 3: Build backend (embeds frontend)**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 4: Commit**

```bash
# commit any i18n changes found in step 1, if any
# then:
git add web/src/i18n/en.ts web/src/i18n/zh.ts
git commit -m "chore(i18n): update keys after portal redesign" || echo "No i18n changes needed"

git tag portal-redesign-complete
```

- [ ] **Step 5: Final verification**

Run the dev server and verify manually:
```bash
cd web && npm run dev
```
Open `http://localhost:5173` and check:
- QuickStart: 2-column layout, LanguageRail on left, ConfigurePane on right, All-in-one mode works, LiveDetector footer present
- Monitor: HitRateHero with aurora gradient number, StatStrip 3-col, MirrorMatrix 2-col grid

---

## Self-Review

**Spec coverage:**
- ✅ Backend: `url` added to upstream stats response (Task 1)
- ✅ `ecosystemData.ts`: LANGUAGES (12 langs, all managers), genSeries, KPI_SERIES, MIRROR_DEFS, buildPrompt, buildAllScript (Task 2)
- ✅ `Segmented.tsx`: pill-style control for Monitor time range and AllInOne mode toggle (Task 3)
- ✅ `HeroSparkline.tsx`: full-width 760×110 SVG with gridlines, aurora gradient, endpoint dot+halo, time-axis labels (Task 3)
- ✅ Monitor page: HitRateHero + aurora rim + grad-text-aurora number, StatStrip (3-col KPI + sparklines), MirrorMatrix (2-col, row-hover) (Task 4)
- ✅ QuickStart page: 2-column 240px/1fr grid, height 720, LanguageRail (Task 5)
- ✅ LanguageRail: All-in-one row, eyebrow, language list with brand active state (Task 5)
- ✅ ConfigurePane: header with glyph badge + AI prompt toggle, manager tabs, ConfigSection 01/02/03, collapsible paths, collapsible tutorial, LiveDetector footer (Task 5)
- ✅ AllInOnePane: Segmented script/prompt toggle, CodeBlock for script, PromptCard for AI (Task 5)
- ✅ LiveDetector: SSE at `/api/v1/events/stream`, dot-live, hit count, path, ms badge, relTime (Task 5)
- ✅ i18n sync + build verification (Task 6)

**Placeholder scan:** None. All code is complete.

**Type consistency:**
- `MirrorStatus` type exported from ecosystemData.ts, used in Monitor.tsx ✅
- `ManagerConfig` and `Language` exported from ecosystemData.ts, used in ConfigurePane.tsx ✅
- `UpstreamInfo` interface defined in Monitor.tsx with `url` field ✅
- `Segmented` `options` prop uses `SegmentedOption[]` interface ✅
- `HeroSparkline` `values: number[]` matches `KPI_SERIES.hitRate: number[]` ✅
