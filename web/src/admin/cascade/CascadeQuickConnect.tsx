import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import SelectV2 from '@/components/Select'
import TabsV2 from '@/components/Tabs'
import CodeBlock from '@/portal/components/CodeBlock'
import type { AdminCascadeConfigState, AdminCascadeInfo } from '@/lib/adminApi.types'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'

const publicBaseStorageKey = 'cascade-public-base'
const placeholderParentURL = 'http://10.0.0.2:23333'

type QuickTab = 'client' | 'parent' | 'child'
type ClientEcosystem = 'npm' | 'pip' | 'go'

interface CascadeQuickConnectProps {
  info: AdminCascadeInfo
  config?: AdminCascadeConfigState
}

function browserOrigin(): string {
  return typeof window === 'undefined' ? '' : window.location.origin
}

function sanitizePeerName(value: string): string {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/^-+|-+$/g, '')
  return normalized.slice(0, 64) || 'parent'
}

function peerNameFromBase(base: string): string {
  try {
    return sanitizePeerName(new URL(base).hostname)
  } catch {
    return sanitizePeerName(base)
  }
}

export default function CascadeQuickConnect({ info, config }: CascadeQuickConnectProps) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<QuickTab>('client')
  const [ecosystem, setEcosystem] = useState<ClientEcosystem>('npm')
  const [publicBase, setPublicBase] = useState(() => readLocalStorage(publicBaseStorageKey) ?? browserOrigin())

  const base = publicBase.trim().replace(/\/+$/, '') || browserOrigin()
  const relayPath = info.relay_path || '/_depsilo/relay/v1'
  const runningPeers = info.peers
  const firstParent = runningPeers[0] ?? config?.peers[0]
  const parentURL = (firstParent?.url ?? placeholderParentURL).replace(/\/+$/, '')
  const parentName = sanitizePeerName(firstParent?.name ?? 'parent')

  function updatePublicBase(value: string) {
    setPublicBase(value)
    writeLocalStorage(publicBaseStorageKey, value)
  }

  const clientCommand = useMemo(() => {
    switch (ecosystem) {
      case 'pip':
        return `pip config set global.index-url ${base}/pypi/simple/`
      case 'go':
        return `go env -w GOPROXY=${base}/go,direct`
      default:
        return `npm config set registry ${base}/npm`
    }
  }, [base, ecosystem])

  const childSnippet = `# ${t('cascade.quick.childSnippetComment')}
[cascade]
enabled = true
token = "<${t('cascade.quick.sharedSecretPlaceholder')}>"

[[cascade.peers]]
name = "${peerNameFromBase(base)}"
url  = "${base}"`

  const childRelayCheck = `curl -fsS -o /dev/null -w '%{http_code}\\n' \\
  -H 'X-Depsilo-Cascade-Token: <${t('cascade.quick.sharedSecretPlaceholder')}>' \\
  -H 'X-Depsilo-Cascade-Chain: probe' \\
  -H 'X-Depsilo-Relay-Target: https://registry.npmjs.org/left-pad' \\
  ${base}${relayPath}`

  const parentSnippet = `# ${t('cascade.quick.parentSnippetComment')}
[cascade]
enabled = true
token = "<${t('cascade.quick.parentSecretPlaceholder')}>"

[[cascade.peers]]
name = "${parentName}"
url  = "${parentURL}"

[[npm.upstreams]]
name = "via-${parentName}"
url  = "https://registry.npmjs.org"
via  = "${parentName}"`

  const parentChecks = [
    `curl -fsS -o /dev/null -w '%{http_code}\\n' \\`,
    `  -H 'X-Depsilo-Cascade-Token: <${t('cascade.quick.parentSecretPlaceholder')}>' \\`,
    `  -H 'X-Depsilo-Cascade-Chain: probe' \\`,
    `  -H 'X-Depsilo-Relay-Target: https://registry.npmjs.org/left-pad' \\`,
    `  ${parentURL}${relayPath}`,
    ...(runningPeers.length > 0
      ? ['', ...runningPeers.map(peer => `curl -fsS ${peer.url.replace(/\/+$/, '')}/health`)]
      : []),
  ].join('\n')

  const tabs = [
    {
      key: 'client',
      label: t('cascade.quick.tabClient'),
      content: (
        <div className="space-y-3 pt-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_160px]">
            <InputV2
              label={t('cascade.quick.baseURL')}
              mono
              value={publicBase}
              hint={t('cascade.quick.baseURLHint')}
              autoCapitalize="none"
              spellCheck={false}
              onChange={event => updatePublicBase(event.target.value)}
              placeholder={browserOrigin()}
            />
            <SelectV2
              label={t('cascade.quick.ecosystem')}
              value={ecosystem}
              onChange={event => setEcosystem(event.target.value as ClientEcosystem)}
            >
              <option value="npm">npm</option>
              <option value="pip">pip</option>
              <option value="go">go</option>
            </SelectV2>
          </div>
          <CodeBlock
            filename={ecosystem}
            copyName={t('cascade.quick.copyClientCommand')}
            code={clientCommand}
          />
          <p className="text-[11px]" style={{ color: 'var(--text-soft)' }}>
            <Link to={getAdminRouteHref('connect')} className="stripe-focus-ring rounded-sm text-[var(--brand-text)] no-underline hover:underline">
              {t('cascade.quick.viewAllClients')}
            </Link>
          </p>
        </div>
      ),
    },
    {
      key: 'parent',
      label: t('cascade.quick.tabParent'),
      content: (
        <div className="space-y-3 pt-3">
          <CodeBlock
            filename="child config.toml"
            copyName={t('cascade.quick.copyChildConfig')}
            code={childSnippet}
          />
          <CodeBlock
            filename={`curl · ${t('cascade.quick.relayCheck')}`}
            copyName={t('cascade.quick.copyRelayCheck')}
            code={childRelayCheck}
          />
          <p className="text-[11px] leading-5" style={{ color: 'var(--text-soft)' }}>
            {t('cascade.quick.relayCheckHint')}
          </p>
        </div>
      ),
    },
    {
      key: 'child',
      label: t('cascade.quick.tabChild'),
      content: (
        <div className="space-y-3 pt-3">
          <CodeBlock
            filename="config.toml"
            copyName={t('cascade.quick.copyUpstreamConfig')}
            code={parentSnippet}
          />
          <CodeBlock
            filename={`curl · ${t('cascade.quick.relayCheck')}`}
            copyName={t('cascade.quick.copyRelayCheck')}
            code={parentChecks}
          />
          <InlineNotice tone="warning">{t('cascade.quick.loopWarning')}</InlineNotice>
        </div>
      ),
    },
  ]

  return (
    <section aria-labelledby="cascade-quick-heading">
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h2 id="cascade-quick-heading" className="text-[14px] font-[600]" style={{ color: 'var(--text)' }}>
          {t('cascade.quick.title')}
        </h2>
        <p className="text-[11px]" style={{ color: 'var(--text-soft)' }}>{t('cascade.quick.hint')}</p>
      </div>
      <TabsV2
        ariaLabel={t('cascade.quick.title')}
        value={tab}
        onValueChange={value => setTab(value as QuickTab)}
        items={tabs}
      />
    </section>
  )
}
