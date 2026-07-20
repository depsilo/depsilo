import { expect, test } from '@playwright/test'

import {
  adminNavigationGroups,
  adminRouteManifest,
  getAdminRouteHref,
  resolveAdminRoute,
} from '../src/admin/routes'

const expectedRoutes = {
  dashboard: '/admin',
  bandwidth: '/admin/bandwidth',
  accessLogs: '/admin/logs',
  auditLogs: '/admin/audit',
  quarantine: '/admin/quarantine',
  cache: '/admin/cache',
  cacheIndexes: '/admin/indexes',
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
  { id: 'operations', routes: ['dashboard', 'bandwidth', 'accessLogs', 'auditLogs'] },
  { id: 'cacheUpstreams', routes: ['cache', 'cacheIndexes', 'upstreams', 'upstreamUpdates'] },
  { id: 'supplyChain', routes: ['quarantine', 'rules', 'security', 'projects'] },
  { id: 'system', routes: ['users', 'license', 'settings'] },
] as const

test('admin route ids and paths have one unique manifest entry', () => {
  expect(new Set(adminRouteManifest.map(route => route.id)).size).toBe(adminRouteManifest.length)
  expect(new Set(adminRouteManifest.map(route => route.path)).size).toBe(adminRouteManifest.length)
  expect(Object.fromEntries(adminRouteManifest.map(route => [route.id, route.href]))).toEqual(expectedRoutes)
  expect(adminRouteManifest.find(route => route.id === 'projects')?.pro).toBe(true)
})

test('navigation groups cover every route once in stable Operator-task order', () => {
  expect(adminNavigationGroups.every(group => group.routes.length > 0)).toBe(true)
  expect(adminNavigationGroups.map(group => ({
    id: group.id,
    routes: group.routes.map(route => route.id),
  }))).toEqual(expectedGroups)

  const groupedRouteIds = adminNavigationGroups.flatMap(group => group.routes.map(route => route.id))
  expect(groupedRouteIds).toEqual(adminRouteManifest.map(route => route.id))
  expect(new Set(groupedRouteIds).size).toBe(adminRouteManifest.length)
})

test('route matching normalizes case and trailing slashes without masking unknown paths', () => {
  expect(getAdminRouteHref('dashboard')).toBe('/admin')
  expect(getAdminRouteHref('bandwidth')).toBe('/admin/bandwidth')
  expect(resolveAdminRoute('/ADMIN/SECURITY/')?.id).toBe('security')
  expect(resolveAdminRoute('/admin')?.id).toBe('dashboard')
  expect(resolveAdminRoute('/admin/projects/42')).toBeUndefined()
  expect(resolveAdminRoute('/admin/does-not-exist')).toBeUndefined()
})
