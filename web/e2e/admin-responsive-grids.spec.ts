import { expect, mockAdminApi, test } from './fixtures/admin-api'

const widths = [320, 390, 768, 1024, 1440]
const routes = ['/admin', '/admin/bandwidth', '/admin/cache', '/admin/security', '/admin/license']
const metricRoutes = new Set(['/admin', '/admin/bandwidth', '/admin/cache', '/admin/security'])

for (const width of widths) {
  for (const route of routes) {
    test(`${route} fits ${width}px`, async ({ page }) => {
      await mockAdminApi(page)
      await page.setViewportSize({ width, height: width < 700 ? 844 : 1000 })
      await page.goto(route)
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
      const escaped = await page.locator('body *:visible').evaluateAll(elements => elements.filter(element => {
        if (element.closest('[data-table-viewport]')) return false
        const tablist = element.closest('[role="tablist"]')
        if (tablist && getComputedStyle(tablist).overflowX === 'auto') return false
        const rect = element.getBoundingClientRect()
        return rect.left < -1 || rect.right > innerWidth + 1
      }).map(element => element.tagName + '.' + element.className))
      expect(escaped).toEqual([])

      if (metricRoutes.has(route)) {
        const metricValues = page.locator('[data-metric-value]')
        await expect(metricValues.first()).toBeVisible()
        const wrappedMetrics = await metricValues.evaluateAll(elements => elements
          .filter(element => getComputedStyle(element).whiteSpace === 'normal')
          .map(element => element.textContent))
        expect(wrappedMetrics).toEqual([])
      }

      if (route === '/admin/security' && width === 320) {
        const tablist = page.getByRole('tablist')
        await expect(tablist).toHaveCSS('overflow-x', 'auto')
        expect(await tablist.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
        expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
      }
    })
  }
}

test('Dashboard keeps its primary metrics in a compact mobile 2 by 2 grid', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  const grid = page.locator('[data-dashboard-kpis]')
  await expect(grid.locator(':scope > *')).toHaveCount(4)
  expect(await grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)).toBe(2)
})
