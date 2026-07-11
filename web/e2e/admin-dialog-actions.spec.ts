import { test, expect } from './fixtures/admin-api'

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
  await expect(page.getByRole('tooltip')).toContainText(/关闭/)

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
  await page.goto('/admin/users')
  await page.getByRole('button', { name: /添加用户/ }).click()

  const button = page.getByRole('dialog').locator('[data-icon-button]:visible')
  await page.keyboard.press('Shift+Tab')

  await expect(button).toBeFocused()
  await expect(page.getByRole('tooltip')).toContainText(/关闭/)
})
