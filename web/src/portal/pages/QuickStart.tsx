import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Icon from '@/components/Icon'
import CodeBlock from '@/portal/components/CodeBlock'

type Tab = 'pip' | 'apt' | 'npm' | 'go' | 'cargo' | 'maven' | 'rubygems' | 'composer'

interface MethodProps {
  icon: string
  title: string
  description: string
  children: React.ReactNode
}

function Method({ icon, title, description, children }: MethodProps) {
  return (
    <div className="rounded-xl border border-outline-variant/15 bg-surface-low overflow-hidden">
      <div className="px-5 py-4 flex items-start gap-3">
        <span className="flex items-center justify-center w-8 h-8 rounded-lg bg-primary/10 text-primary shrink-0 mt-0.5">
          <Icon name={icon} size="sm" />
        </span>
        <div className="flex-1 min-w-0">
          <p className="font-semibold text-on-surface text-sm">{title}</p>
          <p className="text-[13px] text-on-surface-variant mt-0.5 leading-relaxed">{description}</p>
        </div>
      </div>
      <div className="px-5 pb-5 -mt-1">
        {children}
      </div>
    </div>
  )
}

export default function QuickStart() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<Tab>('pip')
  const baseURL = useMemo(() => window.location.origin, [])
  const host = useMemo(() => window.location.hostname, [])

  const tabs = [
    { key: 'pip' as Tab, icon: 'code_blocks', label: t('quickstart.pipLabel'), desc: t('quickstart.pipDesc') },
    { key: 'apt' as Tab, icon: 'terminal', label: t('quickstart.aptLabel'), desc: t('quickstart.aptDesc') },
    { key: 'npm' as Tab, icon: 'package_2', label: t('quickstart.npmLabel'), desc: t('quickstart.npmDesc') },
    { key: 'go' as Tab, icon: 'code', label: t('quickstart.goLabel'), desc: t('quickstart.goDesc') },
    { key: 'cargo' as Tab, icon: 'settings', label: t('quickstart.cargoLabel'), desc: t('quickstart.cargoDesc') },
    { key: 'maven' as Tab, icon: 'code_blocks', label: t('quickstart.mavenLabel'), desc: t('quickstart.mavenDesc') },
    { key: 'rubygems' as Tab, icon: 'diamond', label: t('quickstart.rubygemsLabel'), desc: t('quickstart.rubygemsDesc') },
    { key: 'composer' as Tab, icon: 'music_note', label: t('quickstart.composerLabel'), desc: t('quickstart.composerDesc') },
  ]

  return (
    <div className="space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold text-on-surface mb-1">{t('quickstart.title')}</h1>
        <p className="text-on-surface-variant">
          {t('quickstart.subtitle')}
        </p>
      </div>

      {/* Tab selector */}
      <div className="flex gap-2 p-1 bg-surface-container rounded-xl">
        {tabs.map((tab) => {
          const isActive = activeTab === tab.key
          return (
            <button
              key={tab.key}
              onClick={() => setActiveTab(tab.key)}
              className={`flex items-center gap-3 flex-1 rounded-lg px-4 py-3 text-left transition-all cursor-pointer ${
                isActive
                  ? 'bg-surface-bright shadow-sm'
                  : 'bg-transparent hover:bg-surface-high/50'
              }`}
            >
              <span
                className={`flex items-center justify-center w-9 h-9 rounded-lg shrink-0 transition-colors ${
                  isActive
                    ? 'bg-primary text-on-primary'
                    : 'bg-surface-high text-on-surface-variant'
                }`}
              >
                <Icon name={tab.icon} size="sm" />
              </span>
              <div>
                <p className={`font-semibold text-sm ${isActive ? 'text-on-surface' : 'text-on-surface-variant'}`}>
                  {tab.label}
                </p>
                <p className="text-[11px] text-on-surface-variant mt-0.5 leading-tight">
                  {tab.desc}
                </p>
              </div>
            </button>
          )
        })}
      </div>

      {/* Methods */}
      <div className="grid gap-4">
        {activeTab === 'pip' && (
          <>
            <Method
              icon="bolt"
              title={t('quickstart.tempUse')}
              description={t('quickstart.tempUseDesc')}
            >
              <CodeBlock
                language="bash"
                code={`pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}`}
              />
            </Method>

            <Method
              icon="settings"
              title={t('quickstart.permanentConfig')}
              description={t('quickstart.permanentConfigDesc')}
            >
              <CodeBlock
                filename="~/.config/pip/pip.conf"
                code={`[global]\nindex-url = ${baseURL}/pypi/simple/\ntrusted-host = ${host}`}
              />
            </Method>

            <Method
              icon="speed"
              title={t('quickstart.uvUser')}
              description={t('quickstart.uvUserDesc')}
            >
              <CodeBlock
                language="bash"
                code={`uv pip install <package> --index-url ${baseURL}/pypi/simple/`}
              />
            </Method>

            <Method
              icon="library_books"
              title={t('quickstart.poetryUser')}
              description={t('quickstart.poetryUserDesc')}
            >
              <CodeBlock
                filename="pyproject.toml"
                code={`[[tool.poetry.source]]\nname = "depsilo"\nurl = "${baseURL}/pypi/simple/"\npriority = "primary"`}
              />
            </Method>

            <Method
              icon="deployed_code"
              title={t('quickstart.dockerPip')}
              description={t('quickstart.dockerPipDesc')}
            >
              <CodeBlock
                language="bash"
                code={`DOCKER_BUILDKIT=1 docker build --network host \\\n  --build-arg PIP_INDEX_URL=${baseURL}/pypi/simple/ \\\n  --build-arg PIP_TRUSTED_HOST=${host} \\\n  -t myapp .`}
              />
            </Method>
          </>
        )}

        {activeTab === 'apt' && (
          <>
            <Method
              icon="add_circle"
              title={t('quickstart.addSource')}
              description={t('quickstart.addSourceDesc')}
            >
              <CodeBlock
                filename="/etc/apt/sources.list.d/depsilo.list"
                code={`deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-updates main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-security main restricted universe multiverse`}
              />
            </Method>

            <Method
              icon="find_replace"
              title={t('quickstart.replaceSource')}
              description={t('quickstart.replaceSourceDesc')}
            >
              <CodeBlock
                language="bash"
                code={`sudo sed -i 's|https\\?://[^/]*/ubuntu|${baseURL}/apt/ubuntu|g' /etc/apt/sources.list`}
              />
            </Method>

            <Method
              icon="verified"
              title={t('quickstart.verifyConfig')}
              description={t('quickstart.verifyConfigDesc')}
            >
              <CodeBlock language="bash" code="sudo apt update" />
            </Method>

            <Method
              icon="deployed_code"
              title={t('quickstart.dockerApt')}
              description={t('quickstart.dockerAptDesc')}
            >
              <CodeBlock
                language="bash"
                code={`DOCKER_BUILDKIT=1 docker build --network host \\\n  --build-arg http_proxy=${baseURL} \\\n  -t myapp .`}
              />
            </Method>
          </>
        )}

        {activeTab === 'npm' && (
          <>
            <Method
              icon="bolt"
              title={t('quickstart.npmTempUse')}
              description={t('quickstart.npmTempUseDesc')}
            >
              <CodeBlock
                language="bash"
                code={`npm install <package> --registry ${baseURL}/npm/`}
              />
            </Method>

            <Method
              icon="settings"
              title={t('quickstart.npmPermanentConfig')}
              description={t('quickstart.npmPermanentConfigDesc')}
            >
              <CodeBlock
                language="bash"
                code={`npm config set registry ${baseURL}/npm/`}
              />
              <p className="text-[13px] text-on-surface-variant mt-3 mb-2">{t('quickstart.npmNpmrc')}</p>
              <CodeBlock
                filename="~/.npmrc"
                code={`registry=${baseURL}/npm/`}
              />
            </Method>

            <Method
              icon="speed"
              title={t('quickstart.npmYarnPnpm')}
              description={t('quickstart.npmYarnPnpmDesc')}
            >
              <CodeBlock
                language="bash"
                code={`# yarn v1\nyarn config set registry ${baseURL}/npm/\n\n# pnpm (uses .npmrc automatically)`}
              />
            </Method>
          </>
        )}

        {activeTab === 'go' && (
          <>
            <Method
              icon="bolt"
              title={t('quickstart.goTempUse')}
              description={t('quickstart.goTempUseDesc')}
            >
              <CodeBlock
                language="bash"
                code={`GOPROXY=${baseURL}/go,direct go get <package>`}
              />
            </Method>

            <Method
              icon="settings"
              title={t('quickstart.goPermanentConfig')}
              description={t('quickstart.goPermanentConfigDesc')}
            >
              <CodeBlock
                language="bash"
                code={`go env -w GOPROXY=${baseURL}/go,direct`}
              />
            </Method>

            <Method
              icon="verified"
              title={t('quickstart.goVerify')}
              description={t('quickstart.goVerifyDesc')}
            >
              <CodeBlock language="bash" code="go env GOPROXY" />
            </Method>
          </>
        )}

        {activeTab === 'cargo' && (
          <>
            <Method
              icon="settings"
              title={t('quickstart.cargoConfig')}
              description={t('quickstart.cargoConfigDesc')}
            >
              <CodeBlock
                filename="~/.cargo/config.toml"
                code={`[source.crates-io]\nreplace-with = "depsilo"\n\n[source.depsilo]\nregistry = "sparse+${baseURL}/crates/"`}
              />
            </Method>

            <Method
              icon="verified"
              title={t('quickstart.cargoVerify')}
              description={t('quickstart.cargoVerifyDesc')}
            >
              <CodeBlock language="bash" code="cargo install ripgrep" />
            </Method>
          </>
        )}

        {activeTab === 'maven' && (
          <>
            <Method
              icon="settings"
              title={t('quickstart.mavenSettings')}
              description={t('quickstart.mavenSettingsDesc')}
            >
              <CodeBlock
                filename="~/.m2/settings.xml"
                code={`<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <mirrorOf>central</mirrorOf>\n      <url>${baseURL}/maven/</url>\n    </mirror>\n  </mirrors>\n</settings>`}
              />
            </Method>

            <Method
              icon="code_blocks"
              title={t('quickstart.mavenGradle')}
              description={t('quickstart.mavenGradleDesc')}
            >
              <CodeBlock
                filename="build.gradle"
                code={`repositories {\n    maven { url "${baseURL}/maven/" }\n}`}
              />
            </Method>
          </>
        )}

        {activeTab === 'rubygems' && (
          <>
            <Method
              icon="settings"
              title={t('quickstart.rubygemsBundler')}
              description={t('quickstart.rubygemsBundlerDesc')}
            >
              <CodeBlock
                language="bash"
                code={`bundle config mirror.https://rubygems.org ${baseURL}/rubygems/`}
              />
            </Method>

            <Method
              icon="find_replace"
              title={t('quickstart.rubygemsGemSource')}
              description={t('quickstart.rubygemsGemSourceDesc')}
            >
              <CodeBlock
                language="bash"
                code={`gem sources --add ${baseURL}/rubygems/ --remove https://rubygems.org/`}
              />
            </Method>
          </>
        )}

        {activeTab === 'composer' && (
          <>
            <Method
              icon="settings"
              title={t('quickstart.composerGlobal')}
              description={t('quickstart.composerGlobalDesc')}
            >
              <CodeBlock
                language="bash"
                code={`composer config -g repo.packagist composer ${baseURL}/composer/`}
              />
            </Method>

            <Method
              icon="verified"
              title={t('quickstart.composerVerify')}
              description={t('quickstart.composerVerifyDesc')}
            >
              <CodeBlock
                language="bash"
                code="composer config -g --list | grep repositories"
              />
            </Method>
          </>
        )}
      </div>

      {/* Info tip */}
      <div className="flex items-start gap-3 rounded-xl bg-primary/5 border border-primary/10 px-5 py-4">
        <span className="flex items-center justify-center w-8 h-8 rounded-lg bg-primary/10 shrink-0">
          <Icon name="lightbulb" size="sm" className="text-primary" />
        </span>
        <div>
          <p className="font-semibold text-on-surface text-sm">{t('quickstart.firstDownloadTitle')}</p>
          <p className="text-sm text-on-surface-variant mt-0.5 leading-relaxed">
            {t('quickstart.firstDownloadDesc')}
          </p>
        </div>
      </div>
    </div>
  )
}
