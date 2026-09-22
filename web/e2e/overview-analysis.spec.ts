import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

const report = {
  range: { start: '2026-09-14', end: '2026-09-20' },
  summary: { total_bytes: 1000, hit_bytes: 300, miss_bytes: 700, savings_rate: .3, total_requests: 100, hit_requests: 90, miss_requests: 10, time_saved_ms: 250, avg_hit_latency: 1, avg_miss_latency: 2 },
  daily: [],
  by_ecosystem: [{ ecosystem: 'pypi', hit_bytes: 300, miss_bytes: 700, hit_count: 90, miss_count: 10, avg_hit_latency_ms: 1, avg_miss_latency_ms: 2 }],
  top_packages: [{ ecosystem: 'pypi', package_name: 'requests', total_bytes: 700, hit_bytes: 200, request_count: 20 }],
  by_upstream: [{ upstream: 'pypi.org', miss_bytes: 700, request_count: 10, avg_latency_ms: 2 }],
}

test('overview analysis shares report scope and full denominators', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/bandwidth': report,
    'GET /api/v1/admin/dashboard/trends': { points: [] },
  })
  await page.goto('/admin?analysisRange=7d&group=packages')
  const analysis = page.locator('[data-query-key="overview-analysis"]')
  await expect(analysis).toContainText('2026-09-14')
  await expect(analysis).toContainText('300 B')
  await expect(analysis).toContainText('30%')
  await analysis.getByRole('button', { name: 'Ecosystems' }).click()
  await expect(page).toHaveURL(/group=ecosystems/)
  await expect(analysis).toContainText('pypi')
  await analysis.getByRole('link', { name: /Full analysis/ }).click()
  await expect(page).toHaveURL(/\/admin\/bandwidth\?group=ecosystems&range=7d/)
})

test('bandwidth drilldown keeps URL range and exposes analysis basis', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, { 'GET /api/v1/admin/bandwidth': report })
  await page.goto('/admin/bandwidth?range=90d')
  await expect(page).toHaveURL(/range=90d/)
  await expect(page.getByRole('link', { name: /返回总览/ })).toHaveAttribute('href', /analysisRange=90d/)
  await expect(page.getByText('统计说明')).toBeVisible()
  await expect(page.locator('[data-admin-page-navigation="overview"]')).toHaveCount(0)
})
