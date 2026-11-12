import type { Locator } from '@playwright/test'

import type { RuntimeResponse } from '../src/lib/adminApi.types'
import { adminApiDefaults, expect, mockAdminApi, test } from './fixtures/admin-api'

function populatedDashboard(overrides: Record<string, unknown> = {}) {
  return {
    range: { key: '30d', start: '2026-09-03T00:00:00Z', end: '2026-10-03T00:00:00Z' },
    window: {
      total_requests: 1200,
      hit_count: 1104,
      miss_count: 96,
      hit_requests: 1104,
      miss_requests: 96,
      hit_rate: 0.92,
      bytes_served: 734003200,
      hit_bytes: 671088640,
      miss_bytes: 62914560,
      avg_latency_ms: 42,
      avg_hit_latency_ms: 18,
      avg_miss_latency_ms: 220,
      upstream_requests: 104,
      upstream_bytes: 66060288,
      errors: 2,
    },
    prev: {
      total_requests: 1000,
      hit_count: 900,
      miss_count: 100,
      hit_requests: 900,
      miss_requests: 100,
      hit_rate: 0.9,
      bytes_served: 629145600,
      hit_bytes: 566231040,
      miss_bytes: 62914560,
      avg_latency_ms: 48,
      avg_hit_latency_ms: 20,
      avg_miss_latency_ms: 240,
      upstream_requests: 110,
      upstream_bytes: 69206016,
      errors: 3,
    },
    origin_coverage: { measured: true, since: '2026-09-01T00:00:00Z', window_complete: true },
    daily_stats: [],
    last_24h: { total_requests: 40, hit_count: 36, hit_rate: 0.9, bytes_served: 2048, avg_latency_ms: 42 },
    prev_24h: { total_requests: 32, hit_rate: 0.85, bytes_served: 1024, avg_latency_ms: 50 },
    top_packages: {},
    cache_usage_percent: 12,
    upstreams: [],
    ...overrides,
  }
}

// Each tile's figure must sit inside its own card: a clipped or overflowing
// value is a wrong number, not a cosmetic issue.
function tileGeometry(grid: Locator) {
  return grid.evaluate(element => [...element.children].map(tile => {
    // The latency tile shows a pair of figures instead of one headline value.
    const values = [...tile.querySelectorAll('[data-tile-value], .dash-tile-pair dd')]
    const style = getComputedStyle(tile)
    const rect = tile.getBoundingClientRect()
    const contentRight = rect.right - parseFloat(style.paddingRight) - parseFloat(style.borderRightWidth)
    return {
      tile: tile.getAttribute('data-tile'),
      text: values.map(value => value.textContent).join(' | '),
      font: Math.min(...values.map(value => parseFloat(getComputedStyle(value).fontSize))),
      fits: values.length > 0 && values.every(value => {
        const valueRect = value.getBoundingClientRect()
        return valueRect.left >= rect.left && valueRect.right <= contentRight + 0.5
      }),
    }
  }))
}

function columnCount(grid: Locator) {
  return grid.evaluate(element => getComputedStyle(element).gridTemplateColumns.split(/\s+/).length)
}

