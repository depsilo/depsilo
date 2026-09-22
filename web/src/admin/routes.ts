import { BarChart3, CloudSync, Cpu, FolderCog, HardDrive, History, Inbox, KeyRound, LayoutDashboard, Link2, Package, ReceiptText, Settings, Shield, ShieldAlert, ShieldCheck, Users, type LucideIcon } from 'lucide-react'

const navGroupDefinitions = [
  { id: 'overview', titleKey: 'nav.workspaces.overview', icon: LayoutDashboard, landingRouteId: 'dashboard' },
  { id: 'upstreams', titleKey: 'nav.workspaces.upstreams', icon: CloudSync, landingRouteId: 'upstreams' },
  { id: 'cache', titleKey: 'nav.workspaces.cache', icon: HardDrive, landingRouteId: 'cache' },
  { id: 'logs', titleKey: 'nav.workspaces.logs', icon: ReceiptText, landingRouteId: 'accessLogs' },
  { id: 'security', titleKey: 'nav.workspaces.security', icon: ShieldCheck, landingRouteId: 'security' },
  { id: 'projects', titleKey: 'nav.workspaces.projects', icon: FolderCog, landingRouteId: 'projects' },
  { id: 'instance', titleKey: 'nav.instanceManagement', icon: Settings, landingRouteId: 'users', hiddenFromSidebar: true },
] as const

export type AdminNavGroup = (typeof navGroupDefinitions)[number]['id']

interface AdminRouteDefinition {
  id: string
  path: string
  titleKey: string
  icon: LucideIcon
  navGroup: AdminNavGroup
  pro?: true
  hiddenFromNavigation?: true
}

const routeDefinitions = [
  { id: 'dashboard', path: '', titleKey: 'nav.workspaces.overview', icon: LayoutDashboard, navGroup: 'overview' },
  { id: 'bandwidth', path: 'bandwidth', titleKey: 'bandwidth.title', icon: BarChart3, navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'connect', path: 'connect', titleKey: 'onboarding.title', icon: Link2, navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'attention', path: 'attention', titleKey: 'nav.attention', icon: Inbox, navGroup: 'overview', hiddenFromNavigation: true },
  { id: 'upstreams', path: 'upstreams', titleKey: 'nav.upstreams', icon: CloudSync, navGroup: 'upstreams' },
  { id: 'cache', path: 'cache', titleKey: 'nav.cacheManage', icon: HardDrive, navGroup: 'cache' },
  { id: 'cacheIndexes', path: 'indexes', titleKey: 'nav.cacheIndexes', icon: Package, navGroup: 'cache' },
  { id: 'compileCache', path: 'compile-cache', titleKey: 'nav.compileCache', icon: Cpu, navGroup: 'cache' },
  { id: 'accessLogs', path: 'logs', titleKey: 'nav.accessLogs', icon: ReceiptText, navGroup: 'logs' },
  { id: 'upstreamUpdates', path: 'upstream-updates', titleKey: 'nav.upstreamUpdates', icon: History, navGroup: 'logs' },
  { id: 'auditLogs', path: 'audit', titleKey: 'nav.auditLogs', icon: ShieldCheck, navGroup: 'logs' },
  { id: 'security', path: 'security', titleKey: 'nav.security', icon: ShieldCheck, navGroup: 'security' },
  { id: 'quarantine', path: 'quarantine', titleKey: 'nav.quarantine', icon: ShieldAlert, navGroup: 'security' },
  { id: 'rules', path: 'rules', titleKey: 'nav.rules', icon: Shield, navGroup: 'security' },
  { id: 'projects', path: 'projects', titleKey: 'nav.projects', icon: FolderCog, navGroup: 'projects', pro: true },
  { id: 'users', path: 'users', titleKey: 'nav.userManage', icon: Users, navGroup: 'instance' },
  { id: 'settings', path: 'settings', titleKey: 'nav.settings', icon: Settings, navGroup: 'instance' },
  { id: 'license', path: 'license', titleKey: 'license.title', icon: KeyRound, navGroup: 'instance' },
] as const satisfies readonly AdminRouteDefinition[]

export type AdminRouteId = (typeof routeDefinitions)[number]['id']

export interface AdminRoute {
  id: AdminRouteId
  /** Path relative to the /admin route used by React Router. */
  path: string
  /** Canonical absolute URL used by navigation and exact title matching. */
  href: string
  titleKey: string
  icon: LucideIcon
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
  icon: LucideIcon
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
