import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import EcosystemIcon from '@/components/EcosystemIcon'
import CodeBlockV2 from '@/portal/components/CodeBlockV2'

type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer' | 'nuget' | 'conda' | 'cran' | 'helm'

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
