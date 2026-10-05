import { lazyRoute, type LazyRouteComponent } from '@/routing/lazyRoute'

import { adminRouteLoaders, createRoutePrefetcher } from './routeLoaders'
import type { AdminRouteId } from './routes'

/**
 * Route elements for the Admin surface. Building them here (instead of inline
 * in AdminApp) lets the workspace navigation warm sibling chunks through the
 * very same component instances the router renders.
 */
export const adminRouteComponents = {
  dashboard: lazyRoute(adminRouteLoaders.dashboard),
  connect: lazyRoute(adminRouteLoaders.connect),
  attention: lazyRoute(adminRouteLoaders.attention),
  cache: lazyRoute(adminRouteLoaders.cache),
  cacheIndexes: lazyRoute(adminRouteLoaders.cacheIndexes),
  compileCache: lazyRoute(adminRouteLoaders.compileCache),
  upstreams: lazyRoute(adminRouteLoaders.upstreams),
  upstreamUpdates: lazyRoute(adminRouteLoaders.upstreamUpdates),
  accessLogs: lazyRoute(adminRouteLoaders.accessLogs),
  auditLogs: lazyRoute(adminRouteLoaders.auditLogs),
  quarantine: lazyRoute(adminRouteLoaders.quarantine),
  rules: lazyRoute(adminRouteLoaders.rules),
  security: lazyRoute(adminRouteLoaders.security),
  projects: lazyRoute(adminRouteLoaders.projects),
  users: lazyRoute(adminRouteLoaders.users),
  license: lazyRoute(adminRouteLoaders.license),
  settings: lazyRoute(adminRouteLoaders.settings),
} satisfies Record<AdminRouteId, LazyRouteComponent<object>>

export const prefetchAdminRoute = createRoutePrefetcher(adminRouteComponents)
