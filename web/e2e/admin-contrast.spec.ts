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
    upstream_requests: misses,
    upstream_bytes: misses * 120,
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
  const group = page.getByRole('group', { name: '统计周期' })
  const selectedRange = group.getByRole('button', { name: '24 小时' })
  await selectedRange.click()
  await expect(selectedRange).toHaveAttribute('aria-pressed', 'true')
  await expect(group.getByRole('button', { name: '1 小时' })).toHaveAttribute('aria-pressed', 'false')
})

test('trend metric tabs support roving keyboard focus and keep the chart mounted', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, { 'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints } })
  await page.goto('/admin')
  const tablist = page.getByRole('tablist', { name: 'Activity trend' })
  // "Traffic" is a bucket byte total, not a bandwidth rate.
  const metrics = ['Requests', 'Traffic', 'Latency', 'Errors'].map(name => (
    tablist.getByRole('tab', { name, exact: true })
  ))
  await expect(tablist).toBeVisible()
  await expect(metrics[0]).toHaveAttribute('aria-selected', 'true')
  await expect(metrics[2]).toHaveAttribute('aria-selected', 'false')
  const chart = page.locator('[data-trend-chart] .recharts-wrapper')
  await expect(chart).toBeVisible()
  const chartElement = await chart.elementHandle()
  await metrics[0].focus()
  await page.keyboard.press('ArrowRight')
  await expect(metrics[1]).toBeFocused()
  await expect(metrics[1]).toHaveAttribute('aria-selected', 'true')
  await expect(metrics[0]).toHaveAttribute('tabindex', '-1')
  const panel = page.getByRole('tabpanel', { name: 'Traffic', exact: true })
  await expect(panel).toBeVisible()
  expect(await metrics[1].getAttribute('aria-controls')).toBe(await panel.getAttribute('id'))
  await metrics[2].click()
  await expect(metrics[0]).toHaveAttribute('aria-selected', 'false')
  await expect(metrics[2]).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('tabpanel', { name: 'Latency', exact: true })).toBeVisible()
  expect(await chartElement!.evaluate(element => element.isConnected)).toBe(true)
  await page.keyboard.press('ArrowLeft')
  await expect(metrics[1]).toBeFocused()
  await expect(metrics[1]).toHaveAttribute('aria-selected', 'true')
})

test('trend chart exposes the selected metric and range to assistive technology', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints },
  })
  await page.goto('/admin')

  const chartDescription = page.locator('[data-query-key="dashboard-trends"] .recharts-wrapper > svg.recharts-surface > desc').first()
  await expect(chartDescription).toHaveText('Requests trend for 1h. Use the left and right arrow keys to inspect time points.')

  await page.getByRole('tablist', { name: 'Activity trend' }).getByRole('tab', { name: 'Latency' }).click()
  await page.getByRole('group', { name: 'Statistics range' }).getByRole('button', { name: '24h' }).click()
  await expect(chartDescription).toHaveText('Latency trend for 24h. Use the left and right arrow keys to inspect time points.')
})

test('trend series share a full-width smooth area chart without stacking quantities', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  // Identical quantities must overlap. Stacking these series would incorrectly
  // add origin requests to the served total and produce different paths.
  const points = populatedTrendPoints.map(point => ({ ...point, upstream_requests: point.requests }))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')

  const card = page.locator('[data-query-key="dashboard-trends"]')
  const chart = card.locator('[data-trend-chart] .recharts-wrapper')
  const paths = card.locator('.recharts-area-curve')
  await expect(chart).toHaveCount(1)
  await expect(paths).toHaveCount(2)
  await expect(card.locator('[data-trend-series]')).toHaveCount(2)
  await expect(chart.locator('.recharts-xAxis')).toHaveCount(1)
  await expect(chart.locator('.recharts-yAxis, .recharts-legend-wrapper, .recharts-cartesian-grid-vertical line')).toHaveCount(0)
  expect(await chart.locator('.recharts-cartesian-grid-horizontal line').count()).toBeGreaterThan(0)
  const chartBox = (await chart.boundingBox())!
  const cardBox = (await card.boundingBox())!
  expect(chartBox.height).toBe(250)
  expect(cardBox.width - chartBox.width).toBeLessThan(48)
  for (let i = 0; i < await paths.count(); i += 1) {
    await expect(paths.nth(i)).toHaveAttribute('d', /C/)
  }
  expect(await paths.nth(0).getAttribute('d')).toBe(await paths.nth(1).getAttribute('d'))
  const fills = chart.locator('.recharts-area-area')
  await expect(fills).toHaveCount(2)
  for (const fill of await fills.all()) await expect(fill).toHaveAttribute('fill', /^url\(/)
})

