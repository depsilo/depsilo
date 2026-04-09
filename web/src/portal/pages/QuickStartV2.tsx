import { useMemo, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import EcosystemIcon from '@/components/EcosystemIcon'
import CodeBlockV2 from '@/portal/components/CodeBlockV2'

type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer' | 'nuget' | 'conda' | 'cran' | 'helm'

// ── Format generators ──────────────────────────────────────────────

function genMarkdown(u: string, h: string): string {
  return `# Package Proxy Configuration (Depsilo)

> This project uses **Depsilo** (\`${u}\`) as the package proxy cache.
> When installing any dependencies, you **MUST** use the proxy URLs below instead of the default public registries.

## Proxy Address

\`${u}\`

## Configuration by Ecosystem

### Python (pip / uv / Poetry)
\`\`\`bash
pip install <package> -i ${u}/pypi/simple/ --trusted-host ${h}
uv pip install <package> --index-url ${u}/pypi/simple/
\`\`\`

### APT (Debian / Ubuntu)
\`\`\`
deb ${u}/apt/ubuntu noble main restricted universe multiverse
\`\`\`

### npm / yarn / pnpm
\`\`\`bash
npm config set registry ${u}/npm/
\`\`\`

### Go Modules
\`\`\`bash
go env -w GOPROXY=${u}/go,direct
\`\`\`

### Cargo (Rust)
\`\`\`toml
# ~/.cargo/config.toml
[source.crates-io]
replace-with = "depsilo"
[source.depsilo]
registry = "sparse+${u}/crates/"
\`\`\`

### Maven / Gradle
\`\`\`xml
<mirror><id>depsilo</id><mirrorOf>central</mirrorOf><url>${u}/maven/</url></mirror>
\`\`\`

### RubyGems
\`\`\`bash
bundle config mirror.https://rubygems.org ${u}/rubygems/
\`\`\`

### Composer (PHP)
\`\`\`bash
composer config -g repo.packagist composer ${u}/composer/
\`\`\`

### NuGet (.NET)
\`\`\`bash
dotnet nuget add source ${u}/nuget/v3/index.json -n depsilo
\`\`\`

### Conda
\`\`\`yaml
channels:
  - ${u}/conda/pkgs/main
  - defaults
\`\`\`

### CRAN (R)
\`\`\`r
options(repos = c(CRAN = "${u}/cran/"))
\`\`\`

### Helm
\`\`\`bash
helm repo add depsilo ${u}/helm/
\`\`\`

## Docker Build
\`\`\`bash
docker build --build-arg PIP_INDEX_URL=${u}/pypi/simple/ --build-arg PIP_TRUSTED_HOST=${h} -t myapp .
\`\`\`

## Rules
- **Always** use the Depsilo proxy URLs when running install commands.
- **Never** use default public registries (pypi.org, registry.npmjs.org, etc.) directly.
`
}

function genEnvVars(u: string, h: string): string {
  return `# Depsilo Proxy — Environment Variables
# Add to .env, shell profile (~/.bashrc), or CI/CD environment

# Python (pip / uv / Poetry)
PIP_INDEX_URL=${u}/pypi/simple/
PIP_TRUSTED_HOST=${h}
UV_INDEX_URL=${u}/pypi/simple/

# npm / yarn / pnpm
npm_config_registry=${u}/npm/

# Go Modules
GOPROXY=${u}/go,direct

# Conda
CONDA_CHANNELS=${u}/conda/pkgs/main

# NuGet
NUGET_SOURCE=${u}/nuget/v3/index.json

# Helm
HELM_REPO_URL=${u}/helm/

# Docker Build (pass as --build-arg)
# PIP_INDEX_URL, PIP_TRUSTED_HOST, npm_config_registry, GOPROXY
`
}

function genShellScript(u: string, h: string): string {
  return `#!/usr/bin/env bash
# Depsilo Proxy — One-click setup script
# Run: curl -fsSL ${u}/setup.sh | bash
# Or copy this script and execute it locally.

set -e
PROXY="${u}"
HOST="${h}"

echo "🔧 Configuring package managers to use Depsilo proxy: $PROXY"

# pip
mkdir -p ~/.config/pip
cat > ~/.config/pip/pip.conf << EOF
[global]
index-url = $PROXY/pypi/simple/
trusted-host = $HOST
EOF
echo "  ✓ pip configured"

# npm
npm config set registry "$PROXY/npm/" 2>/dev/null && echo "  ✓ npm configured" || true

# Go
go env -w GOPROXY="$PROXY/go,direct" 2>/dev/null && echo "  ✓ Go configured" || true

# Cargo
mkdir -p ~/.cargo
if ! grep -q 'source.depsilo' ~/.cargo/config.toml 2>/dev/null; then
  cat >> ~/.cargo/config.toml << EOF

[source.crates-io]
replace-with = "depsilo"

[source.depsilo]
registry = "sparse+$PROXY/crates/"
EOF
  echo "  ✓ Cargo configured"
fi

# Conda
if command -v conda &>/dev/null; then
  conda config --add channels "$PROXY/conda/pkgs/main" 2>/dev/null
  echo "  ✓ Conda configured"
fi

# Composer
if command -v composer &>/dev/null; then
  composer config -g repo.packagist composer "$PROXY/composer/" 2>/dev/null
  echo "  ✓ Composer configured"
fi

# RubyGems
if command -v bundle &>/dev/null; then
  bundle config mirror.https://rubygems.org "$PROXY/rubygems/" 2>/dev/null
  echo "  ✓ RubyGems configured"
fi

echo ""
echo "✅ Done! All available package managers are now using Depsilo."
`
}

function genClaudeSkill(u: string, h: string): string {
  return `---
name: setup-depsilo-proxy
description: Configure all package managers to use the Depsilo proxy cache at ${u}
---

Configure the following package manager proxies for this project.
Apply the relevant ones based on the project's tech stack.

**Proxy base URL:** \`${u}\`

Python (pip):
\`\`\`
pip install <pkg> -i ${u}/pypi/simple/ --trusted-host ${h}
\`\`\`
Or set env: \`PIP_INDEX_URL=${u}/pypi/simple/ PIP_TRUSTED_HOST=${h}\`

npm:
\`\`\`
npm config set registry ${u}/npm/
\`\`\`

Go:
\`\`\`
go env -w GOPROXY=${u}/go,direct
\`\`\`

Cargo (~/.cargo/config.toml):
\`\`\`toml
[source.crates-io]
replace-with = "depsilo"
[source.depsilo]
registry = "sparse+${u}/crates/"
\`\`\`

Maven (~/.m2/settings.xml):
\`\`\`xml
<mirror><id>depsilo</id><mirrorOf>central</mirrorOf><url>${u}/maven/</url></mirror>
\`\`\`

Composer: \`composer config -g repo.packagist composer ${u}/composer/\`
RubyGems: \`bundle config mirror.https://rubygems.org ${u}/rubygems/\`
NuGet: \`dotnet nuget add source ${u}/nuget/v3/index.json -n depsilo\`
Conda: add \`${u}/conda/pkgs/main\` to channels in ~/.condarc
CRAN: \`options(repos = c(CRAN = "${u}/cran/"))\`
Helm: \`helm repo add depsilo ${u}/helm/\`

Docker build args:
\`\`\`
--build-arg PIP_INDEX_URL=${u}/pypi/simple/
--build-arg PIP_TRUSTED_HOST=${h}
--build-arg npm_config_registry=${u}/npm/
--build-arg GOPROXY=${u}/go,direct
\`\`\`
`
}

function genMCPConfig(u: string): string {
  return `{
  "mcpServers": {
    "depsilo": {
      "command": "npx",
      "args": ["-y", "@anthropic/proxy-mcp-server"],
      "env": {
        "DEPSILO_URL": "${u}",
        "PIP_INDEX_URL": "${u}/pypi/simple/",
        "npm_config_registry": "${u}/npm/",
        "GOPROXY": "${u}/go,direct"
      }
    }
  }
}

// ──────────────────────────────────────────────
// NOTE: Depsilo itself is not an MCP server.
// This config injects Depsilo proxy env vars
// into any MCP server's execution environment.
//
// Usage:
//   Claude Code  → .claude/settings.json
//   Cursor       → .cursor/mcp.json
//   Generic      → mcp_config.json
//
// Any MCP tool server that runs shell commands
// (install deps, build, test) will automatically
// use the Depsilo proxy via these env vars.
// ──────────────────────────────────────────────
`
}

function genDevcontainer(u: string, h: string): string {
  return `// .devcontainer/devcontainer.json
// Injects Depsilo proxy env vars into dev containers,
// Codespaces, and any devcontainer-compatible IDE.
{
  "name": "Dev Container with Depsilo Proxy",
  "remoteEnv": {
    "PIP_INDEX_URL": "${u}/pypi/simple/",
    "PIP_TRUSTED_HOST": "${h}",
    "npm_config_registry": "${u}/npm/",
    "GOPROXY": "${u}/go,direct",
    "NUGET_SOURCE": "${u}/nuget/v3/index.json"
  },
  "postCreateCommand": "echo '✅ Depsilo proxy configured via env vars'"

  // Works with:
  //   VS Code Dev Containers
  //   GitHub Codespaces
  //   JetBrains Gateway
  //   DevPod
  //   Gitpod (use .gitpod.yml for Gitpod-native)
}
`
}

// ── Format type definitions ────────────────────────────────────────

type FormatKey = 'markdown' | 'env' | 'shell' | 'skill' | 'mcp' | 'devcontainer'

interface FormatDef {
  key: FormatKey
  icon: string
  generate: (u: string, h: string) => string
}

const FORMATS: FormatDef[] = [
  { key: 'markdown', icon: 'description',     generate: genMarkdown },
  { key: 'env',      icon: 'data_object',     generate: genEnvVars },
  { key: 'shell',    icon: 'terminal',        generate: genShellScript },
  { key: 'skill',    icon: 'auto_awesome',    generate: genClaudeSkill },
  { key: 'mcp',      icon: 'hub',             generate: genMCPConfig },
  { key: 'devcontainer', icon: 'deployed_code', generate: genDevcontainer },
]

// ── AI Integration Panel ───────────────────────────────────────────

function AIInstructionsCard({ baseURL, host }: { baseURL: string; host: string }) {
  const { t } = useTranslation()
  const [activeFormat, setActiveFormat] = useState<FormatKey>('markdown')
  const [copied, setCopied] = useState(false)
  const [expanded, setExpanded] = useState(false)

  const content = useMemo(() => {
    const fmt = FORMATS.find(f => f.key === activeFormat)!
    return fmt.generate(baseURL, host)
  }, [activeFormat, baseURL, host])

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(content).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [content])

  const formatLabel = (k: FormatKey) => t(`quickstart.fmt_${k}`)
  const formatDesc = (k: FormatKey) => t(`quickstart.fmtDesc_${k}`)

  return (
    <div
      className="rounded-[6px] overflow-hidden"
      style={{ background: 'var(--surface)', border: '1px solid var(--border)', boxShadow: 'var(--shadow-soft)' }}
    >
      {/* Header */}
      <div className="px-5 py-4 flex items-start gap-3">
        <span
          className="flex items-center justify-center w-9 h-9 rounded-[6px] shrink-0 mt-0.5"
          style={{ background: 'linear-gradient(135deg, rgba(83,58,253,0.12), rgba(249,107,238,0.10))' }}
        >
          <Icon name="auto_awesome" size="sm" style={{ color: 'var(--stripe-purple)' }} />
        </span>
        <div className="flex-1 min-w-0">
          <p className="font-[400] text-[15px]" style={{ color: 'var(--heading)' }}>
            {t('quickstart.aiInstructionsTitle')}
          </p>
          <p className="text-[13px] mt-0.5 leading-relaxed" style={{ color: 'var(--body)' }}>
            {t('quickstart.aiInstructionsDesc')}
          </p>
        </div>
      </div>

      {/* Format tabs */}
      <div className="px-5 flex gap-1.5 overflow-x-auto pb-1" style={{ scrollbarWidth: 'none' }}>
        {FORMATS.map((fmt) => {
          const isActive = activeFormat === fmt.key
          return (
            <button
              key={fmt.key}
              onClick={() => { setActiveFormat(fmt.key); setCopied(false) }}
              className="flex items-center gap-1.5 px-3 py-1.5 text-[12px] font-[400] rounded-[4px] whitespace-nowrap cursor-pointer transition-all duration-150 shrink-0"
              style={{
                background: isActive ? 'var(--stripe-purple)' : 'transparent',
                color: isActive ? 'var(--on-primary)' : 'var(--body)',
                border: isActive ? '1px solid transparent' : '1px solid var(--border)',
              }}
            >
              <Icon name={fmt.icon} size="sm" />
              {formatLabel(fmt.key)}
            </button>
          )
        })}
      </div>

      {/* Active format description + tool list */}
      <div className="px-5 py-3">
        <p className="text-[13px] leading-relaxed" style={{ color: 'var(--body)' }}>
          {formatDesc(activeFormat)}
        </p>
      </div>

      {/* Actions */}
      <div className="px-5 py-3 flex items-center gap-2" style={{ borderTop: '1px solid var(--border)' }}>
        <button
          onClick={handleCopy}
          className="inline-flex items-center gap-2 px-4 py-2 text-[14px] font-[400] rounded-[4px] cursor-pointer transition-all duration-150"
          style={{
            background: copied ? 'rgba(21,190,83,0.1)' : 'var(--stripe-purple)',
            color: copied ? 'var(--success-text)' : 'var(--on-primary)',
            border: copied ? '1px solid rgba(21,190,83,0.3)' : '1px solid transparent',
          }}
        >
          <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
          {copied ? t('quickstart.aiInstructionsCopied') : t('quickstart.aiInstructionsCopy')}
        </button>
        <button
          onClick={() => setExpanded(!expanded)}
          className="inline-flex items-center gap-1.5 px-3 py-2 text-[13px] font-[400] rounded-[4px] bg-transparent cursor-pointer transition-colors duration-150"
          style={{ color: 'var(--stripe-purple)', border: '1px solid var(--border-purple)' }}
        >
          <Icon name={expanded ? 'unfold_less' : 'unfold_more'} size="sm" />
          {expanded ? t('quickstart.aiInstructionsCollapse') : t('quickstart.aiInstructionsExpand')}
        </button>
      </div>

      {/* Collapsible preview */}
      {expanded && (
        <div style={{ borderTop: '1px solid var(--border)' }}>
          <pre
            className="px-5 py-4 overflow-x-auto text-[12px] font-mono font-[500] leading-[1.7] whitespace-pre-wrap"
            style={{ background: 'var(--code-bg)', color: 'var(--code-text)', maxHeight: '480px', overflowY: 'auto' }}
          >
            {content}
          </pre>
        </div>
      )}
    </div>
  )
}

