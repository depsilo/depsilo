import type { Request } from '@playwright/test'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const operator = {
  id: 2,
  username: 'operator',
  role: 'readonly' as const,
  enabled: true,
  last_login_at: '2026-07-10T00:00:00Z',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
}

const apiToken = {
  id: 9,
  user_id: 1,
  name: 'release-pipeline',
  permissions: 'readwrite' as const,
  expires_at: null,
  last_used_at: '2026-07-10T00:00:00Z',
  created_at: '2026-07-01T00:00:00Z',
}

test('disabling a user requires confirmation, locks dismissal while pending, and restores focus', async ({ page }) => {
  let requestCount = 0
  let requestedEnabled: boolean | undefined
  let markRequestStarted!: () => void
  let releaseRequest!: () => void
  const requestStarted = new Promise<void>((resolve) => { markRequestStarted = resolve })
  const requestReleased = new Promise<void>((resolve) => { releaseRequest = resolve })

  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [operator],
    'PUT /api/v1/admin/users/2': async (request: Request) => {
      requestCount += 1
      requestedEnabled = (request.postDataJSON() as { enabled?: boolean }).enabled
      markRequestStarted()
      await requestReleased
      return { ...operator, enabled: false }
    },
  })
  await page.goto('/admin/users')

  const trigger = page.getByRole('button', { name: /禁用 operator|Disable operator/ })
  await trigger.click()

  const dialog = page.getByRole('dialog', { name: /禁用 operator|Disable operator/ })
  const cancel = dialog.getByRole('button', { name: /取消|Cancel/ })
  const confirm = dialog.getByRole('button', { name: /禁用 operator|Disable operator/ })
  await expect(dialog).toContainText('operator')
  await expect(dialog).toContainText(/登录会话和 API Token 将立即失效|login sessions and API tokens/)
  await expect(cancel).toBeFocused()
  expect(requestCount).toBe(0)

  await cancel.click()
  await expect(dialog).not.toBeVisible()
  await expect(trigger).toBeFocused()
  expect(requestCount).toBe(0)

  await trigger.click()
  await confirm.click()
  await requestStarted

  expect(requestedEnabled).toBe(false)
  await expect(dialog.getByRole('button', { name: /禁用中|Disabling/ })).toHaveAttribute('aria-busy', 'true')
  await expect(cancel).toBeDisabled()
  await expect(dialog.getByRole('button', { name: /关闭|Close/ })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()

  releaseRequest()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('button', { name: /启用 operator|Enable operator/ })).toBeFocused()
})

test('a failed user disable stays in context and can be retried', async ({ page }) => {
  let requestCount = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [operator],
    'PUT /api/v1/admin/users/2': () => {
      requestCount += 1
      if (requestCount === 1) {
        return {
          status: 503,
          body: { code: 'USER_UPDATE_UNAVAILABLE', message: 'User service is temporarily unavailable' },
        }
      }
      return { ...operator, enabled: false }
    },
  })
  await page.goto('/admin/users')

  await page.getByRole('button', { name: /禁用 operator|Disable operator/ }).click()
  const dialog = page.getByRole('dialog', { name: /禁用 operator|Disable operator/ })
  const confirm = dialog.getByRole('button', { name: /禁用 operator|Disable operator/ })
  await confirm.click()

  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toContainText('User service is temporarily unavailable')
  await expect(confirm).toBeEnabled()

  await confirm.click()
  await expect(dialog).not.toBeVisible()
  expect(requestCount).toBe(2)
  await expect(page.getByRole('button', { name: /启用 operator|Enable operator/ })).toBeVisible()
})

test('revoking an API token names the affected client and preserves failures for retry', async ({ page }) => {
  let revoked = false
  let requestCount = 0
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/tokens': () => revoked ? [] : [apiToken],
    'DELETE /api/v1/admin/tokens/9': () => {
      requestCount += 1
      if (requestCount === 1) {
        return {
          status: 500,
          body: { code: 'TOKEN_REVOKE_FAILED', message: 'Token storage is temporarily unavailable' },
        }
      }
      revoked = true
      return { status: 'revoked' }
    },
  })
  await page.goto('/admin/users')

  const trigger = page.getByRole('button', { name: /^撤销$|^Revoke$/ })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: /撤销 API Token|Revoke API token/ })
  const confirm = dialog.getByRole('button', { name: /确认撤销|Revoke token/ })

  await expect(dialog).toContainText('release-pipeline')
  await expect(dialog).toContainText(/立即失去访问权限|immediately lose access/)
  expect(requestCount).toBe(0)

  await confirm.click()
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('alert')).toContainText('Token storage is temporarily unavailable')

  await confirm.click()
  await expect(dialog).not.toBeVisible()
  expect(requestCount).toBe(2)
  await expect(page.getByText(/暂无 Token|No tokens/)).toBeVisible()
})

test('enabling remains direct but exposes a visible failure with retry', async ({ page }) => {
  const disabledOperator = { ...operator, enabled: false }
  let requestCount = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [disabledOperator],
    'PUT /api/v1/admin/users/2': () => {
      requestCount += 1
      if (requestCount === 1) {
        return {
          status: 503,
          body: { code: 'USER_UPDATE_UNAVAILABLE', message: 'User service is temporarily unavailable' },
        }
      }
      return operator
    },
  })
  await page.goto('/admin/users')

  await page.getByRole('button', { name: /启用 operator|Enable operator/ }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)

  const failure = page.getByRole('alert')
  await expect(failure).toContainText(/无法启用 operator|Could not enable operator/)
  await expect(failure).toContainText('User service is temporarily unavailable')

  await failure.getByRole('button', { name: /重试|Retry/ }).click()
  expect(requestCount).toBe(2)
  await expect(failure).not.toBeVisible()
  await expect(page.getByRole('button', { name: /禁用 operator|Disable operator/ })).toBeVisible()
})

test('one-time token stays visible until confirmed, then clears before another generation', async ({ page }) => {
  let releaseRequest!: () => void
  let startRequest!: () => void
  const started = new Promise<void>(resolve => { startRequest = resolve })
  const released = new Promise<void>(resolve => { releaseRequest = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [operator],
    'GET /api/v1/admin/tokens': [apiToken],
    'POST /api/v1/admin/tokens': async () => {
      startRequest()
      await released
      return { token: 'one-time-secret-value' }
    },
  })
  await page.goto('/admin/users')

  await expect(page.getByRole('region', { name: '用户表格' })).toContainText('只读')
  await expect(page.getByRole('region', { name: 'API Token 表格' })).toContainText('读写')
  await page.getByRole('button', { name: '生成 Token' }).click()
  const create = page.getByRole('dialog', { name: '生成 Token' })
  await create.getByRole('textbox', { name: '名称' }).fill('build-agent')
  await create.getByRole('button', { name: '生成', exact: true }).click()
  await started
  await expect(create.getByRole('button', { name: '取消' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(create).toBeVisible()
  releaseRequest()

  const result = page.getByRole('dialog', { name: 'Token 已生成' })
  await expect(result).toContainText('one-time-secret-value')
  await result.getByRole('button', { name: '确定' }).click()
  await expect(result).toHaveCount(0)
  await expect(page.getByText('one-time-secret-value')).toHaveCount(0)
  await page.getByRole('button', { name: '生成 Token' }).click()
  await expect(create.getByRole('textbox', { name: '名称' })).toHaveValue('')
  await expect(create.getByRole('combobox', { name: '权限' })).toHaveValue('readonly')
})
