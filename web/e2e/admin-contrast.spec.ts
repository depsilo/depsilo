import AxeBuilder from '@axe-core/playwright'
import { expectResolvedUiPreferences, setUiPreferences, test, expect } from './fixtures/admin-api'

test('light theme admin chrome has no color-contrast violations', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'zh')
  const results = await new AxeBuilder({ page }).withTags(['wcag2aa']).analyze()
  expect(results.violations.filter(v => v.id === 'color-contrast')).toEqual([])
})

test('trend range exposes its selected state', async ({ page }) => {
  await page.goto('/admin')
  const group = page.getByRole('group', { name: '活动趋势' })
  const selectedRange = group.getByRole('button', { name: '24 小时' })
  await selectedRange.click()
  await expect(selectedRange).toHaveAttribute('aria-pressed', 'true')
  await expect(group.getByRole('button', { name: '1 小时' })).toHaveAttribute('aria-pressed', 'false')
})

test('primary command uses the semantic pressed color without a filter', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/upstreams')
  const button = page.getByRole('button', { name: /添加上游源/i })
  const tokenColors = await page.evaluate(() => {
    const probe = document.createElement('span')
    document.body.appendChild(probe)
    probe.style.backgroundColor = 'var(--btn)'
    const resting = getComputedStyle(probe).backgroundColor
    probe.style.backgroundColor = 'var(--btn-press)'
    const pressed = getComputedStyle(probe).backgroundColor
    probe.remove()
    return { resting, pressed }
  })
  const restingBackground = await button.evaluate(el => getComputedStyle(el).backgroundColor)
  expect(restingBackground).toBe(tokenColors.resting)
  await button.hover()
  await expect.poll(() => button.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(tokenColors.pressed)
  const hoveredBackground = await button.evaluate(el => getComputedStyle(el).backgroundColor)
  expect(hoveredBackground).not.toBe(restingBackground)
  expect(hoveredBackground).toBe(tokenColors.pressed)
  expect(await button.evaluate(el => getComputedStyle(el).filter)).toBe('none')
})
