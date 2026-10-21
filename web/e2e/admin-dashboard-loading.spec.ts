import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

// First load must hold its shape: the skeletons match the settled regions, and
// no tile claims a storage/measurement fact before its payload exists.
test('Overview first-load skeletons cover the settled layout', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  const afterRelease = (key: string) => async () => {
    await pending
    return adminApiDefaults[key]
  }
  await mockAdminApi(page, {
    'GET /ready': afterRelease('GET /ready'),
    'GET /api/v1/now': afterRelease('GET /api/v1/now'),
    'GET /api/v1/admin/runtime': afterRelease('GET /api/v1/admin/runtime'),
    'GET /api/v1/admin/dashboard': afterRelease('GET /api/v1/admin/dashboard'),
    'GET /api/v1/admin/dashboard/trends': afterRelease('GET /api/v1/admin/dashboard/trends'),
    'GET /api/v1/admin/logs': afterRelease('GET /api/v1/admin/logs'),
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('正在检查服务状态')
  await expect(status).not.toContainText('服务正常')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toBeVisible()
  await expect(details).toContainText('未知')
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()

  // Loading: the recent-request table reserves header + five row slots.
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(recent).toHaveAttribute('aria-busy', 'true')
  const skeletonRows = recent.locator('div[aria-hidden="true"] > div.h-\\[55px\\]')
  await expect(skeletonRows).toHaveCount(5)

  // Loading: the cache tile must not claim a missing quota before the sample.
  const cache = page.locator('[data-testid="resource-cache"]')
  await expect(cache).not.toContainText('未设置缓存配额')
  await expect(cache).not.toContainText('0 B')
  await expect(page.locator('[data-dashboard-onboarding]')).toHaveCount(0)
  await expect(page.locator('[data-dashboard-benefits]')).not.toContainText('0 B')

  // Loading: the trend card already occupies the chart's footprint.
  const trendSkeleton = page.locator('[data-dashboard-trends-column] [aria-busy="true"]')
  await expect(trendSkeleton).toBeVisible()

  // Settled: the same regions render real content at a comparable height.
  release()
  await expect(recent).not.toHaveAttribute('aria-busy', 'true')
  await expect(page.locator('[data-dashboard-traffic]')).toBeVisible()
  await expect(page.locator('[data-testid="traffic-served-total"]')).toContainText('次请求')
  await expect(page.locator('[data-dashboard-trends]')).toBeVisible()
  await expect(status).toHaveText('服务正常')
})