interface MethodProps {
  icon: string
  title: string
  description: string
  children: React.ReactNode
}

function Method({ icon, title, description, children }: MethodProps) {
  return (
    <div className="rounded-[5px] overflow-hidden" style={{ border: '1px solid var(--border)', background: 'var(--surface)' }}>
      <div className="px-5 py-4 flex items-start gap-3">
        <span
          className="flex items-center justify-center w-8 h-8 rounded-[6px] shrink-0 mt-0.5"
          style={{ background: 'rgba(83,58,253,0.08)', color: 'var(--stripe-purple)' }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <div className="flex-1 min-w-0">
          <p className="font-[400] text-[14px]" style={{ color: 'var(--heading)' }}>{title}</p>
          <p className="text-[13px] mt-0.5 leading-relaxed" style={{ color: 'var(--body)' }}>{description}</p>
        </div>
      </div>
      <div className="px-5 pb-5 -mt-1 space-y-3">
        {children}
      </div>
    </div>
  )
}

export default function QuickStartV2() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<Tab>('pip')
  const baseURL = useMemo(() => window.location.origin, [])
  const host = useMemo(() => window.location.hostname, [])

  const tabs: { key: Tab; label: string; desc: string }[] = [
    { key: 'pip', label: t('quickstart.pipLabel'), desc: t('quickstart.pipDesc') },
    { key: 'apt', label: t('quickstart.aptLabel'), desc: t('quickstart.aptDesc') },
    { key: 'npm', label: t('quickstart.npmLabel'), desc: t('quickstart.npmDesc') },
    { key: 'go', label: t('quickstart.goLabel'), desc: t('quickstart.goDesc') },
    { key: 'cargo', label: t('quickstart.cargoLabel'), desc: t('quickstart.cargoDesc') },
    { key: 'maven', label: t('quickstart.mavenLabel'), desc: t('quickstart.mavenDesc') },
    { key: 'rubygems', label: t('quickstart.rubygemsLabel'), desc: t('quickstart.rubygemsDesc') },
    { key: 'composer', label: t('quickstart.composerLabel'), desc: t('quickstart.composerDesc') },
    { key: 'nuget', label: t('quickstart.nugetLabel'), desc: t('quickstart.nugetDesc') },
    { key: 'conda', label: t('quickstart.condaLabel'), desc: t('quickstart.condaDesc') },
    { key: 'cran', label: t('quickstart.cranLabel'), desc: t('quickstart.cranDesc') },
    { key: 'helm', label: t('quickstart.helmLabel'), desc: t('quickstart.helmDesc') },
  ]

  return (
    <div className="space-y-8">
      {/* Page header — Stripe style */}
      <div>
        <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--heading)' }}>
          {t('quickstart.title')}
        </h1>
        <p className="text-[16px] font-[300] mt-1" style={{ color: 'var(--body)' }}>
          {t('quickstart.subtitle')}
        </p>
      </div>

      {/* AI Instructions Card */}
      <AIInstructionsCard baseURL={baseURL} host={host} />

      {/* Ecosystem selector — 3 col grid */}
      <div className="grid grid-cols-3 gap-2">
        {tabs.map((tab) => {
          const isActive = activeTab === tab.key
          return (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className="flex items-center gap-3 rounded-[5px] px-3 py-3 text-left transition-all duration-150 cursor-pointer"
              style={{
                border: isActive ? '1px solid var(--border-purple)' : '1px solid var(--border)',
                background: isActive ? 'rgba(83,58,253,0.04)' : 'var(--surface)',
              }}
              onMouseEnter={(e) => {
                if (!isActive) e.currentTarget.style.transform = 'translateY(-1px)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.transform = ''
              }}
            >
              <EcosystemIcon type={tab.key} size={20} />
              <div className="min-w-0">
                <p className="font-[400] text-[14px] truncate" style={{ color: isActive ? 'var(--heading)' : 'var(--label)' }}>
                  {tab.label}
                </p>
                <p className="text-[11px] truncate leading-tight" style={{ color: 'var(--body)' }}>
                  {tab.desc}
                </p>
              </div>
            </button>
          )
        })}
      </div>

      {/* Methods — identical business logic, new styling */}
      <div className="grid gap-4">
        {activeTab === 'pip' && (
          <>
            <Method icon="bolt" title={t('quickstart.tempUse')} description={t('quickstart.tempUseDesc')}>
              <CodeBlockV2 language="bash" code={`pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}`} />
            </Method>
            <Method icon="settings" title={t('quickstart.permanentConfig')} description={t('quickstart.permanentConfigDesc')}>
              <CodeBlockV2 filename="~/.config/pip/pip.conf" code={`[global]\nindex-url = ${baseURL}/pypi/simple/\ntrusted-host = ${host}`} />
            </Method>
            <Method icon="speed" title={t('quickstart.uvUser')} description={t('quickstart.uvUserDesc')}>
              <CodeBlockV2 language="bash" code={`uv pip install <package> --index-url ${baseURL}/pypi/simple/`} />
            </Method>
            <Method icon="library_books" title={t('quickstart.poetryUser')} description={t('quickstart.poetryUserDesc')}>
              <CodeBlockV2 filename="pyproject.toml" code={`[[tool.poetry.source]]\nname = "depsilo"\nurl = "${baseURL}/pypi/simple/"\npriority = "primary"`} />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.dockerPip')} description={t('quickstart.dockerPipDesc')}>
              <CodeBlockV2 filename="Dockerfile" language="dockerfile" code={`# Add these ARG lines before RUN pip install\nARG PIP_INDEX_URL\nARG PIP_TRUSTED_HOST`} />
              <CodeBlockV2 language="bash" code={`docker build \\\n  --build-arg PIP_INDEX_URL=${baseURL}/pypi/simple/ \\\n  --build-arg PIP_TRUSTED_HOST=${host} \\\n  -t myapp .`} />
            </Method>
          </>
        )}

        {activeTab === 'apt' && (
          <>
            <Method icon="add_circle" title={t('quickstart.addSource')} description={t('quickstart.addSourceDesc')}>
              <CodeBlockV2 filename="/etc/apt/sources.list.d/depsilo.list" code={`deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-updates main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-security main restricted universe multiverse`} />
            </Method>
            <Method icon="find_replace" title={t('quickstart.replaceSource')} description={t('quickstart.replaceSourceDesc')}>
              <CodeBlockV2 language="bash" code={`sudo sed -i 's|https\\?://[^/]*/ubuntu|${baseURL}/apt/ubuntu|g' /etc/apt/sources.list`} />
            </Method>
            <Method icon="verified" title={t('quickstart.verifyConfig')} description={t('quickstart.verifyConfigDesc')}>
              <CodeBlockV2 language="bash" code="sudo apt update" />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.dockerApt')} description={t('quickstart.dockerAptDesc')}>
              <CodeBlockV2 language="bash" code={`DOCKER_BUILDKIT=1 docker build --network host \\\n  --build-arg http_proxy=${baseURL} \\\n  -t myapp .`} />
            </Method>
          </>
        )}

        {activeTab === 'npm' && (
          <>
            <Method icon="bolt" title={t('quickstart.npmTempUse')} description={t('quickstart.npmTempUseDesc')}>
              <CodeBlockV2 language="bash" code={`npm install <package> --registry ${baseURL}/npm/`} />
            </Method>
            <Method icon="settings" title={t('quickstart.npmPermanentConfig')} description={t('quickstart.npmPermanentConfigDesc')}>
              <CodeBlockV2 language="bash" code={`npm config set registry ${baseURL}/npm/`} />
              <p className="text-[13px] mt-3 mb-2" style={{ color: 'var(--body)' }}>{t('quickstart.npmNpmrc')}</p>
              <CodeBlockV2 filename="~/.npmrc" code={`registry=${baseURL}/npm/`} />
            </Method>
            <Method icon="speed" title={t('quickstart.npmYarnPnpm')} description={t('quickstart.npmYarnPnpmDesc')}>
              <CodeBlockV2 language="bash" code={`# yarn v1\nyarn config set registry ${baseURL}/npm/\n\n# pnpm (uses .npmrc automatically)`} />
            </Method>
          </>
        )}

        {activeTab === 'go' && (
          <>
            <Method icon="bolt" title={t('quickstart.goTempUse')} description={t('quickstart.goTempUseDesc')}>
              <CodeBlockV2 language="bash" code={`GOPROXY=${baseURL}/go,direct go get <package>`} />
            </Method>
            <Method icon="settings" title={t('quickstart.goPermanentConfig')} description={t('quickstart.goPermanentConfigDesc')}>
              <CodeBlockV2 language="bash" code={`go env -w GOPROXY=${baseURL}/go,direct`} />
            </Method>
            <Method icon="verified" title={t('quickstart.goVerify')} description={t('quickstart.goVerifyDesc')}>
              <CodeBlockV2 language="bash" code="go env GOPROXY" />
            </Method>
          </>
        )}

        {activeTab === 'cargo' && (
          <>
            <Method icon="settings" title={t('quickstart.cargoConfig')} description={t('quickstart.cargoConfigDesc')}>
              <CodeBlockV2 filename="~/.cargo/config.toml" code={`[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "sparse+${baseURL}/crates/"`} />
            </Method>
            <Method icon="verified" title={t('quickstart.cargoVerify')} description={t('quickstart.cargoVerifyDesc')}>
              <CodeBlockV2 language="bash" code="cargo install ripgrep" />
            </Method>
          </>
        )}

        {activeTab === 'maven' && (
          <>
            <Method icon="settings" title={t('quickstart.mavenSettings')} description={t('quickstart.mavenSettingsDesc')}>
              <CodeBlockV2 filename="~/.m2/settings.xml" code={`<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <mirrorOf>central</mirrorOf>\n      <url>${baseURL}/maven/</url>\n    </mirror>\n  </mirrors>\n</settings>`} />
            </Method>
            <Method icon="code_blocks" title={t('quickstart.mavenGradle')} description={t('quickstart.mavenGradleDesc')}>
              <CodeBlockV2 filename="build.gradle" code={`repositories {\n    maven { url "${baseURL}/maven/" }\n}`} />
            </Method>
          </>
        )}

        {activeTab === 'rubygems' && (
          <>
            <Method icon="settings" title={t('quickstart.rubygemsBundler')} description={t('quickstart.rubygemsBundlerDesc')}>
              <CodeBlockV2 language="bash" code={`bundle config mirror.https://rubygems.org ${baseURL}/rubygems/`} />
            </Method>
            <Method icon="find_replace" title={t('quickstart.rubygemsGemSource')} description={t('quickstart.rubygemsGemSourceDesc')}>
              <CodeBlockV2 language="bash" code={`gem sources --add ${baseURL}/rubygems/ --remove https://rubygems.org/`} />
            </Method>
          </>
        )}

        {activeTab === 'composer' && (
          <>
            <Method icon="settings" title={t('quickstart.composerGlobal')} description={t('quickstart.composerGlobalDesc')}>
              <CodeBlockV2 language="bash" code={`composer config -g repo.packagist composer ${baseURL}/composer/`} />
            </Method>
            <Method icon="verified" title={t('quickstart.composerVerify')} description={t('quickstart.composerVerifyDesc')}>
              <CodeBlockV2 language="bash" code="composer config -g --list | grep repositories" />
            </Method>
          </>
        )}

        {activeTab === 'nuget' && (
          <>
            <Method icon="add_circle" title={t('quickstart.nugetAddSource')} description={t('quickstart.nugetAddSourceDesc')}>
              <CodeBlockV2 language="bash" code={`dotnet nuget add source ${baseURL}/nuget/v3/index.json -n depsilo`} />
            </Method>
            <Method icon="verified" title={t('quickstart.nugetVerify')} description={t('quickstart.nugetVerifyDesc')}>
              <CodeBlockV2 language="bash" code="dotnet nuget list source" />
            </Method>
          </>
        )}

        {activeTab === 'conda' && (
          <>
            <Method icon="settings" title={t('quickstart.condaConfig')} description={t('quickstart.condaConfigDesc')}>
              <CodeBlockV2 filename="~/.condarc" code={`channels:\n  - ${baseURL}/conda/pkgs/main\n  - defaults`} />
            </Method>
            <Method icon="terminal" title={t('quickstart.condaCommand')} description={t('quickstart.condaCommandDesc')}>
              <CodeBlockV2 language="bash" code={`conda config --add channels ${baseURL}/conda/pkgs/main`} />
            </Method>
          </>
        )}

        {activeTab === 'cran' && (
          <>
            <Method icon="code" title={t('quickstart.cranConfig')} description={t('quickstart.cranConfigDesc')}>
              <CodeBlockV2 language="r" code={`options(repos = c(CRAN = "${baseURL}/cran/"))`} />
            </Method>
            <Method icon="settings" title={t('quickstart.cranRprofile')} description={t('quickstart.cranRprofileDesc')}>
              <CodeBlockV2 filename="~/.Rprofile" code={`# ~/.Rprofile\noptions(repos = c(CRAN = "${baseURL}/cran/"))`} />
            </Method>
          </>
        )}

        {activeTab === 'helm' && (
          <>
            <Method icon="add_circle" title={t('quickstart.helmAddRepo')} description={t('quickstart.helmAddRepoDesc')}>
              <CodeBlockV2 language="bash" code={`helm repo add depsilo ${baseURL}/helm/`} />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.helmUse')} description={t('quickstart.helmUseDesc')}>
              <CodeBlockV2 language="bash" code="helm install my-release depsilo/nginx" />
            </Method>
          </>
        )}
      </div>

      {/* Info tip — Stripe accent style */}
      <div
        className="flex items-start gap-3 rounded-[5px] px-5 py-4"
        style={{ background: 'rgba(83,58,253,0.04)', border: '1px solid var(--border-purple)' }}
      >
        <span
          className="flex items-center justify-center w-8 h-8 rounded-[6px] shrink-0"
          style={{ background: 'rgba(83,58,253,0.08)' }}
        >
          <Icon name="lightbulb" size="sm" style={{ color: 'var(--stripe-purple)' }} />
        </span>
        <div>
          <p className="font-[400] text-[14px]" style={{ color: 'var(--heading)' }}>{t('quickstart.firstDownloadTitle')}</p>
          <p className="text-[14px] font-[300] mt-0.5 leading-relaxed" style={{ color: 'var(--body)' }}>
            {t('quickstart.firstDownloadDesc')}
          </p>
        </div>
      </div>
    </div>
  )
}
