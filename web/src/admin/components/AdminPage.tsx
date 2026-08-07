import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router'

import { resolveAdminRoute } from '../routes'

export type AdminPageWidth = 'fluid' | 'readable'

interface AdminPageProps {
  children: ReactNode
  title?: ReactNode | false
  description?: ReactNode
  actions?: ReactNode
  width?: AdminPageWidth
}

const widthClasses: Record<AdminPageWidth, string> = {
  fluid: 'max-w-[1840px]',
  readable: 'max-w-3xl',
}

/**
 * Shared page chrome below the Admin utility bar. It owns the route title,
 * introductory copy, responsive actions, readable width, and content seam.
 */
export default function AdminPage({
  children,
  title,
  description,
  actions,
  width = 'fluid',
}: AdminPageProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const activeRoute = resolveAdminRoute(location.pathname)
  const resolvedTitle = title === false
    ? undefined
    : (title ?? (activeRoute ? t(activeRoute.titleKey) : undefined))
  const hasHeader = Boolean(resolvedTitle || description || actions)

  return (
    <div
      data-admin-page
      data-admin-page-width={width}
      className={`mx-auto min-w-0 w-full ${widthClasses[width]}`}
    >
      {hasHeader && (
        <header
          data-admin-page-header
          className={`${description ? 'mb-7' : 'mb-5'} flex min-w-0 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between`}
        >
          {(resolvedTitle || description) && (
            <div className="min-w-0 max-w-[72ch]">
              {resolvedTitle && (
                <h1
                  data-admin-page-title
                  className="text-[28px] font-[680] leading-[1.15]"
                  style={{ color: 'var(--text)', fontFamily: 'var(--font-display)' }}
                >
                  {resolvedTitle}
                </h1>
              )}
              {description && (
                <div
                  data-admin-page-description
                  className={`${resolvedTitle ? 'mt-2' : ''} text-[13px] leading-[1.55]`}
                  style={{ color: 'var(--text-soft)' }}
                >
                  {description}
                </div>
              )}
            </div>
          )}
          {actions && (
            <div data-admin-page-actions className="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:justify-end">
              {actions}
            </div>
          )}
        </header>
      )}
      <div data-admin-page-content className="min-w-0">
        {children}
      </div>
    </div>
  )
}
