import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('AdminPage renders the route title once and wraps its actions on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

  await expect(page.locator('[data-admin-page-title]')).toHaveText('包治理')
  await expect(page.getByRole('heading', { level: 1, name: '包治理', exact: true })).toHaveCount(1)
  await expect(page.locator('[data-admin-topbar]').getByRole('heading')).toHaveCount(0)
  const actions = page.locator('[data-admin-page-actions]')
  await expect(actions.getByRole('button', { name: /测试规则/ })).toBeVisible()
  await expect(actions.getByRole('button', { name: /添加规则/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('readable and fluid pages preserve distinct desktop widths', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })

  await page.goto('/admin/license')
  const readableWidth = await page.locator('[data-admin-page-width="readable"]').evaluate(element => element.getBoundingClientRect().width)

  await page.goto('/admin/upstream-updates')
  const fluidWidth = await page.locator('[data-admin-page-width="fluid"]').evaluate(element => element.getBoundingClientRect().width)
  expect(fluidWidth).toBeGreaterThan(readableWidth)
})

test('Dashboard aligns its snapshot with both fluid page content seams', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 2048, height: 1000 })
  await page.goto('/admin')

  const adminPage = page.locator('[data-admin-page-width="fluid"]')
  const snapshot = page.locator('[data-query-key="dashboard-snapshot"]')
  await expect(adminPage).toBeVisible()
  await expect(adminPage).toHaveAttribute('data-admin-page-width', 'fluid')
  await expect(snapshot).toBeVisible()

  const geometry = await page.evaluate(() => {
    const pageElement = document.querySelector<HTMLElement>('[data-admin-page-width="fluid"]')
    const snapshotElement = document.querySelector<HTMLElement>('[data-query-key="dashboard-snapshot"]')
    if (!pageElement || !snapshotElement) throw new Error('Dashboard layout hooks missing')
    const pageRect = pageElement.getBoundingClientRect()
    const snapshotRect = snapshotElement.getBoundingClientRect()
    return {
      pageLeft: pageRect.left,
      pageRight: pageRect.right,
      snapshotLeft: snapshotRect.left,
      snapshotRight: snapshotRect.right,
      scrollWidth: document.documentElement.scrollWidth,
    }
  })

  expect(Math.abs(geometry.snapshotLeft - geometry.pageLeft)).toBeLessThanOrEqual(1)
  expect(Math.abs(geometry.snapshotRight - geometry.pageRight)).toBeLessThanOrEqual(1)
  expect(geometry.scrollWidth).toBe(2048)
})

test('duplicate page titles are removed while entity and section headings remain', async ({ page }) => {
  await page.goto('/admin/quarantine')
  await expect(page.getByRole('heading', { name: '供应链隔离', exact: true })).toHaveCount(1)

  await page.goto('/admin/license')
  await expect(page.getByRole('heading', { name: 'License 与 Pro', exact: true })).toHaveCount(1)

  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: '用户', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'API 令牌', exact: true })).toBeVisible()
})
