import type { Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('empty index filters offer a reset without losing the cache overview on mobile', async ({ page }) => {
  const queries: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/cache/indexes': (request: Request) => {
      queries.push(new URL(request.url()).search)
      return { items: [], total: 0, page: 1, page_size: 25, summary: [] }
    },
  })
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/indexes')

  await expect(page.getByText('暂无索引缓存')).toBeVisible()
  await expect(page.getByRole('button', { name: '清除筛选' })).toHaveCount(0)
  await expect(page.getByText('通过本服务请求软件包索引后，缓存记录将显示在这里。')).toBeVisible()
  await page.getByRole('textbox', { name: '搜索索引缓存' }).fill('missing-package')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox', { name: '按有效状态筛选' }).selectOption('stale')
  await expect(page.getByText('没有符合条件的索引缓存')).toBeVisible()
  await expect(page.getByRole('button', { name: '清除筛选' })).toBeVisible()
  await expect.poll(() => queries.some(query => query.includes('search=missing-package') && query.includes('status=stale'))).toBe(true)

  await page.getByRole('button', { name: '清除筛选' }).click()
  await expect(page.getByRole('textbox', { name: '搜索索引缓存' })).toHaveValue('')
  await expect(page.getByRole('combobox', { name: '按有效状态筛选' })).toHaveValue('all')
  await expect(page.getByRole('button', { name: '清除筛选' })).toHaveCount(0)
  await expect(page.getByText('暂无索引缓存')).toBeVisible()
  await expect.poll(() => queries.at(-1)).toBe('?page=1&page_size=25')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})