async function exactTrendLabel(page: import('@playwright/test').Page, bucket: number, range: '1h' | '7d' | '30d') {
  return page.evaluate(({ value, showSeconds }) => {
    const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone
    return new Date(value * 1000).toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: showSeconds ? '2-digit' : undefined,
      timeZoneName: 'short',
      timeZone,
    })
  }, { value: bucket, showSeconds: range === '1h' })
}

async function trendEndpointPosition(
  chart: import('@playwright/test').Locator,
  endpoint: 'first' | 'last',
) {
  const box = await chart.boundingBox()
  expect(box).not.toBeNull()
  const gridLine = chart.locator('.recharts-cartesian-grid-horizontal line').first()
  const edge = Number(await gridLine.getAttribute(endpoint === 'first' ? 'x1' : 'x2'))
  return { x: edge + (endpoint === 'first' ? 2 : -2), y: box!.height / 2 }
}

async function hoverTrendEndpoint(
  chart: import('@playwright/test').Locator,
  endpoint: 'first' | 'last',
) {
  await chart.hover({ position: await trendEndpointPosition(chart, endpoint) })
  return chart.locator('.recharts-tooltip-wrapper p').first()
}

test.describe('trend dates follow the selected app language', () => {
  test.use({ locale: 'en-US' })

  test('1h tooltips distinguish adjacent ten-second buckets in Chinese local time', async ({ page }) => {
    const points = populatedTrendPoints.slice(0, 2).map((point, index) => ({
      ...point,
      bucket: populatedTrendPoints[0].bucket + index * 10,
    }))
    await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
    await setUiPreferences(page, 'dark', 'zh')
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard/trends': { points },
    })
    await page.goto('/admin')

    const chart = page.locator('[data-trend-chart] .recharts-wrapper')
    await expect(chart).toBeVisible()
    const firstExpected = await exactTrendLabel(page, points[0].bucket, '1h')
    const lastExpected = await exactTrendLabel(page, points[1].bucket, '1h')

    expect(firstExpected).not.toBe(lastExpected)
    await expect(await hoverTrendEndpoint(chart, 'first')).toHaveText(firstExpected)
    await expect(await hoverTrendEndpoint(chart, 'last')).toHaveText(lastExpected)

    // Even a full-width chart leaves little room on a phone. Its tooltip
    // must remain readable and contained for pointer and keyboard inspection.
    await page.setViewportSize({ width: 320, height: 900 })
    await chart.scrollIntoViewIfNeeded()
    const tooltip = chart.locator('.dashboard-trend-tooltip')
    const expectReadableTooltip = async () => {
      await expect(tooltip).toBeVisible()
      const box = await tooltip.boundingBox()
      expect(box).not.toBeNull()
      expect(box!.x).toBeGreaterThanOrEqual(0)
      expect(box!.y).toBeGreaterThanOrEqual(0)
      expect(box!.x + box!.width).toBeLessThanOrEqual(320)
      expect(box!.y + box!.height).toBeLessThanOrEqual(900)
      expect(await tooltip.evaluate(element => Number.parseFloat(getComputedStyle(element).fontSize))).toBeGreaterThanOrEqual(14)
    }
    await expect(await hoverTrendEndpoint(chart, 'first')).toHaveText(firstExpected)
    await expectReadableTooltip()
    await chart.click({ position: await trendEndpointPosition(chart, 'last') })
    await expect(tooltip).toContainText(lastExpected)
    await expectReadableTooltip()

    await page.mouse.move(0, 0)
    const application = chart.getByRole('application')
    await application.focus()
    await page.keyboard.press('ArrowRight')
    await expect(application).toBeFocused()
    await expect(tooltip).toContainText(lastExpected)
    await expectReadableTooltip()
  })
})

