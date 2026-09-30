import { expect, setUiPreferences, test } from './fixtures/admin-api'

// shadcn's canvas is untextured in both themes. The Instrument film-grain
// layer is gone, so there is no ambient wash left to measure — the contract
// is now simply "plain canvas, no overlay".
test('light Portal uses one untextured pure-white canvas', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  await expect(page.getByRole('heading', { name: '快速开始' })).toBeVisible()
  await expect(page.locator('body')).toHaveCSS('background-color', 'oklch(1 0 0)')
  await expect(page.locator('.page-wash')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement)
      .getPropertyValue('--bg-page')
      .trim()
  ))).toBe('oklch(1 0 0)')
  await expect(page.locator('#root > .min-h-screen')).toHaveCSS(
    'background-color',
    'oklch(1 0 0)',
  )
})

test('dark Portal uses the near-black canvas without a grain layer', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'zh')
  await page.goto('/')

  await expect(page.locator('body')).toHaveCSS(
    'background-color',
    'oklch(0.145 0 0)',
  )
  await expect(page.locator('.page-wash')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement)
      .getPropertyValue('--bg-page')
      .trim()
  ))).toBe('oklch(0.145 0 0)')
})
