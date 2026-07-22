import { expect, setUiPreferences, test } from './fixtures/admin-api'

test('Portal and Admin present minimum release age as an opt-in gate', async ({ page }) => {
  await setUiPreferences(page, 'light', 'zh')

  await page.goto('/')
  await expect(page.getByText('已知恶意版本由恶意清单阻断；如需让所有新版本先经过冷却期，可按需启用最小发布年龄护栏。')).toBeVisible()

  await page.goto('/admin/quarantine')
  await expect(page.locator('[data-admin-page-description]')).toContainText('最小发布年龄默认关闭')
  await expect(page.locator('[data-admin-page-description]')).toContainText('恶意包封锁独立运行')
})