// Measurement notes must describe the aggregation the selected tab actually uses:
// byte/request totals are sums, latency is a sample-weighted mean, and errors
// carry both a count and a rate.
test('trend measurement notes and latency tooltip state their aggregation', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints },
  })
  await page.goto('/admin')

  const card = page.locator('[data-query-key="dashboard-trends"]')
  for (const [tab, expected] of [
    ['请求量', '每桶累计请求数'],
    ['流量', '每桶累计字节'],
    ['延迟', '均值（按请求数加权）'],
    ['错误', '错误数与错误率'],
  ] as const) {
    await card.getByRole('tab', { name: tab, exact: true }).click()
    await expect(card).not.toContainText('最后一个时间桶尚未结束')
    await card.getByRole('button', { name: '活动趋势 · 统计说明', exact: true }).click()
    const notes = page.locator('[data-dashboard-metric-help]')
    await expect(notes).toContainText(expected)
    await expect(notes).toContainText('最后一个时间桶尚未结束')
    await notes.getByRole('button', { name: '关闭', exact: true }).click()
  }

  // Latency tooltips carry the bucket's sample count next to the mean.
  await card.getByRole('tab', { name: '延迟', exact: true }).click()
  const chart = card.locator('.recharts-wrapper')
  await hoverTrendEndpoint(chart, 'last')
  await expect(card.locator('.recharts-tooltip-wrapper')).toContainText(/n=\d/)
  await expect(card.locator('.recharts-tooltip-wrapper > div')).toHaveCSS('font-size', '14px')
})

test('trend summaries use weighted totals and keep error counts distinct from percentages', async ({ page }) => {
  const points = [1, 9].map((requests, index) => ({
    ...populatedTrendPoints[0],
    bucket: populatedTrendPoints[0].bucket + index * 10,
    requests,
    hits: requests - index,
    misses: index,
    bytes_served: requests * 128,
    upstream_requests: index,
    upstream_bytes: index * 120,
    sum_latency_ms: requests * index * 100,
    avg_latency_ms: index * 100,
    errors: index,
  }))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')
  const card = page.locator('[data-query-key="dashboard-trends"]')
  const value = (key: string) => card.locator(`[data-trend-value="${key}"]`)

  await expect(value('serviceRequests')).toHaveText('10')
  await expect(value('originRequests')).toHaveText('1')
  await card.getByRole('tab', { name: '流量', exact: true }).click()
  await expect(value('serviceBytes')).toHaveText('1.3 KB')
  await expect(value('originBytes')).toHaveText('120 B')
  await card.getByRole('tab', { name: '延迟', exact: true }).click()
  // 900 / 10 = 90, not the unweighted average of 0 and 100.
  await expect(value('latency')).toHaveText('90 毫秒')
  await card.getByRole('tab', { name: '错误', exact: true }).click()
  await expect(value('errors')).toHaveText('1')
  await expect(value('errorRatePct')).toHaveText('10.0%')
  const chart = card.locator('[data-trend-chart] .recharts-wrapper')
  await expect(chart.locator('.recharts-area-curve')).toHaveCount(1)
  await expect(chart.locator('.recharts-line-curve')).toHaveAttribute('stroke-dasharray', '4 3')
  await expect(chart.locator('.recharts-yAxis')).toHaveCount(2)
  await expect(chart.locator('.recharts-yAxis-tick-labels')).toContainText(['1', '100%'])
  await hoverTrendEndpoint(chart, 'last')
  await expect(chart.locator('.dashboard-trend-tooltip')).toContainText('错误: 1')
  await expect(chart.locator('.dashboard-trend-tooltip')).toContainText('错误率: 11.1%')
  // The rate retains a 0–100% scale; it must not share the count's auto scale.
  const rateY = Number(await chart.locator('.dashboard-trend-dot[data-series="errorRatePct"]').getAttribute('cy'))
  const gridY = await chart.locator('.recharts-cartesian-grid-horizontal line').evaluateAll(lines => (
    lines.map(line => Number(line.getAttribute('y1')))
  ))
  const top = Math.min(...gridY)
  const bottom = Math.max(...gridY)
  expect((bottom - rateY) / (bottom - top)).toBeCloseTo(0.111, 3)
})

