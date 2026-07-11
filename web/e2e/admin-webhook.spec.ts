import type { Page } from '@playwright/test'
import type { WebhookConfig } from '../src/lib/api'
import { test, expect, mockAdminApi } from './fixtures/admin-api'

const webhookRows = [
  { id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/one', enabled: true, events: '*', cooldown_minutes: 30, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' },
  { id: 2, name: 'audit', platform: 'generic', url: 'https://example.test/two', enabled: false, events: 'tamper_detected', cooldown_minutes: 60, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' },
] satisfies WebhookConfig[]

async function openWebhookTab(page: Page) {
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  return page.getByRole('tabpanel')
}

test('webhook rows and actions fit 390px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  await expect(page.getByRole('button', { name: /添加 Webhook/ })).toBeVisible()
})

test('failed webhook test renders a danger toast', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [{ id: 1, name: 'ops', platform: 'slack', url: 'https://example.test/hook', enabled: true, events: '*', cooldown_minutes: 30, last_sent_at: null, created_at: '2026-07-10T00:00:00Z', updated_at: '2026-07-10T00:00:00Z' }],
    'POST /api/v1/admin/webhooks/1/test': { status: 502, body: { code: 'WEBHOOK_FAILED', message: 'delivery failed' } },
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: /Webhook/ }).click()
  await page.getByRole('button', { name: /测试/ }).click()
  await expect(page.getByRole('alert')).toContainText('delivery failed')
  await expect(page.locator('[data-toast-tone="danger"]')).toContainText('delivery failed')
})

test('renders loading before an empty successful Webhook response', async ({ page }) => {
  let release!: (rows: WebhookConfig[]) => void
  const response = new Promise<WebhookConfig[]>(resolve => { release = resolve })
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': async () => response })
  const panel = await openWebhookTab(page)
  await expect(panel.locator('[aria-busy="true"]')).toBeVisible()
  await expect(page.getByText(/尚未配置 Webhook/)).toHaveCount(0)
  release([])
  await expect(page.getByText(/尚未配置 Webhook/)).toBeVisible()
})

test('renders one Webhook row without an empty or duplicate action state', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': [webhookRows[0]] })
  await openWebhookTab(page)
  await expect(page.getByText('ops')).toHaveCount(1)
  await expect(page.getByRole('button', { name: /测试 ops/ })).toHaveCount(1)
  await expect(page.getByText(/尚未配置 Webhook/)).toHaveCount(0)
})

test('renders enabled and disabled webhooks with shared actions', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': webhookRows })
  await openWebhookTab(page)
  await expect(page.getByText('ops')).toBeVisible()
  const disabled = page.getByText(/^已禁用$/)
  await expect(disabled).toBeVisible()
  await expect(page.getByRole('button', { name: /删除 audit/ })).toBeVisible()
  const colors = await disabled.evaluate(element => {
    const style = getComputedStyle(element)
    return { background: style.backgroundColor, color: style.color }
  })
  expect(colors.background).not.toBe('rgba(0, 0, 0, 0)')
  expect(colors.color).not.toBe('rgba(0, 0, 0, 0)')
  expect(colors.color).not.toBe('')
})

test('keeps Test stable while pending and announces success', async ({ page }) => {
  let release!: (value: { status: string }) => void
  const response = new Promise<{ status: string }>(resolve => { release = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[0]],
    'POST /api/v1/admin/webhooks/1/test': async () => response,
  })
  await openWebhookTab(page)
  const button = page.getByRole('button', { name: /测试 ops/ })
  const before = await button.boundingBox()
  await button.click()
  await expect(button).toBeDisabled()
  await expect(button).toHaveAttribute('aria-busy', 'true')
  expect(await button.boundingBox()).toEqual(before)
  release({ status: 'test sent' })
  await expect(page.locator('[data-toast-tone="success"]')).toContainText(/测试通知已发送/)
})

