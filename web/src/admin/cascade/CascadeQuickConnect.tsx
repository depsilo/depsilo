import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'

import { getAdminRouteHref } from '@/admin/routes'
import InlineNotice from '@/components/InlineNotice'
import InputV2 from '@/components/Input'
import SectionHeader from '@/components/SectionHeader'
import CodeBlock from '@/portal/components/CodeBlock'
import type { AdminCascadeConfigState, AdminCascadeInfo } from '@/lib/adminApi.types'
import { readLocalStorage, writeLocalStorage } from '@/lib/storage'

const publicBaseStorageKey = 'cascade-public-base'
const placeholderParentURL = 'http://10.0.0.2:23333'

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
  const [publicBase, setPublicBase] = useState(() => readLocalStorage(publicBaseStorageKey) ?? browserOrigin())

  const base = publicBase.trim().replace(/\/+$/, '') || browserOrigin()
  const relayPath = info.relay_path || '/_depsilo/relay/v1'
  const configuredPeers = config?.peers ?? []
  const runningPeers = info.peers
  const firstParent = runningPeers[0] ?? configuredPeers[0]
  const parentURL = (firstParent?.url ?? placeholderParentURL).replace(/\/+$/, '')
  const parentName = sanitizePeerName(firstParent?.name ?? 'parent')

  function updatePublicBase(value: string) {
    setPublicBase(value)
    writeLocalStorage(publicBaseStorageKey, value)
  }

  const clientSnippet = `# ${t('cascade.quick.clientSnippetComment')}
[cascade]
enabled = true
token = "<${t('cascade.quick.sharedSecretPlaceholder')}>"

[[cascade.peers]]
name = "${peerNameFromBase(base)}"
url  = "${base}"`

  const clientRelayCheck = `curl -fsS -o /dev/null -w '%{http_code}\\n' \\
  -H 'X-Depsilo-Cascade-Token: <${t('cascade.quick.sharedSecretPlaceholder')}>' \\
  -H 'X-Depsilo-Cascade-Chain: probe' \\
  -H 'X-Depsilo-Relay-Target: https://registry.npmjs.org/left-pad' \\
  ${base}${relayPath}`

  const upstreamSnippet = `# ${t('cascade.quick.upstreamSnippetComment')}
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

  const upstreamRelayCheck = `curl -fsS -o /dev/null -w '%{http_code}\\n' \\
  -H 'X-Depsilo-Cascade-Token: <${t('cascade.quick.parentSecretPlaceholder')}>' \\
  -H 'X-Depsilo-Cascade-Chain: probe' \\
  -H 'X-Depsilo-Relay-Target: https://registry.npmjs.org/left-pad' \\
  ${parentURL}${relayPath}`

  return (
    <section aria-labelledby="cascade-quick-heading">
      <SectionHeader title={t('cascade.quick.title')} hint={t('cascade.quick.hint')} />
      <span id="cascade-quick-heading" className="sr-only">{t('cascade.quick.title')}</span>
      <div className="grid grid-cols-1 gap-x-8 gap-y-8 xl:grid-cols-2">
        <div className="min-w-0 space-y-4">
          <div>
            <h3 className="text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
              {t('cascade.quick.clientTitle')}
            </h3>
            <p className="mt-1 text-[12px] leading-5" style={{ color: 'var(--text-soft)' }}>
              {t('cascade.quick.clientHint')}
            </p>
          </div>
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
          <div className="space-y-3">
            <CodeBlock
              filename="npm"
              copyName={t('cascade.quick.copyNpm')}
              code={`npm config set registry ${base}/npm`}
            />
            <CodeBlock
              filename="pip"
              copyName={t('cascade.quick.copyPip')}
              code={`pip config set global.index-url ${base}/pypi/simple/`}
            />
            <CodeBlock
              filename="go"
              copyName={t('cascade.quick.copyGo')}
              code={`go env -w GOPROXY=${base}/go,direct`}
            />
            <CodeBlock
              filename="child-depsilo config.toml"
              copyName={t('cascade.quick.copyChildConfig')}
              code={clientSnippet}
            />
            <CodeBlock
              filename={`curl · ${t('cascade.quick.relayCheck')}`}
              copyName={t('cascade.quick.copyRelayCheck')}
              code={clientRelayCheck}
            />
          </div>
          <p className="text-[11px] leading-5" style={{ color: 'var(--text-soft)' }}>
            {t('cascade.quick.relayCheckHint')}{' '}
            <Link to={getAdminRouteHref('connect')} className="stripe-focus-ring rounded-sm text-[var(--brand-text)] no-underline hover:underline">
              {t('cascade.quick.viewAllClients')}
            </Link>
          </p>
        </div>

        <div className="min-w-0 space-y-4">
          <div>
            <h3 className="text-[13px] font-[600]" style={{ color: 'var(--text)' }}>
              {t('cascade.quick.upstreamTitle')}
            </h3>
            <p className="mt-1 text-[12px] leading-5" style={{ color: 'var(--text-soft)' }}>
              {t('cascade.quick.upstreamHint')}
            </p>
          </div>
          {runningPeers.length > 0 ? (
            <p className="text-[12px]" style={{ color: 'var(--text-muted)' }}>
              {t('cascade.quick.upstreamConfigured', {
                peers: runningPeers.map(peer => `${peer.name} (${peer.url})`).join(', '),
              })}
            </p>
          ) : (
            <InlineNotice tone="info">{t('cascade.quick.noParentYet')}</InlineNotice>
          )}
          <div className="space-y-3">
            <CodeBlock
              filename="config.toml"
              copyName={t('cascade.quick.copyUpstreamConfig')}
              code={upstreamSnippet}
            />
            <CodeBlock
              filename={`curl · ${t('cascade.quick.relayCheck')}`}
              copyName={t('cascade.quick.copyRelayCheck')}
              code={upstreamRelayCheck}
            />
            {runningPeers.map(peer => (
              <CodeBlock
                key={peer.name}
                filename={`curl · ${peer.name}`}
                copyName={t('cascade.quick.copyPeerCheck', { name: peer.name })}
                code={`curl -fsS ${peer.url.replace(/\/+$/, '')}/health`}
              />
            ))}
          </div>
          <p className="text-[11px] leading-5" style={{ color: 'var(--text-soft)' }}>
            {t('cascade.quick.restartNote')}
          </p>
          <InlineNotice tone="warning">{t('cascade.quick.loopWarning')}</InlineNotice>
        </div>
      </div>
    </section>
  )
}
