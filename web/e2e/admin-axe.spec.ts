import AxeBuilder from '@axe-core/playwright'
import { adminRouteManifest } from '../src/admin/routes'
import {
  assertAccessibleDocument,
  assertAccessibleRoute,
  assertAxe,
  type AccessibilityCase,
} from './fixtures/a11y'
import {
  expect,
  expectResolvedUiPreferences,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

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
