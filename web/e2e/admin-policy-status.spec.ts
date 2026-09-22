import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

test('Admin keeps policy freshness contextual instead of rendering a global banner', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': () => {
      calls += 1
      return calls === 1
        ? {
            status: 'degraded',
            using_stale_snapshot: true,
            snapshot_loaded_at: '2026-09-02T01:00:00Z',
            snapshot_age_seconds: 720,
            refresh_failures: 2,
            on_load_error: 'use_stale_then_allow',
          }
        : {
            status: 'healthy',
            using_stale_snapshot: false,
            snapshot_loaded_at: '2026-09-02T01:12:00Z',
            snapshot_age_seconds: 0,
            refresh_failures: 0,
            on_load_error: 'use_stale_then_allow',
          }
    },
  })
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

  const banner = page.locator('[data-admin-policy-status-banner]')
  await expect(banner).toHaveCount(0)
  await expect(page.locator('[data-admin-topbar]')).toHaveCSS('height', '48px')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  expect(calls).toBe(0)
})

test('Admin does not present an unavailable policy probe as healthy', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': {
      // The engine reports a refresh failure as degraded even when no
      // last-known-good snapshot exists. The shell must not mislabel that
      // state as a stale snapshot.
      status: 'degraded',
      using_stale_snapshot: false,
      snapshot_loaded_at: null,
      snapshot_age_seconds: 0,
      refresh_failures: 1,
      on_load_error: 'use_stale_then_allow',
    },
  })
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin/security')

  await expect(page.locator('[data-admin-policy-status-banner]')).toHaveCount(0)
})

test('policy status belongs to Overview and Governance, including client-side navigation', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': () => {
      calls += 1
      return {
        status: 'degraded',
        using_stale_snapshot: true,
        snapshot_loaded_at: '2026-09-02T01:00:00Z',
        snapshot_age_seconds: 720,
        refresh_failures: 2,
        on_load_error: 'use_stale_then_allow',
      }
    },
  })
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/admin/settings')
  await expect(page.locator('[data-admin-page-title]')).toBeVisible()
  const banner = page.locator('[data-admin-policy-status-banner]')
  await expect(banner).toHaveCount(0)
  expect(calls).toBe(0)

  const navigation = page.locator('[data-admin-nav-surface="sidebar"]')
  await navigation.locator('a[href="/admin"]').click()
  await expect(banner).toHaveCount(0)
  await expect.poll(() => calls).toBeGreaterThan(0)

  await page.locator('aside [data-admin-sidebar-footer] a[href="/admin/users"]').click()
  await expect(page).toHaveURL(/\/admin\/users$/)
  await expect(banner).toHaveCount(0)

  await navigation.locator('a[href="/admin/security"]').click()
  await page.locator('[data-admin-page-navigation="security"] a[href="/admin/rules"]').click()
  await expect(banner).toHaveCount(0)
})
