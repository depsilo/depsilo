import { expect, expectResolvedUiPreferences, setUiPreferences, test } from './fixtures/admin-api'

test('renders every admin route without an unmatched API request', async ({ page }) => {
  const routes = [
    '/admin', '/admin/bandwidth', '/admin/logs', '/admin/audit',
    '/admin/quarantine', '/admin/cache', '/admin/indexes', '/admin/upstreams',
    '/admin/upstream-updates', '/admin/users',
    '/admin/license', '/admin/rules', '/admin/security', '/admin/projects',
    '/admin/settings',
  ]

  for (const route of routes) {
    await page.goto(route)
    await expect(page.locator('h1')).toBeVisible()
  }
})

test('resolves persisted locale and theme into the document', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'en')
})
