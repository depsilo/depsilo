import AxeBuilder from '@axe-core/playwright'

import {
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

const longPackage = '@depsilo/supply-chain-package-with-an-intentionally-long-unbroken-name'

test('quarantine mobile lists keep decision context and available actions visible', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/quarantine/events': {
      items: [{
        id: 1,
        ecosystem: 'npm',
        package: longPackage,
        version: '2026.07.29-security-review-candidate',
        action: 'blocked',
        reason: 'The release is newer than the configured minimum age and needs an operator decision.',
        threshold_seconds: 86400,
        age_at_call_seconds: 60,
        actor_id: 1,
        client_ip: '127.0.0.1',
        created_at: '2026-07-29T08:30:00Z',
      }],
      total: 1,
    },
    'GET /api/v1/admin/quarantine/approvals': {
      items: [{
        id: 2,
        ecosystem: 'pypi',
        package: 'trusted-after-review',
        version: '1.2.3',
        reason: 'Reviewed against the upstream release and approved for the build.',
        approved_by: 1,
        created_at: '2026-07-29T08:35:00Z',
      }],
      total: 1,
    },
    'GET /api/v1/admin/blocklist/status': {
      enabled: true,
      entry_count: 42,
      last_sync_at: '2026-07-29T08:00:00Z',
      last_success_at: '2026-07-29T08:00:00Z',
      last_error: '',
      duration_ms: 120,
      per_ecosystem: { npm: 42 },
      ecosystems: ['npm'],
      running: false,
      next_sync_at: '2026-07-29T09:00:00Z',
    },
    'GET /api/v1/admin/blocklist/overrides': {
      items: [{
        id: 3,
        ecosystem: 'cargo',
        package: 'confirmed-false-positive-with-a-long-package-name',
        version: '4.5.6',
        reason: 'The advisory was reviewed and does not affect this build configuration.',
        actor_id: 1,
        created_at: '2026-07-29T08:40:00Z',
        expires_at: '2026-07-30T08:40:00Z',
      }],
      now: '2026-07-29T08:45:00Z',
    },
  })

  await page.goto('/admin/quarantine')

  const eventList = page.locator('[data-quarantine-mobile-list="events"]')
  await expect(eventList).toBeVisible()
  await expect(eventList.getByText(longPackage, { exact: true })).toBeVisible()
  await expect(eventList.getByText('2026.07.29-security-review-candidate', { exact: true })).toBeVisible()
  await expect(eventList.getByText('Blocked', { exact: true })).toBeVisible()
  await expect(eventList.getByText(/newer than the configured minimum age/)).toBeVisible()
  await expect(page.getByText(/Minimum release age is safety-disabled/)).toBeVisible()
  await expect(eventList.getByRole('button', { name: 'Approve' })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.getByRole('tab', { name: 'Approvals' }).click()
  const approvalList = page.locator('[data-quarantine-mobile-list="approvals"]')
  await expect(approvalList).toBeVisible()
  await expect(approvalList.getByText('trusted-after-review', { exact: true })).toBeVisible()
  await expect(approvalList.getByText('Approved', { exact: true })).toBeVisible()
  await expect(approvalList.getByText(/Reviewed against the upstream release/)).toBeVisible()
  await approvalList.getByRole('button', { name: 'Revoke' }).click()
  const revokeDialog = page.getByRole('dialog', { name: 'Revoke this approval' })
  await expect(revokeDialog.getByLabel('Reason')).toBeVisible()
  await revokeDialog.getByRole('button', { name: 'Cancel' }).click()

  await page.getByRole('tab', { name: 'Malware blocklist' }).click()
  const overrideList = page.locator('[data-quarantine-mobile-list="overrides"]')
  await expect(overrideList).toBeVisible()
  await expect(overrideList.getByText('confirmed-false-positive-with-a-long-package-name', { exact: true })).toBeVisible()
  await expect(overrideList.getByText(/The advisory was reviewed/)).toBeVisible()
  await expect(overrideList.getByRole('button', { name: 'Revoke' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.getByRole('button', { name: 'Add override' }).click()
  const createDialog = page.getByRole('dialog', { name: 'Create malware override' })
  await expect(createDialog.getByLabel('Ecosystem')).toBeVisible()
  await expect(createDialog.getByLabel('Package')).toBeVisible()
  await expect(createDialog.getByLabel('Version')).toBeVisible()
  await expect(createDialog.getByLabel('Reason')).toBeVisible()

  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
})
