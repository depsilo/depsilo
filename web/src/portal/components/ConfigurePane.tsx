import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/portal/components/CodeBlock'
import EcosystemIcon from '@/components/EcosystemIcon'
import Icon from '@/components/Icon'
import { LANGUAGES, type ManagerConfig } from '@/lib/ecosystemData'
import { renderManagerTemplate, resolveServiceOrigin } from '@/lib/packageManagerConfig'
import PyTorchIndexNotice from '@/portal/components/PyTorchIndexNotice'

interface Props {
  languageId: string
  endpoint: string
  pytorchIndexPath?: string
  /**
   * When true, the parent surface owns the border and radius.
   */
  flush?: boolean
}

function managerHintKey(managerId: string): string {
  const normalized = managerId.replace(/-([a-z])/g, (_, letter: string) =>
    letter.toUpperCase(),
  )
  return `quickstart.managerHints.${normalized}`
}

function ManagerChoice({
  manager,
  active,
  onChange,
}: {
  manager: ManagerConfig
  active: boolean
  onChange: (id: string) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      data-active={active ? 'true' : undefined}
      onClick={() => onChange(manager.id)}
      className="manager-choice stripe-focus-ring active:scale-[0.97]"
      style={{
        display: 'flex',
        minWidth: 82,
        minHeight: 40,
        alignItems: 'center',
        justifyContent: 'center',
        padding: '6px 11px',
        background: active ? 'var(--bg-card)' : 'transparent',
        border: '1px solid transparent',
        borderRadius: 6,
        boxShadow: active ? 'var(--shadow-surface)' : 'none',
        textAlign: 'center',
        cursor: 'pointer',
        transition:
          'background 140ms ease, box-shadow 140ms ease, color 140ms ease, transform 120ms cubic-bezier(0.2, 0, 0, 1)',
      }}
    >
      <span
        style={{
          color: active ? 'var(--brand-text)' : 'var(--text)',
          fontSize: 13,
          fontWeight: active ? 660 : 540,
          lineHeight: 1.25,
          whiteSpace: 'nowrap',
        }}
      >
        {manager.name}
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
  const titleId = useId()
  const activeManager = managers.find(manager => manager.id === active) ?? managers[0]
  const activeHint = activeManager
    ? t(managerHintKey(activeManager.id), {
        defaultValue: activeManager.hint,
      })
    : ''

  return (
    <div role="group" aria-labelledby={titleId}>
      <div className="mb-2 flex items-baseline justify-between gap-3">
        <span
          id={titleId}
          className="text-[12px] font-[650] leading-[1.3] text-[var(--text-muted)]"
        >
          {t('quickstart.managerPickerLabel')}
        </span>
        <span className="font-[var(--font-mono)] text-[11px] text-[var(--text-subtle)]">
          {t('quickstart.managerCount', { count: managers.length })}
        </span>
      </div>
      <div
        className="manager-picker-viewport overflow-x-auto rounded-[8px] p-1"
        style={{ background: 'var(--bg-soft)' }}
      >
        <div
          className="grid gap-1"
          style={{
            gridTemplateColumns: `repeat(${managers.length}, minmax(82px, 1fr))`,
            minWidth: `max(100%, ${managers.length * 86}px)`,
          }}
        >
          {managers.map(manager => (
            <ManagerChoice
              key={manager.id}
              manager={manager}
              active={manager.id === active}
              onChange={onChange}
            />
          ))}
        </div>
      </div>
      <p
        data-manager-description
        className="mb-0 mt-2 text-[12px] leading-[1.5] text-[var(--text-muted)]"
      >
        {activeHint}
      </p>
    </div>
  )
}

function StepHeading({
  id,
  number,
  title,
  description,
}: {
  id: string
  number: number
  title: string
  description: string
}) {
  return (
    <div className="mb-3 flex items-start gap-3">
      <span
        aria-hidden="true"
        className="mt-0.5 font-[var(--font-mono)] text-[11px] font-[650] leading-[1.4] text-[var(--brand-text)]"
      >
        {String(number).padStart(2, '0')}
      </span>
      <div>
        <h4
          id={id}
          className="m-0 text-[15px] font-[660] leading-[1.3] text-[var(--text)]"
        >
          {title}
        </h4>
        <p className="mb-0 mt-1 text-[13px] leading-[1.5] text-[var(--text-muted)]">
          {description}
        </p>
      </div>
    </div>
  )
}

function PathsCollapsible({ paths }: { paths: { os: string; path: string }[] }) {
  const { t } = useTranslation()

  return (
    <details
      className="config-disclosure overflow-hidden rounded-[7px]"
      style={{
        border: '1px solid var(--border)',
        background: 'var(--bg-card)',
      }}
    >
      <summary className="stripe-focus-ring flex min-h-10 cursor-pointer list-none items-center justify-between gap-2 rounded-[6px] px-3 text-[13px] font-[540] text-[var(--text-muted)]">
        {t('quickstart.whereReadsFrom')}
        <span className="disclosure-chevron inline-flex">
          <Icon name="expand_more" size="sm" />
        </span>
      </summary>
      <div style={{ borderTop: '1px solid var(--border)', overflow: 'hidden' }}>
        {paths.map((path, index) => (
          <div
            key={`${path.os}-${path.path}`}
            className="grid grid-cols-1 gap-1 px-3 py-2 sm:grid-cols-[120px_minmax(0,1fr)] sm:items-center sm:gap-3"
            style={{
              borderBottom:
                index < paths.length - 1 ? '1px solid var(--border)' : 'none',
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

export default function ConfigurePane({
  languageId,
  endpoint,
  pytorchIndexPath,
  flush = false,
}: Props) {
  const { t } = useTranslation()
  const language = LANGUAGES.find(item => item.id === languageId)
  const [managerId, setManagerId] = useState<string>(
    () => language?.managers[0]?.id ?? '',
  )

  if (!language) return null

  const resolvedEndpoint = resolveServiceOrigin(endpoint)

  const manager =
    language.managers.find(item => item.id === managerId) ?? language.managers[0]
  if (!manager) return null

  const pytorchClient =
    manager.id === 'uv'
      ? 'uv'
      : manager.id === 'pip' || manager.id === 'venv'
        ? 'pip'
        : null

  const fill = (source: string) => renderManagerTemplate(source, resolvedEndpoint)

  return (
    <div
      className={flush ? '' : 'card'}
      style={{ display: 'flex', minWidth: 0, flex: 1, flexDirection: 'column' }}
    >
      <div
        className="flex min-h-[72px] items-center gap-3 px-4 py-3.5 sm:px-6"
        style={{ borderBottom: '1px solid var(--border)' }}
      >
        <span
          aria-hidden="true"
          style={{
            display: 'inline-flex',
            width: 38,
            height: 38,
            flex: '0 0 38px',
            alignItems: 'center',
            justifyContent: 'center',
            background: 'var(--brand-soft)',
            border: '1px solid var(--brand-border)',
            borderRadius: 8,
          }}
        >
          <EcosystemIcon type={language.iconAdapter} size={20} useColor />
        </span>
        <div style={{ minWidth: 0 }}>
          <h3
            className="m-0 font-[var(--font-display)] text-[clamp(20px,2vw,24px)] font-[680] leading-[1.15] text-[var(--text)]"
          >
            {t('quickstart.configureTitle', { name: language.name })}
          </h3>
        </div>
        <div
          className="ml-auto hidden min-w-0 max-w-[48%] items-center gap-2 rounded-[6px] px-2.5 py-1.5 min-[640px]:flex"
          style={{ background: 'var(--bg-soft)' }}
          title={resolvedEndpoint}
        >
          <Icon name="link" size="sm" className="shrink-0 text-[var(--text-subtle)]" />
          <span className="sr-only">{t('quickstart.endpointLabel')}</span>
          <span className="truncate font-[var(--font-mono)] text-[11px] text-[var(--text-muted)]">
            {resolvedEndpoint}
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-7 p-4 sm:p-6 lg:p-7">
        <ManagerPicker
          managers={language.managers}
          active={manager.id}
          onChange={setManagerId}
        />

        <div key={manager.id} className="manager-config-swap flex flex-col gap-7">
          <section aria-labelledby="quickstart-config-step">
            <StepHeading
              id="quickstart-config-step"
              number={1}
              title={t('quickstart.configStep')}
              description={t('quickstart.configStepSubtitle', {
                file: manager.persistent.file,
              })}
            />
            <div className="flex flex-col gap-3">
              <CodeBlock
                filename={manager.persistent.file}
                code={fill(manager.persistent.body)}
                language={manager.persistent.lang}
                copyName={manager.persistent.file}
                tone="ink"
              />
              <PathsCollapsible paths={manager.paths} />
            </div>
          </section>

          {manager.methods && manager.methods.length > 0 && (
            <details className="config-disclosure border-y border-[var(--border)] py-2">
              <summary className="stripe-focus-ring flex min-h-12 cursor-pointer list-none items-center justify-between gap-3 rounded-[6px] px-1">
                <span className="flex items-start gap-3">
                  <span
                    aria-hidden="true"
                    className="mt-0.5 font-[var(--font-mono)] text-[11px] font-[650] leading-[1.4] text-[var(--brand-text)]"
                  >
                    02
                  </span>
                  <span>
                    <span className="block text-[14px] font-[640] text-[var(--text)]">
                      {t('quickstart.quickMethods')}
                    </span>
                    <span className="mt-0.5 block text-[12px] leading-[1.45] text-[var(--text-muted)]">
                      {t('quickstart.quickMethodsDescription')}
                    </span>
                  </span>
                </span>
                <span className="disclosure-chevron inline-flex">
                  <Icon name="expand_more" size="sm" />
                </span>
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
            <StepHeading
              id="quickstart-verify-step"
              number={manager.methods && manager.methods.length > 0 ? 3 : 2}
              title={t('quickstart.verifyStep')}
              description={t('quickstart.verifyStepSubtitle')}
            />
            <CodeBlock
              code={fill(manager.verify.body)}
              language={manager.verify.lang}
              copyName={t('quickstart.verifyStep')}
            />
          </section>

          {language.id === 'python' && pytorchClient && pytorchIndexPath && (
            <PyTorchIndexNotice
              endpoint={resolvedEndpoint}
              path={pytorchIndexPath}
              client={pytorchClient}
            />
          )}
        </div>
      </div>
    </div>
  )
}
