import { useMemo, useState } from 'react'
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

const tabMeta = {
  pip: {
    icon: 'code_blocks',
    label: 'Python (pip)',
    desc: 'pip / uv / Poetry 包管理器',
  },
  apt: {
    icon: 'terminal',
    label: 'APT (Debian)',
    desc: 'Ubuntu / Debian 系统包管理器',
  },
} as const

export default function QuickStart() {
  const [activeTab, setActiveTab] = useState<Tab>('pip')
  const baseURL = useMemo(() => window.location.origin, [])
  const host = useMemo(() => window.location.hostname, [])

  return (
    <div className="space-y-8">
      {/* Page header */}
      <div>
        <h1 className="text-2xl font-bold text-on-surface mb-1">快速开始</h1>
        <p className="text-on-surface-variant">
          配置你的包管理器以使用 RepoCache 代理缓存，享受更快的下载速度。
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
              title="临时使用"
              description="在单次安装命令中指定索引地址："
            >
              <CodeBlock
                language="bash"
                code={`pip install <package> -i ${baseURL}/pypi/simple/ --trusted-host ${host}`}
              />
            </Step>

            <Step
              number={2}
              title="永久配置"
              description="编辑 pip 配置文件，所有安装命令自动走代理："
            >
              <CodeBlock
                filename="~/.config/pip/pip.conf"
                code={`[global]\nindex-url = ${baseURL}/pypi/simple/\ntrusted-host = ${host}`}
              />
            </Step>

            <Step
              number={3}
              title="uv 用户"
              description="如果你使用 uv 作为包管理器："
            >
              <CodeBlock
                language="bash"
                code={`uv pip install <package> --index-url ${baseURL}/pypi/simple/`}
              />
            </Step>

            <Step
              number={4}
              title="Poetry 用户"
              description="在 pyproject.toml 中配置镜像源："
            >
              <CodeBlock
                filename="pyproject.toml"
                code={`[[tool.poetry.source]]\nname = "repocache"\nurl = "${baseURL}/pypi/simple/"\npriority = "primary"`}
              />
            </Step>
          </>
        )}

        {activeTab === 'apt' && (
          <>
            <Step
              number={1}
              title="添加源配置"
              description="创建新的 APT 源配置文件："
            >
              <CodeBlock
                filename="/etc/apt/sources.list.d/repocache.list"
                code={`deb ${baseURL}/apt/ubuntu noble main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-updates main restricted universe multiverse\ndeb ${baseURL}/apt/ubuntu noble-security main restricted universe multiverse`}
              />
            </Step>

            <Step
              number={2}
              title="一键替换现有源"
              description="使用 sed 命令将现有源替换为 RepoCache 代理："
            >
              <CodeBlock
                language="bash"
                code={`sudo sed -i 's|https\\?://[^/]*/ubuntu|${baseURL}/apt/ubuntu|g' /etc/apt/sources.list`}
              />
            </Step>

            <Step
              number={3}
              title="验证配置"
              description="运行以下命令验证源配置是否生效："
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
          <p className="font-medium text-on-surface text-sm">首次下载说明</p>
          <p className="text-sm text-on-surface-variant">
            首次请求某个包时，RepoCache 需要从上游源下载并缓存，速度取决于上游响应。
            后续相同包的请求将直接从本地缓存返回，享受局域网级别的下载速度。
          </p>
        </div>
      </div>
    </div>
  )
}
