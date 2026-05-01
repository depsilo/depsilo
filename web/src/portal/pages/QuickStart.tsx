import { useMemo, useState, useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { statsApi } from '@/lib/api'
import Icon from '@/components/Icon'
import EcosystemIcon from '@/components/EcosystemIcon'
import CodeBlock from '@/portal/components/CodeBlock'

type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer' | 'nuget' | 'conda' | 'cran' | 'helm' | 'docker'

// ── Content generators (by tool) ───────────────────────────────────

function genRulesBlock(u: string, h: string): string {
  return [
    `# Depsilo — Package Proxy (${u})`,
    ``,
    `> **All** dependency installs MUST go through this proxy. Never use public registries directly.`,
    ``,
    `## Quick Reference`,
    ``,
    `| Ecosystem | Command / Config |`,
    `|-----------|-----------------|`,
    `| pip       | \`pip install <pkg> -i ${u}/pypi/simple/ --trusted-host ${h}\` |`,
    `| uv        | \`uv pip install <pkg> --index-url ${u}/pypi/simple/\` |`,
    `| npm       | \`npm config set registry ${u}/npm/\` |`,
    `| Go        | \`go env -w GOPROXY=${u}/go,direct\` |`,
    `| Cargo     | registry = \`sparse+${u}/crates/\` in ~/.cargo/config.toml |`,
    `| Maven     | mirror url = \`${u}/maven/\` in ~/.m2/settings.xml |`,
    `| Composer  | \`composer config -g repo.packagist composer ${u}/composer/\` |`,
    `| RubyGems  | \`bundle config mirror.https://rubygems.org ${u}/rubygems/\` |`,
    `| NuGet     | \`dotnet nuget add source ${u}/nuget/v3/index.json -n depsilo\` |`,
    `| Conda     | add \`${u}/conda/pkgs/main\` to ~/.condarc channels |`,
    `| CRAN      | \`options(repos = c(CRAN = "${u}/cran/"))\` |`,
    `| Helm      | \`helm repo add depsilo ${u}/helm/\` |`,
    ``,
    `## Environment Variables`,
    ``,
    `\`\`\`bash`,
    `export PIP_INDEX_URL=${u}/pypi/simple/`,
    `export PIP_TRUSTED_HOST=${h}`,
    `export npm_config_registry=${u}/npm/`,
    `export GOPROXY=${u}/go,direct`,
    `\`\`\``,
  ].join('\n')
}

function genEnvFile(u: string, h: string): string {
  return [
    `# .env — Depsilo Proxy`,
    `PIP_INDEX_URL=${u}/pypi/simple/`,
    `PIP_TRUSTED_HOST=${h}`,
    `UV_INDEX_URL=${u}/pypi/simple/`,
    `npm_config_registry=${u}/npm/`,
    `GOPROXY=${u}/go,direct`,
  ].join('\n')
}

function genShellSetup(u: string, h: string): string {
  return [
    `#!/usr/bin/env bash`,
    `set -e`,
    `P="${u}" H="${h}"`,
    ``,
    `# pip`,
    `mkdir -p ~/.config/pip`,
    `printf "[global]\\nindex-url = $P/pypi/simple/\\ntrusted-host = $H\\n" > ~/.config/pip/pip.conf`,
    ``,
    `# npm`,
    `npm config set registry "$P/npm/" 2>/dev/null || true`,
    ``,
    `# Go`,
    `go env -w GOPROXY="$P/go,direct" 2>/dev/null || true`,
    ``,
    `# Cargo`,
    `mkdir -p ~/.cargo`,
    `grep -q 'source.depsilo' ~/.cargo/config.toml 2>/dev/null || \\`,
    `  printf "\\n[source.crates-io]\\nreplace-with = \\"depsilo\\"\\n[source.depsilo]\\nregistry = \\"sparse+$P/crates/\\"\\n" >> ~/.cargo/config.toml`,
    ``,
    `echo "Done — all proxies configured."`,
  ].join('\n')
}

function genSkillFile(u: string, h: string): string {
  return [
    `---`,
    `name: setup-depsilo-proxy`,
    `description: Configure package managers to use Depsilo (${u})`,
    `---`,
    ``,
    `Set these proxies based on the project's tech stack:`,
    ``,
    `- pip: \`PIP_INDEX_URL=${u}/pypi/simple/ PIP_TRUSTED_HOST=${h}\``,
    `- npm: \`npm config set registry ${u}/npm/\``,
    `- Go:  \`go env -w GOPROXY=${u}/go,direct\``,
    `- Cargo: registry = \`sparse+${u}/crates/\` (in ~/.cargo/config.toml)`,
    `- Maven: \`<url>${u}/maven/</url>\` (in settings.xml mirror)`,
    `- Composer: \`composer config -g repo.packagist composer ${u}/composer/\``,
    `- RubyGems: \`bundle config mirror.https://rubygems.org ${u}/rubygems/\``,
    `- NuGet: \`dotnet nuget add source ${u}/nuget/v3/index.json -n depsilo\``,
    `- Conda: \`${u}/conda/pkgs/main\` (add to ~/.condarc channels)`,
    `- CRAN: \`options(repos = c(CRAN = "${u}/cran/"))\``,
    `- Helm: \`helm repo add depsilo ${u}/helm/\``,
  ].join('\n')
}

function genMCPEnv(u: string, h: string): string {
  return JSON.stringify({
    mcpServers: {
      'your-server': {
        command: 'npx',
        args: ['-y', 'your-mcp-server'],
        env: {
          PIP_INDEX_URL: `${u}/pypi/simple/`,
          PIP_TRUSTED_HOST: h,
          npm_config_registry: `${u}/npm/`,
          GOPROXY: `${u}/go,direct`,
        },
      },
    },
  }, null, 2)
}

function genDevcontainer(u: string, h: string): string {
  return JSON.stringify({
    name: 'Dev Container with Depsilo',
    remoteEnv: {
      PIP_INDEX_URL: `${u}/pypi/simple/`,
      PIP_TRUSTED_HOST: h,
      npm_config_registry: `${u}/npm/`,
      GOPROXY: `${u}/go,direct`,
    },
  }, null, 2)
}

// ── Tool definitions ───────────────────────────────────────────────

interface ToolDef {
  id: string
  icon: string
  file: string
  gen: (u: string, h: string) => string
}

const AI_TOOLS: ToolDef[] = [
  { id: 'claude',       icon: 'smart_toy',     file: 'CLAUDE.md',                           gen: genRulesBlock },
  { id: 'cursor',       icon: 'edit_note',     file: '.cursorrules',                        gen: genRulesBlock },
  { id: 'copilot',      icon: 'code',          file: '.github/copilot-instructions.md',     gen: genRulesBlock },
  { id: 'windsurf',     icon: 'air',           file: '.windsurfrules',                      gen: genRulesBlock },
  { id: 'skill',        icon: 'auto_awesome',  file: '.claude/commands/setup-proxy.md',     gen: genSkillFile },
  { id: 'mcp',          icon: 'hub',           file: '.claude/settings.json (mcpServers)',  gen: genMCPEnv },
  { id: 'env',          icon: 'data_object',   file: '.env',                                gen: genEnvFile },
  { id: 'shell',        icon: 'terminal',      file: 'setup-proxy.sh',                      gen: genShellSetup },
  { id: 'devcontainer', icon: 'deployed_code', file: '.devcontainer/devcontainer.json',     gen: genDevcontainer },
]

// ── Ecosystem tabs (flat list, 4-column grid) ────────────────────

const tabs: { key: Tab; label: string }[] = [
  { key: 'pip', label: 'pip' },
  { key: 'conda', label: 'Conda' },
  { key: 'npm', label: 'npm' },
  { key: 'go', label: 'Go' },
  { key: 'maven', label: 'Maven' },
  { key: 'nuget', label: 'NuGet' },
  { key: 'cargo', label: 'Cargo' },
  { key: 'rubygems', label: 'RubyGems' },
  { key: 'composer', label: 'Composer' },
  { key: 'cran', label: 'CRAN' },
  { key: 'apt', label: 'APT' },
  { key: 'docker', label: 'Docker' },
  { key: 'helm', label: 'Helm' },
]

// ── Status Bar ─────────────────────────────────────────────────────

function StatusBar() {
  const { t } = useTranslation()
  const url = window.location.origin

  const { data } = useQuery<{
    service: { status: string }
    today: { total_requests: number; hit_rate: number; bytes_saved: number }
  }>({
    queryKey: ['stats-bar'],
    queryFn: async () => { const r = await statsApi.getStats(); return r.data },
    refetchInterval: 30000,
  })

  const isHealthy = data?.service?.status === 'healthy'
  const [copied, setCopied] = useState(false)
  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(url).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [url])

  function fmtBytes(b: number): string {
    if (!b) return '0 B'
    const u = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(b) / Math.log(1024))
    return `${(b / Math.pow(1024, i)).toFixed(1)} ${u[i]}`
  }

  return (
    <div
      className="rounded-[6px] px-4 py-2.5 flex items-center gap-4 flex-wrap"
      style={{
        background: 'color-mix(in srgb, var(--bg-card) 85%, transparent)',
        border: '1px solid var(--border)',
        backdropFilter: 'blur(8px)',
        WebkitBackdropFilter: 'blur(8px)',
      }}
    >
      {/* Service URL + copy */}
      <div className="flex items-center gap-2 min-w-0">
        <span className="relative flex h-2 w-2 shrink-0">
          {isHealthy && <span className="animate-ping-health absolute inline-flex h-full w-full rounded-full opacity-75" style={{ background: 'var(--ok)' }} />}
          <span className="relative inline-flex rounded-full h-2 w-2" style={{ background: data ? (isHealthy ? 'var(--ok)' : 'var(--danger)') : 'var(--text-soft)' }} />
        </span>
        <code className="font-mono text-[13px] truncate" style={{ color: 'var(--brand)' }}>{url}</code>
        <button
          onClick={handleCopy}
          className="bg-transparent p-0.5 rounded-[4px] cursor-pointer shrink-0"
          style={{ color: copied ? 'var(--ok)' : 'var(--text-soft)' }}
        >
          <Icon name={copied ? 'check' : 'content_copy'} size="sm" />
        </button>
      </div>

      {/* Divider */}
      <div className="h-4 w-px shrink-0" style={{ background: 'var(--border)' }} />

      {/* Stats */}
      {data && (
        <div className="flex items-center gap-4 text-[12px] font-mono tabular-nums" style={{ color: 'var(--text-soft)' }}>
          <span>{t('quickstart.statRequests', { count: data.today.total_requests.toLocaleString() })}</span>
          <span>{t('quickstart.statHitRate', { rate: (data.today.hit_rate * 100).toFixed(1) })}</span>
          <span>{t('quickstart.statSaved', { size: fmtBytes(data.today.bytes_saved) })}</span>
        </div>
      )}
    </div>
  )
}

