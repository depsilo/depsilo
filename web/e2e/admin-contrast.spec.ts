import AxeBuilder from '@axe-core/playwright'
import { adminApiDefaults, expectResolvedUiPreferences, mockAdminApi, setUiPreferences, test, expect } from './fixtures/admin-api'

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

// 2026-07-10T00:00:00Z. Trend buckets are UTC-aligned; the activity chart
// re-cuts them into spans aligned to the viewer's local clock.
const TREND_BASE = 1783641600

// The trend API's fixed series per range (internal/api/admin/dashboard.go).
const TREND_SERIES = {
  '1h': { step: 10, count: 360 },
  '24h': { step: 5 * 60, count: 288 },
  '7d': { step: 30 * 60, count: 336 },
  '30d': { step: 2 * 60 * 60, count: 360 },
} as const
type TrendRange = keyof typeof TREND_SERIES

interface TrendCounts { requests: number; hits?: number; misses?: number; errors?: number; upstreamRequests?: number }

function trendPoint(bucket: number, { requests, hits = requests, misses = requests - hits, errors = 0, upstreamRequests = misses }: TrendCounts) {
  return {
    bucket,
    date: new Date(bucket * 1000).toISOString().slice(0, 16).replace('T', ' '),
    requests,
    hits,
    misses,
    hit_rate: requests > 0 ? hits / requests : 0,
    bytes_served: requests * 128,
    bytes_hit: hits * 128,
    bytes_miss: misses * 128,
    upstream_requests: upstreamRequests,
    upstream_bytes: upstreamRequests * 120,
    sum_latency_ms: requests * 10,
    avg_latency_ms: requests > 0 ? 10 : 0,
    errors,
  }
}

function trendSeries(range: TrendRange, counts: (index: number) => TrendCounts = () => ({ requests: 1 })) {
  const { step, count } = TREND_SERIES[range]
  return Array.from({ length: count }, (_, index) => trendPoint(TREND_BASE + index * step, counts(index)))
}

function activityCard(page: import('@playwright/test').Page) {
  const card = page.locator('[data-dashboard-trends]')
  return {
    card,
    chart: card.locator('[data-trend-chart]'),
    columns: card.locator('.dash-chart-col'),
    tooltip: card.locator('[data-trend-tooltip]'),
    tooltipLines: card.locator('[data-trend-tooltip] p'),
    readout: card.locator('[data-trend-chart] [aria-live]'),
  }
}

async function columnRequests(columns: import('@playwright/test').Locator) {
  return columns.evaluateAll(nodes => nodes.map(node => Number(node.getAttribute('data-requests'))))
}

// The chart is one keyboard stop: arrow keys move the inspected bar without
// moving focus, and Tab leaves the chart. A range change must keep the same
// chart (and the previous range's bars, labelled as that range) while the new
// range loads, instead of remounting it as a skeleton.
test('activity chart is one keyboard stop and stays mounted while a new range loads', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': async (request: import('@playwright/test').Request) => {
      const range = new URL(request.url()).searchParams.get('range') as TrendRange
      if (range === '24h') await gate
      return { points: trendSeries(range) }
    },
  })
  await page.goto('/admin')

  const { card, chart, columns, tooltip } = activityCard(page)
  await expect(chart).toHaveAccessibleName('1 小时的请求活动，每格 1 分钟。按左右方向键查看各时间段。')
  await expect(columns).toHaveCount(60)
  await expect(card.locator('.dash-chart-col[tabindex], .dash-chart-col button')).toHaveCount(0)

  await chart.focus()
  await expect(columns.last()).toHaveAttribute('data-active', 'true')
  await page.keyboard.press('ArrowLeft')
  await expect(chart).toBeFocused()
  await expect(columns.nth(58)).toHaveAttribute('data-active', 'true')
  await page.keyboard.press('Home')
  await expect(columns.first()).toHaveAttribute('data-active', 'true')
  await page.keyboard.press('End')
  await expect(columns.last()).toHaveAttribute('data-active', 'true')
  await page.keyboard.press('Tab')
  await expect(chart).not.toBeFocused()
  await expect(tooltip).toHaveCount(0)

  const chartElement = await chart.elementHandle()
  await page.getByRole('group', { name: '统计周期' }).getByRole('button', { name: '24 小时' }).click()
  await expect(card).toHaveAttribute('data-trend-pending', 'true')
  await expect(card).toHaveAttribute('aria-busy', 'true')
  await expect(card.getByRole('status').filter({ hasText: '正在切换统计周期，当前仍显示上一周期的数据。' })).toBeVisible()
  // Still the 1h bars, still named as 1h: the previous data never claims the new range.
  await expect(chart).toHaveAccessibleName(/^1 小时的请求活动/)
  await expect(columns).toHaveCount(60)

  release()
  await expect(card).not.toHaveAttribute('data-trend-pending', 'true')
  await expect(chart).toHaveAccessibleName('24 小时的请求活动，每格 30 分钟。按左右方向键查看各时间段。')
  await expect(columns).toHaveCount(48)
  expect(await chartElement!.evaluate(element => element.isConnected)).toBe(true)
})

