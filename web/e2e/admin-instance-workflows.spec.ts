import { test, expect, mockAdminApi, adminApiDefaults } from './fixtures/admin-api'
import type { Request } from '@playwright/test'
import type { AdminSettingsResponse, UpdateAdminSettingsRequest } from '../src/lib/adminApi.types'
import type { EntitlementStatus, WebhookConfig } from '../src/lib/api'

test('webhooks remain accessible when configuration cannot be read', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/settings': { status: 500, body: { code: 'CONFIG_READ_FAILED', message: 'config unavailable' } } })
  await page.goto('/admin/settings')
  await expect(page.getByRole('alert')).toContainText('无法加载设置')
  await page.getByRole('tab', { name: 'Webhook 通知' }).click()
  await expect(page.getByRole('button', { name: '添加 Webhook' })).toBeVisible()
  await expect(page.getByText('无法加载设置')).toHaveCount(0)
})

test('settings review covers all sections and restores only this account’s draft across navigation', async ({ page }) => {
  const base = adminApiDefaults['GET /api/v1/admin/settings'] as AdminSettingsResponse
  let saved: UpdateAdminSettingsRequest | undefined
  await mockAdminApi(page, { 'PUT /api/v1/admin/settings': (request: Request) => {
    saved = request.postDataJSON()
    return { ...base, configured: { ...base.configured, cache: { ...base.configured.cache, max_size_gb: 50 }, auth: { token_ttl: '24h' } }, changed: ['cache.max_size_gb', 'auth.token_ttl'], applied_now: [], restart_required: ['cache.max_size_gb', 'auth.token_ttl'], blocked_by_override: [] }
  } })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: '缓存策略' }).click()
  await page.getByLabel('最大缓存容量 (GB)', { exact: true }).fill('50')
  await page.getByRole('tab', { name: '登录会话' }).click()
  await page.getByLabel('登录会话有效期', { exact: true }).fill('24h')
  await expect(page.getByText('2 项待保存变更')).toBeVisible()
  await page.getByRole('link', { name: '用户管理', exact: true }).click()
  await page.getByRole('link', { name: '系统设置', exact: true }).click()
  await expect(page.getByText(/已恢复当前账号/)).toBeVisible()
  await page.getByRole('button', { name: '查看变更' }).click()
  const review = page.getByRole('dialog')
  await expect(review).toContainText('最大缓存容量')
  await expect(review).toContainText('登录会话有效期')
  await review.getByRole('button', { name: '保存全部变更' }).click()
  await expect.poll(() => saved).toEqual({ cache: { max_size_gb: 50 }, auth: { token_ttl: '24h' } })
  await expect(review).toHaveCount(0)
  await expect(page.getByText('2 项待保存变更')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => Object.keys(sessionStorage).filter(key => key.startsWith('depsilo:settings-draft:')))).toEqual([])
})

test('webhook operations retain failure details, mask list addresses, and preserve queued semantics', async ({ page }) => {
  let config: WebhookConfig = { id: 1, name: 'ops', platform: 'slack', url: 'https://hooks.example.test/credential-secret', enabled: false, events: 'upstream_down', cooldown_minutes: 30, last_sent_at: null, created_at: '', updated_at: '' }
  const writes: unknown[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': () => [config],
    'PUT /api/v1/admin/webhooks/1': (request: Request) => {
      const body = request.postDataJSON()
      writes.push(body)
      if (Object.keys(body).length === 1) { config = { ...config, ...body }; return { status: 'ok' } }
      return { status: 422, body: { code: 'INVALID_SETTING', message: 'fixture save rejected' } }
    },
    'POST /api/v1/admin/webhooks/1/test': { status: 202, body: { status: 'test queued' } },
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: 'Webhook 通知' }).click()
  await expect(page.getByRole('tabpanel')).not.toContainText('credential-secret')
  await expect(page.getByRole('tabpanel')).toContainText('上游源全部故障')
  await page.getByRole('button', { name: '启用 ops', exact: true }).click()
  await expect.poll(() => writes[0]).toEqual({ enabled: true })
  await expect(page.getByRole('button', { name: '暂停 ops', exact: true })).toBeVisible()
  await page.getByRole('button', { name: /测试.*ops/ }).click()
  await expect(page.getByRole('tabpanel')).toContainText('测试通知已加入发送队列')
  await page.getByRole('button', { name: '编辑 ops' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('冷却时间（分钟）', { exact: true }).fill('4')
  await expect(dialog.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  await dialog.getByLabel('冷却时间（分钟）', { exact: true }).fill('60')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('fixture save rejected')
  await expect(dialog.getByLabel('冷却时间（分钟）', { exact: true })).toHaveValue('60')
})

test('license validation errors survive reload and revalidation waits for a new status', async ({ page }) => {
  let status: EntitlementStatus = { is_pro: false, source: 'none', days_left: 0, trial_used: true, trial_available: false, expires_at: '2026-10-01', license_key_masked: 'depsilo-***', license_error: 'fixture validation unavailable', last_checked: '2026-10-21T00:00:00Z' }
  await mockAdminApi(page, {
    'GET /api/v1/admin/license/status': () => status,
    'POST /api/v1/admin/license/revalidate': { status: 202, body: { message: 'revalidation started' } },
  })
  await page.goto('/admin/license')
  await expect(page.getByText('fixture validation unavailable')).toBeVisible()
  await page.reload()
  await expect(page.getByText('fixture validation unavailable')).toBeVisible()
  const retry = page.getByRole('button', { name: '重新校验', exact: true })
  await retry.click()
  await expect(retry).toBeDisabled()
  await expect(page.getByText('校验已发起，正在等待新结果。')).toBeVisible()
  status = { ...status, is_pro: true, source: 'paid', license_error: undefined, last_checked: '2026-10-22T00:00:00Z' }
  await expect(page.getByText('Pro 已激活', { exact: true })).toBeVisible()
  await expect(retry).toBeEnabled()
  await expect(page.getByText('fixture validation unavailable')).toHaveCount(0)
})
