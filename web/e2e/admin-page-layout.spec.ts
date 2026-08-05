import { expect, test } from './fixtures/admin-api'

test('page actions wrap on mobile without creating a second title', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

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

test('duplicate page titles are removed while entity and section headings remain', async ({ page }) => {
  await page.goto('/admin/quarantine')
  await expect(page.getByRole('heading', { name: '供应链隔离', exact: true })).toHaveCount(1)

  await page.goto('/admin/license')
  await expect(page.getByRole('heading', { name: 'License 与 Pro', exact: true })).toHaveCount(1)

  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: '用户', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'API 令牌', exact: true })).toBeVisible()
})
