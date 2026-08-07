import type { Page } from '@playwright/test'
import type { AdminSettingsResponse, UpdateAdminSettingsResponse } from '../src/lib/adminApi.types'
import {
  adminApiDefaults,
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

const defaultSettings = adminApiDefaults['GET /api/v1/admin/settings'] as AdminSettingsResponse

const settingsResponse = (
  overrides: Partial<AdminSettingsResponse> = {},
): AdminSettingsResponse => ({
  ...defaultSettings,
  ...overrides,
})

async function expectSettingsPageChrome(page: Page) {
  const adminPage = page.locator('[data-admin-page]')
  await expect(adminPage).toHaveCount(1)
  await expect(adminPage).toHaveAttribute('data-admin-page-width', 'fluid')
  await expect(adminPage.locator('[data-admin-page-description]')).toContainText(
    '查看配置来源与实际生效值，修改可写的运行设置并管理 Webhook 通知。',
  )
  await expect(page.locator('h1:visible')).toHaveCount(1)
  await expect(adminPage.locator('[data-admin-page-title]')).toHaveText('系统设置')
  await expect(page.locator('[data-admin-topbar]').getByRole('heading')).toHaveCount(0)
}

test('keeps stable Admin page chrome while settings load and after success', async ({ page }) => {
  let release!: (response: AdminSettingsResponse) => void
  const response = new Promise<AdminSettingsResponse>(resolve => { release = resolve })
  await mockAdminApi(page, { 'GET /api/v1/admin/settings': async () => response })

  await page.goto('/admin/settings')

  await expect(page.locator('[data-admin-page-content] [aria-busy="true"]')).toBeVisible()
  await expectSettingsPageChrome(page)

  release(settingsResponse({ pending_restart: [] }))
  await expect(page.getByRole('tab', { name: /基础配置/ })).toBeVisible()
  await expectSettingsPageChrome(page)
})

test('keeps stable Admin page chrome when the initial settings request fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': {
      status: 500,
      body: { code: 'SETTINGS_UNAVAILABLE', message: 'offline' },
    },
  })

  await page.goto('/admin/settings')

  await expect(page.getByText(/无法加载设置/)).toBeVisible()
  await expectSettingsPageChrome(page)
})

test('owns Save at page level and keeps Add Webhook inside the Webhook tab', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': settingsResponse({ pending_restart: [] }),
  })
  await page.goto('/admin/settings')

  const pageActions = page.locator('[data-admin-page-actions]')
  await expect(pageActions.getByRole('button', { name: /^保存$/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /^保存$/ })).toHaveCount(1)

  await page.getByRole('tab', { name: /Webhook/ }).click()

  await expect(page.locator('[data-admin-page-actions]')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^保存$/ })).toHaveCount(0)
  const addWebhook = page.getByRole('tabpanel').getByRole('button', { name: /添加 Webhook/ })
  await expect(addWebhook).toBeVisible()
  await expect(addWebhook).toBeEnabled()
})

test('scopes config-file read-only feedback to configuration tabs', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': settingsResponse({
      config_writable: false,
      pending_restart: [],
    }),
  })
  await page.goto('/admin/settings')

  const configReadOnly = page.getByText(/配置文件只读/)
  await expect(configReadOnly).toBeVisible()
  await expect(page.locator('[data-admin-page-actions]').getByRole('button', { name: /^保存$/ })).toBeDisabled()

  await page.getByRole('tab', { name: /Webhook/ }).click()

  await expect(configReadOnly).not.toBeVisible()
  const addWebhook = page.getByRole('tabpanel').getByRole('button', { name: /添加 Webhook/ })
  await expect(addWebhook).toBeEnabled()
  await addWebhook.click()
  await expect(page.getByRole('dialog', { name: /添加 Webhook/ })).toBeVisible()
})

test('hides Webhook mutation controls for a read-only principal', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': {
      id: 2,
      username: 'viewer',
      role: 'viewer',
      enabled: true,
      auth_method: 'jwt',
      token_permissions: null,
      can_write: false,
    },
    'GET /api/v1/admin/settings': settingsResponse({ pending_restart: [] }),
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()

  await expect(page.getByText(/当前账号为只读/)).toBeVisible()
  await expect(page.getByRole('button', { name: /添加 Webhook/ })).toHaveCount(0)
})

test('renders one restart notice after a save that requires restart', async ({ page }) => {
  const initial = settingsResponse({ pending_restart: [] })
  const updated: UpdateAdminSettingsResponse = {
    ...initial,
    configured: {
      ...initial.configured,
      cache: { ...initial.configured.cache, ttl_index: '10m' },
    },
    pending_restart: ['cache.ttl_index'],
    changed: ['cache.ttl_index'],
    applied_now: [],
    restart_required: ['cache.ttl_index'],
    blocked_by_override: [],
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': initial,
    'PUT /api/v1/admin/settings': updated,
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /缓存策略/ }).click()
  await page.getByLabel(/索引 TTL/).fill('10m')
  await page.locator('[data-admin-page-actions]').getByRole('button', { name: /^保存$/ }).click()

  const pageContent = page.locator('[data-admin-page-content]')
  await expect(pageContent.getByText(/^重启后生效$/)).toHaveCount(1)
  await expect(pageContent.getByText(/^等待重启$/)).toHaveCount(0)
})

test('formats English setting field lists without Chinese punctuation', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': settingsResponse({
      pending_restart: ['cache.ttl_index', 'cache.ttl_blob'],
    }),
  })
  await page.goto('/admin/settings')

  const restartCopy = page.getByText(
    /^Configured values waiting for a restart: Index TTL and File TTL$/,
  )
  await expect(restartCopy).toBeVisible()
  expect(await restartCopy.textContent()).not.toContain('、')
})

test('has no root overflow across Settings and Webhooks at 320px in English', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/settings': settingsResponse({ pending_restart: [] }),
  })
  await page.goto('/admin/settings')

  await expect(page.getByRole('button', { name: /^Save$/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.getByRole('tab', { name: /Webhooks/ }).click()
  await expect(page.getByRole('button', { name: /Add Webhook/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})
