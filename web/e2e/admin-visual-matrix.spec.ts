import AxeBuilder from '@axe-core/playwright'
import { expect, expectResolvedUiPreferences, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const fullResolutionTrendPoints = Array.from({ length: 360 }, (_, index) => {
  const requests = 20 + (index % 11)
  const misses = 2 + (index % 4)
  const hits = requests - misses
  const errors = index % 17 === 0 ? 1 : 0
  return {
    bucket: 1783641600 + index * 3600,
    date: '2026-07-10',
    requests,
    hits,
    misses,
    hit_rate: hits / requests,
    bytes_served: requests * 256,
    bytes_hit: hits * 256,
    bytes_miss: misses * 256,
    sum_latency_ms: requests * (8 + (index % 9)),
    avg_latency_ms: 8 + (index % 9),
    errors,
  }
})

for (const viewport of [
  { label: 'desktop', width: 1280, height: 800 },
  { label: 'mobile', width: 390, height: 844 },
]) {
  test(`Trend chart renders 360 points without overflow on ${viewport.label}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await setUiPreferences(page, 'light', 'en')
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard/trends': { points: fullResolutionTrendPoints },
    })
    await page.goto('/admin')
    await expectResolvedUiPreferences(page, 'light', 'en')
    await page.getByRole('group', { name: 'Activity Trend' }).getByRole('button', { name: '7d' }).click()

    const trendChart = page.locator('[data-query-key="dashboard-trends"] .recharts-wrapper')
    await expect(trendChart).toBeVisible()
    const chartBox = await trendChart.boundingBox()
    expect(chartBox?.height).toBeGreaterThanOrEqual(220)
    const seriesPath = await trendChart.locator('.recharts-area-curve').first().getAttribute('d')
    expect(seriesPath?.match(/L/g) ?? []).toHaveLength(fullResolutionTrendPoints.length - 1)

    const visibleTicks = trendChart.locator('.recharts-xAxis-tick-labels .recharts-cartesian-axis-tick-value:visible')
    await expect(visibleTicks.first()).toBeVisible()
    const visibleTickCount = await visibleTicks.count()
    expect(visibleTickCount).toBeGreaterThanOrEqual(3)
    expect(visibleTickCount).toBeLessThanOrEqual(8)
    const pageWidths = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      innerWidth: window.innerWidth,
    }))
    expect(pageWidths.scrollWidth).toBeLessThanOrEqual(pageWidths.innerWidth)
  })
}

test('Admin outlet is centered in main and capped at 2560px', async ({ page }) => {
  const width = 2560
  await page.setViewportSize({ width, height: 1080 })
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'en')
  await expect(page.locator('[data-admin-main]')).toBeVisible()
  await expect(page.locator('[data-admin-outlet]')).toBeVisible()
  const metrics = await page.evaluate(() => {
    const main = document.querySelector<HTMLElement>('[data-admin-main]')
    const outlet = document.querySelector<HTMLElement>('[data-admin-outlet]')
    if (!main || !outlet) throw new Error('admin geometry hooks missing')
    const mainRect = main.getBoundingClientRect()
    const outletRect = outlet.getBoundingClientRect()
    return {
      mainLeft: mainRect.left,
      mainWidth: mainRect.width,
      outletLeft: outletRect.left,
      outletWidth: outletRect.width,
    }
  })
  expect(metrics.outletWidth).toBeCloseTo(1840, 0)
  expect(Math.abs(
    (metrics.outletLeft - metrics.mainLeft) - (metrics.mainWidth - metrics.outletWidth) / 2,
  )).toBeLessThanOrEqual(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
})

const portalVisualCases = [
  { route: '/', width: 390, theme: 'light' },
  { route: '/', width: 1440, theme: 'dark' },
  { route: '/monitor', width: 390, theme: 'dark' },
  { route: '/monitor', width: 1440, theme: 'light' },
] as const

for (const { route, width, theme } of portalVisualCases) {
  test(`Portal ${route} ${width} ${theme} has no token regression`, async ({ page }) => {
    const consoleErrors: string[] = []
    page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
    await setUiPreferences(page, theme, 'zh')
    await page.goto(route)
    await expectResolvedUiPreferences(page, theme, 'zh')
    await page.waitForFunction(() => {
      const animatedElements = [...document.querySelectorAll('.fade-up')]
      return animatedElements.length > 0 && animatedElements.every(element =>
        element.getAnimations().every(animation => animation.playState === 'finished'),
      )
    })
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
    expect(consoleErrors).toEqual([])
  })
}
