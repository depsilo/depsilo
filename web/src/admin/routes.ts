const navGroupDefinitions = [
  { id: 'operations', titleKey: 'nav.groups.operations' },
  { id: 'cacheUpstreams', titleKey: 'nav.groups.cacheUpstreams' },
  { id: 'supplyChain', titleKey: 'nav.groups.supplyChain' },
  { id: 'system', titleKey: 'nav.groups.system' },
] as const

export type AdminNavGroup = (typeof navGroupDefinitions)[number]['id']

interface AdminRouteDefinition {
  id: string
  path: string
  titleKey: string
  icon: string
  navGroup: AdminNavGroup
  pro?: true
}

const routeDefinitions = [
  { id: 'dashboard', path: '', titleKey: 'nav.dashboard', icon: 'dashboard', navGroup: 'operations' },
  { id: 'bandwidth', path: 'bandwidth', titleKey: 'bandwidth.title', icon: 'bar_chart', navGroup: 'operations' },
  { id: 'accessLogs', path: 'logs', titleKey: 'nav.accessLogs', icon: 'receipt_long', navGroup: 'operations' },
  { id: 'auditLogs', path: 'audit', titleKey: 'nav.auditLogs', icon: 'policy', navGroup: 'operations' },
  { id: 'cache', path: 'cache', titleKey: 'nav.cacheManage', icon: 'storage', navGroup: 'cacheUpstreams' },
  { id: 'cacheIndexes', path: 'indexes', titleKey: 'nav.cacheIndexes', icon: 'inventory_2', navGroup: 'cacheUpstreams' },
  { id: 'compileCache', path: 'compile-cache', titleKey: 'nav.compileCache', icon: 'memory', navGroup: 'cacheUpstreams' },
  { id: 'upstreams', path: 'upstreams', titleKey: 'nav.upstreams', icon: 'cloud_sync', navGroup: 'cacheUpstreams' },
  { id: 'upstreamUpdates', path: 'upstream-updates', titleKey: 'nav.upstreamUpdates', icon: 'update', navGroup: 'cacheUpstreams' },
  { id: 'quarantine', path: 'quarantine', titleKey: 'nav.quarantine', icon: 'shield_lock', navGroup: 'supplyChain' },
  { id: 'rules', path: 'rules', titleKey: 'nav.rules', icon: 'shield', navGroup: 'supplyChain' },
  { id: 'security', path: 'security', titleKey: 'nav.security', icon: 'security', navGroup: 'supplyChain' },
  { id: 'projects', path: 'projects', titleKey: 'nav.projects', icon: 'folder_managed', navGroup: 'supplyChain', pro: true },
  { id: 'users', path: 'users', titleKey: 'nav.userManage', icon: 'group', navGroup: 'system' },
  { id: 'license', path: 'license', titleKey: 'license.title', icon: 'key', navGroup: 'system' },
  { id: 'settings', path: 'settings', titleKey: 'nav.settings', icon: 'settings', navGroup: 'system' },
] as const satisfies readonly AdminRouteDefinition[]

export type AdminRouteId = (typeof routeDefinitions)[number]['id']

export interface AdminRoute {
  id: AdminRouteId
  /** Path relative to the /admin route used by React Router. */
  path: string
  /** Canonical absolute URL used by navigation and exact title matching. */
  href: string
  titleKey: string
  icon: string
  navGroup: AdminNavGroup
  index: boolean
  pro: boolean
}

export const adminRouteManifest: readonly AdminRoute[] = Object.freeze(
  routeDefinitions.map(route => Object.freeze({
    ...route,
    href: route.path ? `/admin/${route.path}` : '/admin',
    index: route.path === '',
    pro: 'pro' in route && route.pro === true,
  })),
)

export interface AdminNavigationGroup {
  id: AdminNavGroup
  titleKey: string
  routes: readonly AdminRoute[]
}

/** Ordered Operator task domains projected from the canonical route manifest. */
export const adminNavigationGroups: readonly AdminNavigationGroup[] = Object.freeze(
  navGroupDefinitions.map(group => Object.freeze({
    ...group,
    routes: Object.freeze(adminRouteManifest.filter(route => route.navGroup === group.id)),
  })),
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
