import type { IconName } from '@/components/Icon'

const navGroupDefinitions = [
  { id: 'overview', titleKey: 'nav.workspaces.overview', icon: 'dashboard', landingRouteId: 'dashboard' },
  { id: 'monitor', titleKey: 'nav.workspaces.monitor', icon: 'monitoring', landingRouteId: 'accessLogs' },
  { id: 'delivery', titleKey: 'nav.workspaces.delivery', icon: 'cloud_sync', landingRouteId: 'upstreams' },
  { id: 'security', titleKey: 'nav.workspaces.security', icon: 'security', landingRouteId: 'security' },
  { id: 'administration', titleKey: 'nav.workspaces.administration', icon: 'settings', landingRouteId: 'users' },
] as const

export type AdminNavGroup = (typeof navGroupDefinitions)[number]['id']

interface AdminRouteDefinition {
  id: string
  path: string
  titleKey: string
  icon: IconName
  navGroup: AdminNavGroup
  pro?: true
  hiddenFromNavigation?: true
}

const routeDefinitions = [
  { id: 'dashboard', path: '', titleKey: 'nav.workspaces.overview', icon: 'dashboard', navGroup: 'overview' },
  { id: 'connect', path: 'connect', titleKey: 'onboarding.title', icon: 'link', navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'attention', path: 'attention', titleKey: 'nav.attention', icon: 'inbox', navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'accessLogs', path: 'logs', titleKey: 'nav.accessLogs', icon: 'receipt_long', navGroup: 'monitor' },
  { id: 'upstreamUpdates', path: 'upstream-updates', titleKey: 'nav.upstreamUpdates', icon: 'update', navGroup: 'monitor' },
  { id: 'bandwidth', path: 'bandwidth', titleKey: 'bandwidth.title', icon: 'bar_chart', navGroup: 'monitor' },
  { id: 'upstreams', path: 'upstreams', titleKey: 'nav.upstreams', icon: 'cloud_sync', navGroup: 'delivery' },
  { id: 'cache', path: 'cache', titleKey: 'nav.cacheManage', icon: 'storage', navGroup: 'delivery' },
  { id: 'cacheIndexes', path: 'indexes', titleKey: 'nav.cacheIndexes', icon: 'inventory_2', navGroup: 'delivery' },
  { id: 'compileCache', path: 'compile-cache', titleKey: 'nav.compileCache', icon: 'memory', navGroup: 'delivery' },
  { id: 'auditLogs', path: 'audit', titleKey: 'nav.auditLogs', icon: 'policy', navGroup: 'security' },
  { id: 'security', path: 'security', titleKey: 'nav.security', icon: 'security', navGroup: 'security' },
  { id: 'quarantine', path: 'quarantine', titleKey: 'nav.quarantine', icon: 'shield_lock', navGroup: 'security' },
  { id: 'rules', path: 'rules', titleKey: 'nav.rules', icon: 'shield', navGroup: 'security' },
  { id: 'projects', path: 'projects', titleKey: 'nav.projects', icon: 'folder_managed', navGroup: 'administration', pro: true },
  { id: 'users', path: 'users', titleKey: 'nav.userManage', icon: 'group', navGroup: 'administration' },
  { id: 'settings', path: 'settings', titleKey: 'nav.settings', icon: 'settings', navGroup: 'administration' },
  { id: 'license', path: 'license', titleKey: 'license.title', icon: 'key', navGroup: 'administration' },
] as const satisfies readonly AdminRouteDefinition[]

export type AdminRouteId = (typeof routeDefinitions)[number]['id']

export interface AdminRoute {
  id: AdminRouteId
  /** Path relative to the /admin route used by React Router. */
  path: string
  /** Canonical absolute URL used by navigation and exact title matching. */
  href: string
  titleKey: string
  icon: IconName
  navGroup: AdminNavGroup
  index: boolean
  pro: boolean
  hiddenFromNavigation: boolean
}

export const adminRouteManifest: readonly AdminRoute[] = Object.freeze(
  routeDefinitions.map(route => Object.freeze({
    ...route,
    href: route.path ? `/admin/${route.path}` : '/admin',
    index: route.path === '',
    pro: 'pro' in route && route.pro === true,
    hiddenFromNavigation: 'hiddenFromNavigation' in route && route.hiddenFromNavigation === true,
  })),
)

export interface AdminNavigationGroup {
  id: AdminNavGroup
  titleKey: string
  icon: IconName
  href: string
  routes: readonly AdminRoute[]
}

/** Ordered Operator task domains projected from the canonical route manifest. */
export const adminNavigationGroups: readonly AdminNavigationGroup[] = Object.freeze(
  navGroupDefinitions.map(group => {
    const landingRoute = adminRouteManifest.find(route => route.id === group.landingRouteId)
    if (!landingRoute) throw new Error(`Unknown Admin workspace landing route: ${group.landingRouteId}`)

    return Object.freeze({
      id: group.id,
      titleKey: group.titleKey,
      icon: group.icon,
      href: landingRoute.href,
      routes: Object.freeze(adminRouteManifest.filter(route => (
        route.navGroup === group.id && !route.hiddenFromNavigation
      ))),
    })
  }),
)

/** Resolve a canonical Admin URL without duplicating paths in page components. */
export function getAdminRouteHref(id: AdminRouteId): string {
  const route = adminRouteManifest.find(candidate => candidate.id === id)
  if (!route) throw new Error(`Unknown Admin route: ${id}`)
  return route.href
}

function normalizePathname(pathname: string): string {
  const withoutQuery = pathname.split(/[?#]/, 1)[0] || '/'
  const withoutTrailingSlash = withoutQuery.replace(/\/+$/, '') || '/'
  return withoutTrailingSlash.toLowerCase()
}

/** Resolve only routes currently registered by AdminApp; unknown descendants stay 404s. */
export function resolveAdminRoute(pathname: string): AdminRoute | undefined {
  const normalized = normalizePathname(pathname)
  return adminRouteManifest.find(route => route.href.toLowerCase() === normalized)
}
