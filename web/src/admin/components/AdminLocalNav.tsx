import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router'

import Badge from '@/components/Badge'
import { prefetchAdminRoute } from '../lazyRoutes'
import { resolveAdminRoute, type AdminNavigationGroup } from '../routes'

interface AdminLocalNavProps {
  workspace: AdminNavigationGroup
}

/** Page-level destinations are the same workspace projection used by the sidebar. */
export default function AdminLocalNav({ workspace }: AdminLocalNavProps) {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const currentRoute = resolveAdminRoute(pathname)

  // Sibling destinations are one click apart and their chunks are a few KB, so
  // warm them as soon as the current page paints rather than on first click.
  const currentRouteId = currentRoute?.id
  useEffect(() => {
    const siblings = workspace.routes.filter(route => route.id !== currentRouteId)
    if (siblings.length === 0) return undefined
    const timer = window.setTimeout(() => {
      for (const route of siblings) prefetchAdminRoute(route.id)
    }, 0)
    return () => window.clearTimeout(timer)
  }, [currentRouteId, workspace])

  return (
    <nav
      data-admin-page-navigation={workspace.id}
      aria-label={t('nav.workspaceNavigation', { workspace: t(workspace.titleKey) })}
      className="admin-page-navigation"
    >
      <div className="admin-page-destinations">
        {workspace.routes.map(route => (
          <Link
            key={route.id}
            to={route.href}
            onPointerEnter={() => prefetchAdminRoute(route.id)}
            onFocus={() => prefetchAdminRoute(route.id)}
            aria-current={route.id === currentRoute?.id ? 'page' : undefined}
            className="stripe-focus-ring admin-page-destination"
          >
            {t(route.titleKey)}
            {route.pro && <Badge variant="pro">Pro</Badge>}
          </Link>
        ))}
      </div>
    </nav>
  )
}