test('shows the service error and no success Toast when Test fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[0]],
    'POST /api/v1/admin/webhooks/1/test': { status: 502, body: { code: 'WEBHOOK_FAILED', message: 'fixture webhook failure' } },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /测试 ops/ }).click()
  await expect(page.getByRole('alert')).toContainText('fixture webhook failure')
  await expect(page.locator('[data-toast-tone="success"]')).toHaveCount(0)
})

test('hides mutation controls when the principal cannot write', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { id: 2, username: 'viewer', role: 'viewer', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: false },
    'GET /api/v1/admin/webhooks': webhookRows,
  })
  await openWebhookTab(page)
  await expect(page.getByText('ops')).toBeVisible()
  await expect(page.getByRole('button', { name: /添加 Webhook/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /测试 ops/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /编辑 ops/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /删除 ops/ })).toHaveCount(0)
})

test('does not infer wildcard from an explicit seven-item set with a custom member', async ({ page }) => {
  let payload: unknown
  const customWebhook = { ...webhookRows[1], enabled: true, events: 'tamper_detected,custom_specific' }
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [customWebhook],
    'PUT /api/v1/admin/webhooks/2': request => {
      payload = request.postDataJSON()
      return customWebhook
    },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /编辑 audit/ }).click()
  await expect(page.getByRole('checkbox', { name: /检测到内容篡改/ })).toBeChecked()
  for (const eventName of [/磁盘使用率过高/, /严重安全漏洞/, /许可证即将到期/, /供应链隔离拦截/, /恶意软件拦截/]) {
    await page.getByRole('checkbox', { name: eventName }).check()
  }
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect.poll(() => payload).toMatchObject({
    events: 'tamper_detected,custom_specific,disk_high,vuln_critical,license_expiring,quarantine_blocked,malware_blocked',
  })
  expect(payload).not.toMatchObject({ events: '*' })
})

test('keeps an untouched server wildcard while saving another field', async ({ page }) => {
  let payload: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[0]],
    'PUT /api/v1/admin/webhooks/1': request => {
      payload = request.postDataJSON()
      return webhookRows[0]
    },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /编辑 ops/ }).click()
  await page.getByRole('textbox', { name: /^名称$/ }).fill('ops renamed')
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect.poll(() => payload).toMatchObject({ name: 'ops renamed', events: '*' })
})

test('serializes all seven explicitly selected events without wildcard', async ({ page }) => {
  let payload: unknown
  const specificWebhook = { ...webhookRows[1], enabled: true }
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [specificWebhook],
    'PUT /api/v1/admin/webhooks/2': request => {
      payload = request.postDataJSON()
      return specificWebhook
    },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /编辑 audit/ }).click()
  for (const eventName of [/上游源全部故障/, /磁盘使用率过高/, /严重安全漏洞/, /许可证即将到期/, /供应链隔离拦截/, /恶意软件拦截/]) {
    await page.getByRole('checkbox', { name: eventName }).check()
  }
  await page.getByRole('button', { name: /^保存$/ }).click()
  await expect.poll(() => payload).toMatchObject({
    events: 'tamper_detected,upstream_down,disk_high,vuln_critical,license_expiring,quarantine_blocked,malware_blocked',
  })
  expect(payload).not.toMatchObject({ events: '*' })
})

test('prevents an empty event selection from being saved as wildcard', async ({ page }) => {
  let updateRequests = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/webhooks': [webhookRows[1]],
    'PUT /api/v1/admin/webhooks/2': request => {
      updateRequests += 1
      return request.postDataJSON()
    },
  })
  await openWebhookTab(page)
  await page.getByRole('button', { name: /编辑 audit/ }).click()
  await page.getByRole('checkbox', { name: /检测到内容篡改/ }).uncheck()
  await expect(page.getByRole('alert')).toContainText(/至少选择一个触发事件/)
  await expect(page.getByRole('button', { name: /^保存$/ })).toBeDisabled()
  expect(updateRequests).toBe(0)
})

test('opens a named delete Dialog and restores the Delete trigger', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/webhooks': webhookRows })
  await openWebhookTab(page)
  const trigger = page.getByRole('button', { name: /删除 audit/ })
  await trigger.click()
  await expect(page.getByRole('dialog', { name: /删除.*Webhook/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
})
