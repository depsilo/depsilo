import { useEffect, useId, useRef } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'

type RouteArea = 'admin' | 'portal'

export default function RouteNotFound({ area }: { area: RouteArea }) {
  const { t } = useTranslation()
  const titleId = useId()
  const routeStateRef = useRef<HTMLElement>(null)
  const Heading = area === 'portal' ? 'h1' : 'h2'
  const destination = area === 'portal' ? '/' : '/admin'
  const action = area === 'portal' ? t('notFound.portalAction') : t('notFound.adminAction')

  useEffect(() => {
    routeStateRef.current?.focus()
  }, [])

  return (
    <section
      ref={routeStateRef}
      data-route-state="not-found"
      aria-labelledby={titleId}
      tabIndex={-1}
      className="programmatic-focus-target grid min-h-60 place-items-center px-4 py-12 text-center"
    >
      <div className="max-w-[520px]">
        <p aria-hidden="true" className="font-mono text-body font-semibold text-primary">
          404
        </p>
        <Heading id={titleId} className="mt-2 text-metric-sm font-semibold text-foreground">
          {t('notFound.title')}
        </Heading>
        <p className="mt-2 text-body leading-6 text-muted-foreground">
          {t('notFound.hint')}
        </p>
        <Link
          to={destination}
          className="stripe-focus-ring mt-5 inline-flex min-h-[40px] items-center justify-center rounded-[5px] px-3 py-1.5 text-body font-medium no-underline transition-[background,transform] duration-150 active:scale-[0.96]"
          style={{
            color: 'var(--primary-foreground)',
            background: 'var(--primary)',
            boxShadow: 'inset 0 1px 0 color-mix(in oklab, white 16%, transparent), 0 1px 2px rgba(0, 0, 0, 0.18)',
          }}
        >
          {action}
        </Link>
      </div>
    </section>
  )
}
