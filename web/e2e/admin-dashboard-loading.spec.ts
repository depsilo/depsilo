import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

// First load must hold its shape: the skeletons match the settled regions, and
// no tile claims a storage/measurement fact before its payload exists.
test('Overview first-load skeletons cover the settled layout', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/now': async () => { await delay(2000); return adminApiDefaults['GET /api/v1/now'] },
    'GET /api/v1/admin/runtime': async () => { await delay(2000); return adminApiDefaults['GET /api/v1/admin/runtime'] },
    'GET /api/v1/admin/dashboard': async () => { await delay(2000); return adminApiDefaults['GET /api/v1/admin/dashboard'] },
    'GET /api/v1/admin/dashboard/trends': async () => { await delay(2000); return { points: [] } },
    'GET /api/v1/admin/logs': async () => { await delay(2000); return { items: [], total: 0, page: 1, page_size: 5 } },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin')

  // Loading: the recent-request table reserves header + five row slots.
  const recent = page.locator('[data-dashboard-recent-requests]')
  await expect(recent).toHaveAttribute('aria-busy', 'true')
  const skeletonRows = recent.locator('div[aria-hidden="true"] > div.h-\\[55px\\]')
  await expect(skeletonRows).toHaveCount(5)

  // Loading: the cache tile must not claim a missing quota before the sample.
  const cache = page.locator('[data-testid="resource-cache"]')
  await expect(cache).not.toContainText('未设置缓存配额')
  await expect(cache).not.toContainText('0 B')

  // Loading: the trend card already occupies the chart's footprint.
  const trendSkeleton = page.locator('[data-dashboard-root] div.h-\\[272px\\]').first()
  await expect(trendSkeleton).toBeVisible()

  // Settled: the same regions render real content at a comparable height.
  await expect(recent).not.toHaveAttribute('aria-busy', 'true')
  await expect(page.locator('[data-dashboard-traffic]')).toBeVisible()
  await expect(page.locator('[data-testid="traffic-served-total"]')).toContainText('次请求')
  const trendHeight = await page.locator('[data-dashboard-trends]').evaluate(node => node.getBoundingClientRect().height)
  expect(trendHeight).toBeGreaterThan(240)
})
