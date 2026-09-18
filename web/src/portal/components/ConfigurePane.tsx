import { ChevronDown, Link2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import CodeBlock from '@/components/app/code-block'
import EcosystemIcon from '@/components/app/ecosystem-icon'
import { LANGUAGES, type ManagerConfig } from '@/lib/ecosystemData'
import { renderManagerTemplate, resolveServiceOrigin } from '@/lib/packageManagerConfig'
import PyTorchIndexNotice from '@/portal/components/PyTorchIndexNotice'
import { cn } from '@/lib/utils'

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
      className={cn(
        'inline-flex min-h-10 min-w-[82px] cursor-pointer items-center justify-center rounded-sm border border-transparent px-3 py-1.5 text-center transition-[background,box-shadow,color,transform] duration-150 active:scale-[0.97]',
        active ? 'bg-card text-primary shadow-surface' : 'bg-transparent text-foreground',
      )}
    >
      <span className={cn('whitespace-nowrap text-body leading-tight', active ? 'font-semibold' : 'font-medium')}>
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
          className="text-label font-semibold leading-[1.3] text-muted-foreground"
        >
          {t('quickstart.managerPickerLabel')}
        </span>
        <span className="font-mono text-meta text-muted-foreground">
          {t('quickstart.managerCount', { count: managers.length })}
        </span>
      </div>
      <div
        className="overflow-x-auto rounded-lg bg-muted p-1 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]"
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
        className="mb-0 mt-2 text-label leading-[1.5] text-muted-foreground"
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
        className="mt-0.5 font-mono text-meta font-semibold leading-[1.4] text-primary"
      >
        {String(number).padStart(2, '0')}
      </span>
      <div>
        <h4
          id={id}
          className="m-0 text-title font-semibold leading-[1.3] text-foreground"
        >
          {title}
        </h4>
        <p className="mb-0 mt-1 text-body leading-[1.5] text-muted-foreground">
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
      className="group overflow-hidden rounded-sm border border-border bg-card [&>summary::-webkit-details-marker]:hidden"
    >
      <summary className="flex min-h-10 cursor-pointer list-none items-center justify-between gap-2 rounded-sm px-3 text-body font-medium text-muted-foreground">
        {t('quickstart.whereReadsFrom')}
        <span className="inline-flex text-muted-foreground transition-transform group-open:rotate-180 group-open:text-foreground">
          <ChevronDown className="icon icon-sm" aria-hidden="true" />
        </span>
      </summary>
      <div className="divide-y divide-border overflow-hidden border-t border-border">
        {paths.map((path) => (
          <div
            key={`${path.os}-${path.path}`}
            className="grid grid-cols-1 gap-1 px-3 py-2 sm:grid-cols-[120px_minmax(0,1fr)] sm:items-center sm:gap-3"
          >
            <span className="whitespace-nowrap font-semibold text-label text-muted-foreground">
              {path.os}
            </span>
            <span
              className="truncate font-mono text-label text-foreground"
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
      className={flush
        ? 'flex min-w-0 flex-1 flex-col'
        : 'flex min-w-0 flex-1 flex-col rounded-lg border border-border bg-card shadow-card'}
    >
      <div
        className="flex min-h-18 items-center gap-3 border-b border-border px-4 py-3.5 sm:px-6"
      >
        <span
          aria-hidden="true"
          className="inline-flex size-10 shrink-0 items-center justify-center rounded-sm border border-border bg-accent"
        >
          <EcosystemIcon type={language.iconAdapter} size={20} useColor />
        </span>
        <div className="min-w-0">
          <h3
            className="m-0 text-subhead font-semibold text-foreground"
          >
            {t('quickstart.configureTitle', { name: language.name })}
          </h3>
        </div>
        <div
          className="ml-auto hidden min-w-0 max-w-[48%] items-center gap-2 rounded-sm px-2.5 py-1.5 min-[640px]:flex bg-muted"
          title={resolvedEndpoint}
        >
          <Link2 className="shrink-0 text-muted-foreground icon icon-sm" aria-hidden="true" />
          <span className="sr-only">{t('quickstart.endpointLabel')}</span>
          <span className="truncate font-mono text-meta text-muted-foreground">
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

        <div key={manager.id} className="flex flex-col gap-7">
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
            <details className="group border-y border-border py-2 [&>summary::-webkit-details-marker]:hidden">
              <summary className="flex min-h-12 cursor-pointer list-none items-center justify-between gap-3 rounded-sm px-1">
                <span className="flex items-start gap-3">
                  <span
                    aria-hidden="true"
                    className="mt-0.5 font-mono text-meta font-semibold leading-[1.4] text-primary"
                  >
                    02
                  </span>
                  <span>
                    <span className="block text-body font-semibold text-foreground">
                      {t('quickstart.quickMethods')}
                    </span>
                    <span className="mt-0.5 block text-label leading-[1.45] text-muted-foreground">
                      {t('quickstart.quickMethodsDescription')}
                    </span>
                  </span>
                </span>
                <span className="inline-flex text-muted-foreground transition-transform group-open:rotate-180 group-open:text-foreground">
                  <ChevronDown className="icon icon-sm" aria-hidden="true" />
                </span>
              </summary>
              <div className="flex flex-col gap-3 pb-2 pt-3">
                {manager.methods.map(method => (
                  <div key={`${method.label}-${method.body}`} className="flex flex-col gap-1">
                    <span className="px-0.5 text-label font-medium text-muted-foreground">
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