test('Overview resource metrics stay readable and never overflow on mobile', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  // The period figures are the page's headline, so a phone shows them at once
  // instead of behind a disclosure; they pair up rather than squeezing eight
  // values into one row.
  const grid = page.locator('[data-dashboard-kpis]')
  await expect(grid).toBeVisible()
  await expect(grid.locator(':scope > [data-tile]')).toHaveCount(8)
  await expect(grid.locator('[data-tile="served"] [data-tile-value]')).toHaveText('700.0MB')
  expect(await columnCount(grid)).toBe(2)
  const narrow = await tileGeometry(grid)
  expect(narrow.every(tile => tile.fits && tile.font >= 14), JSON.stringify(narrow)).toBe(true)
  await expect(grid).not.toContainText(/NaN|undefined/)

  // Process resources stay on a phone too, below the traffic they serve.
  const runtime = page.locator('[data-dashboard-resources]')
  await expect(runtime.locator('[data-testid="resource-cpu"]')).toContainText('3.2%')
  await expect(runtime.locator('[data-testid="resource-memory"]')).toContainText('180.0 MB')
  await expect(runtime).not.toContainText(/NaN|undefined/)
  await expect(page.locator('[data-query-key="now"]')).not.toContainText(/NaN|undefined/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.setViewportSize({ width: 800, height: 900 })
  await expect.poll(() => columnCount(grid)).toBe(4)
  expect((await tileGeometry(grid)).every(tile => tile.fits)).toBe(true)
  await expect(page.locator('[data-dashboard-live]')).toBeVisible()
  await expect(page.locator('[data-dashboard-live]')).not.toContainText(/NaN|undefined/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(800)

  await page.setViewportSize({ width: 1440, height: 900 })
  await expect.poll(() => columnCount(grid)).toBe(8)
  const wide = await tileGeometry(grid)
  expect(wide.every(tile => tile.fits), JSON.stringify(wide)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

// Cache inventory remains compact even when the capacity has a long value.
function tileMetrics(locator: Locator) {
  return locator.evaluate(card => {
    const value = card.querySelector('[data-tile-value]')
    const sub = card.querySelector('.dash-tile-sub > span')
    const style = getComputedStyle(card)
    const contentRight = card.getBoundingClientRect().right -
      parseFloat(style.paddingRight) - parseFloat(style.borderRightWidth)
    return {
      valueFont: value ? parseFloat(getComputedStyle(value).fontSize) : 0,
      // Space left between the last glyph and the card's content edge.
      slack: value ? Math.round(contentRight - value.getBoundingClientRect().right) : 0,
      // The used / quota line truncates with an ellipsis when it runs out of
      // room; a clipped capacity figure would misreport the inventory.
      capacityFits: sub ? sub.scrollWidth <= sub.clientWidth : false,
      capacityRight: sub ? Math.round(contentRight - sub.getBoundingClientRect().right) : -1,
    }
  })
}

test('Overview cache summary keeps a long capacity value readable at every width', async ({ page }) => {
  const runtime = adminApiDefaults['GET /api/v1/admin/runtime'] as RuntimeResponse
  await mockAdminApi(page, {
    'GET /api/v1/admin/runtime': {
      ...runtime,
      cache: { ...runtime.cache, logical_bytes: 1_099_400_000_000, quota_bytes: 2_199_023_255_552 },
    },
  })

  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')
  const cell = page.locator('[data-testid="resource-cache"]')
  await expect(cell.locator('[data-tile-value]')).toHaveText('50%')
  await expect(cell).toContainText('1023.9 GB / 2.0 TB')
  const compact = await tileMetrics(cell)
  expect(compact.valueFont).toBeGreaterThanOrEqual(16)
  expect(compact.slack).toBeGreaterThan(0)
  expect(compact.capacityFits, JSON.stringify(compact)).toBe(true)
  expect(compact.capacityRight).toBeGreaterThanOrEqual(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.setViewportSize({ width: 1920, height: 900 })
  await expect.poll(() => columnCount(page.locator('[data-dashboard-kpis]'))).toBe(8)
  const wide = await tileMetrics(cell)
  expect(wide.valueFont).toBeLessThanOrEqual(24)
  expect(wide.slack).toBeGreaterThan(0)
  expect(wide.capacityFits, JSON.stringify(wide)).toBe(true)
  expect(wide.capacityRight).toBeGreaterThanOrEqual(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1920)
})

test('Overview title is rendered once with a single range control', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  await expect(page.getByRole('heading', { level: 1, name: '总览', exact: true })).toHaveCount(1)
  const rangeGroup = page.getByRole('group', { name: '统计周期' })
  await expect(rangeGroup.getByRole('button')).toHaveCount(4)
  await expect(rangeGroup.getByRole('button', { name: '30 天' })).toHaveAttribute('aria-pressed', 'true')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

test('Overview mobile priorities and touch targets remain usable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': {
      status: 'degraded', using_stale_snapshot: true,
      snapshot_loaded_at: '2026-10-06T01:00:00Z', snapshot_age_seconds: 720,
      refresh_failures: 2, on_load_error: 'use_stale_then_allow',
    },
  })
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')

  // The status entry comes before any statistic, so a phone shows whether the
  // service needs attention before it shows how much traffic it served.
  const issues = page.locator('[data-dashboard-status]')
  const tiles = page.locator('[data-dashboard-benefits]')
  await expect(issues).toBeVisible()
  await expect(issues).toContainText('服务状态 · 需关注')
  await expect(issues).toHaveAttribute('aria-haspopup', 'dialog')
  await expect(tiles).toBeVisible()
  expect((await issues.boundingBox())!.y).toBeLessThan((await tiles.boundingBox())!.y)
  expect((await issues.boundingBox())!.height).toBeGreaterThanOrEqual(40)

  // In the single column the cards read in order of urgency: what needs an
  // operator, then traffic, then upstreams, then the process itself.
  const attention = page.locator('[data-dashboard-attention]')
  await expect(attention.locator('[data-attention-item]')).toHaveCount(1)
  await expect(attention.locator('[data-attention-item="problem"]')).toContainText('包规则正在使用旧快照')
  await expect(attention.locator('[data-check="policy"]')).toHaveAttribute('data-check-tone', 'warning')
  const recent = page.locator('[data-dashboard-recent-requests]')
  const order = [
    attention,
    page.locator('[data-dashboard-trends]'),
    recent,
    page.locator('[data-dashboard-upstreams]'),
    page.locator('[data-dashboard-resources-panel]'),
  ]
  const tops: number[] = []
  for (const card of order) tops.push((await card.boundingBox())!.y)
  expect(tops, JSON.stringify(tops)).toEqual([...tops].sort((a, b) => a - b))
  expect(tops[0]).toBeGreaterThan((await tiles.boundingBox())!.y)

  await issues.click()
  await expect(page.getByRole('dialog')).toContainText('服务状态详情')
  await expect(page.locator('[data-dashboard-info-dialog]')).toContainText('包规则')
  await page.keyboard.press('Escape')
  await expect(issues).toBeFocused()

  // Every control on the page, header included, is a 40px touch target on a
  // phone; the dense desktop sizing must not leak into coarse layouts.
  const targets = await page.locator('.dashboard-surface').locator('button, a[href], [role="button"]').evaluateAll(controls =>
    controls.filter(control => control.getClientRects().length > 0 && !control.closest('.sr-only')).map(control => ({
      label: control.getAttribute('aria-label') ?? control.textContent?.trim(),
      width: control.getBoundingClientRect().width,
      height: control.getBoundingClientRect().height,
    })),
  )
  expect(targets.length).toBeGreaterThan(10)
  expect(targets.every(target => target.width >= 40 && target.height >= 40), JSON.stringify(targets)).toBe(true)
  const ranges = await page.getByRole('group', { name: '统计周期' }).getByRole('button').evaluateAll(buttons =>
    buttons.map(button => button.getBoundingClientRect().height),
  )
  expect(ranges.every(height => height >= 40)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('Overview keeps the transfer tiles readable at 1024 and the empty analytics compact', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1024, height: 900 })
  await page.goto('/admin')

  // The byte tiles read in download order: what the cache answered, what came
  // back from an upstream, then what clients received. Each figure stays on
  // one line instead of wrapping its unit under the number.
  const grid = page.locator('[data-dashboard-kpis]')
  await expect.poll(() => columnCount(grid)).toBe(4)
  await expect(grid.locator(':scope > [data-tile]').evaluateAll(tiles => tiles.slice(0, 3).map(tile => tile.getAttribute('data-tile'))))
    .resolves.toEqual(['cache', 'origin', 'served'])
  const expected: [string, string][] = [
    ['[data-tile="cache"]', '640.0MB'],
    ['[data-testid="traffic-origin-total"]', '63.0MB'],
    ['[data-testid="traffic-served-total"]', '700.0MB'],
  ]
  for (const [selector, text] of expected) {
    const value = grid.locator(selector).locator('[data-tile-value]')
    await expect(value).toHaveText(text)
    const lineHeight = await value.evaluate(node => parseFloat(getComputedStyle(node).lineHeight) || parseFloat(getComputedStyle(node).fontSize))
    expect((await value.boundingBox())!.height).toBeLessThanOrEqual(lineHeight + 1)
  }
  await expect(grid.locator('[data-testid="traffic-origin-total"]')).toContainText('104 次回源请求')
  await expect(grid.locator('[data-testid="traffic-served-total"]')).toContainText('1,200 次请求')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1024)

  // With no trend buckets and no recent requests, the two cards collapse to
  // their empty notes and stack in the traffic column instead of reserving
  // chart-sized blank space.
  await page.setViewportSize({ width: 1440, height: 900 })
  const chart = page.locator('[data-dashboard-trends]')
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(recent).toHaveAttribute('data-dashboard-recent-empty', 'true')
  await expect(chart.locator('[data-trend-chart]')).toContainText('这段时间没有请求')
  await expect(chart.locator('.dash-chart-col[data-requests]:not([data-requests="0"])')).toHaveCount(0)
  const chartRect = (await chart.boundingBox())!
  const recentRect = (await recent.boundingBox())!
  expect(chartRect.height).toBeLessThan(240)
  expect(recentRect.height).toBeLessThan(240)
  expect(Math.abs(chartRect.width - recentRect.width)).toBeLessThan(2)
  expect(Math.abs(chartRect.x - recentRect.x)).toBeLessThan(2)
  expect(recentRect.y).toBeGreaterThanOrEqual(chartRect.y + chartRect.height)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

// 1024–1279px keeps four columns from squeezing the status labels and the
// request table from clipping its time column; both were visible regressions
// when the strip used the lg breakpoint and the table demanded 680px.
test('Overview status entry, its details and recent table stay readable at 1024 and 1440', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': {
      status: 'degraded',
      uptime_seconds: 3 * 86400,
      now_unix: 1786032000,
      version: 'dev',
      rate: {
        requests_per_min: 744,
        ingress_bps: 3_400_000,
        egress_bps: 380_000,
        has_data: true,
        measured: true,
        window_seconds: 60,
        service_requests_per_sec: 12.4,
        service_bytes_per_sec: 3_400_000,
        origin_requests_per_sec: 0.7,
        origin_bytes_per_sec: 380_000,
      },
      upstreams: { healthy: 1, total: 2 },
      sparkline: [],
    },
    'GET /api/v1/admin/dashboard': populatedDashboard({
      upstreams: [
        { id: 1, name: 'ruby-china', adapter: 'rubygems', healthy: false, avg_latency_ms: 890, success_rate: 0.71 },
        { id: 2, name: 'tuna', adapter: 'pypi', healthy: true, avg_latency_ms: 42, success_rate: 0.99 },
      ],
    }),
    'GET /api/v1/admin/logs': {
      items: [
        {
          id: 1, adapter_type: 'rubygems', package_name: 'nokogiri', version: '1.18.4',
          hit: false, cache_result: 'miss', upstream: 'crash', latency_ms: 30_120,
          status_code: 500, bytes_sent: 0, client_ip: '10.0.0.9',
          created_at: '2026-10-06T10:00:00Z',
        },
      ],
      total: 1, page: 1, page_size: 5,
    },
  })

  await page.setViewportSize({ width: 1024, height: 900 })
  await page.goto('/admin')

  const status = page.locator('[data-dashboard-status]')
  await expect(status).toContainText('1 个上游需关注')
  expect(await status.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await status.click()
  const details = page.getByRole('dialog')
  await expect(details).toContainText('ruby-china')
  await expect(details).toContainText('rubygems · #1')
  expect((await details.boundingBox())!.width).toBeLessThan(1024)
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()

  // The request tail keeps every column, latency and time included, inside its
  // card even when one stalled upstream pushes a row into seconds.
  await page.setViewportSize({ width: 1440, height: 900 })
  const recent = page.locator('[data-dashboard-recent-requests]')
  const row = recent.locator('[data-recent-request-id="1"]')
  await expect(row).toContainText('nokogiri@1.18.4')
  // A 500 is a failure, named in words beside its mark, not a cache miss.
  await expect(row.locator('.dash-outcome[data-outcome="failed"]')).toHaveText('失败')
  await expect(row).toContainText('30.1 s')
  await expect(row.locator('time')).toHaveAttribute('datetime', '2026-10-06T10:00:00Z')
  const fit = await recent.evaluate(card => {
    const table = card.querySelector('table')!
    const cardRect = card.getBoundingClientRect()
    const cells = [...table.querySelectorAll('tbody td')].filter(cell => cell.getClientRects().length > 0)
    return {
      tableFits: table.getBoundingClientRect().right <= cardRect.right + 1 && table.scrollWidth <= card.clientWidth + 1,
      cellsInside: cells.every(cell => cell.getBoundingClientRect().right <= cardRect.right + 1),
      visibleCells: cells.length,
    }
  })
  expect(fit).toEqual({ tableFits: true, cellsInside: true, visibleCells: 6 })

  // The live strip names each direction with its own last-60-second rate.
  await expect(page.locator('[data-testid="traffic-service-flow"]')).toHaveText('交付 3.2 MB/s')
  await expect(page.locator('[data-testid="traffic-origin-flow"]')).toHaveText('回源 371.1 KB/s')
  await expect(page.locator('[data-dashboard-live] [data-live="requests"]')).toHaveText('744 次/分')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(1440)
})

test('Overview idle health summary keeps its evidence and uptime readable', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveAttribute('data-health', 'healthy')
  await expect(status).toContainText('服务正常')
  await status.click()
  const details = page.getByRole('dialog')
  await expect(details).toContainText('数据库与存储就绪')
  await expect(details).toContainText('已运行时间')
  await expect(details.getByText('数据库', { exact: true })).toBeVisible()
  await expect(details.getByText('缓存存储', { exact: true })).toBeVisible()
  expect(await details.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()
})

test('Overview surfaces a degraded upstream as an actionable centered dialog', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': {
      status: 'degraded',
      uptime_seconds: 3600,
      now_unix: 1785200000,
      version: 'dev',
      last_activity: null,
      rate: {
        requests_per_min: 1,
        ingress_bps: 0,
        egress_bps: 0,
        has_data: true,
        measured: true,
        window_seconds: 60,
        service_requests_per_sec: 0.02,
        service_bytes_per_sec: 128,
        origin_requests_per_sec: 0.01,
        origin_bytes_per_sec: 64,
      },
      upstreams: { healthy: 1, total: 2 },
      sparkline: [],
    },
    'GET /api/v1/admin/dashboard': populatedDashboard({
      upstreams: [
        { id: 1, name: 'npmjs', adapter: 'npm', healthy: false, avg_latency_ms: 0, success_rate: 0.4 },
        { id: 2, name: 'PyPI', adapter: 'pypi', healthy: true, avg_latency_ms: 100, success_rate: 1 },
      ],
    }),
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  const problemsButton = page.locator('[data-dashboard-status]')
  // The entry counts distinct affected upstream IDs; the dialog keeps the
  // underlying checks visible without counting them as additional upstreams.
  await expect(problemsButton).toContainText('1 个上游需关注')
  await expect(problemsButton).toBeVisible()
  await expect(problemsButton).toHaveAttribute('aria-haspopup', 'dialog')
  // The Attention card lists the affected upstream once, with what it means
  // for downloads: no other npm upstream is usable, so the item is danger,
  // carried by its glyph and words as well as its colour.
  const attention = page.locator('[data-dashboard-attention]')
  await expect(attention.locator('[data-attention-item]')).toHaveCount(1)
  const item = attention.locator('[data-attention-item="upstream"]')
  await expect(item).toHaveAttribute('data-attention-tone', 'danger')
  await expect(item).toContainText('上游 npmjs（npm）不可用')
  await expect(item).toContainText('npm 没有其他可用上游')
  await expect(item.getByRole('link', { name: '查看上游' })).toHaveAttribute('href', '/admin/upstreams?status=attention')
  await expect(attention.locator('[data-check="upstreams"]')).toContainText('1/2')
  const benefits = page.locator('[data-dashboard-benefits]')
  expect((await problemsButton.boundingBox())!.height).toBeLessThan(80)
  expect((await problemsButton.boundingBox())!.y).toBeLessThan((await benefits.boundingBox())!.y)
  await problemsButton.focus()
  await page.keyboard.press('Enter')

  const dialog = page.locator('[data-slot="dialog-content"]').filter({ has: page.locator('[data-dashboard-info-dialog]') })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText(/npmjs/)).toBeVisible()
  await expect(dialog).toContainText('1 个上游需关注 · 1 项异常检查')
  await expect(dialog.locator('[data-upstream-health-id="1"]')).toContainText('npm · #1')
  await expect(dialog.getByRole('link', { name: '查看上游' })).toHaveAttribute('href', '/admin/upstreams')
  await expect(page.locator('[data-slot="sheet-content"]')).toHaveCount(0)

  const geometry = await dialog.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return { center: rect.left + rect.width / 2, viewport: window.innerWidth / 2 }
  })
  expect(Math.abs(geometry.center - geometry.viewport)).toBeLessThan(2)

  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(problemsButton).toBeFocused()
})
