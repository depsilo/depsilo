import { test, expect } from './fixtures/admin-api'

test('tabs use arrow keys and expose their selected panel', async ({ page }) => {
  await page.goto('/admin/security')
  const first = page.getByRole('tab').first()
  await first.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab').nth(1)).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tabpanel')).toBeVisible()
})

test('mobile tables scroll locally without widening the page', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/logs')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  await expect(page.getByRole('region', { name: /访问日志表格/ })).toHaveCSS('overflow-x', 'auto')
})
