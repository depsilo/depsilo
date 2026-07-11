import AxeBuilder from '@axe-core/playwright'
import { expect, expectResolvedUiPreferences, setUiPreferences, test } from './fixtures/admin-api'

for (const width of [1920, 2560]) {
  test(`Admin outlet is centered in main and capped at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1080 })
    await setUiPreferences(page, 'light', 'en')
    await page.goto('/admin')
    await expectResolvedUiPreferences(page, 'light', 'en')
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
    expect(metrics.outletWidth).toBeLessThanOrEqual(1840)
    expect(Math.abs(
      (metrics.outletLeft - metrics.mainLeft) - (metrics.mainWidth - metrics.outletWidth) / 2,
    )).toBeLessThanOrEqual(1)
    if (width === 2560) expect(metrics.outletWidth).toBeCloseTo(1840, 0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
  })
}

for (const route of ['/', '/monitor']) for (const width of [390, 1440]) for (const theme of ['light', 'dark'] as const) {
  test(`Portal ${route} ${width} ${theme} has no token regression`, async ({ page }) => {
    const consoleErrors: string[] = []
    page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
    await setUiPreferences(page, theme, 'zh')
    await page.goto(route)
    await expectResolvedUiPreferences(page, theme, 'zh')
    await page.waitForFunction(() => [...document.querySelectorAll('.fade-up')].every(element =>
      element.getAnimations().every(animation => animation.playState === 'finished'),
    ))
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
    expect(consoleErrors).toEqual([])
  })
}
