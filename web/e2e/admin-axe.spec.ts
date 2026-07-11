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

test.describe.configure({ mode: 'parallel' })

const routes = ['/admin', '/admin/bandwidth', '/admin/logs', '/admin/audit', '/admin/quarantine', '/admin/cache', '/admin/upstreams', '/admin/users', '/admin/license', '/admin/rules', '/admin/security', '/admin/projects', '/admin/settings'] as const
const themes = ['light', 'dark'] as const satisfies readonly UiTheme[]
const locales = ['zh', 'en'] as const satisfies readonly UiLocale[]

async function assertRouteMatrix(page: Page, route: string, width: number, theme: UiTheme, locale: UiLocale) {
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

for (const width of [390, 1440]) for (const theme of themes) for (const locale of locales) {
  for (const route of routes) {
    test(`${route} ${width} ${theme} ${locale} passes axe`, async ({ page }) => {
      await assertRouteMatrix(page, route, width, theme, locale)
    })
  }
}

const targetedRoutes = ['/admin/settings', '/admin/bandwidth', '/admin/cache', '/admin/logs', '/admin/security', '/admin/quarantine'] as const
for (const width of [320, 768, 1024]) for (const theme of themes) for (const locale of locales) {
  for (const route of targetedRoutes) {
    test(`${route} targeted ${width} ${theme} ${locale} passes axe`, async ({ page }) => {
      await assertRouteMatrix(page, route, width, theme, locale)
    })
  }
}
