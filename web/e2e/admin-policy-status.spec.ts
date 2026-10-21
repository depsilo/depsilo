import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

test('a policy engine awaiting its first evaluation is informative, not an active issue', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': {
      status: 'unavailable', degraded: false, using_stale_snapshot: false,
      snapshot_loaded_at: null, snapshot_age_seconds: 0, refresh_failures: 0,
      on_load_error: 'use_stale_then_allow',
    },
  })
  await setUiPreferences(page, 'light', 'zh')
  const policyResponse = page.waitForResponse(response => response.url().endsWith('/api/v1/admin/policy/status'))
  await page.goto('/admin')
  await policyResponse

  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('服务正常')
  await expect(status).toHaveAttribute('data-health', 'healthy')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toHaveCount(1)
  await expect(details).toBeVisible()
  await expect(details).not.toContainText('包规则状态暂时不可用')
  await expect(details).not.toContainText('包规则正在使用旧快照')
  await expect(details.getByRole('button', { name: '刷新状态', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(status).toBeFocused()

  await page.goto('/admin/rules')
  await expect(page.locator('[data-admin-policy-status-banner]')).toHaveCount(0)
  await expect(page.locator('[data-admin-policy-idle]')).toContainText('首次检查')
})

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
  await expect(page.locator('[data-admin-topbar]')).toHaveCount(0)
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
  await page.goto('/admin/security')

  const banner = page.locator('[data-admin-policy-status-banner]')
  await expect(banner).toBeVisible()
  await expect(banner).toContainText('Policy status is temporarily unavailable.')
  await expect(banner).not.toContainText('Policy rules are using a stale snapshot.')

  await page.goto('/admin')
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveAttribute('data-health', 'partial')
  await status.click()
  const details = page.getByRole('dialog', { name: 'Service status details' })
  await expect(details).toHaveCount(1)
  await expect(details).toContainText('Policy status is temporarily unavailable')
  await expect(details).not.toContainText('Policy rules are using a stale snapshot')
  await expect(details.getByRole('link', { name: 'Review rules' })).toHaveAttribute('href', '/admin/rules')
  await expect(page.locator('[data-admin-policy-idle]')).toHaveCount(0)
})

test('a failed policy status request remains actionable', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/policy/status': () => {
      calls += 1
      return { status: 503, body: { error: 'unavailable' } }
    },
  })
  await setUiPreferences(page, 'light', 'zh')
  await page.goto('/admin')

  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveText('状态未知')
  await expect(status).toHaveAttribute('data-health', 'unknown')
  await status.click()
  const details = page.getByRole('dialog', { name: '服务状态详情' })
  await expect(details).toHaveCount(1)
  await expect(details).toContainText('包规则健康状态暂不可用')
  await expect(details).not.toContainText('包规则正在使用旧快照')
  const previousCalls = calls
  await details.getByRole('button', { name: '刷新状态', exact: true }).click()
  await expect.poll(() => calls).toBeGreaterThan(previousCalls)
  await expect(status).toHaveAttribute('data-health', 'unknown')
  await page.goto('/admin/rules')
  await expect(page.locator('[data-admin-policy-status-banner]')).toBeVisible()
  await expect(page.locator('[data-admin-policy-idle]')).toHaveCount(0)
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
  // Overview surfaces policy status from the permanent status button, without a
  // separate panel ahead of the traffic and benefit sections.
  const status = page.locator('[data-dashboard-status]')
  await expect(status).toHaveAttribute('data-health', 'partial')
  await status.click()
  const problemDialog = page.getByRole('dialog', { name: 'Service status details' })
  await expect(problemDialog).toHaveCount(1)
  await expect(problemDialog).toContainText('using a stale snapshot')
  await expect(problemDialog.getByRole('link', { name: 'Review rules' })).toHaveAttribute('href', '/admin/rules')
  await page.keyboard.press('Escape')
  await expect(problemDialog).toHaveCount(0)
  await expect(banner).toHaveCount(0)

  await page.locator('aside [data-admin-sidebar-footer] a[href="/admin/users"]').click()
  await expect(page).toHaveURL(/\/admin\/users$/)
  await expect(banner).toHaveCount(0)

  await navigation.locator('a[href="/admin/security"]').click()
  await page.locator('[data-admin-page-navigation="security"] a[href="/admin/rules"]').click()
  await expect(banner).toContainText('Policy rules are using a stale snapshot.')
})
