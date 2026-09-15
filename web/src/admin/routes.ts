import type { IconName } from '@/components/Icon'

const navGroupDefinitions = [
  { id: 'overview', titleKey: 'nav.workspaces.overview', icon: 'dashboard', landingRouteId: 'dashboard' },
  { id: 'upstreams', titleKey: 'nav.workspaces.upstreams', icon: 'cloud_sync', landingRouteId: 'upstreams' },
  { id: 'cache', titleKey: 'nav.workspaces.cache', icon: 'storage', landingRouteId: 'cache' },
  { id: 'logs', titleKey: 'nav.workspaces.logs', icon: 'receipt_long', landingRouteId: 'accessLogs' },
  { id: 'security', titleKey: 'nav.workspaces.security', icon: 'security', landingRouteId: 'security' },
  { id: 'projects', titleKey: 'nav.workspaces.projects', icon: 'folder_managed', landingRouteId: 'projects' },
  { id: 'instance', titleKey: 'nav.instanceManagement', icon: 'settings', landingRouteId: 'users', hiddenFromSidebar: true },
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
  { id: 'bandwidth', path: 'bandwidth', titleKey: 'bandwidth.title', icon: 'bar_chart', navGroup: 'overview' },
  { id: 'connect', path: 'connect', titleKey: 'onboarding.title', icon: 'link', navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'attention', path: 'attention', titleKey: 'nav.attention', icon: 'inbox', navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'upstreams', path: 'upstreams', titleKey: 'nav.upstreams', icon: 'cloud_sync', navGroup: 'upstreams' },
  { id: 'cache', path: 'cache', titleKey: 'nav.cacheManage', icon: 'storage', navGroup: 'cache' },
  { id: 'cacheIndexes', path: 'indexes', titleKey: 'nav.cacheIndexes', icon: 'inventory_2', navGroup: 'cache' },
  { id: 'compileCache', path: 'compile-cache', titleKey: 'nav.compileCache', icon: 'memory', navGroup: 'cache' },
  { id: 'accessLogs', path: 'logs', titleKey: 'nav.accessLogs', icon: 'receipt_long', navGroup: 'logs' },
  { id: 'upstreamUpdates', path: 'upstream-updates', titleKey: 'nav.upstreamUpdates', icon: 'update', navGroup: 'logs' },
  { id: 'auditLogs', path: 'audit', titleKey: 'nav.auditLogs', icon: 'policy', navGroup: 'logs' },
  { id: 'security', path: 'security', titleKey: 'nav.security', icon: 'security', navGroup: 'security' },
  { id: 'quarantine', path: 'quarantine', titleKey: 'nav.quarantine', icon: 'shield_lock', navGroup: 'security' },
  { id: 'rules', path: 'rules', titleKey: 'nav.rules', icon: 'shield', navGroup: 'security' },
  { id: 'projects', path: 'projects', titleKey: 'nav.projects', icon: 'folder_managed', navGroup: 'projects', pro: true },
  { id: 'users', path: 'users', titleKey: 'nav.userManage', icon: 'group', navGroup: 'instance' },
  { id: 'settings', path: 'settings', titleKey: 'nav.settings', icon: 'settings', navGroup: 'instance' },
  { id: 'license', path: 'license', titleKey: 'license.title', icon: 'key', navGroup: 'instance' },
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
  hiddenFromSidebar: boolean
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
      hiddenFromSidebar: 'hiddenFromSidebar' in group && group.hiddenFromSidebar === true,
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
