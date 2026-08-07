import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

test('dialog traps focus and restores its trigger', async ({ page }) => {
  await page.goto('/admin/users')
  const trigger = page.getByRole('button', { name: /添加用户/ })
  await trigger.click()

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  for (let index = 0; index < 12; index += 1) {
    await page.keyboard.press('Tab')
  }

  await expect.poll(() => dialog.evaluate((element) => element.contains(document.activeElement))).toBe(true)

  const closeButton = dialog.locator('[data-icon-button]:visible')
  await closeButton.focus()
  const tooltip = page.getByRole('tooltip')
  await expect(tooltip).toContainText(/关闭/)
  await expect(tooltip).toHaveCSS('background-color', 'rgb(233, 236, 238)')
  await expect(tooltip).toHaveCSS('color', 'rgb(11, 13, 15)')

  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
})

test('every visible icon action is named and at least 40px', async ({ page }) => {
  await page.goto('/admin/users')
  await page.getByRole('button', { name: /添加用户/ }).click()

  const button = page.getByRole('dialog').locator('[data-icon-button]:visible')
  await expect(button).toHaveCount(1)
  await expect(button).toHaveAttribute('aria-label', /关闭/)

  const box = await button.boundingBox()
  expect(box?.width).toBeGreaterThanOrEqual(40)
  expect(box?.height).toBeGreaterThanOrEqual(40)

  await button.hover()
  await expect(page.getByRole('tooltip')).toContainText(/关闭/)
})

test('icon action exposes its tooltip on keyboard focus', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/users')
  await page.getByRole('button', { name: /添加用户/ }).click()

  const button = page.getByRole('dialog').locator('[data-icon-button]:visible')
  await page.keyboard.press('Shift+Tab')

  await expect(button).toBeFocused()
  const tooltip = page.getByRole('tooltip')
  await expect(tooltip).toContainText(/关闭/)
  await expect(tooltip).toHaveCSS('background-color', 'rgb(20, 24, 26)')
  await expect(tooltip).toHaveCSS('color', 'rgb(255, 255, 255)')
})

test('a pending mutation locks every dialog close path until completion', async ({ page }) => {
  let release!: (value: unknown) => void
  const response = new Promise(resolve => { release = resolve })
  await mockAdminApi(page, {
    'POST /api/v1/admin/users': async () => response,
  })
  await page.goto('/admin/users')
  await page.getByRole('button', { name: /添加用户|Add User/ }).click()

  const dialog = page.getByRole('dialog', { name: /添加用户|Add User/ })
  await dialog.getByRole('textbox', { name: /用户名|Username/ }).fill('operator')
  await dialog.getByLabel(/密码|Password/).fill('correct horse battery staple')
  await dialog.getByRole('button', { name: /^保存$|^Save$/ }).click()

  await expect(dialog.getByRole('button', { name: /取消|Cancel/ })).toBeDisabled()
  await expect(dialog.locator('[data-icon-button]')).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()

  release({ id: 2, username: 'operator', role: 'readonly', enabled: true })
  await expect(dialog).toBeHidden()
})

const deleteRecoveryCases = [
  {
    name: 'package rule',
    path: '/admin/rules',
    listEndpoint: 'GET /api/v1/admin/rules',
    deleteEndpoint: 'DELETE /api/v1/admin/rules/7',
    listResponse: [{
      id: 7,
      ecosystem: 'pypi',
      package_name: 'unsafe-package',
      version: '*',
      action: 'deny',
      reason: 'blocked for review',
      created_by: 'admin',
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
    }],
    triggerName: /删除 unsafe-package 的规则|Delete rule for unsafe-package/,
    dialogName: /确认删除|Confirm Delete/,
    context: 'unsafe-package',
    failure: 'Rule storage is temporarily unavailable',
  },
  {
    name: 'project',
    path: '/admin/projects',
    listEndpoint: 'GET /api/v1/admin/projects',
    deleteEndpoint: 'DELETE /api/v1/admin/projects/11',
    listResponse: {
      items: [{
        id: 11,
        name: 'release-service',
        slug: 'release-service',
        description: 'Release automation',
        created_at: '2026-08-01T00:00:00Z',
        updated_at: '2026-08-01T00:00:00Z',
        package_count: 4,
        last_activity_at: '2026-08-06T00:00:00Z',
      }],
      total: 1,
    },
    triggerName: /删除 release-service|Delete release-service/,
    dialogName: /删除项目|Delete Project/,
    context: 'release-service',
    failure: 'Project storage is temporarily unavailable',
  },
] as const

for (const deleteCase of deleteRecoveryCases) {
  test(`failed ${deleteCase.name} deletion preserves context and retries safely`, async ({ page }) => {
    let deleteRequests = 0
    let markRequestStarted!: () => void
    let releaseFailure!: (value: unknown) => void
    const requestStarted = new Promise<void>((resolve) => { markRequestStarted = resolve })
    const firstResponse = new Promise<unknown>((resolve) => { releaseFailure = resolve })

    await mockAdminApi(page, {
      [deleteCase.listEndpoint]: deleteCase.listResponse,
      [deleteCase.deleteEndpoint]: () => {
        deleteRequests += 1
        if (deleteRequests === 1) {
          markRequestStarted()
          return firstResponse
        }
        return { deleted: true }
      },
    })
    await page.goto(deleteCase.path)

    await page.getByRole('button', { name: deleteCase.triggerName }).click()
    const dialog = page.getByRole('dialog', { name: deleteCase.dialogName })
    await expect(dialog).toContainText(deleteCase.context)

    await dialog.getByRole('button', { name: /^删除$|^Delete$/ }).click()
    await requestStarted

    await expect(dialog.getByRole('button', { name: /删除中|Deleting/ })).toHaveAttribute('aria-busy', 'true')
    await expect(dialog.getByRole('button', { name: /取消|Cancel/ })).toBeDisabled()
    await expect(dialog.getByRole('button', { name: /关闭|Close/ })).toBeDisabled()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeVisible()

    releaseFailure({
      status: 503,
      body: { code: 'DELETE_UNAVAILABLE', message: deleteCase.failure },
    })
    await expect(dialog.getByRole('alert')).toContainText(deleteCase.failure)
    await expect(dialog).toContainText(deleteCase.context)

    await dialog.getByRole('button', { name: /^删除$|^Delete$/ }).click()
    await expect(dialog).toBeHidden()
    expect(deleteRequests).toBe(2)
  })
}
