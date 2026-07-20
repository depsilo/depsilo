import { expect, test } from './fixtures/admin-api'

const portalAppModule = /\/(?:src\/portal\/PortalApp\.tsx|assets\/PortalApp-[^/?]+\.js)(?:\?.*)?$/
const monitorModule = /\/(?:src\/portal\/pages\/Monitor\.tsx|assets\/Monitor-[^/?]+\.js)(?:\?.*)?$/

test('top-level lazy route failure refreshes the same URL and recovers', async ({ page }) => {
  let moduleRequests = 0
  await page.addInitScript(() => {
    const count = Number(sessionStorage.getItem('route-document-loads') || '0')
    sessionStorage.setItem('route-document-loads', String(count + 1))
  })
  await page.route(portalAppModule, async route => {
    moduleRequests += 1
    if (moduleRequests === 1) {
      await route.abort('failed')
      return
    }
    await route.continue()
  })

  await page.goto('/?source=route-boundary#quickstart')

  const failure = page.getByRole('alert')
  await expect(failure).toContainText('页面无法加载')
  await expect(failure).toContainText('页面资源可能已更新或暂时不可用')
  await expect(failure).toBeFocused()
  await expect(page.locator('header')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('route-document-loads'))).toBe('1')

  await failure.getByRole('button', { name: '刷新页面' }).click()

  await expect(page).toHaveURL(/\/\?source=route-boundary#quickstart$/)
  await expect(page.getByRole('heading', { name: '装得快，也装得放心' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('route-document-loads'))).toBe('2')
  expect(moduleRequests).toBeGreaterThanOrEqual(2)
})

test('nested lazy route failure preserves the Portal shell and recovers after refresh', async ({ page }) => {
  let moduleRequests = 0
  await page.route(monitorModule, async route => {
    moduleRequests += 1
    if (moduleRequests === 1) {
      await route.abort('failed')
      return
    }
    await route.continue()
  })

  await page.goto('/monitor?source=route-boundary#upstreams')

  await expect(page.locator('header')).toBeVisible()
  await expect(page.getByRole('link', { name: '快速开始' })).toBeVisible()
  const failure = page.locator('main').getByRole('alert')
  await expect(failure).toContainText('页面无法加载')
  await expect(failure).toBeFocused()

  await failure.getByRole('button', { name: '刷新页面' }).click()

  await expect(page).toHaveURL(/\/monitor\?source=route-boundary#upstreams$/)
  await expect(page.locator('header')).toBeVisible()
  await expect(page.getByRole('heading', { name: '实时监控' })).toBeVisible()
  expect(moduleRequests).toBeGreaterThanOrEqual(2)
})
