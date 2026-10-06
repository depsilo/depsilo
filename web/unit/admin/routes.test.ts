import { describe, expect, it } from 'vitest'

import {
  adminNavigationGroups,
  adminRouteManifest,
  getAdminRouteHref,
  resolveAdminRoute,
  resolveWorkspaceNavigation,
} from '../../src/admin/routes'

const expectedRoutes = {
  dashboard: '/admin',
  connect: '/admin/connect',
  attention: '/admin/attention',
  accessLogs: '/admin/logs',
  auditLogs: '/admin/audit',
  quarantine: '/admin/quarantine',
  cache: '/admin/cache',
  cacheIndexes: '/admin/indexes',
  compileCache: '/admin/compile-cache',
  upstreams: '/admin/upstreams',
  cascade: '/admin/cascade',
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
  { id: 'upstreams', routes: ['upstreams', 'cascade'] },
  { id: 'cache', routes: ['cache', 'cacheIndexes', 'compileCache'] },
  { id: 'logs', routes: ['accessLogs', 'upstreamUpdates', 'auditLogs'] },
  { id: 'security', routes: ['security', 'quarantine', 'rules'] },
  { id: 'projects', routes: ['projects'] },
  { id: 'instance', routes: ['users', 'settings', 'license'] },
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
    expect(resolveAdminRoute('/admin/connect')).toMatchObject({ id: 'connect', hiddenFromNavigation: true })
    expect(adminNavigationGroups.flatMap(group => group.routes).some(route => route.id === 'connect')).toBe(false)
  })

  it('normalizes case and trailing slashes without masking unknown paths', () => {
    expect(getAdminRouteHref('dashboard')).toBe('/admin')
    expect(resolveAdminRoute('/ADMIN/SECURITY/')?.id).toBe('security')
    expect(resolveAdminRoute('/admin')?.id).toBe('dashboard')
    expect(resolveAdminRoute('/admin/projects/42')).toBeUndefined()
    expect(resolveAdminRoute('/admin/does-not-exist')).toBeUndefined()
  })

  it('promotes only workspaces with sibling destinations into the page header tabs', () => {
    expect(resolveWorkspaceNavigation('/admin/cache')?.id).toBe('cache')
    expect(resolveWorkspaceNavigation('/ADMIN/COMPILE-CACHE/')?.id).toBe('cache')
    expect(resolveWorkspaceNavigation('/admin/audit')?.id).toBe('logs')
    expect(resolveWorkspaceNavigation('/admin/users')?.id).toBe('instance')
    expect(resolveWorkspaceNavigation('/admin/cascade')?.id).toBe('upstreams')
    expect(resolveWorkspaceNavigation('/admin/upstreams')?.id).toBe('upstreams')

    // Single-destination workspaces keep the standalone page title.
    expect(resolveWorkspaceNavigation('/admin')).toBeUndefined()
    expect(resolveWorkspaceNavigation('/admin/projects')).toBeUndefined()

    // Hidden onboarding and attention routes are not sibling destinations.
    expect(resolveWorkspaceNavigation('/admin/connect')).toBeUndefined()
    expect(resolveWorkspaceNavigation('/admin/attention')).toBeUndefined()
    expect(resolveWorkspaceNavigation('/admin/does-not-exist')).toBeUndefined()
  })
})
