import { adminApiDefaults, test, expect, mockAdminApi } from './fixtures/admin-api'

test('closed mobile drawer has no focusable offscreen links', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeVisible()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: /管理导航/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
})

test('failed now request never displays healthy', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': { status: 500, body: { code: 'FAILED', message: 'down' } },
  })
  await page.goto('/admin')
  await expect(page.getByText(/状态不可用/)).toBeVisible()
  await expect(page.getByText(/健康|已就绪/)).toHaveCount(0)
})

test('stale cached status keeps its refresh action visible', async ({ page }) => {
  let calls = 0
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/now': () => {
      calls += 1
      return calls === 1
        ? adminApiDefaults['GET /api/v1/now']
        : { status: 500, body: { code: 'FAILED', message: 'down' } }
    },
  })
  await page.goto('/admin')
  const staleLabel = page.getByText('数据已过期')
  const refresh = page.getByRole('button', { name: '刷新' })
  await expect(staleLabel).toBeVisible({ timeout: 10_000 })
  await expect(refresh).toBeVisible()
  await expect(refresh).toBeInViewport()
  const strip = page.locator('[data-query-key="now"]')
  expect(await refresh.evaluate((button, root) => {
    const buttonBox = button.getBoundingClientRect()
    const rootBox = (root as HTMLElement).getBoundingClientRect()
    return buttonBox.right <= rootBox.right && buttonBox.left >= rootBox.left
  }, await strip.elementHandle())).toBe(true)
})

test('principal failure gates the outlet until Retry succeeds', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': () => {
      calls += 1
      return calls === 1
        ? { status: 500, body: { code: 'FAILED', message: 'principal unavailable' } }
        : adminApiDefaults['GET /api/v1/auth/me']
    },
  })
  await page.goto('/admin')
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)
  await page.getByRole('button', { name: /重试/ }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.locator('[data-admin-outlet]')).toBeVisible()
})
