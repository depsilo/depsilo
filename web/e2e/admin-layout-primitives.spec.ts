import { test, expect } from './fixtures/admin-api'

test('Security view selection supports the keyboard without nested tabs', async ({ page }) => {
  // macOS headless Chromium opens the native <select> popup on ArrowDown and
  // never commits on Enter, so this keyboard path cannot be driven here — a
  // bare <select> in page.setContent reproduces it. CI runs on ubuntu-latest,
  // where the full path is exercised; skipping keeps local runs honest rather
  // than deleting the contract.
  test.skip(
    process.platform === 'darwin',
    'native select keyboard cannot be driven in macOS headless Chromium',
  )

  await page.goto('/admin/security')
  const view = page.getByRole('combobox', { name: /情报视图|Intelligence view/ })
  await view.focus()
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(view).toHaveValue('vulnerabilities')
  await expect(page).toHaveURL(/tab=vulnerabilities/)
  await expect(page.getByRole('tablist')).toHaveCount(0)
})