// The chart's accessible name carries the range its bars belong to and how
// long each bar is; the span follows the API's series, not a fixed guess.
test('activity chart exposes its range and bar span to assistive technology', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': (request: import('@playwright/test').Request) => (
      { points: trendSeries(new URL(request.url()).searchParams.get('range') as TrendRange) }
    ),
  })
  await page.goto('/admin')

  const { card, chart } = activityCard(page)
  await expect(chart).toHaveRole('application')
  await expect(chart).toHaveAccessibleName('Request activity over 1h, 1 min per bar. Use the left and right arrow keys to read each bar.')
  await expect(card.locator('.dash-chip')).toHaveText('1 min per bar')

  for (const [range, label, span] of [
    ['24h', '24h', '30 min'],
    ['7d', '7d', '3 h'],
    ['30d', '30d', '12 h'],
  ] as const) {
    await page.getByRole('group', { name: 'Statistics range' }).getByRole('button', { name: label, exact: true }).click()
    await expect(card).toHaveAttribute('data-trend-range', range)
    await expect(chart).toHaveAccessibleName(`Request activity over ${label}, ${span} per bar. Use the left and right arrow keys to read each bar.`)
    await expect(card.locator('.dash-chip')).toHaveText(`${span} per bar`)
  }
})

// Origin fetches are part of a client request, not extra requests. A column's
// height is the bucket's request count; hits and origin fetches split it
// rather than stack on top of it.
test('activity columns count each client request once and span the card', async ({ page }) => {
  const points = populatedTrendPoints.map(point => ({ ...point, upstream_requests: point.requests }))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')

  const { card, chart, columns } = activityCard(page)
  // Five-minute API buckets stay five-minute bars: four columns, one per bucket.
  await expect(columns).toHaveCount(4)
  await expect(card.locator('.dash-chip')).toHaveText('每格 5 分钟')
  expect(await columnRequests(columns)).toEqual([12, 13, 14, 15])

  const plotHeight = (await chart.boundingBox())!.height
  // The axis tops out at 15, so the tallest bar fills the plot and the others
  // are their exact share of it.
  for (const [index, requests] of [[0, 12], [3, 15]] as const) {
    const column = columns.nth(index)
    const stack = (await column.locator('.dash-chart-stack').boundingBox())!
    expect(stack.height / plotHeight).toBeCloseTo(requests / 15, 1)
    const hit = (await column.locator('.is-hit').boundingBox())!.height
    const miss = (await column.locator('.is-miss').boundingBox())!.height
    expect(hit / miss).toBeCloseTo(points[index].hits / points[index].misses, 0)
  }

  const chartBox = (await chart.boundingBox())!
  const cardBox = (await card.boundingBox())!
  // The plot takes the card width except its padding and the y-axis gutter.
  expect(cardBox.width - chartBox.width).toBeLessThan(72)
})

