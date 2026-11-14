import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { Toggle } from '@/components/ui/toggle'
import { resolveServiceOrigin } from '@/lib/packageManagerConfig'
import CodeBlock from '@/portal/components/CodeBlock'
import StepHeading from '@/portal/components/StepHeading'

type Client = 'ccache' | 'sccache'

const CLIENTS: Client[] = ['ccache', 'sccache']

/**
 * The client setting with placeholders for the namespace and build token
 * an operator issues in the Admin. Mirrors the format the Admin generates
 * with a real token (internal/api/admin/compile_cache.go): ccache reaches
 * HTTPS storage through a helper, which only receives `@`-prefixed
 * attributes.
 */
function clientConfig(client: Client, endpoint: string, namespace: string, token: string): string {
  if (client === 'sccache') {
    return `[cache.webdav]\nendpoint = "${endpoint}/sccache/v1/${namespace}"\ntoken = "${token}"`
  }
  const bearer = endpoint.toLowerCase().startsWith('https://') ? '@bearer-token' : 'bearer-token'
  return `export CCACHE_REMOTE_STORAGE='${endpoint}/ccache/v1/${namespace}|${bearer}=${token}'`
}

/**
 * The catalog's compile cache entry, in the same pane shape as an
 * ecosystem: a client picker and numbered steps.
 */
export default function CompileCachePane() {
  const { t } = useTranslation()
  const endpoint = resolveServiceOrigin()
  const [client, setClient] = useState<Client>('ccache')
  const config = clientConfig(client, endpoint, t('quickstart.cc.namespacePlaceholder'), t('quickstart.cc.tokenPlaceholder'))
  const verify = `${client} --zero-stats\n${client} gcc -c hello.c -o hello.o\n${client} --show-stats`

  return (
    <div data-quickstart-pane="compile-cache" className="flex min-w-0 flex-1 flex-col">
      <div className="flex min-h-[72px] flex-wrap items-center gap-3 px-4 py-3.5 sm:px-6" style={{ borderBottom: '1px solid var(--border)' }}>
        <span
          aria-hidden="true"
          className="inline-flex size-[38px] shrink-0 items-center justify-center rounded-lg text-[var(--brand-text)]"
          style={{ background: 'var(--brand-soft)', border: '1px solid var(--brand-border)' }}
        >
          <Icon name="memory" />
        </span>
        <h3 className="m-0 font-display text-[clamp(20px,2vw,24px)] font-[600] leading-[1.15] text-[var(--text)]">
          {t('quickstart.cc.paneTitle')}
        </h3>
        <span
          className="rounded px-1.5 py-0.5 text-[12px] font-[500] text-[var(--text-muted)]"
          style={{ background: 'var(--bg-soft)', border: '1px solid var(--border)' }}
        >
          {t('quickstart.catalog.optionalGroup')}
        </span>
      </div>

      <div className="flex flex-col gap-7 p-4 sm:p-6 lg:p-7">
        <div role="group" aria-labelledby="quickstart-cc-clients" className="flex flex-col gap-2">
          <span id="quickstart-cc-clients" className="text-[12px] font-[600] leading-[1.3] text-[var(--text-muted)]">
            {t('quickstart.cc.clientLabel')}
          </span>
          <div className="grid grid-cols-2 gap-1 rounded-md p-1" style={{ background: 'var(--bg-soft)' }}>
            {CLIENTS.map(item => {
              const active = item === client
              return (
                <Toggle
                  key={item}
                  type="button"
                  pressed={active}
                  onPressedChange={() => setClient(item)}
                  className="manager-choice stripe-focus-ring min-h-10 active:scale-[0.97]"
                  style={{
                    minHeight: 40,
                    justifyContent: 'center',
                    background: active ? 'var(--bg-card)' : 'transparent',
                    border: '1px solid transparent',
                    borderRadius: 6,
                    boxShadow: active ? 'var(--shadow-surface)' : 'none',
                    color: active ? 'var(--brand-text)' : 'var(--text)',
                    fontSize: 13,
                    fontWeight: active ? 600 : 500,
                  }}
                >
                  {item}
                </Toggle>
              )
            })}
          </div>
          <p className="m-0 text-[12px] leading-[1.5] text-[var(--text-muted)]">
            {t(client === 'ccache' ? 'quickstart.cc.ccacheHint' : 'quickstart.cc.sccacheHint')}
          </p>
        </div>

        <section aria-labelledby="quickstart-cc-credential">
          <StepHeading
            id="quickstart-cc-credential"
            number={1}
            title={t('quickstart.cc.credentialTitle')}
            description={t('quickstart.cc.credentialDescription')}
          />
          <a
            href="/admin/compile-cache"
            className="stripe-focus-ring inline-flex min-h-10 items-center gap-2 rounded-md px-3 text-[13px] font-[500] no-underline"
            style={{ color: 'var(--btn-fg)', background: 'var(--btn)' }}
          >
            {t('quickstart.compileCacheAction')}
            <Icon name="arrow_forward" size="sm" />
          </a>
        </section>

        <section aria-labelledby="quickstart-cc-client">
          <StepHeading
            id="quickstart-cc-client"
            number={2}
            title={t('quickstart.cc.configTitle')}
            description={t(client === 'ccache' ? 'quickstart.cc.ccacheConfigDescription' : 'quickstart.cc.sccacheConfigDescription')}
          />
          <CodeBlock
            filename={client === 'ccache' ? 'shell' : '~/.config/sccache/config'}
            code={config}
            language={client === 'ccache' ? 'sh' : 'toml'}
            copyName={client === 'ccache' ? 'CCACHE_REMOTE_STORAGE' : t('quickstart.cc.sccacheConfigName')}
            flat
          />
        </section>

        <section aria-labelledby="quickstart-cc-verify">
          <StepHeading
            id="quickstart-cc-verify"
            number={3}
            title={t('quickstart.verifyStep')}
            description={t('quickstart.cc.verifyDescription')}
          />
          <CodeBlock code={verify} language="sh" copyName={t('quickstart.verifyStep')} flat />
        </section>

        <details className="config-disclosure border-t border-[var(--border)] pt-1">
          <summary className="stripe-focus-ring -mx-1 flex min-h-10 cursor-pointer list-none items-center justify-between gap-3 rounded-md px-1 text-[13px] font-[500] text-[var(--text-muted)] hover:text-[var(--text)]">
            {t('quickstart.compileCacheDetails')}
            <span className="disclosure-chevron inline-flex"><Icon name="expand_more" size="sm" /></span>
          </summary>
          <ul className="mb-1 mt-2 flex list-none flex-col gap-2 p-0 text-[12px] leading-[1.5] text-[var(--text-muted)]">
            <li className="flex items-start gap-2"><Icon name="cloud_sync" size="sm" className="mt-0.5 shrink-0 text-[var(--brand-text)]" /><span>{t('quickstart.compileCacheProtocol')}</span></li>
            <li className="flex items-start gap-2"><Icon name="storage" size="sm" className="mt-0.5 shrink-0 text-[var(--brand-text)]" /><span>{t('quickstart.compileCacheStorage')}</span></li>
            <li className="flex items-start gap-2"><Icon name="info" size="sm" className="mt-0.5 shrink-0 text-[var(--warn-text)]" /><span>{t('quickstart.compileCacheLimitation')}</span></li>
          </ul>
        </details>
      </div>
    </div>
  )
}
