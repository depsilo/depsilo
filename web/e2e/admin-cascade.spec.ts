import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const upstream = (overrides: Record<string, unknown>) => ({
  id: 1,
  adapter_type: 'npm',
  name: 'npmjs',
  url: 'https://registry.npmjs.org',
  proxy: '',
  priority: 1,
  probe_mode: 'active',
  probe_interval: '30m',
  healthy: true,
  avg_latency_ms: 42,
  success_rate: 1,
  last_checked_at: '2026-10-07T00:00:00Z',
  worker_running: true,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  via: '',
  ...overrides,
})

test('cascade page shows peers, bindings, and dangling references', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/admin/cascade': {
      enabled: true,
      instance_id: 'a82f263278943a612967728374c76e45',
      relay_path: '/_depsilo/relay/v1',
      max_hops: 4,
      max_ttl_seconds: 604800,
      allow_insecure_http: false,
      peers: [
        { name: 'home', url: 'http://192.168.1.10:23333', forward_credentials: false },
        { name: 'lab', url: 'https://cache.lab.example/depsilo', forward_credentials: true },
      ],
    },
    'GET /api/v1/admin/upstreams': {
      items: [
        upstream({ id: 1, via: 'home' }),
        upstream({ id: 3, adapter_type: 'go', name: 'goproxy', url: 'https://goproxy.cn', priority: 3, via: 'removed-peer' }),
      ],
      total: 2,
    },
  })

  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-peer="home"]')).toContainText('192.168.1.10')
  await expect(page.locator('[data-cascade-upstream="1"]')).toContainText('home')
  await expect(page.locator('[data-cascade-upstream="3"]')).toContainText('失效')
  await expect(page.getByRole('link', { name: '管理上游源' })).toBeVisible()
})

test('cascade page explains how to enable the feature when it is off', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-config-example]')).toContainText('[cascade]')
  await expect(page.getByText('当前实例未启用级联')).toBeVisible()
})
