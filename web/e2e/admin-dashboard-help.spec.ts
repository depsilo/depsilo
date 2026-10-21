import AxeBuilder from '@axe-core/playwright'
import { expect, setUiPreferences, test } from './fixtures/admin-api'

test('metric explanations open by keyboard and return focus after Escape', async ({ page }) => {
  await page.goto('/admin')
  const trigger = page.getByRole('button', { name: /查看节省口径$/ })
  await trigger.focus()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.keyboard.press('Enter')

  const explanation = page.getByRole('dialog', { name: '节省回源流量 · 估算', exact: true })
  await expect(explanation).toBeVisible()
  await expect(explanation).toContainText('缓存命中')
  await page.keyboard.press('Escape')
  await expect(explanation).toBeHidden()
  await expect(trigger).toBeFocused()

  await page.keyboard.press('Space')
  await expect(explanation).toBeVisible()
  await explanation.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(explanation).toBeHidden()
  await expect(trigger).toBeFocused()
})

test('switching metric explanations keeps one popup and outside click dismisses it', async ({ page }) => {
  await page.goto('/admin')
  await page.getByRole('button', { name: /查看节省口径$/ }).click()
  await expect(page.getByRole('dialog', { name: '节省回源流量 · 估算', exact: true })).toBeVisible()

  await page.getByRole('button', { name: /查看响应耗时口径$/ }).click()
  const explanation = page.getByRole('dialog', { name: '平均响应耗时', exact: true })
  await expect(explanation).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(1)
  await expect(explanation).toContainText('响应传输')
  await expect(explanation).toContainText('1,104')
  await expect(explanation).toContainText('96')

  await page.getByRole('heading', { name: '总览', exact: true }).click()
  await expect(explanation).toBeHidden()
})

test.describe('touch metric explanations', () => {
  test.use({ viewport: { width: 320, height: 900 }, hasTouch: true })

  test('a tapped explanation stays readable within a narrow viewport and has a reachable close action', async ({ page }) => {
    await page.goto('/admin')
    const trigger = page.getByRole('button', { name: /查看缓存空间口径$/ })
    await trigger.tap()
    const explanation = page.getByRole('dialog', { name: '当前缓存占用', exact: true })
    await expect(explanation).toBeVisible()
    await expect(explanation).toContainText('配额')

    await expect.poll(async () => {
      const box = await explanation.boundingBox()
      return Boolean(box && box.x >= 8 && box.x + box.width <= 312 && box.y >= 0 && box.y + box.height <= 900)
    }).toBe(true)
    const body = page.locator('[data-dashboard-metric-help]')
    expect(await body.evaluate(element => parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(14)
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    const close = explanation.getByRole('button', { name: '关闭', exact: true })
    await expect.poll(async () => (await close.boundingBox())?.width ?? 0).toBeGreaterThanOrEqual(40)
    await expect.poll(async () => (await close.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(40)
    await close.tap()
    await expect(explanation).toBeHidden()
    await expect(trigger).toBeFocused()
  })
})

test('English metric labels and light-theme explanations remain legible', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/admin')
  const trigger = page.getByRole('button', { name: /View savings measurement basis$/ })
  await expect(trigger).toContainText('Origin traffic saved')
  expect(await trigger.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true)
  await trigger.click()

  const explanation = page.getByRole('dialog', { name: 'Origin traffic saved · estimated', exact: true })
  await expect(explanation).toBeVisible()
  const body = page.locator('[data-dashboard-metric-help]')
  expect(await body.evaluate(element => parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(14)
  const accessibility = await new AxeBuilder({ page }).include('[data-dashboard-metric-help]').withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(accessibility.violations).toEqual([])
  await explanation.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(explanation).toBeHidden()
})
