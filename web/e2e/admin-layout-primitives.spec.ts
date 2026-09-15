import { test, expect } from './fixtures/admin-api'

test('Security view selection supports the keyboard without nested tabs', async ({ page }) => {
  await page.goto('/admin/security')
  const view = page.getByRole('combobox', { name: /情报视图|Intelligence view/ })
  await view.focus()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(view).toHaveValue('vulnerabilities')
  await expect(page).toHaveURL(/tab=vulnerabilities/)
  await expect(page.getByRole('tablist')).toHaveCount(0)
})
