import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router'

import Badge from '@/components/app/badge'
import { adminNavigationGroups, resolveAdminRoute } from '../routes'

/** Page-level destinations are the same workspace projection used by the sidebar. */
export default function AdminLocalNav() {
  const { t } = useTranslation()
  const { pathname } = useLocation()
  const currentRoute = resolveAdminRoute(pathname)
  const workspace = adminNavigationGroups.find(group => group.id === currentRoute?.navGroup)

  if (!workspace || workspace.routes.length < 2 || currentRoute?.id === 'connect') return null

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
