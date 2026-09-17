import type { Page } from '@playwright/test'
import { expect, setUiPreferences, test } from './fixtures/admin-api'

/**
 * Relative luminance of a token's resolved colour.
 *
 * Colours are painted onto a canvas and read back as sRGB bytes, so this works
 * for any colour space the token layer uses and never pins a literal value.
 */
async function tokenLuminance(page: Page, token: string): Promise<number> {
  return page.evaluate((name) => {
    const probe = document.createElement('span')
    probe.style.color = `var(${name})`
    document.body.appendChild(probe)
    const resolved = getComputedStyle(probe).color
    probe.remove()

    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const context = canvas.getContext('2d')
    if (!context) throw new Error('2d context unavailable')
    context.fillStyle = resolved
    context.fillRect(0, 0, 1, 1)
    const [r, g, b] = context.getImageData(0, 0, 1, 1).data
    return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
  }, token)
}

async function expectCanvasIsToken(page: Page, token: string) {
  const resolved = await page.evaluate((name) => {
    const probe = document.createElement('span')
    probe.style.color = `var(${name})`
    document.body.appendChild(probe)
    const value = getComputedStyle(probe).color
    probe.remove()
    return value
  }, token)
  await expect(page.locator('body')).toHaveCSS('background-color', resolved)
}

test('light Portal paints the shared light canvas without a decorative overlay', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  await expect(page.getByRole('heading', { name: '快速开始' })).toBeVisible()
  await expectCanvasIsToken(page, '--background')

  // Light means a near-white canvas that is clearly lighter than the text role.
  const [background, foreground] = await Promise.all([
    tokenLuminance(page, '--background'),
    tokenLuminance(page, '--foreground'),
  ])
  expect(background).toBeGreaterThan(0.8)
  expect(foreground).toBeLessThan(0.3)

  // Stage A has no ambient wash or grain layer behind the product surfaces.
  await expect(page.locator('.page-wash')).toHaveCount(0)
})

test('dark Portal resolves the dark canvas and the inverted text role', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'zh')
  await page.goto('/')

  await expectCanvasIsToken(page, '--background')
  const [background, foreground] = await Promise.all([
    tokenLuminance(page, '--background'),
    tokenLuminance(page, '--foreground'),
  ])
  expect(background).toBeLessThan(0.3)
  expect(foreground).toBeGreaterThan(0.8)
  await expect(page.locator('.page-wash')).toHaveCount(0)
})
