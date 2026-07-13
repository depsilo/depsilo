import AxeBuilder from '@axe-core/playwright'
import { expectResolvedUiPreferences, mockAdminApi, setUiPreferences, test, expect } from './fixtures/admin-api'

const populatedTrendPoints = [0, 1, 2, 3].map(index => {
  const requests = 12 + index
  const hits = 8 + index
  const misses = requests - hits
  return {
    bucket: 1783641600 + index * 300,
    date: '2026-07-10',
    requests,
    hits,
    misses,
    hit_rate: hits / requests,
    bytes_served: requests * 128,
    bytes_hit: hits * 128,
    bytes_miss: misses * 128,
    sum_latency_ms: requests * (10 + index),
    avg_latency_ms: 10 + index,
    errors: index % 2,
  }
})

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

test('trend metric selector exposes button and pressed semantics', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin')
  const group = page.getByRole('group', { name: 'Trend metric' })
  const metrics = ['Requests', 'Bandwidth', 'Latency', 'Errors'].map(name => (
    group.getByRole('button', { name, exact: true })
  ))
  await expect(group).toBeVisible()
  for (const metric of metrics) await expect(metric).toHaveAttribute('type', 'button')

  await expect(metrics[0]).toHaveAttribute('aria-pressed', 'true')
  await expect(metrics[2]).toHaveAttribute('aria-pressed', 'false')
  await metrics[2].click()
  await expect(metrics[0]).toHaveAttribute('aria-pressed', 'false')
  await expect(metrics[2]).toHaveAttribute('aria-pressed', 'true')
})

test('trend series use linear paths', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints },
  })
  await page.goto('/admin')

  const paths = page.locator('[data-query-key="dashboard-trends"] .recharts-area-curve, [data-query-key="dashboard-trends"] .recharts-line-curve')
  await expect(paths).toHaveCount(3)
  for (let i = 0; i < await paths.count(); i += 1) {
    await expect(paths.nth(i)).not.toHaveAttribute('d', /C/)
  }
})

test('trend tooltip formats numeric buckets in local time', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints },
  })
  await page.goto('/admin')

  const chart = page.locator('[data-query-key="dashboard-trends"] .recharts-wrapper')
  await expect(chart).toBeVisible()
  await chart.hover({ position: { x: 40, y: 80 } })
  const expectedLabel = await page.evaluate(bucket => {
    const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
    return new Date(bucket * 1000).toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      timeZone,
    })
  }, populatedTrendPoints[0].bucket)
  await expect(page.locator('[data-query-key="dashboard-trends"] .recharts-tooltip-wrapper p').first()).toHaveText(expectedLabel)
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
