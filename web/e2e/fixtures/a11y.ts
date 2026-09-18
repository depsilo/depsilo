import AxeBuilder from '@axe-core/playwright'
import { expect } from '@playwright/test'
import type { Page } from '@playwright/test'

import { expectResolvedUiPreferences, setUiPreferences } from './admin-api'
import type { UiLocale, UiTheme } from './admin-api'

/**
 * The accessibility contract every surface shares: Admin, Portal, and the
 * first-run Setup.
 *
 * It is one function on purpose. Stage B step 14 gave the anonymous surfaces
 * the standard the Admin routes already had, and the way that stays true is
 * that any route can be dropped into a case list rather than re-deriving the
 * checks — the Admin spec had four of these and no way to reuse them.
 */

export interface AccessibilityCase {
  route: string
  width: number
  theme: UiTheme
  locale: UiLocale
}

export async function assertAxe(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
}

export async function assertAccessibleDocument(page: Page, width: number) {
  // No horizontal document scroll: a dense product either fits or scrolls
  // inside a labelled region, never by moving the page sideways.
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  // Icon-only controls keep the 40px target floor in every state, including
  // the pending state of a mutation.
  expect(await page.locator('button:visible').evaluateAll(buttons => buttons.filter(button => {
    const rect = button.getBoundingClientRect()
    return button.dataset.iconButton === '' && (rect.width < 40 || rect.height < 40)
  }).length)).toBe(0)
  // No letter-spacing anywhere: the product has no display type that needs
  // tracking, and negative tracking was how the old Portal titles were sized.
  expect(await page.locator('#root *:visible').evaluateAll(elements => elements.filter(element => {
    const spacing = getComputedStyle(element).letterSpacing
    return spacing !== 'normal' && Math.abs(Number.parseFloat(spacing)) > 0.01
  }).length)).toBe(0)
  await assertAxe(page)
}

export async function assertAccessibleRoute(page: Page, testCase: AccessibilityCase) {
  const { route, width, theme, locale } = testCase
  await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 })
  await setUiPreferences(page, theme, locale)
  await page.goto(route)
  await expectResolvedUiPreferences(page, theme, locale)
  await expect(page.locator('h1')).toBeVisible()
  await assertAccessibleDocument(page, width)
}

/** `1440 dark en` and `390 light zh`: the two shapes every surface must hold. */
export const RESPONSIVE_VIEWPORTS = [
  { width: 1440, theme: 'dark', locale: 'en' },
  { width: 390, theme: 'light', locale: 'zh' },
] satisfies readonly Omit<AccessibilityCase, 'route'>[]
