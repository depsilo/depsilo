import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router'

import Badge from '@/components/app/badge'
import { cn } from '@/lib/utils'
import { adminNavigationGroups, resolveAdminRoute } from '../routes'

export type AdminPageWidth = 'fluid' | 'readable'

interface AdminPageProps {
  children: ReactNode
  title?: ReactNode | false
  description?: ReactNode
  actions?: ReactNode
  width?: AdminPageWidth
}

/**
 * `fluid` inherits the Admin outlet's cap rather than declaring its own. The
 * cap used to be written twice — once on the outlet and once here — which is
 * two places to change and one place for the policy-status banner (rendered
 * above this frame) to drift out of alignment with the page below it.
 */
const WIDTH_CLASS: Record<AdminPageWidth, string> = {
  fluid: '',
  readable: 'max-w-3xl',
}

/**
 * Page chrome below the Admin utility bar: the canonical route title, optional
 * description, responsive actions, the workspace's destination rail, and the
 * content seam.
 *
 * The destination rail is a projection of the route manifest — it never
 * introduces a second route registry, and a workspace with a single page
 * renders no rail at all.
 */
export default function AdminPage({
  children,
  title,
  description,
  actions,
  width = 'fluid',
}: AdminPageProps) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const activeRoute = resolveAdminRoute(pathname)
  const resolvedTitle = title === false
    ? undefined
    : (title ?? (activeRoute ? t(activeRoute.titleKey) : undefined))
  const hasHeader = Boolean(resolvedTitle || description || actions)

  const workspace = adminNavigationGroups.find(group => group.id === activeRoute?.navGroup)
  const showDestinations = Boolean(
    workspace && workspace.routes.length > 1 && activeRoute?.id !== 'connect',
  )

  return (
    <div
      data-admin-page
      data-admin-page-width={width}
      className={cn('mx-auto w-full min-w-0', WIDTH_CLASS[width])}
    >
      {hasHeader && (
        <header
          data-admin-page-header
          className="mb-4 flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"
        >
          {(resolvedTitle || description) && (
            <div className="min-w-0 max-w-[72ch]">
              {resolvedTitle && (
                <h1 data-admin-page-title className="text-page-title leading-[1.25] font-semibold text-foreground">
                  {resolvedTitle}
                </h1>
              )}
              {description && (
                <div
                  data-admin-page-description
                  className={cn('text-label leading-[1.6] text-muted-foreground', resolvedTitle && 'mt-1.5')}
                >
                  {description}
                </div>
              )}
            </div>
          )}
          {actions && (
            <div
              data-admin-page-actions
              className="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto sm:justify-end"
            >
              {actions}
            </div>
          )}
        </header>
      )}

      {showDestinations && workspace && (
        <nav
          data-admin-page-navigation={workspace.id}
          aria-label={t('nav.workspaceNavigation', { workspace: t(workspace.titleKey) })}
          className="mb-5 flex items-stretch gap-3 border-b border-border sm:mb-6 sm:gap-4"
        >
          <div className="flex min-w-0 flex-1 gap-4 overflow-x-auto overscroll-x-contain sm:gap-5">
            {workspace.routes.map(route => (
              <Link
                key={route.id}
                to={route.href}
                aria-current={route.id === activeRoute?.id ? 'page' : undefined}
                className={cn(
                  'inline-flex min-h-10 shrink-0 items-center gap-1.5 border-b-2 border-transparent px-0.5 py-2 text-label font-medium whitespace-nowrap text-muted-foreground no-underline transition-colors hover:text-foreground',
                  'focus-visible:outline-offset-[-3px]',
                  'aria-[current=page]:border-primary aria-[current=page]:font-semibold aria-[current=page]:text-foreground',
                )}
              >
                {t(route.titleKey)}
                {route.pro && <Badge variant="pro">Pro</Badge>}
              </Link>
            ))}
          </div>
        </nav>
      )}

      <div data-admin-page-content className="min-w-0">
        {children}
      </div>
    </div>
  )
}
