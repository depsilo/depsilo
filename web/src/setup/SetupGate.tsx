import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import Button from '@/components/Button'
import { setupApi } from '@/lib/api'
import { lazyRoute } from '@/routing/lazyRoute'

const SetupWizard = lazyRoute(() => import('./SetupWizard'), { surface: 'page' })

function SetupChecking() {
  const { t } = useTranslation()
  return (
    <main
      data-setup-gate-state="checking"
      role="status"
      aria-live="polite"
      aria-busy="true"
      className="grid min-h-screen place-items-center p-4"
    >
      <p className="text-[13px] text-[var(--text-soft)]">{t('setupGate.checking')}</p>
    </main>
  )
}

function SetupUnavailable({ retrying, onRetry }: { retrying: boolean; onRetry: () => void }) {
  const { t } = useTranslation()
  const titleId = useId()
  const errorRef = useRef<HTMLElement>(null)

  useEffect(() => {
    errorRef.current?.focus()
  }, [])

  return (
    <main className="grid min-h-screen place-items-center p-4">
      <section
        ref={errorRef}
        data-setup-gate-state="unavailable"
        role="alert"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="w-full max-w-[560px] rounded-[8px] border border-[var(--danger-border)] bg-[var(--bg-card)] p-5 shadow-[var(--shadow-pop)]"
      >
        <h1 id={titleId} className="text-[18px] font-[600] text-[var(--text)]">
          {t('setupGate.title')}
        </h1>
        <p className="mt-2 text-[13px] leading-6 text-[var(--text-soft)]">
          {t('setupGate.hint')}
        </p>
        <Button
          className="mt-4"
          type="button"
          disabled={retrying}
          aria-busy={retrying || undefined}
          onClick={onRetry}
        >
          {t(retrying ? 'setupGate.retrying' : 'setupGate.retry')}
        </Button>
      </section>
    </main>
  )
}

export default function SetupGate({ children }: { children: ReactNode }) {
  const [retrying, setRetrying] = useState(false)
  const status = useQuery({
    queryKey: ['setup-status'],
    queryFn: setupApi.getStatus,
    retry: false,
    staleTime: Infinity,
  })

  async function retry() {
    setRetrying(true)
    try {
      await status.refetch()
    } finally {
      setRetrying(false)
    }
  }

  if (status.isPending && !retrying) return <SetupChecking />

  if (status.isError || retrying || !status.data) {
    return (
      <SetupUnavailable
        retrying={retrying}
        onRetry={() => { void retry() }}
      />
    )
  }

  if (status.data.kind === 'setup-required') {
    return <SetupWizard tokenRequired={status.data.tokenRequired} />
  }

  return <>{children}</>
}
