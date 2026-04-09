import { useMemo, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import EcosystemIcon from '@/components/EcosystemIcon'
import CodeBlockV2 from '@/portal/components/CodeBlockV2'

type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer' | 'nuget' | 'conda' | 'cran' | 'helm'

function generateAIMarkdown(baseURL: string, host: string): string {
  return `# Package Proxy Configuration (Depsilo)

> This project uses **Depsilo** (\`${baseURL}\`) as the package proxy cache.
> When installing any dependencies, you **MUST** use the proxy URLs below instead of the default public registries.

## Proxy Address

\`${baseURL}\`

## Configuration by Ecosystem

### Python (pip / uv / Poetry)

\`\`\`bash
# pip
pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}

# uv
uv pip install <package> --index-url ${baseURL}/pypi/simple/

# permanent config (~/.config/pip/pip.conf)
[global]
index-url = ${baseURL}/pypi/simple/
trusted-host = ${host}
\`\`\`

### APT (Debian / Ubuntu)

\`\`\`bash
# Use this as the mirror base URL in sources.list:
deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse
\`\`\`

### npm / yarn / pnpm

\`\`\`bash
npm config set registry ${baseURL}/npm/
# or in ~/.npmrc
registry=${baseURL}/npm/
\`\`\`

### Go Modules

\`\`\`bash
go env -w GOPROXY=${baseURL}/go,direct
\`\`\`

### Cargo (Rust)

\`\`\`toml
# ~/.cargo/config.toml
[source.crates-io]
replace-with = "depsilo"

[source.depsilo]
registry = "sparse+${baseURL}/crates/"
\`\`\`

### Maven / Gradle (Java)

\`\`\`xml
<!-- ~/.m2/settings.xml -->
<mirror>
  <id>depsilo</id>
  <mirrorOf>central</mirrorOf>
  <url>${baseURL}/maven/</url>
</mirror>
\`\`\`

### RubyGems

\`\`\`bash
bundle config mirror.https://rubygems.org ${baseURL}/rubygems/
\`\`\`

### Composer (PHP)

\`\`\`bash
composer config -g repo.packagist composer ${baseURL}/composer/
\`\`\`

### NuGet (.NET)

\`\`\`bash
dotnet nuget add source ${baseURL}/nuget/v3/index.json -n depsilo
\`\`\`

### Conda

\`\`\`yaml
# ~/.condarc
channels:
  - ${baseURL}/conda/pkgs/main
  - defaults
\`\`\`

### CRAN (R)

\`\`\`r
options(repos = c(CRAN = "${baseURL}/cran/"))
\`\`\`

### Helm (Kubernetes)

\`\`\`bash
helm repo add depsilo ${baseURL}/helm/
\`\`\`

## Docker Build

When building Docker images that install dependencies, pass the proxy via build args:

\`\`\`bash
# For pip
docker build \\
  --build-arg PIP_INDEX_URL=${baseURL}/pypi/simple/ \\
  --build-arg PIP_TRUSTED_HOST=${host} \\
  -t myapp .

# For apt (add to Dockerfile: ARG http_proxy)
docker build --network host \\
  --build-arg http_proxy=${baseURL} \\
  -t myapp .
\`\`\`

## Rules

- **Always** use the Depsilo proxy URLs above when running install commands.
- **Never** use the default public registry URLs (pypi.org, registry.npmjs.org, etc.) directly.
- The first download of a package may be slower (fetched from upstream); subsequent downloads are served from cache at LAN speed.
`
}

/** AI Instructions card with copy + collapsible preview */
function AIInstructionsCard({ baseURL, host }: { baseURL: string; host: string }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [expanded, setExpanded] = useState(false)

  const markdown = useMemo(() => generateAIMarkdown(baseURL, host), [baseURL, host])

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(markdown).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }, [markdown])

  const tools = [
    { icon: 'smart_toy', text: t('quickstart.aiToolClaude') },
    { icon: 'edit_note', text: t('quickstart.aiToolCursor') },
    { icon: 'code', text: t('quickstart.aiToolCopilot') },
    { icon: 'air', text: t('quickstart.aiToolWindsurf') },
    { icon: 'extension', text: t('quickstart.aiToolGeneric') },
  ]

  return (
    <div
      className="rounded-[6px] overflow-hidden"
      style={{
        background: 'var(--surface)',
        border: '1px solid var(--border)',
        boxShadow: 'var(--shadow-soft)',
      }}
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

      {/* Tool list */}
      <div className="px-5 pb-3">
        <p className="text-[11px] font-[400] uppercase tracking-widest mb-2" style={{ color: 'var(--body)' }}>
          {t('quickstart.aiSupportedTools')}
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
          {tools.map((tool) => (
            <div key={tool.text} className="flex items-center gap-2 text-[12px]" style={{ color: 'var(--label)' }}>
              <Icon name={tool.icon} size="sm" className="opacity-50" />
              <span>{tool.text}</span>
            </div>
          ))}
        </div>
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
        <div className="border-t" style={{ borderColor: 'var(--border)' }}>
          <pre
            className="px-5 py-4 overflow-x-auto text-[12px] font-mono font-[500] leading-[1.7] whitespace-pre-wrap"
            style={{ background: 'var(--code-bg)', color: 'var(--code-text)', maxHeight: '480px', overflowY: 'auto' }}
          >
            {markdown}
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
