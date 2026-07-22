import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import {
  expect,
  expectResolvedUiPreferences,
  mockAdminApi,
  setUiPreferences,
  test,
  type UiLocale,
  type UiTheme,
} from './fixtures/admin-api'

test.describe.configure({ mode: 'parallel' })

const routes = ['/admin', '/admin/bandwidth', '/admin/logs', '/admin/audit', '/admin/quarantine', '/admin/cache', '/admin/indexes', '/admin/compile-cache', '/admin/upstreams', '/admin/upstream-updates', '/admin/users', '/admin/license', '/admin/rules', '/admin/security', '/admin/projects', '/admin/settings'] as const
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

const targetedRoutes = ['/admin', '/admin/settings', '/admin/bandwidth', '/admin/cache', '/admin/logs', '/admin/security', '/admin/quarantine'] as const
for (const width of [320, 768, 1024]) for (const theme of themes) for (const locale of locales) {
  for (const route of targetedRoutes) {
    test(`${route} targeted ${width} ${theme} ${locale} passes axe`, async ({ page }) => {
      await assertRouteMatrix(page, route, width, theme, locale)
    })
  }
}

test('/admin/upstream-updates populated table is keyboard-scrollable and passes axe', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstream-updates': {
      items: [{
        id: 1,
        cache_entry_id: 42,
        ecosystem: 'npm',
        upstream: 'npmjs',
        package: '@depsilo/example-package-with-a-long-name',
        result: 'updated',
        detail: 'cached metadata refreshed',
        latency_ms: 123,
        occurrence_count: 1,
        first_seen_at: '2026-07-17T08:00:00Z',
        last_seen_at: '2026-07-17T08:00:00Z',
        created_at: '2026-07-17T08:00:00Z',
      }],
      total: 1,
      next_cursor: null,
    },
  })

  await page.goto('/admin/upstream-updates')

  const viewport = page.getByRole('region', { name: /上游更新记录/ })
  await expect(viewport).toBeVisible()
  await expect(page.getByRole('cell', { name: '已更新', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '已刷新缓存元数据。', exact: true })).toBeVisible()
  expect(await viewport.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await viewport.focus()
  await expect(viewport).toBeFocused()
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
})

test('opened mobile Admin drawer passes axe and restores trigger focus', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await mockAdminApi(page)
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
