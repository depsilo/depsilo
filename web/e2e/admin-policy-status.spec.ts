import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

test('Admin shows when policy decisions use a stale snapshot and can refresh it', async ({ page }) => {
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
  await expect(banner).toBeVisible()
  await expect(banner).toContainText('Policy rules are using a stale snapshot.')
  await expect(banner).toContainText('Last successful refresh: 12 minutes ago')
  await expect(page.locator('[data-admin-topbar]')).toHaveCSS('height', '48px')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await banner.getByRole('button', { name: 'Refresh policy status' }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(banner).toHaveCount(0)
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
  await page.goto('/admin/settings')

  const banner = page.locator('[data-admin-policy-status-banner]')
  await expect(banner).toBeVisible()
  await expect(banner).toContainText('Policy status is temporarily unavailable.')
  await expect(banner).not.toContainText('Policy rules are using a stale snapshot.')
})
