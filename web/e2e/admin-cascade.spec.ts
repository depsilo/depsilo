import type { Request } from '@playwright/test'
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
        { name: 'home', url: 'http://192.168.1.10:23333', forward_credentials: false, last_success_at: '2026-10-07T00:00:00Z', last_fallback_at: null, last_failure: '', fallback_count: 0 },
        { name: 'lab', url: 'https://cache.lab.example/depsilo', forward_credentials: true, last_success_at: null, last_fallback_at: '2026-10-07T00:00:00Z', last_failure: 'dial tcp: connection refused', fallback_count: 3 },
      ],
      direct_fallback: true,
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
  await expect(page.getByRole('tab', { name: '客户端接入' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '加入上级' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '作为上级' })).toBeVisible()
  await page.getByRole('tab', { name: '客户端接入' }).click()
  await expect(page.getByRole('link', { name: '查看全部客户端接入方式' })).toHaveAttribute('href', '/admin/connect')
  // A parent that failed shows why its upstreams fetch directly.
  await expect(page.locator('[data-cascade-peer-fallback="lab"]')).toContainText('connection refused')
  await expect(page.locator('[data-cascade-peer-ok="home"]')).toBeVisible()

})

test('cascade page explains how to enable the feature when it is off', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-topology]')).toContainText('未参与级联')
  await expect(page.locator('[data-cascade-config-form]')).toBeVisible()
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
  await expect(page.getByLabel('共享密钥')).toHaveAttribute('placeholder', '已设置 · 留空保持')
  await expect(page.locator('[data-cascade-peer-editor]').getByLabel('名称')).toHaveValue('home')
  // Editing reveals the sticky save bar without any dialog layer.
  await page.getByLabel('最大跳数').fill('5')
  await expect(page.locator('[data-cascade-save-bar]')).toContainText('有未保存的修改')
  await page.getByRole('button', { name: '放弃修改' }).click()
  await expect(page.locator('[data-cascade-save-bar]')).toHaveCount(0)
})

test('pairing: a parent generates a code and a child joins with it', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  let pairingURL = ''
  let joinBody: Record<string, unknown> = {}
  await mockAdminApi(page, {
    'GET /api/v1/admin/cascade': { enabled: false, instance_id: 'node-1', relay_path: '/_depsilo/relay/v1', max_hops: 4, max_ttl_seconds: 604800, allow_insecure_http: false, direct_fallback: true, peers: [] },
    'GET /api/v1/admin/upstreams': { items: [upstream({ id: 1 })], total: 1 },
    'POST /api/v1/admin/cascade/pairing': (request: Request) => {
      pairingURL = (request.postDataJSON() as { url: string }).url
      return { code: 'depsilo-pair-1.eyJ1IjoiaHR0cDovLzE5Mi4xNjguMS4xMDoyMzMzMyJ9', url: pairingURL, enabled_now: true, loopback: false, restart_required: false }
    },
    'POST /api/v1/admin/cascade/join': (request: Request) => {
      joinBody = request.postDataJSON() as Record<string, unknown>
      return { peer: 'parent', url: 'http://192.168.1.10:23333', parent_instance_id: 'parent-node', bound: 12, skipped: ['pypi/intranet'], insecure_http_allowed: true, restart_required: false, state: { enabled: true, max_hops: 4, max_ttl: '168h', allow_insecure_http: true, direct_fallback: true, token_set: true, peers: [], config_writable: true, pending_restart: false } }
    },
  })
  await page.goto('/admin/cascade')

  await page.getByRole('tab', { name: '作为上级' }).click()
  const address = page.getByLabel('子节点访问本机的地址')
  await expect(address).toHaveValue(new URL(page.url()).origin)
  // The test server is on loopback, which other machines cannot reach.
  await expect(page.getByText('这是本机回环地址')).toBeVisible()
  await address.fill('http://192.168.1.10:23333')
  await expect(page.getByText('这是本机回环地址')).toHaveCount(0)
  await page.getByRole('button', { name: '生成配对码' }).click()
  await expect(page.locator('[data-cascade-pairing-code]')).toContainText('depsilo-pair-1.')
  await expect(page.getByText('已启用级联，本机现在可以作为上级。')).toBeVisible()
  expect(pairingURL).toBe('http://192.168.1.10:23333')

  await page.getByRole('tab', { name: '加入上级' }).click()
  const join = page.getByRole('button', { name: '加入', exact: true })
  await expect(join).toBeDisabled()
  await page.getByLabel('配对码').fill('  depsilo-pair-1.abc  ')
  await page.getByRole('checkbox', { name: '所有上游都经由上级获取' }).isChecked()
  await join.click()
  await expect(page.locator('[data-cascade-joined]')).toContainText('12 个上游改为经由上级获取')
  await expect(page.locator('[data-cascade-joined]')).toContainText('pypi/intranet')
  await expect(page.getByText(/已自动开启“允许明文 HTTP peer”/)).toBeVisible()
  expect(joinBody).toEqual({ code: 'depsilo-pair-1.abc', route_all: true })
})