test('origin-only activity keeps true zero totals and no-sample averages distinct', async ({ page }) => {
  const point = {
    ...populatedTrendPoints[0],
    requests: 0,
    hits: 0,
    misses: 0,
    bytes_served: 0,
    upstream_requests: 1,
    upstream_bytes: 120,
    sum_latency_ms: 0,
    avg_latency_ms: 0,
    errors: 0,
  }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: [point] },
  })
  await page.goto('/admin')
  const card = page.locator('[data-query-key="dashboard-trends"]')
  const value = (key: string) => card.locator(`[data-trend-value="${key}"]`)

  await expect(value('serviceRequests')).toHaveText('0')
  await expect(value('originRequests')).toHaveText('1')
  await card.getByRole('tab', { name: '流量', exact: true }).click()
  await expect(value('serviceBytes')).toHaveText('0 B')
  await card.getByRole('tab', { name: '延迟', exact: true }).click()
  await expect(value('latency')).toHaveText('—')
  await expect(card.locator('[data-trend-series="latency"]')).toContainText('暂无请求样本')
  await card.getByRole('tab', { name: '错误', exact: true }).click()
  await expect(value('errors')).toHaveText('0')
  await expect(value('errorRatePct')).toHaveText('—')
  await expect(card.locator('[data-trend-series="errorRatePct"]')).toContainText('暂无请求样本')
})

test('empty trend buckets keep zero counts but do not invent latency or error-rate samples', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  const points = [0, 1, 0, 2, 0].map((requests, index) => ({
    ...populatedTrendPoints[0],
    bucket: populatedTrendPoints[0].bucket + index * 10,
    requests,
    hits: requests,
    misses: 0,
    hit_rate: requests > 0 ? 1 : 0,
    bytes_served: requests * 128,
    bytes_hit: requests * 128,
    bytes_miss: 0,
    upstream_requests: 0,
    upstream_bytes: 0,
    sum_latency_ms: index === 3 ? requests * 11 : 0,
    avg_latency_ms: index === 3 ? 11 : 0,
    errors: 0,
  }))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')
  const card = page.locator('[data-query-key="dashboard-trends"]')
  const chart = card.locator('[data-trend-chart] .recharts-wrapper')
  await card.getByRole('tab', { name: '延迟', exact: true }).click()
  // Neither a missing middle bucket nor the empty final bucket is a zero-ms
  // measurement. Isolated real samples still need a visible mark.
  await expect(chart.locator('.dashboard-trend-dot')).toHaveCount(2)
  const latencyPath = await chart.locator('.recharts-area-curve').getAttribute('d')
  expect(latencyPath?.match(/M/g)).toHaveLength(2)
  await hoverTrendEndpoint(chart, 'first')
  await expect(card.locator('.recharts-tooltip-wrapper')).toContainText('暂无请求样本')
  await expect(card.locator('.recharts-tooltip-wrapper')).not.toContainText('0 毫秒')
  await page.mouse.move(0, 0)
  await chart.getByRole('application').focus()
  await expect(chart.getByRole('application')).toBeFocused()
  await page.keyboard.press('ArrowRight')
  await expect(card.locator('.recharts-tooltip-wrapper')).toContainText('客户端响应耗时: 0 毫秒')
  await page.keyboard.press('ArrowRight')
  await expect(card.locator('.recharts-tooltip-wrapper')).toContainText('暂无请求样本')
  await page.keyboard.press('ArrowRight')
  await expect(card.locator('.recharts-tooltip-wrapper')).toContainText('客户端响应耗时: 11 毫秒')
  await card.getByRole('tab', { name: '错误', exact: true }).click()
  await hoverTrendEndpoint(chart, 'first')
  await expect(chart.locator('.recharts-tooltip-wrapper')).toContainText('暂无请求样本')
  await expect(chart.locator('.recharts-tooltip-wrapper')).toContainText('错误: 0')
})

