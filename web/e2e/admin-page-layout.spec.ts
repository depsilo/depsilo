import { expect, mockAdminApi, test } from './fixtures/admin-api'
import type { CacheDistributionResponse } from '../src/lib/adminApi.types'

test('AdminPage renders the route title once and wraps its actions on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

  await expect(page.locator('[data-admin-page-title]')).toHaveText('包规则')
  await expect(page.getByRole('heading', { level: 1, name: '包规则', exact: true })).toHaveCount(1)
  await expect(page.locator('[data-admin-topbar]')).toHaveCount(0)
  const actions = page.locator('[data-admin-page-actions]')
  await expect(actions.getByRole('button', { name: /测试规则/ })).toBeVisible()
  await expect(actions.getByRole('button', { name: /添加规则/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('workspace navigation remains stable while readable content has a narrower measure', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })

  await page.goto('/admin/license')
  const readableWidth = await page.locator('[data-admin-page-width="readable"] [data-admin-page-content]').evaluate(element => element.getBoundingClientRect().width)

  await page.goto('/admin/upstream-updates')
  const fluidWidth = await page.locator('[data-admin-page-width="fluid"] [data-admin-page-content]').evaluate(element => element.getBoundingClientRect().width)
  expect(fluidWidth).toBeGreaterThan(readableWidth)
})

test('workspace destinations are the page header tabs', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin/cache')

  const header = page.locator('[data-admin-page-header]')
  const localNavigation = header.locator('[data-admin-page-navigation="cache"]')
  await expect(localNavigation).toBeVisible()
  await expect(localNavigation.getByRole('link', { name: '缓存对象', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(localNavigation.getByRole('link', { name: '索引缓存', exact: true })).toHaveAttribute('href', '/admin/indexes')
  await expect(localNavigation.getByRole('link', { name: '编译缓存', exact: true })).toHaveAttribute('href', '/admin/compile-cache')

  // The rail stands in for the page title, so the route heading stays in the
  // accessibility tree without repeating the visible label.
  const title = header.locator('[data-admin-page-title]')
  await expect(title).toHaveText('缓存对象')
  expect(await title.evaluate(element => element.getBoundingClientRect().width)).toBeLessThanOrEqual(2)
  await expect(page.locator('[data-admin-page-content] [data-admin-page-navigation]')).toHaveCount(0)

  // Switching destinations keeps the rail in the header and moves the current tab.
  await localNavigation.getByRole('link', { name: '编译缓存', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/compile-cache$/)
  await expect(header.locator('[data-admin-page-navigation="cache"] a[aria-current="page"]')).toHaveText('编译缓存')
})

test('tab workspaces keep their own commands below the rail', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin/cache')

  const description = page.locator('[data-admin-page-description]')
  const actions = page.locator('[data-admin-page-actions]')
  await expect(actions.getByRole('button', { name: '预热索引', exact: true })).toBeVisible()
  await expect(actions.getByRole('button', { name: /清理缓存/ })).toBeVisible()

  // The rail owns the header band; the selected tab's copy and commands sit one
  // level below it instead of sharing the workspace navigation row.
  const geometry = await page.evaluate(() => {
    const box = (selector: string) => {
      const element = document.querySelector<HTMLElement>(selector)
      if (!element) throw new Error(`missing ${selector}`)
      const rect = element.getBoundingClientRect()
      return { top: rect.top, bottom: rect.bottom }
    }
    return { rail: box('[data-admin-page-navigation="cache"]'), actions: box('[data-admin-page-actions]') }
  })
  expect(geometry.actions.top).toBeGreaterThanOrEqual(geometry.rail.bottom)
  await expect(description).toBeVisible()
})

test('empty cache keeps the search controls close to the storage overview on mobile', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/cache')

  await expect(page.getByRole('textbox', { name: '搜索缓存' })).toBeVisible()
  await expect(page.getByText('暂无缓存数据', { exact: true })).toBeVisible()
  const filters = await page.locator('[data-admin-filters]').boundingBox()
  expect(filters?.y).toBeLessThan(750)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
})

test('cache capacity uses its real quota and omits the meter when the quota is unknown', async ({ page }) => {
  let distribution: CacheDistributionResponse = {
    total_size: 0, max_size: 1024, usage_percent: 0,
    by_type: [{ type: 'npm', size: 0, file_count: 0 }], top_packages: [],
  }
  await mockAdminApi(page, { 'GET /api/v1/admin/cache/distribution': () => distribution })
  await page.goto('/admin/cache')
  const meter = page.getByRole('meter', { name: '存储概览' })
  await expect(meter).toHaveAttribute('aria-valuenow', '0')
  await expect(meter).toHaveAttribute('aria-valuemax', '1024')
  await expect(meter.locator('[data-slot="meter-track"] > div')).toHaveCSS('width', '0px')

  distribution = { ...distribution, total_size: 512, usage_percent: 50, by_type: [{ type: 'npm', size: 512, file_count: 1 }] }
  await page.reload()
  await expect(meter).toHaveAttribute('aria-valuenow', '512')
  await expect(meter).toHaveAttribute('aria-valuetext', '512 B / 1.0 KB')
  const track = (await meter.locator('[data-slot="meter-track"]').boundingBox())!
  const segment = (await meter.locator('[data-slot="meter-track"] > div').boundingBox())!
  expect(segment.width / track.width).toBeCloseTo(0.5, 2)

  distribution = { ...distribution, max_size: 0, usage_percent: 0 }
  await page.reload()
  await expect(page.getByRole('region', { name: '存储概览' })).toContainText('512 B')
  await expect(meter).toHaveCount(0)
  await expect(page.getByRole('region', { name: '存储概览' })).not.toContainText('/ 0 B')
})

test('warmed workspace destinations switch without a loading flash', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })

  // The rail warms sibling chunks as soon as the current page paints.
  const warm = page.waitForResponse(response => /CacheIndexes/.test(response.url()))
  await page.goto('/admin/cache')

  const rail = page.locator('[data-admin-page-navigation="cache"]')
  await expect(rail).toBeVisible()
  await warm
  await page.waitForTimeout(300)

  await page.evaluate(() => {
    const probe = { seen: false }
    Reflect.set(window, '__loadingProbe', probe)
    new MutationObserver(() => {
      if (document.querySelector('[data-route-state="loading"]')) probe.seen = true
    }).observe(document.body, { childList: true, subtree: true })
  })

  await rail.getByRole('link', { name: '索引缓存', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/indexes$/)
  await expect(rail.getByRole('link', { name: '索引缓存', exact: true })).toHaveAttribute('aria-current', 'page')
  const sawLoading = await page.evaluate(() => Boolean(
    (Reflect.get(window, '__loadingProbe') as { seen: boolean }).seen,
  ))
  expect(sawLoading).toBe(false)
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
  await expect(page.getByRole('heading', { name: '隔离与拦截', exact: true })).toHaveCount(1)

  await page.goto('/admin/license')
  await expect(page.getByRole('heading', { name: 'License 与 Pro', exact: true })).toHaveCount(1)

  await page.goto('/admin/users')
  await expect(page.getByRole('heading', { name: '用户', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'API 令牌', exact: true })).toBeVisible()
})
