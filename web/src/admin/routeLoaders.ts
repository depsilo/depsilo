import type { ComponentType } from 'react'

import type { AdminRouteId } from './routes'

/** Every Admin route resolves through one registry so navigation and prefetch
 *  share the same dynamic import and a warm chunk is never downloaded twice. */
export const adminRouteLoaders: Record<AdminRouteId, () => Promise<{ default: ComponentType<object> }>> = {
  dashboard: () => import('./pages/Dashboard'),
  connect: () => import('./pages/ConnectProject'),
  attention: () => import('./pages/Attention'),
  cache: () => import('./pages/CacheManage'),
  cacheIndexes: () => import('./pages/CacheIndexes'),
  compileCache: () => import('./pages/CompileCache'),
  upstreams: () => import('./pages/Upstreams'),
  upstreamUpdates: () => import('./pages/UpstreamUpdates'),
  accessLogs: () => import('./pages/AccessLogs'),
  auditLogs: () => import('./pages/AuditLogs'),
  quarantine: () => import('./pages/Quarantine'),
  rules: () => import('./pages/Rules'),
  security: () => import('./pages/Security'),
  projects: () => import('./pages/Projects'),
  users: () => import('./pages/Users'),
  license: () => import('./pages/License'),
  settings: () => import('./pages/Settings'),
}

/**
 * Warm route chunks once each. The lazy components behind `routes` already
 * ignore repeat warm-ups, so this only keeps hover traffic from re-entering
 * the loader on every pointer move.
 */
export function createRoutePrefetcher<Id extends string>(routes: Record<Id, { preload: () => void }>) {
  const warmed = new Set<Id>()
  return (id: Id) => {
    if (warmed.has(id)) return
    warmed.add(id)
    routes[id].preload()
  }
}
