import type { DashboardBreakdownResponse, DashboardResponse } from '../src/lib/adminApi.types'
import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const defaultDashboard = adminApiDefaults['GET /api/v1/admin/dashboard'] as DashboardResponse

const breakdown: DashboardBreakdownResponse = {
  range: { key: '30d', start: '2026-09-03T00:00:00Z', end: '2026-10-03T00:00:00Z' },
  by_ecosystem: [
    { ecosystem: 'pypi', hit_requests: 900, miss_requests: 60, hit_bytes: 600 * 1024 ** 2, miss_bytes: 40 * 1024 ** 2, avg_hit_latency_ms: 6, avg_miss_latency_ms: 410 },
    { ecosystem: 'apt', hit_requests: 204, miss_requests: 36, hit_bytes: 71 * 1024 ** 2, miss_bytes: 22 * 1024 ** 2, avg_hit_latency_ms: 3, avg_miss_latency_ms: 1858 },
  ],
  // TUNA serves both ecosystems: two Upstreams with one name. Coalesced clients
  // received less than PyPI's mirror actually sent, so the two byte counts differ.
  by_upstream: [
    { ecosystem: 'pypi', upstream: 'tuna', upstream_requests: 58, upstream_bytes: 45 * 1024 ** 2, miss_requests: 60, miss_bytes: 40 * 1024 ** 2, avg_miss_latency_ms: 410 },
    { ecosystem: 'apt', upstream: 'tuna', upstream_requests: 36, upstream_bytes: 18 * 1024 ** 2, miss_requests: 36, miss_bytes: 22 * 1024 ** 2, avg_miss_latency_ms: 1858 },
  ],
  top_packages: [
    { ecosystem: 'pypi', package_name: 'numpy', requests: 143, total_bytes: 300 * 1024 ** 2, hit_bytes: 250 * 1024 ** 2 },
  ],
  top_packages_grain: 'utc_day',
}

test.describe('collapsible navigation rail', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('collapses to named icons, keeps keyboard focus on its toggle, and remembers the choice', async ({ page }) => {
    await setUiPreferences(page, 'light', 'zh')
    await page.goto('/admin')
    const rail = page.locator('#admin-rail-navigation')
    const main = page.locator('[data-admin-main]')
    const toggle = page.locator('[data-admin-rail-toggle]')
    await expect(rail).toHaveCSS('width', '232px')
    await expect(toggle).toHaveAttribute('aria-expanded', 'true')

    // Keyboard activation must not remount the focused control.
    await toggle.focus()
    await page.keyboard.press('Enter')
    await expect(rail).toHaveCSS('width', '60px')
    await expect(main).toHaveCSS('margin-left', '60px')
    await expect(toggle).toBeFocused()
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(toggle).toHaveAccessibleName('展开菜单')

    // Every workspace link keeps its name when only its icon shows.
    const nav = page.getByRole('navigation', { name: '管理导航' })
    await expect(nav.getByRole('link', { name: '总览', exact: true })).toBeVisible()
    await expect(nav.getByRole('link', { name: '上游源', exact: true })).toBeVisible()

    await page.reload()
    await expect(rail).toHaveCSS('width', '60px')
    await page.locator('[data-admin-rail-toggle]').focus()
    await page.keyboard.press('Space')
    await expect(rail).toHaveCSS('width', '232px')
    await expect(page.locator('[data-admin-rail-toggle]')).toBeFocused()
    expect(await page.evaluate(() => localStorage.getItem('depsilo-admin-rail'))).toBe('expanded')
  })
})

test('full-screen mode hides the rail, survives a refused Fullscreen API, and leaves on Escape', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  // Headless Chromium may refuse fullscreen; the mode must not depend on it.
  await page.addInitScript(() => {
    Element.prototype.requestFullscreen = () => Promise.reject(new Error('refused'))
  })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')
  const rail = page.locator('#admin-rail-navigation')
  const button = page.locator('[data-dashboard-fullscreen]')
  await expect(button).toHaveAccessibleName('全屏显示')
  await button.click()
  await expect(rail).toBeHidden()
  await expect(page.locator('[data-admin-main]')).toHaveCSS('margin-left', '0px')
  await expect(button).toHaveAccessibleName('退出全屏')

  await page.keyboard.press('Escape')
  await expect(rail).toBeVisible()
  await expect(button).toHaveAccessibleName('全屏显示')

  // Leaving the page ends the mode, so no other page loses its navigation.
  await button.click()
  await expect(rail).toBeHidden()
  await page.goto('/admin/upstreams')
  await expect(rail).toBeVisible()
})

test('origin fetches list measured upstream bytes per ecosystem-qualified upstream', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockAdminApi(page, { 'GET /api/v1/admin/dashboard/breakdown': breakdown })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')

  const origin = page.locator('[data-dashboard-origin-upstreams]')
  const pypi = origin.locator('[data-upstream="pypi/tuna"]')
  const apt = origin.locator('[data-upstream="apt/tuna"]')
  await expect(pypi).toContainText('45.0 MB')
  await expect(pypi).toContainText('58')
  await expect(apt).toContainText('18.0 MB')
  // A latency over a second reads in seconds rather than as a four-digit ms figure.
  await expect(apt).toContainText('1.86 s')
  await expect(origin).not.toContainText('40.0 MB')

  const ecosystems = page.locator('[data-dashboard-ecosystems]')
  await expect(ecosystems.locator('[data-ecosystem="pypi"]')).toContainText('94%')
  await expect(page.locator('[data-dashboard-packages]')).toContainText('numpy')
})

test('origin fetches are withheld, not shown as zero, before origin metering ran', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': { ...defaultDashboard, origin_coverage: { measured: false, since: null, window_complete: false } },
    'GET /api/v1/admin/dashboard/breakdown': breakdown,
  })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')
  const origin = page.locator('[data-dashboard-origin-upstreams]')
  await expect(origin).toContainText('回源计量尚未启用')
  await expect(origin.locator('table')).toHaveCount(0)
})

test('attention never claims an all-clear while open security suggestions are unknown', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': { status: 500, body: { code: 'FAILED', message: 'fixture failure' } },
  })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')
  const attention = page.locator('[data-dashboard-attention]')
  await expect(attention).toContainText('安全建议尚未确认')
  await expect(attention.locator('[data-attention-clear]')).toHaveCount(0)
})
