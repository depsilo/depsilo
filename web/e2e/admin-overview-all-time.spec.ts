import type { Request } from '@playwright/test'

import type { DashboardResponse } from '../src/lib/adminApi.types'
import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const defaultDashboard = adminApiDefaults['GET /api/v1/admin/dashboard'] as DashboardResponse

// All time has no previous period; the server omits `prev`.
const allTimeDashboard: DashboardResponse = {
  ...defaultDashboard,
  prev: undefined,
  range: { key: 'all', start: '2025-07-01T00:00:00Z', end: '2026-10-03T00:00:00Z', archived: true },
}

/** Ten UTC days of trend buckets, as the server returns them for `all`. */
function dailyPoints() {
  const start = Date.UTC(2026, 8, 24) / 1000
  return Array.from({ length: 10 }, (_, index) => {
    const requests = index === 0 ? 4 : index === 9 ? 6 : 0
    return {
      bucket: start + index * 86400, date: '', requests, hits: requests, misses: 0, hit_rate: requests ? 1 : 0,
      bytes_served: requests * 100, bytes_hit: requests * 100, bytes_miss: 0, upstream_requests: 0, upstream_bytes: 0,
      sum_latency_ms: requests * 5, avg_latency_ms: requests ? 5 : 0, errors: 0,
    }
  })
}

test.describe('all-time range', () => {
  test.use({ timezoneId: 'Pacific/Auckland' })

  test('is offered beside the period ranges, names where coverage begins, and shows no period change', async ({ page }) => {
    const requested: string[] = []
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard': (request: Request) => {
        const range = new URL(request.url()).searchParams.get('range') ?? ''
        requested.push(range)
        return range === 'all' ? allTimeDashboard : defaultDashboard
      },
      'GET /api/v1/admin/dashboard/trends': (request: Request) => (
        new URL(request.url()).searchParams.get('range') === 'all' ? { points: dailyPoints() } : { points: [] }
      ),
    })
    await setUiPreferences(page, 'light', 'zh')
    await page.goto('/admin')

    const rangeGroup = page.getByRole('group', { name: '统计周期' })
    await rangeGroup.getByRole('button', { name: '全部', exact: true }).click()
    await expect(page.locator('[data-dashboard-period]')).toHaveAttribute('data-period-range', 'all')
    expect(requested).toContain('all')
    expect(await page.evaluate(() => localStorage.getItem('depsilo-dashboard-range'))).toBe('all')

    // A UTC-dated start: Auckland's local date would read 7/1 too, but the
    // Los Angeles test below would read 6/30 if it were formatted locally.
    const note = page.locator('[data-dashboard-all-time]')
    await expect(note).toContainText('全部统计自 2025/7/1（UTC）起')
    await expect(note).toContainText('超过保留期的日期只保留汇总')
    // No previous period, so no tile carries a change chip.
    await expect(page.locator('.dash-tile-delta')).toHaveCount(0)

    // Day-grained bars are UTC days: in Auckland (UTC+13) the first bar is
    // still 9/24, not shifted into the next local day.
    const trends = page.locator('[data-dashboard-trends]')
    await expect(trends).toHaveAttribute('data-trend-range', 'all')
    await expect(trends.locator('.dash-chart-col')).toHaveCount(10)
    await trends.locator('[data-trend-chart]').focus()
    await page.keyboard.press('Home')
    await expect(trends.locator('[data-trend-tooltip] p').first()).toHaveText('9/24')
    await expect(trends.locator('[data-trend-tooltip] p').nth(1)).toHaveText('4 次请求')
  })
})

test.describe('all-time range west of UTC', () => {
  test.use({ timezoneId: 'America/Los_Angeles' })

  test('names the UTC start date, and says nothing about archived days when none exist', async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/admin/dashboard': { ...allTimeDashboard, range: { ...allTimeDashboard.range!, archived: false } },
      'GET /api/v1/admin/dashboard/trends': { points: dailyPoints().slice(-1) },
    })
    await page.addInitScript(() => localStorage.setItem('depsilo-dashboard-range', 'all'))
    await setUiPreferences(page, 'light', 'zh')
    await page.goto('/admin')
    const note = page.locator('[data-dashboard-all-time]')
    await expect(note).toContainText('全部统计自 2025/7/1（UTC）起')
    await expect(note).not.toContainText('超过保留期')
    // One day of history is one day-wide bar, not a one-minute one.
    const trends = page.locator('[data-dashboard-trends]')
    await expect(trends).toContainText('每格 1 天')
    await expect(trends.locator('.dash-chart-col')).toHaveCount(1)
  })
})