// Measurement notes sit behind the card title, not in the card body, and say
// what a bar aggregates and that the last span is still open.
test('activity measurement notes state the bar aggregation and the open final span', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: populatedTrendPoints },
  })
  await page.goto('/admin')

  const { card } = activityCard(page)
  await expect(card.locator('.dash-chip')).toHaveText('每格 5 分钟')
  await expect(card).not.toContainText('最后一根柱子所在的时间段尚未结束')
  await card.getByRole('button', { name: '活动 · 统计说明', exact: true }).click()
  const notes = page.locator('[data-dashboard-metric-help]')
  await expect(notes).toContainText('每根柱子是一个时间段内的请求数')
  await expect(notes).toContainText('绿色部分由缓存直接响应，蓝色部分回源后响应')
  await expect(notes).toContainText('最后一根柱子所在的时间段尚未结束。')
  await expect(notes).toContainText('左右方向键')
  await expect(notes).toHaveCSS('font-size', '14px')
  await notes.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(notes).toHaveCount(0)
})

// A bar that merges several API buckets shows their sums, not an average of
// them. Failures stay a count; the hit share is a percentage of the bar.
test('activity bars sum merged buckets and keep error counts distinct from percentages', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  const points = [
    trendPoint(TREND_BASE, { requests: 1, hits: 1 }),
    trendPoint(TREND_BASE + 10, { requests: 9, hits: 8, errors: 1 }),
  ]
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')

  const { chart, columns, tooltipLines, readout } = activityCard(page)
  await expect(columns).toHaveCount(1)
  expect(await columnRequests(columns)).toEqual([10])
  await expect(columns.first().locator('.dash-chart-error')).toHaveCount(1)

  await chart.focus()
  await expect(tooltipLines.nth(1)).toHaveText('10 次请求')
  await expect(tooltipLines.nth(2)).toHaveText('9缓存命中 · 90%')
  await expect(tooltipLines.nth(3)).toHaveText('1回源')
  await expect(tooltipLines.nth(4)).toHaveText('1失败')
  await expect(readout).toContainText('10 次请求, 缓存命中 9, 回源 1, 失败 1')

  // The period tile keeps the same split: a count as the value, the rate beside it.
  const errors = page.locator('[data-tile="errors"]')
  await expect(errors.locator('[data-tile-value]')).toHaveText('2')
  await expect(errors.locator('.dash-tile-sub')).toContainText('失败率 0.2%')
})

// An origin fetch with no client request is a measured zero for client
// requests, but there is nothing to take a hit share or error rate of.
test('origin-only activity keeps true zero totals and no-sample rates distinct', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  const dashboard = adminApiDefaults['GET /api/v1/admin/dashboard'] as { window: Record<string, number> }
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: [trendPoint(TREND_BASE, { requests: 0, upstreamRequests: 1 })] },
    'GET /api/v1/admin/dashboard': {
      ...dashboard,
      window: {
        ...Object.fromEntries(Object.keys(dashboard.window).map(key => [key, 0])),
        upstream_requests: 1,
        upstream_bytes: 120,
      },
    },
  })
  await page.goto('/admin')

  const { card, chart, columns, tooltipLines } = activityCard(page)
  await expect(columns).toHaveCount(1)
  expect(await columnRequests(columns)).toEqual([0])
  await expect(columns.first().locator('.dash-chart-stack')).toHaveCount(0)
  await expect(card.locator('.dash-chart-empty')).toHaveText('这段时间没有请求')
  await expect(card.locator('[data-activity-bursts]')).toHaveCount(0)
  await chart.focus()
  await expect(tooltipLines.nth(1)).toHaveText('0 次请求')
  await expect(tooltipLines.nth(2)).toHaveText('0缓存命中')

  const tile = (id: string) => page.locator(`[data-tile="${id}"]`)
  await expect(tile('errors').locator('[data-tile-value]')).toHaveText('0')
  // The count is measured (and may still differ from the previous period);
  // only the rate has no denominator.
  await expect(tile('errors').locator('.dash-tile-sub > span').first()).toHaveText('暂无请求样本')
  await expect(tile('errors')).not.toContainText('%')
  await expect(tile('hit-rate').locator('[data-tile-value]')).toHaveText('—')
  await expect(tile('hit-rate').locator('.dash-tile-sub > span').first()).toHaveText('暂无请求样本')
})

