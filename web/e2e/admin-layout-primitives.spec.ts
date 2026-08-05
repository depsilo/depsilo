import { test, expect } from './fixtures/admin-api'

test('tabs use arrow keys and expose their selected panel', async ({ page }) => {
  await page.goto('/admin/security')
  const first = page.getByRole('tab').first()
  await first.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab').nth(1)).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tabpanel')).toBeVisible()
})
