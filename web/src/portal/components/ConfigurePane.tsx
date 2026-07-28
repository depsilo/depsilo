import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/portal/components/CodeBlock'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import { LANGUAGES, type ManagerConfig } from '@/lib/ecosystemData'

interface Props {
  languageId: string
  endpoint: string
  /**
   * When true, the parent surface owns the border and radius.
   */
  flush?: boolean
}

function ManagerChoice({
  manager,
  active,
  recommended,
  onChange,
}: {
  manager: ManagerConfig
  active: boolean
  recommended: boolean
  onChange: (id: string) => void
}) {
  const { t } = useTranslation()

  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={() => onChange(manager.id)}
      className="stripe-focus-ring active:scale-[0.97]"
      style={{
        display: 'inline-flex',
        minWidth: 138,
        minHeight: 48,
        flexDirection: 'column',
        justifyContent: 'center',
        padding: '7px 11px',
        background: active ? 'var(--brand-soft)' : 'var(--bg-card)',
        border: `1px solid ${active ? 'var(--brand-border)' : 'var(--border)'}`,
        borderRadius: 8,
        textAlign: 'left',
        cursor: 'pointer',
        transition:
          'background 120ms ease, border-color 120ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
      }}
    >
      <span style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
        <span
          style={{
            color: active ? 'var(--brand-text)' : 'var(--text)',
            fontSize: 14,
            fontWeight: active ? 650 : 560,
            lineHeight: 1.25,
            whiteSpace: 'nowrap',
          }}
        >
          {manager.name}
        </span>
        {recommended && (
          <span
            style={{
              padding: '1px 5px',
              color: 'var(--brand-text)',
              background: 'var(--brand-soft)',
              borderRadius: 4,
              fontSize: 12,
              fontWeight: 600,
              lineHeight: 1.4,
              whiteSpace: 'nowrap',
            }}
          >
            {t('quickstart.recommendedManager')}
          </span>
        )}
      </span>
      <span
        style={{
          overflow: 'hidden',
          marginTop: 2,
          color: 'var(--text-muted)',
          fontSize: 12,
          lineHeight: 1.25,
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
      >
        {manager.hint}
      </span>
    </button>
  )
}

function ManagerPicker({
  managers,
  active,
  onChange,
}: {
  managers: ManagerConfig[]
  active: string
  onChange: (id: string) => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const primary = managers[0]
  const alternatives = managers.slice(1)
  const visibleManagers = expanded
    ? managers
    : managers.filter(
        manager => manager.id === primary?.id || manager.id === active,
      )
  const hiddenAlternativeCount = alternatives.filter(
    manager => manager.id !== active,
  ).length

  return (
    <div role="group" aria-label={t('quickstart.managerPickerLabel')}>
      <div id="quickstart-manager-alternatives" className="flex flex-wrap gap-2">
        {visibleManagers.map(manager => (
          <ManagerChoice
            key={manager.id}
            manager={manager}
            active={manager.id === active}
            recommended={manager.id === primary?.id}
            onChange={onChange}
          />
        ))}
      </div>

      {alternatives.length > 0 && (expanded || hiddenAlternativeCount > 0) && (
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls="quickstart-manager-alternatives"
          onClick={() => setExpanded(value => !value)}
          className="stripe-focus-ring mt-2 inline-flex min-h-10 items-center gap-1 rounded-[6px] px-1 text-[13px] font-[560] text-[var(--text-muted)] hover:text-[var(--text)]"
          style={{ background: 'transparent', border: 0, cursor: 'pointer' }}
        >
          {expanded
            ? t('quickstart.hideOtherManagers')
            : t('quickstart.showOtherManagers', {
                count: hiddenAlternativeCount,
              })}
          <Icon name={expanded ? 'expand_less' : 'expand_more'} size="sm" />
        </button>
      )}
    </div>
  )
}

function PathsCollapsible({ paths }: { paths: { os: string; path: string }[] }) {
  const { t } = useTranslation()

  return (
    <details
      style={{
        border: '0.5px solid var(--border)',
        borderRadius: 6,
        background: 'var(--bg-soft)',
      }}
    >
      <summary className="stripe-focus-ring flex min-h-10 cursor-pointer list-none items-center justify-between gap-2 rounded-[6px] px-3 text-[13px] font-[540] text-[var(--text-muted)]">
        {t('quickstart.whereReadsFrom')}
        <Icon name="expand_more" size="sm" />
      </summary>
      <div style={{ borderTop: '0.5px solid var(--border)', overflow: 'hidden' }}>
        {paths.map((path, index) => (
          <div
            key={`${path.os}-${path.path}`}
            className="grid grid-cols-1 gap-1 px-3 py-2 sm:grid-cols-[120px_minmax(0,1fr)] sm:items-center sm:gap-3"
            style={{
              borderBottom:
                index < paths.length - 1 ? '0.5px solid var(--border)' : 'none',
            }}
          >
            <span
              style={{
                color: 'var(--text-muted)',
                fontSize: 12,
                fontWeight: 600,
                whiteSpace: 'nowrap',
              }}
            >
              {path.os}
            </span>
            <span
              style={{
                overflow: 'hidden',
                color: 'var(--text)',
                fontFamily: 'var(--font-mono)',
                fontSize: 12,
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
              }}
              title={path.path}
            >
              {path.path}
            </span>
          </div>
        ))}
      </div>
    </details>
  )
}

export default function ConfigurePane({ languageId, endpoint, flush = false }: Props) {
  const { t } = useTranslation()
  const language = LANGUAGES.find(item => item.id === languageId)
  const [managerId, setManagerId] = useState<string>(
    () => language?.managers[0]?.id ?? '',
  )

  if (!language) return null

  const manager =
    language.managers.find(item => item.id === managerId) ?? language.managers[0]
  if (!manager) return null

  const host = endpoint.replace(/^https?:\/\//, '')
  const plainHTTP = /^http:\/\//i.test(endpoint)
  const fill = (source: string) => {
    let value = source.replace(/\{URL\}/g, endpoint).replace(/\{HOST\}/g, host)
    if (!plainHTTP) {
      value = value
        .replace(/\ntrusted-host = [^\n]*/g, '')
        .replace(/,\n\s*"insecure-registries": \[[^\n]*\]/g, '')
        .replace(/\ninsecure = true/g, '')
    }
    return value
  }

  return (
    <div
      className={flush ? '' : 'card'}
      style={{ display: 'flex', minWidth: 0, flex: 1, flexDirection: 'column' }}
    >
      <div
        className="flex min-h-[76px] items-center gap-3 px-4 py-3 sm:px-6"
        style={{ borderBottom: '0.5px solid var(--border)' }}
      >
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            width: 36,
            height: 36,
            flex: '0 0 36px',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'var(--brand-soft)',
            border: '0.5px solid var(--brand-border)',
            borderRadius: 8,
          }}
        >
          <EcosystemIcon type={language.iconAdapter} size={19} useColor />
        </span>
        <div style={{ minWidth: 0 }}>
          <h3
            className="m-0 font-[var(--font-display)] text-[clamp(21px,2.2vw,26px)] font-[680] leading-[1.15] text-[var(--text)]"
          >
            {t('quickstart.configureTitle', { name: language.name })}
          </h3>
          <p className="mt-1 text-[12px] leading-[1.35] text-[var(--text-muted)]">
            {t('quickstart.configureLead', {
              manager: language.managers[0]?.name ?? '',
            })}
          </p>
        </div>
      </div>

      <div className="flex flex-col gap-6 p-4 sm:p-6">
        <ManagerPicker
          managers={language.managers}
          active={manager.id}
          onChange={setManagerId}
        />

        <section aria-labelledby="quickstart-config-step">
          <h4
            id="quickstart-config-step"
            className="m-0 text-[15px] font-[650] leading-[1.3] text-[var(--text)]"
          >
            {t('quickstart.configStep')}
          </h4>
          <p className="mb-3 mt-1 text-[13px] leading-[1.5] text-[var(--text-muted)]">
            {t('quickstart.configStepSubtitle', { file: manager.persistent.file })}
          </p>
          <div className="flex flex-col gap-2">
            <CodeBlock
              filename={manager.persistent.file}
              code={fill(manager.persistent.body)}
              language={manager.persistent.lang}
              copyName={manager.persistent.file}
            />
            <PathsCollapsible paths={manager.paths} />
          </div>
        </section>

        {manager.methods && manager.methods.length > 0 && (
          <details className="border-y border-[var(--border)] py-2">
            <summary className="stripe-focus-ring flex min-h-10 cursor-pointer list-none items-center justify-between gap-3 rounded-[6px] px-1">
              <span>
                <span className="block text-[13px] font-[620] text-[var(--text)]">
                  {t('quickstart.quickMethods')}
                </span>
                <span className="mt-0.5 block text-[12px] leading-[1.4] text-[var(--text-muted)]">
                  {t('quickstart.quickMethodsDescription')}
                </span>
              </span>
              <Icon name="expand_more" size="sm" />
            </summary>
            <div className="flex flex-col gap-3 pb-2 pt-3">
              {manager.methods.map(method => (
                <div key={`${method.label}-${method.body}`} className="flex flex-col gap-1">
                  <span className="px-0.5 text-[12px] font-[560] text-[var(--text-muted)]">
                    {t(method.label)}
                  </span>
                  <CodeBlock
                    code={fill(method.body)}
                    language={method.lang}
                    copyName={t(method.label)}
                  />
                </div>
              ))}
            </div>
          </details>
        )}

        <section aria-labelledby="quickstart-verify-step">
          <div className="mb-3 flex flex-wrap items-end justify-between gap-2">
            <div>
              <h4
                id="quickstart-verify-step"
                className="m-0 text-[15px] font-[650] leading-[1.3] text-[var(--text)]"
              >
                {t('quickstart.verifyStep')}
              </h4>
              <p className="mt-1 text-[13px] leading-[1.5] text-[var(--text-muted)]">
                {t('quickstart.verifyStepSubtitle')}
              </p>
            </div>
            <a
              href="/monitor"
              className="stripe-focus-ring inline-flex min-h-10 items-center gap-1 rounded-[6px] px-2 text-[13px] font-[600] text-[var(--brand-text)] no-underline hover:bg-[var(--brand-soft)]"
            >
              {t('quickstart.openMonitor')}
              <Icon name="arrow_forward" size="sm" />
            </a>
          </div>
          <CodeBlock
            code={fill(manager.verify.body)}
            language={manager.verify.lang}
            copyName={t('quickstart.verifyStep')}
          />
        </section>
      </div>
    </div>
  )
}