// Empty API buckets are measured zeros and keep their own bar slot, so later
// bars stay at their true time. A zero bar has no hit share to report.
test('empty trend buckets keep zero bars without inventing a hit share', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  const points = [0, 1, 0, 2, 0].map((requests, index) => trendPoint(TREND_BASE + index * 60, { requests }))
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points },
  })
  await page.goto('/admin')

  const { card, chart, columns, tooltipLines } = activityCard(page)
  await expect(columns).toHaveCount(5)
  expect(await columnRequests(columns)).toEqual([0, 1, 0, 2, 0])
  for (const index of [0, 2, 4]) await expect(columns.nth(index).locator('.dash-chart-stack')).toHaveCount(0)
  for (const index of [1, 3]) await expect(columns.nth(index).locator('.dash-chart-stack')).toHaveCount(1)

  await chart.focus()
  await page.keyboard.press('Home')
  await expect(tooltipLines.nth(1)).toHaveText('0 次请求')
  await expect(tooltipLines.nth(2)).toHaveText('0缓存命中')
  await page.keyboard.press('ArrowRight')
  await expect(tooltipLines.nth(1)).toHaveText('1 次请求')
  await expect(tooltipLines.nth(2)).toHaveText('1缓存命中 · 100%')
  await page.keyboard.press('ArrowRight')
  await expect(tooltipLines.nth(1)).toHaveText('0 次请求')
  await expect(tooltipLines.nth(2)).toHaveText('0缓存命中')

  // One quiet bucket does not split a burst: 1 + 2 requests read as one run.
  const bursts = card.locator('[data-activity-bursts] li')
  await expect(bursts).toHaveCount(1)
  await expect(bursts.first()).toContainText('3 次')
  await expect(bursts.first()).toContainText('命中 100%')
})

// Bars are cut on the viewer's local clock, not UTC, and their labels use the
// app language rather than the browser locale. Each zone below has an offset
// that a UTC-aligned cut would expose in the label.
const LOCAL_TIME_CASES: Array<{
  range: TrendRange
  timezoneId: string
  bars: number
  first: { label: string; requests: number }
  second: { label: string; requests: number }
  last: { label: string; requests: number }
}> = [
  {
    // 10-second buckets from 05:30:00 local; 05:30 and 05:31 minutes.
    range: '1h',
    timezoneId: 'Asia/Kolkata',
    bars: 60,
    first: { label: '7/10 05:30–05:31', requests: 6 },
    second: { label: '7/10 05:31–05:32', requests: 6 },
    last: { label: '7/10 06:29–06:30', requests: 6 },
  },
  {
    // 30-minute buckets from 05:30 local; a UTC cut would read 05:30–08:30.
    // The window starts inside the 03:00–06:00 bar, so that bar names where its data begins.
    range: '7d',
    timezoneId: 'Asia/Kolkata',
    bars: 57,
    first: { label: '7/10 05:30–06:00', requests: 1 },
    second: { label: '7/10 06:00–09:00', requests: 6 },
    last: { label: '7/17 03:00–06:00', requests: 5 },
  },
  {
    // 2-hour buckets start at odd local hours (+9 against a 2-hour UTC grid). A
    // bar must hold whole buckets, so the 12-hour grid sits at 01:00 and 13:00
    // local instead of splitting the 11:00–13:00 bucket at noon.
    range: '30d',
    timezoneId: 'Asia/Tokyo',
    bars: 61,
    first: { label: '7/10 09:00–13:00', requests: 2 },
    second: { label: '7/10 13:00–7/11 01:00', requests: 6 },
    last: { label: '8/9 01:00–13:00', requests: 4 },
  },
]

