import { describe, expect, it } from 'vitest'

import {
  adminNavigationGroups,
  adminRouteManifest,
  getAdminRouteHref,
  resolveAdminRoute,
} from '../../src/admin/routes'

const expectedRoutes = {
  dashboard: '/admin',
  attention: '/admin/attention',
  bandwidth: '/admin/bandwidth',
  accessLogs: '/admin/logs',
  auditLogs: '/admin/audit',
  quarantine: '/admin/quarantine',
  cache: '/admin/cache',
  cacheIndexes: '/admin/indexes',
  compileCache: '/admin/compile-cache',
  upstreams: '/admin/upstreams',
  upstreamUpdates: '/admin/upstream-updates',
  users: '/admin/users',
  license: '/admin/license',
  rules: '/admin/rules',
  security: '/admin/security',
  projects: '/admin/projects',
  settings: '/admin/settings',
} as const

const expectedGroups = [
  { id: 'overview', routes: ['dashboard'] },
  { id: 'history', routes: ['accessLogs', 'upstreamUpdates', 'auditLogs', 'bandwidth'] },
  { id: 'sourcesCache', routes: ['upstreams', 'cache', 'cacheIndexes', 'compileCache'] },
  { id: 'governance', routes: ['security', 'quarantine', 'rules', 'projects'] },
  { id: 'system', routes: ['users', 'settings', 'license'] },
] as const

describe('Admin route manifest', () => {
  it('has one unique entry for every route id and path', () => {
    expect(new Set(adminRouteManifest.map(route => route.id)).size).toBe(adminRouteManifest.length)
    expect(new Set(adminRouteManifest.map(route => route.path)).size).toBe(adminRouteManifest.length)
    expect(Object.fromEntries(adminRouteManifest.map(route => [route.id, route.href]))).toEqual(expectedRoutes)
    expect(adminRouteManifest.find(route => route.id === 'projects')?.pro).toBe(true)
  })

  it('covers every visible route once in stable Operator-task order', () => {
    expect(adminNavigationGroups.every(group => group.routes.length > 0)).toBe(true)
    expect(adminNavigationGroups.map(group => ({
      id: group.id,
      routes: group.routes.map(route => route.id),
    }))).toEqual(expectedGroups)

    const groupedRouteIds = adminNavigationGroups.flatMap(group => group.routes.map(route => route.id))
    const visibleRouteIds = adminRouteManifest
      .filter(route => !route.hiddenFromNavigation)
      .map(route => route.id)
    expect(groupedRouteIds).toEqual(visibleRouteIds)
    expect(new Set(groupedRouteIds).size).toBe(visibleRouteIds.length)
  })

  it('keeps attention directly reachable without exposing it in primary navigation', () => {
    expect(resolveAdminRoute('/admin/attention')).toMatchObject({
      id: 'attention',
      hiddenFromNavigation: true,
      navGroup: 'overview',
    })
    expect(adminNavigationGroups.flatMap(group => group.routes).some(route => route.id === 'attention')).toBe(false)
  })

  it('normalizes case and trailing slashes without masking unknown paths', () => {
    expect(getAdminRouteHref('dashboard')).toBe('/admin')
    expect(getAdminRouteHref('bandwidth')).toBe('/admin/bandwidth')
    expect(resolveAdminRoute('/ADMIN/SECURITY/')?.id).toBe('security')
    expect(resolveAdminRoute('/admin')?.id).toBe('dashboard')
    expect(resolveAdminRoute('/admin/projects/42')).toBeUndefined()
    expect(resolveAdminRoute('/admin/does-not-exist')).toBeUndefined()
  })
})
