import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import Icon from '@/components/Icon'
import { resolveServiceOrigin } from '@/lib/packageManagerConfig'
import CodeBlock from '@/portal/components/CodeBlock'
import StepHeading from '@/portal/components/StepHeading'
import { integrationPromptPath, type AIScope } from '@/portal/integrationPrompt'

/**
 * Handing the setup to a coding agent, as three steps: a short starter
 * prompt that points the agent at this service's integration guide, the full
 * guide for agents that cannot reach it, and what to check in the agent's
 * change. With a scope the guide and the starter cover that ecosystem only.
 */
export function AIIntegrationSteps({ scope }: { scope?: AIScope }) {
  const { t } = useTranslation()
  const endpoint = resolveServiceOrigin()
  const path = integrationPromptPath(scope)
  const guideUrl = `${endpoint}${path}`
  const idPrefix = scope ? `quickstart-ai-${scope.id}` : 'quickstart-ai'
  const starter = scope
    ? t('quickstart.ai.starterScoped', { guide: guideUrl, name: scope.name })
    : t('quickstart.ai.starter', { guide: guideUrl })

  const full = useQuery<string>({
    queryKey: ['integration-prompt', scope?.id ?? null],
    queryFn: async ({ signal }) => {
      const response = await fetch(path, { signal })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      return response.text()
    },
    staleTime: 5 * 60 * 1000,
  })

  return (
    <>
      <section aria-labelledby={`${idPrefix}-starter`}>
        <StepHeading
          id={`${idPrefix}-starter`}
          number={1}
          title={t('quickstart.ai.starterTitle')}
          description={scope
            ? t('quickstart.ai.starterScopedDescription', { name: scope.name })
            : t('quickstart.ai.starterDescription')}
        />
        <CodeBlock
          filename={t('quickstart.ai.starterFile')}
          code={starter}
          language="text"
          copyName={t('quickstart.ai.starterFile')}
          flat
          wrap
        />
        <p className="mb-0 mt-2 flex items-start gap-1.5 text-[12px] leading-[1.5] text-[var(--text-subtle)]">
          <Icon name="info" size="sm" className="mt-px shrink-0" />
          {t('quickstart.ai.reachNote', { endpoint })}
        </p>
      </section>

      <section aria-labelledby={`${idPrefix}-full`}>
        <StepHeading
          id={`${idPrefix}-full`}
          number={2}
          title={t('quickstart.ai.fullTitle')}
          description={scope
            ? t('quickstart.ai.fullScopedDescription', { name: scope.name })
            : t('quickstart.ai.fullDescription')}
        />
        {full.isError ? (
          <p role="alert" className="m-0 text-[13px] text-[var(--danger-text)]">
            {t('quickstart.aiIntegrationError')}
          </p>
        ) : (
          <details
            data-quickstart-full-prompt
            className="config-disclosure overflow-hidden rounded-md"
            style={{ border: '1px solid var(--border)', background: 'var(--bg-card)' }}
          >
            <summary className="stripe-focus-ring flex min-h-10 cursor-pointer list-none items-center justify-between gap-2 rounded-md px-3 text-[13px] font-[500] text-[var(--text-muted)]">
              {full.data
                ? t('quickstart.ai.fullShow', { lines: full.data.split('\n').length })
                : t('quickstart.loading')}
              <span className="disclosure-chevron inline-flex"><Icon name="expand_more" size="sm" /></span>
            </summary>
            <div className="max-h-[420px] overflow-auto p-2" style={{ borderTop: '1px solid var(--border)' }}>
              {full.data && (
                <CodeBlock
                  code={full.data}
                  language="markdown"
                  copyName={t('quickstart.ai.fullFile')}
                  flat
                  wrap
                />
              )}
            </div>
          </details>
        )}
      </section>

      <section aria-labelledby={`${idPrefix}-review`}>
        <StepHeading
          id={`${idPrefix}-review`}
          number={3}
          title={t('quickstart.ai.reviewTitle')}
          description={scope
            ? t('quickstart.ai.reviewScopedDescription', { name: scope.name })
            : t('quickstart.ai.reviewDescription')}
        />
      </section>
    </>
  )
}

/** The catalog's "AI setup" entry: the whole project handed to a coding agent. */
export default function AIIntegrationPane() {
  const { t } = useTranslation()
  return (
    <div data-quickstart-pane="ai" className="flex min-w-0 flex-1 flex-col">
      <div className="flex min-h-[72px] items-center gap-3 px-4 py-3.5 sm:px-6" style={{ borderBottom: '1px solid var(--border)' }}>
        <span
          aria-hidden="true"
          className="inline-flex size-[38px] shrink-0 items-center justify-center rounded-lg text-[var(--brand-text)]"
          style={{ background: 'var(--brand-soft)', border: '1px solid var(--brand-border)' }}
        >
          <Icon name="lightbulb" />
        </span>
        <div className="min-w-0">
          <h3 className="m-0 font-display text-[clamp(20px,2vw,24px)] font-[600] leading-[1.15] text-[var(--text)]">
            {t('quickstart.ai.paneTitle')}
          </h3>
          <p className="m-0 mt-0.5 text-[12px] text-[var(--text-muted)]">{t('quickstart.ai.agents')}</p>
        </div>
      </div>

      <div className="flex flex-col gap-7 p-4 sm:p-6 lg:p-7">
        <p className="m-0 max-w-[72ch] text-[13px] leading-[1.6] text-[var(--text-muted)]">
          {t('quickstart.ai.intro')}
        </p>
        <AIIntegrationSteps />
      </div>
    </div>
  )
}