for (const { range, timezoneId, bars, first, second, last } of LOCAL_TIME_CASES) {
  test.describe(`${range} activity in ${timezoneId}`, () => {
    test.use({ locale: 'en-US', timezoneId })

    test(`${range} bars keep local bucket times and sums through pointer and keyboard inspection`, async ({ page }) => {
      await page.addInitScript(value => localStorage.setItem('depsilo-dashboard-range', value), range)
      await setUiPreferences(page, 'dark', 'zh')
      await mockAdminApi(page, {
        'GET /api/v1/admin/dashboard/trends': { points: trendSeries(range) },
      })
      await page.goto('/admin')

      const { chart, columns, tooltip, tooltipLines, readout } = activityCard(page)
      await expect(columns).toHaveCount(bars)
      const requests = await columnRequests(columns)
      expect(requests.reduce((sum, value) => sum + value, 0)).toBe(TREND_SERIES[range].count)
      expect([requests[0], requests[1], requests.at(-1)]).toEqual([first.requests, second.requests, last.requests])

      const expectBar = async (bar: { label: string; requests: number }) => {
        await expect(tooltipLines.first()).toHaveText(bar.label)
        await expect(tooltipLines.nth(1)).toHaveText(`${bar.requests} 次请求`)
      }
      await columns.first().hover()
      await expectBar(first)
      await columns.nth(1).hover()
      await expectBar(second)

      await page.mouse.move(0, 0)
      await expect(tooltip).toHaveCount(0)
      await chart.focus()
      await expectBar(last)
      await expect(readout).toHaveText(new RegExp(`^${last.label}, ${last.requests} 次请求`))
      await page.keyboard.press('Home')
      await expectBar(first)
      await page.keyboard.press('ArrowRight')
      await expectBar(second)
      await expect(readout).toHaveText(new RegExp(`^${second.label}, ${second.requests} 次请求`))
      await page.keyboard.press('ArrowLeft')
      await expectBar(first)
    })
  })
}

// The local offset belongs to each bar's own date. A 30d range viewed in
// November still holds July bars, which were cut at July's offset (EDT, -4h),
// not today's (EST, -5h); otherwise every summer bar is an hour off the clock.
test.describe('activity bars across a daylight-saving change', () => {
  test.use({ locale: 'en-US', timezoneId: 'America/New_York' })

  test('30d bars use each bucket\'s own local offset', async ({ page }) => {
    await page.clock.setFixedTime(new Date('2026-11-12T12:00:00Z'))
    await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '30d'))
    await setUiPreferences(page, 'dark', 'zh')
    // 2-hour buckets from 2026-07-09 20:00 EDT.
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard/trends': { points: trendSeries('30d') },
    })
    await page.goto('/admin')

    const { chart, columns, tooltipLines } = activityCard(page)
    await expect(columns).toHaveCount(61)
    await chart.focus()
    await page.keyboard.press('Home')
    // The window begins at 20:00 inside the 12:00–00:00 bar.
    await expect(tooltipLines.first()).toHaveText('7/9 20:00–00:00')
    await page.keyboard.press('ArrowRight')
    await expect(tooltipLines.first()).toHaveText('7/10 00:00–12:00')
    expect((await columnRequests(columns)).slice(0, 2)).toEqual([2, 6])
  })
})

// Even a full-width chart leaves little room on a phone. Its tooltip must stay
// inside the viewport and at the 14px tooltip size for pointer and keyboard
// inspection (docs/development/frontend-components.md).
test('activity tooltip stays readable and contained on a phone', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', '1h'))
  await page.setViewportSize({ width: 320, height: 900 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard/trends': { points: trendSeries('1h') },
  })
  await page.goto('/admin')

  const { chart, columns, tooltip } = activityCard(page)
  await expect(columns).toHaveCount(60)
  await chart.scrollIntoViewIfNeeded()
  const fontSizes: number[] = []
  const expectContainedTooltip = async () => {
    await expect(tooltip).toBeVisible()
    const box = (await tooltip.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(320)
    fontSizes.push(await tooltip.locator('p').nth(1).evaluate(element => Number.parseFloat(getComputedStyle(element).fontSize)))
  }
  await columns.first().hover()
  await expectContainedTooltip()
  await columns.nth(30).hover()
  await expectContainedTooltip()
  await page.mouse.move(0, 0)
  await chart.focus()
  await expect(columns.last()).toHaveAttribute('data-active', 'true')
  await expectContainedTooltip()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320)
  // Checked last so a size regression does not hide a containment one.
  for (const size of fontSizes) expect(size).toBeGreaterThanOrEqual(14)
})

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