// ── AI Integration (inline link + expandable panel) ───────────────

function AIInstructionsLink({ baseURL, host }: { baseURL: string; host: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const handleCopy = useCallback((tool: ToolDef) => {
    const content = tool.gen(baseURL, host)
    navigator.clipboard.writeText(content).then(() => {
      setCopiedId(tool.id)
      setTimeout(() => setCopiedId(null), 2000)
    })
  }, [baseURL, host])

  return (
    <div>
      <button
        onClick={() => setOpen(!open)}
        className="inline-flex items-center gap-1.5 bg-transparent cursor-pointer text-[13px] font-[400] transition-colors duration-150 p-0"
        style={{ color: 'var(--brand)', border: 'none' }}
      >
        <Icon name="auto_awesome" size="sm" />
        <span>{t('quickstart.aiTitle')}</span>
        <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>— {t('quickstart.aiSubtitle')}</span>
        <Icon name={open ? 'expand_less' : 'expand_more'} size="sm" style={{ color: 'var(--text-soft)' }} />
      </button>

      {open && (
        <div className="mt-3 rounded-[5px] overflow-hidden" style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}>
          {AI_TOOLS.map((tool) => {
            const isCopied = copiedId === tool.id
            return (
              <div
                key={tool.id}
                className="flex items-center gap-3 px-4 py-1.5"
                style={{ borderBottom: '1px solid var(--border)' }}
              >
                <Icon name={tool.icon} size="sm" style={{ color: 'var(--text-soft)', flexShrink: 0 }} />
                <span className="text-[12px] font-[400] w-[90px] shrink-0" style={{ color: 'var(--text)' }}>
                  {t(`quickstart.ai_${tool.id}`)}
                </span>
                <code className="flex-1 text-[11px] font-mono truncate" style={{ color: 'var(--text-soft)' }}>{tool.file}</code>
                <button
                  onClick={() => handleCopy(tool)}
                  className="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] font-[400] rounded-[3px] cursor-pointer transition-all duration-150 shrink-0"
                  style={{
                    background: isCopied ? 'rgba(21,190,83,0.1)' : 'var(--brand)',
                    color: isCopied ? 'var(--ok-text)' : 'white',
                    border: isCopied ? '1px solid rgba(21,190,83,0.3)' : '1px solid transparent',
                  }}
                >
                  <Icon name={isCopied ? 'check' : 'content_copy'} size="sm" />
                  {isCopied ? t('quickstart.aiCopied') : t('quickstart.aiCopy')}
                </button>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// ── Config test button ─────────────────────────────────────────────

function TestButton({ ecosystem, baseURL }: { ecosystem: Tab; baseURL: string }) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<'idle' | 'testing' | 'ok' | 'fail'>('idle')

  const testPaths: Record<Tab, string> = {
    pip: '/pypi/simple/',
    apt: '/apt/',
    npm: '/npm/',
    go: '/go/',
    cargo: '/crates/config.json',
    maven: '/maven/',
    rubygems: '/rubygems/',
    composer: '/composer/packages.json',
    nuget: '/nuget/v3/index.json',
    conda: '/conda/',
    cran: '/cran/',
    helm: '/helm/',
    docker: '/v2/',
  }

  const handleTest = async () => {
    setStatus('testing')
    try {
      const res = await fetch(`${baseURL}${testPaths[ecosystem]}`, { method: 'HEAD', mode: 'no-cors' })
      // no-cors always gives opaque response, but if fetch doesn't throw, connection is ok
      setStatus(res.type === 'opaque' || res.ok ? 'ok' : 'fail')
    } catch {
      setStatus('fail')
    }
    setTimeout(() => setStatus('idle'), 3000)
  }

  const colors = {
    idle: { bg: 'transparent', color: 'var(--brand)', border: 'var(--border-purple)', shadow: '0 0 8px rgba(83,58,253,0.25)' },
    testing: { bg: 'transparent', color: 'var(--text-soft)', border: 'var(--border)', shadow: 'none' },
    ok: { bg: 'rgba(21,190,83,0.1)', color: 'var(--ok-text)', border: 'rgba(21,190,83,0.3)', shadow: 'none' },
    fail: { bg: 'var(--danger-fill)', color: 'var(--danger)', border: 'var(--danger)', shadow: 'none' },
  }
  const c = colors[status]
  const labels = {
    idle: t('quickstart.testConnection'),
    testing: t('quickstart.testing'),
    ok: t('quickstart.testOk'),
    fail: t('quickstart.testFail'),
  }
  const icons = { idle: 'wifi_tethering', testing: 'hourglass_empty', ok: 'check_circle', fail: 'error' }

  return (
    <button
      onClick={handleTest}
      disabled={status === 'testing'}
      className="inline-flex items-center gap-1.5 px-3 py-1.5 text-[12px] font-[400] rounded-[4px] cursor-pointer transition-all duration-150 disabled:cursor-wait"
      style={{ background: c.bg, color: c.color, border: `1px solid ${c.border}`, boxShadow: c.shadow }}
    >
      <Icon name={icons[status]} size="sm" />
      {labels[status]}
    </button>
  )
}

// ── Method card ────────────────────────────────────────────────────

interface MethodProps {
  icon: string
  title: string
  description: string
  children: React.ReactNode
}

function Method({ icon, title, description, children }: MethodProps) {
  return (
    <div className="rounded-[5px] overflow-hidden" style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}>
      <div className="px-5 py-4 flex items-start gap-3">
        <span
          className="flex items-center justify-center w-7 h-7 rounded-[4px] shrink-0 mt-0.5"
          style={{ background: 'linear-gradient(135deg, var(--brand), var(--brand-soft))', color: 'white' }}
        >
          <Icon name={icon} size="sm" />
        </span>
        <div className="flex-1 min-w-0">
          <p className="font-[400] text-[13px]" style={{ color: 'var(--text)' }}>{title}</p>
          <p className="text-[12px] mt-0.5 leading-relaxed" style={{ color: 'var(--text-soft)' }}>{description}</p>
        </div>
      </div>
      <div className="px-5 pb-5 -mt-1 space-y-3">
        {children}
      </div>
    </div>
  )
}

// ── Main Page ──────────────────────────────────────────────────────

export default function QuickStartV2() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<Tab>('pip')
  const baseURL = useMemo(() => window.location.origin, [])
  const host = useMemo(() => window.location.hostname, [])

  const { data: statsData } = useQuery<{
    extra_indexes?: { name: string; path: string }[]
  }>({
    queryKey: ['stats-extra'],
    queryFn: async () => { const r = await statsApi.getStats(); return r.data },
    staleTime: Infinity,
  })
  const extraIndexes = statsData?.extra_indexes || []

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div>
        <h1 className="text-[32px] font-[300] tracking-[-0.64px]" style={{ color: 'var(--text)' }}>
          {t('quickstart.title')}
        </h1>
        <p className="text-[14px] mt-1" style={{ color: 'var(--text-soft)' }}>
          {t('quickstart.subtitle')}
        </p>
      </div>

      {/* Status bar — shows service health + today stats */}
      <StatusBar />

      {/* Ecosystem selector — flat 4-column grid */}
      <div className="grid grid-cols-4 gap-2">
        {tabs.map((tab) => {
          const isActive = activeTab === tab.key
          return (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className="flex items-center gap-2 px-3 py-2 text-left cursor-pointer"
              style={{
                border: 'none',
                borderBottom: isActive ? '2px solid var(--brand)' : '2px solid transparent',
                background: 'transparent',
                transition: 'all 0.2s ease',
                transform: 'translateY(0)',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.transform = 'translateY(-1px)'
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.transform = 'translateY(0)'
              }}
            >
              <EcosystemIcon type={tab.key} size={16} />
              <span className="text-[13px] font-[400]" style={{ color: isActive ? 'var(--text)' : 'var(--text-soft)', transition: 'color 0.2s ease' }}
                onMouseEnter={(e) => { if (!isActive) e.currentTarget.style.color = 'var(--text)' }}
                onMouseLeave={(e) => { if (!isActive) e.currentTarget.style.color = 'var(--text-soft)' }}
              >
                {tab.label}
              </span>
            </button>
          )
        })}
      </div>

      {/* Test connection for active ecosystem */}
      <div className="flex items-center gap-3">
        <TestButton ecosystem={activeTab} baseURL={baseURL} />
        <span className="text-[12px]" style={{ color: 'var(--text-soft)' }}>
          {t('quickstart.testHint')}
        </span>
      </div>

      {/* Methods */}
      <div className="grid gap-5">
        {activeTab === 'pip' && (
          <>
            <Method icon="bolt" title={t('quickstart.tempUse')} description={t('quickstart.tempUseDesc')}>
              <CodeBlock language="bash" code={`pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}`} />
            </Method>
            <Method icon="settings" title={t('quickstart.permanentConfig')} description={t('quickstart.permanentConfigDesc')}>
              <CodeBlock filename="~/.config/pip/pip.conf" code={`[global]\nindex-url = ${baseURL}/pypi/simple/\ntrusted-host = ${host}`} />
            </Method>
            <Method icon="speed" title={t('quickstart.uvUser')} description={t('quickstart.uvUserDesc')}>
              <CodeBlock language="bash" code={`uv pip install <package> --index-url ${baseURL}/pypi/simple/`} />
            </Method>
            <Method icon="library_books" title={t('quickstart.poetryUser')} description={t('quickstart.poetryUserDesc')}>
              <CodeBlock filename="pyproject.toml" code={`[[tool.poetry.source]]\nname = "depsilo"\nurl = "${baseURL}/pypi/simple/"\npriority = "primary"`} />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.dockerPip')} description={t('quickstart.dockerPipDesc')}>
              <CodeBlock filename="Dockerfile" language="dockerfile" code={`# Add these ARG lines before RUN pip install\nARG PIP_INDEX_URL\nARG PIP_TRUSTED_HOST`} />
              <CodeBlock language="bash" code={`docker build \\\n  --build-arg PIP_INDEX_URL=${baseURL}/pypi/simple/ \\\n  --build-arg PIP_TRUSTED_HOST=${host} \\\n  -t myapp .`} />
            </Method>
            {extraIndexes.length > 0 && (
              <Method icon="memory" title={t('quickstart.extraIndexes')} description={t('quickstart.extraIndexesDesc')}>
                {extraIndexes.map((idx) => (
                  <div key={idx.name}>
                    <p className="text-[13px] font-[500] mb-1.5" style={{ color: 'var(--text)' }}>{idx.name}</p>
                    <CodeBlock language="bash" code={`pip3 install torch torchvision --index-url ${baseURL}/${idx.path}/simple/`} />
                  </div>
                ))}
                <p className="text-[13px] font-[500] mt-3 mb-1.5" style={{ color: 'var(--text)' }}>Dockerfile</p>
                <CodeBlock filename="Dockerfile" language="dockerfile" code={`ARG PIP_INDEX_URL=${baseURL}/pypi/simple/\nARG PIP_TRUSTED_HOST=${host}\n\n# Standard packages (via main index)\nRUN pip3 install --no-cache-dir transformers accelerate\n\n# CUDA packages (via extra index)\nRUN pip3 install --no-cache-dir torch torchvision \\\n    --index-url ${baseURL}/${extraIndexes[0]?.path}/simple/`} />
              </Method>
            )}
          </>
        )}

        {activeTab === 'apt' && (
          <>
            <Method icon="add_circle" title={t('quickstart.addSource')} description={t('quickstart.addSourceDesc')}>
              <CodeBlock filename="/etc/apt/sources.list.d/depsilo.list" code={`deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-updates main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-security main restricted universe multiverse`} />
            </Method>
            <Method icon="find_replace" title={t('quickstart.replaceSource')} description={t('quickstart.replaceSourceDesc')}>
              <CodeBlock language="bash" code={`sudo sed -i 's|https\\?://[^/]*/ubuntu|${baseURL}/apt/ubuntu|g' /etc/apt/sources.list`} />
            </Method>
            <Method icon="verified" title={t('quickstart.verifyConfig')} description={t('quickstart.verifyConfigDesc')}>
              <CodeBlock language="bash" code="sudo apt update" />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.dockerApt')} description={t('quickstart.dockerAptDesc')}>
              <CodeBlock language="bash" code={`DOCKER_BUILDKIT=1 docker build --network host \\\n  --build-arg http_proxy=${baseURL} \\\n  -t myapp .`} />
            </Method>
          </>
        )}

        {activeTab === 'npm' && (
          <>
            <Method icon="bolt" title={t('quickstart.npmTempUse')} description={t('quickstart.npmTempUseDesc')}>
              <CodeBlock language="bash" code={`npm install <package> --registry ${baseURL}/npm/`} />
            </Method>
            <Method icon="settings" title={t('quickstart.npmPermanentConfig')} description={t('quickstart.npmPermanentConfigDesc')}>
              <CodeBlock language="bash" code={`npm config set registry ${baseURL}/npm/`} />
              <p className="text-[13px] mt-3 mb-2" style={{ color: 'var(--text-soft)' }}>{t('quickstart.npmNpmrc')}</p>
              <CodeBlock filename="~/.npmrc" code={`registry=${baseURL}/npm/`} />
            </Method>
            <Method icon="speed" title={t('quickstart.npmYarnPnpm')} description={t('quickstart.npmYarnPnpmDesc')}>
              <CodeBlock language="bash" code={`# yarn v1\nyarn config set registry ${baseURL}/npm/\n\n# pnpm (uses .npmrc automatically)`} />
            </Method>
          </>
        )}

        {activeTab === 'go' && (
          <>
            <Method icon="bolt" title={t('quickstart.goTempUse')} description={t('quickstart.goTempUseDesc')}>
              <CodeBlock language="bash" code={`GOPROXY=${baseURL}/go,direct go get <package>`} />
            </Method>
            <Method icon="settings" title={t('quickstart.goPermanentConfig')} description={t('quickstart.goPermanentConfigDesc')}>
              <CodeBlock language="bash" code={`go env -w GOPROXY=${baseURL}/go,direct`} />
            </Method>
            <Method icon="verified" title={t('quickstart.goVerify')} description={t('quickstart.goVerifyDesc')}>
              <CodeBlock language="bash" code="go env GOPROXY" />
            </Method>
          </>
        )}

        {activeTab === 'cargo' && (
          <>
            <Method icon="settings" title={t('quickstart.cargoConfig')} description={t('quickstart.cargoConfigDesc')}>
              <CodeBlock filename="~/.cargo/config.toml" code={`[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "sparse+${baseURL}/crates/"`} />
            </Method>
            <Method icon="verified" title={t('quickstart.cargoVerify')} description={t('quickstart.cargoVerifyDesc')}>
              <CodeBlock language="bash" code="cargo install ripgrep" />
            </Method>
          </>
        )}

        {activeTab === 'maven' && (
          <>
            <Method icon="settings" title={t('quickstart.mavenSettings')} description={t('quickstart.mavenSettingsDesc')}>
              <CodeBlock filename="~/.m2/settings.xml" code={`<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <mirrorOf>central</mirrorOf>\n      <url>${baseURL}/maven/</url>\n    </mirror>\n  </mirrors>\n</settings>`} />
            </Method>
            <Method icon="code_blocks" title={t('quickstart.mavenGradle')} description={t('quickstart.mavenGradleDesc')}>
              <CodeBlock filename="build.gradle" code={`repositories {\n    maven { url "${baseURL}/maven/" }\n}`} />
            </Method>
          </>
        )}

        {activeTab === 'rubygems' && (
          <>
            <Method icon="settings" title={t('quickstart.rubygemsBundler')} description={t('quickstart.rubygemsBundlerDesc')}>
              <CodeBlock language="bash" code={`bundle config mirror.https://rubygems.org ${baseURL}/rubygems/`} />
            </Method>
            <Method icon="find_replace" title={t('quickstart.rubygemsGemSource')} description={t('quickstart.rubygemsGemSourceDesc')}>
              <CodeBlock language="bash" code={`gem sources --add ${baseURL}/rubygems/ --remove https://rubygems.org/`} />
            </Method>
          </>
        )}

        {activeTab === 'composer' && (
          <>
            <Method icon="settings" title={t('quickstart.composerGlobal')} description={t('quickstart.composerGlobalDesc')}>
              <CodeBlock language="bash" code={`composer config -g repo.packagist composer ${baseURL}/composer/`} />
            </Method>
            <Method icon="verified" title={t('quickstart.composerVerify')} description={t('quickstart.composerVerifyDesc')}>
              <CodeBlock language="bash" code="composer config -g --list | grep repositories" />
            </Method>
          </>
        )}

        {activeTab === 'nuget' && (
          <>
            <Method icon="add_circle" title={t('quickstart.nugetAddSource')} description={t('quickstart.nugetAddSourceDesc')}>
              <CodeBlock language="bash" code={`dotnet nuget add source ${baseURL}/nuget/v3/index.json -n depsilo`} />
            </Method>
            <Method icon="verified" title={t('quickstart.nugetVerify')} description={t('quickstart.nugetVerifyDesc')}>
              <CodeBlock language="bash" code="dotnet nuget list source" />
            </Method>
          </>
        )}

        {activeTab === 'conda' && (
          <>
            <Method icon="settings" title={t('quickstart.condaConfig')} description={t('quickstart.condaConfigDesc')}>
              <CodeBlock filename="~/.condarc" code={`channels:\n  - ${baseURL}/conda/pkgs/main\n  - defaults`} />
            </Method>
            <Method icon="terminal" title={t('quickstart.condaCommand')} description={t('quickstart.condaCommandDesc')}>
              <CodeBlock language="bash" code={`conda config --add channels ${baseURL}/conda/pkgs/main`} />
            </Method>
          </>
        )}

        {activeTab === 'cran' && (
          <>
            <Method icon="code" title={t('quickstart.cranConfig')} description={t('quickstart.cranConfigDesc')}>
              <CodeBlock language="r" code={`options(repos = c(CRAN = "${baseURL}/cran/"))`} />
            </Method>
            <Method icon="settings" title={t('quickstart.cranRprofile')} description={t('quickstart.cranRprofileDesc')}>
              <CodeBlock filename="~/.Rprofile" code={`# ~/.Rprofile\noptions(repos = c(CRAN = "${baseURL}/cran/"))`} />
            </Method>
          </>
        )}

        {activeTab === 'helm' && (
          <>
            <Method icon="add_circle" title={t('quickstart.helmAddRepo')} description={t('quickstart.helmAddRepoDesc')}>
              <CodeBlock language="bash" code={`helm repo add depsilo ${baseURL}/helm/`} />
            </Method>
            <Method icon="deployed_code" title={t('quickstart.helmUse')} description={t('quickstart.helmUseDesc')}>
              <CodeBlock language="bash" code="helm install my-release depsilo/nginx" />
            </Method>
          </>
        )}

        {activeTab === 'docker' && (
          <>
            <Method icon="shield" title={t('quickstart.dockerInsecure')} description={t('quickstart.dockerInsecureDesc')}>
              <CodeBlock filename="/etc/docker/daemon.json" language="json" code={`{\n  "insecure-registries": ["${host}:${location.port || '23333'}"],\n  "registry-mirrors": ["http://${host}:${location.port || '23333'}"]\n}`} />
            </Method>
            <Method icon="restart_alt" title={t('quickstart.dockerRestart')} description={t('quickstart.dockerRestartDesc')}>
              <CodeBlock language="bash" code="sudo systemctl restart docker" />
            </Method>
            <Method icon="download" title={t('quickstart.dockerDirect')} description={t('quickstart.dockerDirectDesc')}>
              <CodeBlock language="bash" code={`docker pull ${host}:${location.port || '23333'}/nginx:latest`} />
            </Method>
            <Method icon="cloud_sync" title={t('quickstart.dockerOther')} description={t('quickstart.dockerOtherDesc')}>
              <CodeBlock language="bash" code={`docker pull ${host}:${location.port || '23333'}/ghcr.io/owner/repo:tag`} />
            </Method>
          </>
        )}
      </div>

      {/* AI Integration — inline link */}
      <AIInstructionsLink baseURL={baseURL} host={host} />

      {/* Info tip */}
      <div
        className="flex items-start gap-3 rounded-[5px] px-4 py-3"
        style={{ background: 'rgba(83,58,253,0.04)', border: '1px solid var(--border-purple)' }}
      >
        <Icon name="lightbulb" size="sm" style={{ color: 'var(--brand)', marginTop: 2 }} />
        <div>
          <p className="font-[400] text-[14px]" style={{ color: 'var(--text)' }}>{t('quickstart.firstDownloadTitle')}</p>
          <p className="text-[14px] font-[300] mt-0.5 leading-relaxed" style={{ color: 'var(--text-soft)' }}>
            {t('quickstart.firstDownloadDesc')}
          </p>
        </div>
      </div>
    </div>
  )
}
