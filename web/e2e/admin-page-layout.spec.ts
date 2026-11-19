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

test('single pages and tabbed workspaces share one header band', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const band = async (path: string) => {
    await page.goto(path)
    const title = page.locator('[data-admin-page-header] .admin-page-navigation')
    await expect(title).toBeVisible()
    return title.evaluate(element => {
      const box = element.getBoundingClientRect()
      return { top: Math.round(box.top), height: Math.round(box.height) }
    })
  }
  // Projects has no sibling destinations; Users is a tab in System. Moving
  // between them must not shift the top of the page.
  const single = await band('/admin/projects')
  const tabbed = await band('/admin/users')
  expect(single).toEqual(tabbed)
  await page.goto('/admin/projects')
  await expect(page.getByRole('heading', { level: 1, name: '项目管理' })).toBeVisible()
})

test('tables name ecosystems by product and keep routine states quiet', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockAdminApi(page, {
    'GET /api/v1/admin/logs': {
      items: [{ id: 1, adapter_type: 'crates', method: 'GET', cache_key: 'k', package_name: 'serde', hit: true, cache_result: 'hit', upstream: 'rsproxy', latency_ms: 12, status_code: 200, client_ip: '10.0.0.2', bytes_sent: 1024, created_at: '2026-11-15T08:00:00Z' }],
      total: 1, page: 1, page_size: 50,
    },
    'GET /api/v1/admin/cache/indexes': {
      items: [{ id: 7, key: 'npm/left-pad', adapter_type: 'npm', package_name: 'left-pad', size: 2048, hit_count: 3, etag: '', last_modified: '', last_accessed: '2026-11-15T08:00:00Z', expires_at: '2026-11-15T07:00:00Z', updated_at: '2026-11-15T06:00:00Z', status: 'stale' }],
      summary: [{ adapter_type: 'npm', total: 1, fresh: 0, stale: 1, last_updated: '2026-11-15T06:00:00Z' }],
      total: 1, page: 1, page_size: 25,
    },
  })

  await page.goto('/admin/logs')
  const logs = page.locator('[data-admin-page-content]')
  // An internal alias is shown by the product name an Operator knows.
  await expect(logs.getByRole('cell', { name: 'Cargo' })).toBeVisible()
  const head = logs.locator('th').first()
  await expect(head).toHaveCSS('text-transform', 'none')
  await expect(head).toHaveCSS('font-weight', '500')
  await expect(head).toHaveCSS('font-size', '12px')

  await page.goto('/admin/indexes')
  const stale = page.locator('[data-admin-page-content] table').getByText('已过期', { exact: true })
  await expect(stale).toBeVisible()
  // A stale index is routine: it must not wear the warning colour.
  const warn = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--warn-text').trim())
  const color = await stale.evaluate(element => getComputedStyle(element).color)
  expect(color).not.toBe(warn)
  expect(await stale.evaluate(element => element.closest('[data-slot="badge"]')?.className ?? '')).not.toContain('warn')
  await expect(page.locator('[data-admin-page-content]').getByText('npm', { exact: true }).first()).toBeVisible()
})
