import type { Locator, Page } from '@playwright/test'
import {
  expect,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

async function expectPureWhiteCanvas(page: Page) {
  await expect(page.locator('body')).toHaveCSS(
    'background-color',
    'rgb(255, 255, 255)',
  )
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement)
      .getPropertyValue('--bg-page')
      .trim()
  ))).toBe('#FFFFFF')

  const wash = page.locator('.page-wash')
  await expect(wash).toHaveCount(1)
  await expect.poll(() => wash.evaluate(element => (
    getComputedStyle(element, '::before').opacity
  ))).toBe('0')
}

async function expectPureWhiteSurface(surface: Locator) {
  await expect(surface).toHaveCSS('background-color', 'rgb(255, 255, 255)')
}

test('light Portal uses one untextured pure-white canvas', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/')

  await expect(page.getByRole('heading', { name: '快速开始' })).toBeVisible()
  await expectPureWhiteCanvas(page)
  await expectPureWhiteSurface(page.locator('#root > .min-h-screen'))
})

test('dark Portal keeps one subtle grain layer', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'zh')
  await page.goto('/')

  await expect(page.locator('body')).toHaveCSS(
    'background-color',
    'rgb(11, 13, 15)',
  )
  const wash = page.locator('.page-wash')
  await expect(wash).toHaveCount(1)
  await expect.poll(() => wash.evaluate(element => (
    getComputedStyle(element, '::before').opacity
  ))).toBe('0.07')
})
