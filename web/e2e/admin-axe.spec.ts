import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import {
  expect,
  expectResolvedUiPreferences,
  setUiPreferences,
  test,
  type UiLocale,
  type UiTheme,
} from './fixtures/admin-api'

interface AccessibilityCase {
  route: string
  width: number
  theme: UiTheme
  locale: UiLocale
}

async function assertAccessibleRoute(page: Page, testCase: AccessibilityCase) {
  const { route, width, theme, locale } = testCase
  await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 })
  await setUiPreferences(page, theme, locale)
  await page.goto(route)
  await expectResolvedUiPreferences(page, theme, locale)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  expect(await page.locator('button:visible').evaluateAll(buttons => buttons.filter(button => {
    const rect = button.getBoundingClientRect()
    return button.dataset.iconButton === '' && (rect.width < 40 || rect.height < 40)
  }).length)).toBe(0)
  expect(await page.locator('#root *:visible').evaluateAll(elements => elements.filter(element => {
    const spacing = getComputedStyle(element).letterSpacing
    return spacing !== 'normal' && Math.abs(Number.parseFloat(spacing)) > 0.01
  }).length)).toBe(0)
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
}

const representativeAdminCases = [
  { route: '/admin', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/upstreams', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/security', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/settings', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin', width: 1440, theme: 'dark', locale: 'en' },
] satisfies readonly AccessibilityCase[]

for (const testCase of representativeAdminCases) {
  test(`${testCase.route} ${testCase.width} ${testCase.theme} ${testCase.locale} passes the Admin accessibility contract`, async ({ page }) => {
    await assertAccessibleRoute(page, testCase)
  })
}

test('opened mobile Admin drawer passes axe and restores trigger focus', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('/admin')

  const trigger = page.getByRole('button', { name: /打开导航/ })
  await trigger.click()
  const drawer = page.getByRole('dialog', { name: /管理导航/ })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByRole('link', { name: '总览', exact: true })).toBeFocused()
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])

  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
})
