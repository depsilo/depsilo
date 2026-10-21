import { test, expect, setUiPreferences } from './fixtures/admin-api'

for (const width of [1440, 320]) {
  test(`settings tabs retain directional keyboard navigation at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/admin/settings')
    const tabs = page.getByRole('tablist').getByRole('tab')
    await expect(tabs.first()).toHaveAttribute('aria-selected', 'true')
    await tabs.first().focus()
    await page.keyboard.press(width >= 768 ? 'ArrowDown' : 'ArrowRight')
    await expect(tabs.nth(1)).toBeFocused()
    await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tabpanel')).toHaveCount(1)
    await expect(page.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', await tabs.nth(1).getAttribute('id') ?? '')
    await page.keyboard.press('Home')
    await expect(tabs.first()).toBeFocused()
    await expect(tabs.first()).toHaveAttribute('aria-selected', 'true')
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  })
}

for (const theme of ['dark', 'light'] as const) {
  test(`native dropdown options remain readable in ${theme} mode with an opposite OS theme`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme === 'dark' ? 'light' : 'dark' })
    await setUiPreferences(page, theme, 'en')
    await page.goto('/admin/security')
    const view = page.getByRole('combobox', { name: 'Intelligence view', exact: true })
    await expect(view).toBeVisible()

    // Native popup surfaces are painted outside the page on some platforms.
    // Each option must supply an opaque, readable pair instead of combining
    // the app's foreground with an OS-provided popup background.
    const optionPaints = await page.getByRole('combobox').locator('option').evaluateAll(options => {
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 1
      const context = canvas.getContext('2d')!
      function rgba(color: string) {
        context.clearRect(0, 0, 1, 1)
        context.fillStyle = color
        context.fillRect(0, 0, 1, 1)
        return [...context.getImageData(0, 0, 1, 1).data]
      }
      function luminance(channels: number[]) {
        const linear = channels.slice(0, 3).map(channel => {
          const value = channel / 255
          return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
        })
        return linear[0] * 0.2126 + linear[1] * 0.7152 + linear[2] * 0.0722
      }
      return options.map(option => {
        const style = getComputedStyle(option)
        const background = rgba(style.backgroundColor)
        const foreground = rgba(style.color)
        const light = Math.max(luminance(background), luminance(foreground))
        const dark = Math.min(luminance(background), luminance(foreground))
        return { label: option.textContent, backgroundAlpha: background[3], contrast: (light + 0.05) / (dark + 0.05) }
      })
    })
    expect(optionPaints.length).toBeGreaterThan(0)
    for (const option of optionPaints) {
      expect(option.backgroundAlpha, `${option.label}: opaque popup background`).toBe(255)
      expect(option.contrast, `${option.label}: readable popup text`).toBeGreaterThanOrEqual(4.5)
    }

    await view.selectOption('vulnerabilities')
    await expect(page).toHaveURL(/tab=vulnerabilities/)

  })
}

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
