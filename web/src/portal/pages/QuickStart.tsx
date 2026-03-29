import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import Card from '@/components/Card'
import Icon from '@/components/Icon'
import CodeBlock from '@/portal/components/CodeBlock'

type Tab = 'pip' | 'apt'

interface StepProps {
  number: number
  title: string
  description: string
  children: React.ReactNode
}

function Step({ number, title, description, children }: StepProps) {
  return (
    <Card className="space-y-3">
      <div className="flex items-start gap-3">
        <span className="w-7 h-7 rounded-full bg-primary-container text-primary flex items-center justify-center text-xs font-bold shrink-0 mt-0.5">
          {number}
        </span>
        <div className="flex-1 min-w-0">
          <p className="font-medium text-on-surface">{title}</p>
          <p className="text-sm text-on-surface-variant mt-0.5">{description}</p>
        </div>
      </div>
      <div className="ml-10">{children}</div>
    </Card>
  )
}

export default function QuickStart() {
  const { t } = useTranslation()
  const [activeTab, setActiveTab] = useState<Tab>('pip')
  const baseURL = useMemo(() => window.location.origin, [])
  const host = useMemo(() => window.location.hostname, [])

  const tabMeta = {
    pip: {
      icon: 'code_blocks',
      label: t('quickstart.pipLabel'),
      desc: t('quickstart.pipDesc'),
    },
    apt: {
      icon: 'terminal',
      label: t('quickstart.aptLabel'),
      desc: t('quickstart.aptDesc'),
    },
  } as const

  return (
    <div className="space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold text-on-surface mb-1">{t('quickstart.title')}</h1>
        <p className="text-on-surface-variant">
          {t('quickstart.subtitle')}
        </p>
      </div>

      {/* Tab selector — card style */}
      <div className="grid grid-cols-2 gap-3">
        {(['pip', 'apt'] as const).map((tab) => {
          const meta = tabMeta[tab]
          const isActive = activeTab === tab
          return (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`flex items-center gap-4 rounded-[0.375rem] px-5 py-4 text-left transition-all cursor-pointer border-2 ${
                isActive
                  ? 'border-primary bg-primary/5'
                  : 'border-outline-variant/15 bg-surface-low hover:border-outline-variant/30'
              }`}
            >
              <span
                className={`flex items-center justify-center w-10 h-10 rounded-[0.375rem] shrink-0 ${
                  isActive
                    ? 'bg-primary text-white'
                    : 'bg-surface-container text-on-surface-variant'
                }`}
              >
                <Icon name={meta.icon} />
              </span>
              <div>
                <p className={`font-semibold text-sm ${isActive ? 'text-primary' : 'text-on-surface'}`}>
                  {meta.label}
                </p>
                <p className="text-xs text-on-surface-variant mt-0.5">
                  {meta.desc}
                </p>
              </div>
            </button>
          )
        })}
      </div>

      {/* Steps */}
      <div className="space-y-4">
        {activeTab === 'pip' && (
          <>
            <Step
              number={1}
              title={t('quickstart.tempUse')}
              description={t('quickstart.tempUseDesc')}
            >
              <CodeBlock
                language="bash"
                code={`pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}`}
              />
            </Step>

            <Step
              number={2}
              title={t('quickstart.permanentConfig')}
              description={t('quickstart.permanentConfigDesc')}
            >
              <CodeBlock
                filename="~/.config/pip/pip.conf"
                code={`[global]\nindex-url = ${baseURL}/pypi/simple/\ntrusted-host = ${host}`}
              />
            </Step>

            <Step
              number={3}
              title={t('quickstart.uvUser')}
              description={t('quickstart.uvUserDesc')}
            >
              <CodeBlock
                language="bash"
                code={`uv pip install <package> --index-url ${baseURL}/pypi/simple/`}
              />
            </Step>

            <Step
              number={4}
              title={t('quickstart.poetryUser')}
              description={t('quickstart.poetryUserDesc')}
            >
              <CodeBlock
                filename="pyproject.toml"
                code={`[[tool.poetry.source]]\nname = "depslio"\nurl = "${baseURL}/pypi/simple/"\npriority = "primary"`}
              />
            </Step>
          </>
        )}

        {activeTab === 'apt' && (
          <>
            <Step
              number={1}
              title={t('quickstart.addSource')}
              description={t('quickstart.addSourceDesc')}
            >
              <CodeBlock
                filename="/etc/apt/sources.list.d/depslio.list"
                code={`deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-updates main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-security main restricted universe multiverse`}
              />
            </Step>

            <Step
              number={2}
              title={t('quickstart.replaceSource')}
              description={t('quickstart.replaceSourceDesc')}
            >
              <CodeBlock
                language="bash"
                code={`sudo sed -i 's|https\\?://[^/]*/ubuntu|${baseURL}/apt/ubuntu|g' /etc/apt/sources.list`}
              />
            </Step>

            <Step
              number={3}
              title={t('quickstart.verifyConfig')}
              description={t('quickstart.verifyConfigDesc')}
            >
              <CodeBlock language="bash" code="sudo apt update" />
            </Step>
          </>
        )}
      </div>

      {/* Info tip */}
      <div className="bg-primary/5 border-l-2 border-primary p-4 rounded-[0.25rem] flex items-start gap-3">
        <Icon name="lightbulb" size="sm" className="text-primary shrink-0 mt-0.5" />
        <div className="space-y-1">
          <p className="font-medium text-on-surface text-sm">{t('quickstart.firstDownloadTitle')}</p>
          <p className="text-sm text-on-surface-variant">
            {t('quickstart.firstDownloadDesc')}
          </p>
        </div>
      </div>
    </div>
  )
}
