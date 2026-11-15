import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router'

import { resolveAdminRoute, resolveWorkspaceNavigation } from '../routes'
import AdminLocalNav from './AdminLocalNav'
import PolicyStatusNotice from './PolicyStatusNotice'

export type AdminPageWidth = 'fluid' | 'readable'

interface AdminPageProps {
  children: ReactNode
  title?: ReactNode | false
  description?: ReactNode
  /** Compact status shown on the title's own line, for a page whose header is its summary. */
  titleAccessory?: ReactNode
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
 *
 * When the route belongs to a workspace with sibling destinations the tab rail
 * takes the header slot — the header *is* the tab options — and the route title
 * stays in the accessibility tree as the h1.
 */
export default function AdminPage({
  children,
  title,
  description,
  titleAccessory,
  actions,
  width = 'fluid',
}: AdminPageProps) {
  const { t } = useTranslation()
  const location = useLocation()
  const activeRoute = resolveAdminRoute(location.pathname)
  const resolvedTitle = title === false
    ? undefined
    : (title ?? (activeRoute ? t(activeRoute.titleKey) : undefined))
  const workspaceTabs = resolveWorkspaceNavigation(location.pathname)
  const hasHeader = Boolean(resolvedTitle || description || actions)

  return (
    <div
      data-admin-page
      data-admin-page-width={width}
      className={`mx-auto min-w-0 w-full ${widthClasses[workspaceTabs ? 'fluid' : width]}`}
    >
      {hasHeader && (
        <header data-admin-page-header className="mb-4 min-w-0">
          {workspaceTabs ? (
            <>
              {resolvedTitle && (
                <h1 data-admin-page-title className="sr-only">
                  {resolvedTitle}
                </h1>
              )}
              {/* The rail owns the header band; the destinations' own copy and
                  commands belong to the selected tab, one level below it. */}
              <AdminLocalNav workspace={workspaceTabs} />
              {(description || actions) && (
                <div className="mt-2 flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                  {description && (
                    <div
                      data-admin-page-description
                      className="min-w-0 max-w-[72ch] text-[13px] leading-[1.6]"
                      style={{ color: 'var(--text-soft)' }}
                    >
                      {description}
                    </div>
                  )}
                  {actions && (
                    <div data-admin-page-actions className="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:justify-end">
                      {actions}
                    </div>
                  )}
                </div>
              )}
            </>
          ) : (
            <>
              {/* A page without sibling destinations uses the same header band
                  as a tabbed workspace: its title sits where the selected tab
                  would, at the same size and on the same hairline, so moving
                  between pages never shifts the top of the page. */}
              {(resolvedTitle || titleAccessory || actions) && (
                <div data-admin-page-band className="admin-page-navigation admin-page-band">
                  <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-2">
                    {resolvedTitle && (
                      <h1 data-admin-page-title className="admin-page-heading">
                        {resolvedTitle}
                      </h1>
                    )}
                    {titleAccessory}
                  </div>
                  {actions && (
                    <div data-admin-page-actions className="flex min-w-0 flex-wrap items-center gap-2 self-center sm:justify-end">
                      {actions}
                    </div>
                  )}
                </div>
              )}
              {description && (
                <div
                  data-admin-page-description
                  className="mt-2 min-w-0 max-w-[72ch] text-[13px] leading-[1.6]"
                  style={{ color: 'var(--text-soft)' }}
                >
                  {description}
                </div>
              )}
            </>
          )}
        </header>
      )}
      <div data-admin-page-content className={`min-w-0 ${workspaceTabs && width === 'readable' ? 'max-w-3xl' : ''}`}>
        {/* Policy runtime status belongs to the page that owns policy, not to a
            shell banner above every page. Overview surfaces it in the
            attention queue instead. */}
        {activeRoute?.navGroup === 'security' && <PolicyStatusNotice />}
        {children}
      </div>
    </div>
  )
}
