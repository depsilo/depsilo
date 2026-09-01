import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import { adminRouteManifest } from '../src/admin/routes'
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

async function assertAxe(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
}

async function assertAccessibleDocument(page: Page, width: number) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  expect(await page.locator('button:visible').evaluateAll(buttons => buttons.filter(button => {
    const rect = button.getBoundingClientRect()
    return button.dataset.iconButton === '' && (rect.width < 40 || rect.height < 40)
  }).length)).toBe(0)
  expect(await page.locator('#root *:visible').evaluateAll(elements => elements.filter(element => {
    const spacing = getComputedStyle(element).letterSpacing
    return spacing !== 'normal' && Math.abs(Number.parseFloat(spacing)) > 0.01
  }).length)).toBe(0)
  await assertAxe(page)
}

async function assertAccessibleRoute(page: Page, testCase: AccessibilityCase) {
  const { route, width, theme, locale } = testCase
  await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 })
  await setUiPreferences(page, theme, locale)
  await page.goto(route)
  await expectResolvedUiPreferences(page, theme, locale)
  await expect(page.locator('h1')).toBeVisible()
  await assertAccessibleDocument(page, width)
}

test('every registered Admin route passes the desktop accessibility contract', async ({ page }) => {
  test.setTimeout(120_000)
  const width = 1440
  const theme = 'dark'
  const locale = 'en'
  await page.setViewportSize({ width, height: 1000 })
  await setUiPreferences(page, theme, locale)

  for (const route of adminRouteManifest) {
    await test.step(`${route.id}: ${route.href}`, async () => {
      await page.goto(route.href)
      await expectResolvedUiPreferences(page, theme, locale)
      await expect(page.locator('h1')).toBeVisible()
      await expect(page.getByRole('navigation', { name: 'Admin navigation' })).toHaveCount(1)
      await assertAccessibleDocument(page, width)
    })
  }
})

const representativeAdminCases = [
  { route: '/admin', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/upstreams', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/security', width: 390, theme: 'light', locale: 'zh' },
  { route: '/admin/settings', width: 390, theme: 'light', locale: 'zh' },
] satisfies readonly AccessibilityCase[]

for (const testCase of representativeAdminCases) {
  test(`${testCase.route} ${testCase.width} ${testCase.theme} ${testCase.locale} passes the Admin accessibility contract`, async ({ page }) => {
    await assertAccessibleRoute(page, testCase)
  })
}

test.describe('unauthenticated Admin login', () => {
  test.use({ initialToken: null })

  test('/admin/login passes the accessibility contract', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await setUiPreferences(page, 'light', 'zh')
    await page.goto('/admin/login')
    await expectResolvedUiPreferences(page, 'light', 'zh')
    await expect(page.locator('h1')).toBeVisible()
    await assertAxe(page)
  })
})

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
