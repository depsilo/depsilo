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
  await expect(page.locator('[data-cascade-upstream="1"] select')).toHaveValue('home')
  await expect(page.locator('[data-cascade-upstream="1"]')).toContainText('home')
  await expect(page.locator('[data-cascade-upstream="3"]')).toContainText('失效')
  await expect(page.getByRole('link', { name: '管理上游源' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '快速连接' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '让客户端连接本机' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '连接上游 Depsilo' })).toBeVisible()
  await expect(page.getByText('npm config set registry http://127.0.0.1:4173/npm').first()).toBeVisible()
  await expect(page.getByText('curl -fsS http://192.168.1.10:23333/health').first()).toBeVisible()

  // The generated commands follow the client-reachable address field.
  await page.getByLabel('本机访问地址').fill('http://10.0.0.9:23333')
  await expect(page.getByText('npm config set registry http://10.0.0.9:23333/npm').first()).toBeVisible()
})

test('cascade page explains how to enable the feature when it is off', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-config-example]')).toContainText('[cascade]')
  await expect(page.getByText('当前实例未启用级联')).toBeVisible()
})

test('cascade page surfaces a read-only config file', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/admin/cascade/config': {
      enabled: true,
      max_hops: 4,
      max_ttl: '168h',
      allow_insecure_http: false,
      token_set: true,
      peers: [{ name: 'home', url: 'https://cache.example', token_set: false, forward_credentials: false }],
      config_writable: false,
      pending_restart: false,
    },
  })
  await page.goto('/admin/cascade')
  await expect(page.getByText('config.toml 当前不可写')).toBeVisible()
  await expect(page.getByLabel('共享密钥')).toBeDisabled()
})

test('cascade inline editor keeps stored secrets masked', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/admin/cascade': {
      enabled: true,
      instance_id: 'node-1',
      relay_path: '/_depsilo/relay/v1',
      max_hops: 4,
      max_ttl_seconds: 604800,
      allow_insecure_http: false,
      peers: [{ name: 'home', url: 'https://cache.example', forward_credentials: false }],
    },
    'GET /api/v1/admin/cascade/config': {
      enabled: true,
      max_hops: 4,
      max_ttl: '168h',
      allow_insecure_http: false,
      token_set: true,
      peers: [{ name: 'home', url: 'https://cache.example', token_set: true, forward_credentials: false }],
      config_writable: true,
      pending_restart: false,
    },
  })
  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-config-form]')).toBeVisible()
  await expect(page.getByLabel('共享密钥')).toHaveValue('')
  await expect(page.getByText('已有密钥，留空保持不变')).toBeVisible()
  await expect(page.locator('[data-cascade-peer-editor]').getByLabel('名称')).toHaveValue('home')
  // Editing reveals the sticky save bar without any dialog layer.
  await page.getByLabel('最大跳数').fill('5')
  await expect(page.locator('[data-cascade-save-bar]')).toContainText('有未保存的修改')
  await page.getByRole('button', { name: '放弃修改' }).click()
  await expect(page.locator('[data-cascade-save-bar]')).toHaveCount(0)
})
