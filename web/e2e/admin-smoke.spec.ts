import { adminRouteManifest } from '../src/admin/routes'
import { expect, expectResolvedUiPreferences, setUiPreferences, test } from './fixtures/admin-api'

test('renders every admin route without an unmatched API request', { tag: '@smoke' }, async ({ page }) => {
  // This one contract intentionally deep-links every Admin route. Give the
  // aggregate loop a larger budget while each navigation/assertion remains
  // independently required.
  test.setTimeout(60_000)
  for (const route of adminRouteManifest) {
    await page.goto(route.href)
    await expect(page.locator('h1')).toBeVisible()
  }
})

test('resolves persisted locale and theme into the document', { tag: '@smoke' }, async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin')
  await expectResolvedUiPreferences(page, 'light', 'en')
})