for (const [range, interval] of [['7d', 30 * 60], ['30d', 2 * 60 * 60]] as const) {
  test(`${range} trends preserve bucket times and values through pointer and keyboard inspection`, async ({ page }) => {
    const points = populatedTrendPoints.slice(0, 2).map((point, index) => ({
      ...point,
      bucket: populatedTrendPoints[0].bucket + index * interval,
      requests: index + 1,
      hits: 0,
      misses: index + 1,
      hit_rate: 0,
      upstream_requests: index + 1,
    }))
    await page.addInitScript(value => localStorage.setItem('depsilo-dashboard-range', value), range)
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard/trends': { points },
    })
    await page.goto('/admin')

    const card = page.locator('[data-query-key="dashboard-trends"]')
    const chart = card.locator('[data-trend-chart] .recharts-wrapper')
    await expect(chart).toBeVisible()
    const firstExpected = await exactTrendLabel(page, points[0].bucket, range)
    const lastExpected = await exactTrendLabel(page, points[1].bucket, range)

    await expect(chart.locator('.recharts-area-curve')).toHaveCount(2)
    await expect(chart.locator('.recharts-bar')).toHaveCount(0)

    expect(firstExpected).not.toBe(lastExpected)
    await expect(await hoverTrendEndpoint(chart, 'first')).toHaveText(firstExpected)
    await expect(await hoverTrendEndpoint(chart, 'last')).toHaveText(lastExpected)
    await expect(chart.locator('.recharts-tooltip-wrapper')).toContainText('服务请求: 2')

    await page.mouse.move(0, 0)
    await chart.getByRole('application').focus()
    await expect(chart.locator('.recharts-tooltip-wrapper p').first()).toHaveText(firstExpected)
    await page.keyboard.press('ArrowRight')
    await expect(chart.locator('.recharts-tooltip-wrapper p').first()).toHaveText(lastExpected)
    await expect(chart.locator('.recharts-tooltip-wrapper')).toContainText('服务请求: 2')
    await page.keyboard.press('ArrowLeft')
    await expect(chart.locator('.recharts-tooltip-wrapper p').first()).toHaveText(firstExpected)
    await expect(chart.locator('.recharts-tooltip-wrapper')).toContainText('服务请求: 1')
  })
}

test('primary command uses the semantic primary color without a filter', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin/upstreams')
  const button = page.getByRole('button', { name: /添加上游源/i })
  const tokenColor = await page.evaluate(() => {
    const probe = document.createElement('span')
    document.body.appendChild(probe)
    probe.style.backgroundColor = 'var(--btn)'
    const value = getComputedStyle(probe).backgroundColor
    probe.remove()
    return value
  })
  const restingBackground = await button.evaluate(el => getComputedStyle(el).backgroundColor)
  expect(restingBackground).toBe(tokenColor)
  await button.hover()
  const hoveredBackground = await button.evaluate(el => getComputedStyle(el).backgroundColor)
  expect(hoveredBackground).not.toBe(restingBackground)
  expect(await button.evaluate(el => getComputedStyle(el).filter)).toBe('none')
})
