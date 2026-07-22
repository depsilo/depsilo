import { expect, mockAdminApi, test } from './fixtures/admin-api'

const pageContracts = [
  {
    route: '/admin/rules',
    width: 'fluid',
    description: '定义软件包允许与拒绝规则，并在保存前验证匹配结果。',
  },
  {
    route: '/admin/upstream-updates',
    width: 'fluid',
    description: '已缓存且具备上游验证器的元数据主动检查结果',
  },
  {
    route: '/admin/users',
    width: 'fluid',
    description: '管理运维账号与 API Token。',
  },
  {
    route: '/admin/quarantine',
    width: 'fluid',
    description: '最小发布年龄默认关闭；启用并配置阈值后可隔离刚发布的版本。',
  },
  {
    route: '/admin/license',
    width: 'readable',
    description: '管理您的 Pro 试用与许可证密钥',
  },
] as const

for (const contract of pageContracts) {
  test(`${contract.route} uses the shared Admin page contract`, async ({ page }) => {
    await mockAdminApi(page)
    await page.goto(contract.route)

    const adminPage = page.locator('[data-admin-page]')
    await expect(adminPage).toHaveAttribute('data-admin-page-width', contract.width)
    await expect(adminPage.locator('[data-admin-page-description]')).toContainText(contract.description)
    await expect(page.locator('h1:visible')).toHaveCount(1)
    await expect(page.locator('[data-admin-topbar] h1')).toHaveCount(1)
  })
}

test('page actions wrap on mobile without creating a second title', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

  const actions = page.locator('[data-admin-page-actions]')
  await expect(actions.getByRole('button', { name: /测试规则/ })).toBeVisible()
  await expect(actions.getByRole('button', { name: /添加规则/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('readable and fluid pages preserve distinct desktop widths', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 1000 })

  await page.goto('/admin/license')
  const readableWidth = await page.locator('[data-admin-page-width="readable"]').evaluate(element => element.getBoundingClientRect().width)

  await page.goto('/admin/upstream-updates')
  const fluidWidth = await page.locator('[data-admin-page-width="fluid"]').evaluate(element => element.getBoundingClientRect().width)
  expect(fluidWidth).toBeGreaterThan(readableWidth)
})

test('License keeps readable page chrome when its initial request fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/license/status': { status: 500, body: { code: 'FAILED', message: 'license unavailable' } },
  })
  await page.goto('/admin/license')

  await expect(page.locator('[data-admin-page-width="readable"]')).toBeVisible()
  await expect(page.locator('[data-admin-page-description]')).toContainText('管理您的 Pro 试用与许可证密钥')
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.locator('[data-admin-topbar] h1')).toHaveText('License 与 Pro')
})

test('duplicate page titles are removed while entity and section headings remain', async ({ page }) => {
  await mockAdminApi(page)

  await page.goto('/admin/quarantine')
  await expect(page.getByRole('heading', { name: '供应链隔离', exact: true })).toHaveCount(1)

  await page.goto('/admin/license')
  await expect(page.getByRole('heading', { name: 'License 与 Pro', exact: true })).toHaveCount(1)

  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: '用户', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'API Tokens', exact: true })).toBeVisible()
})
