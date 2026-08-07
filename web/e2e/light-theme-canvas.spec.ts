import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page } from '@playwright/test'
import {
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

async function expectPureWhiteCanvas(page: Page) {
  await expect(page.locator('body')).toHaveCSS(
    'background-color',
    'rgb(255, 255, 255)',
  )
  await expect.poll(() => page.evaluate(() => (
    getComputedStyle(document.documentElement)
      .getPropertyValue('--bg-page')
      .trim()
  ))).toBe('#FFFFFF')

  const wash = page.locator('.page-wash')
  await expect(wash).toHaveCount(1)
  await expect.poll(() => wash.evaluate(element => (
    getComputedStyle(element, '::before').opacity
  ))).toBe('0')
}

async function expectPureWhiteSurface(surface: Locator) {
  await expect(surface).toHaveCSS('background-color', 'rgb(255, 255, 255)')
}

const lightPortalRoutes = [
  { route: '/', readyName: '快速开始' },
  { route: '/monitor', readyName: '实时监控' },
  { route: '/does-not-exist', readyName: '页面不存在' },
]

for (const { route, readyName } of lightPortalRoutes) {
  test(`light Portal ${route} uses one untextured pure-white canvas`, async ({ page }) => {
    await setUiPreferences(page, 'light', 'zh')
    await page.goto(route)

    await expect(page.getByRole('heading', { name: readyName })).toBeVisible()
    await expectPureWhiteCanvas(page)
    await expectPureWhiteSurface(page.locator('#root > .min-h-screen'))

    if (route === '/does-not-exist') {
      const notFound = page.locator('[data-route-state="not-found"]')
      await expect(notFound).toBeFocused()
      await expect(notFound).toHaveCSS('outline-style', 'none')
      const returnLinkBox = await notFound.getByRole('link', { name: '返回快速开始' }).boundingBox()
      expect(returnLinkBox?.height).toBeGreaterThanOrEqual(40)
    }
  })
}

test('light Setup keeps a pure-white canvas through checking, recovery, and first-run configuration', async ({ page }) => {
  let setupAttempts = 0
  let releaseInitialCheck!: (value: unknown) => void
  const initialCheck = new Promise<unknown>(resolve => {
    releaseInitialCheck = resolve
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'zh')
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': () => {
      setupAttempts += 1
      return setupAttempts === 1
        ? initialCheck
        : { needs_setup: true, token_required: false }
    },
  })
  await page.goto('/')

  await expect(page.locator('[data-setup-gate-state="checking"]')).toBeVisible()
  await expectPureWhiteCanvas(page)

  releaseInitialCheck({
    status: 503,
    body: { code: 'UNAVAILABLE', message: 'setup status unavailable' },
  })

  const unavailable = page.locator('[data-setup-gate-state="unavailable"]')
  await expect(unavailable).toBeVisible()
  await expect(unavailable).toBeFocused()
  await expect(unavailable).toHaveCSS('outline-style', 'none')
  await expectPureWhiteCanvas(page)

  await unavailable.getByRole('button', { name: '重试检查' }).click()
  await expect(page.getByText('Depsilo', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
  await expect(page.getByRole('button', { name: '完成初始化' })).toBeVisible()
  await expect(page.locator('details')).not.toHaveAttribute('open', '')
  await expectPureWhiteCanvas(page)
  await expectPureWhiteSurface(page.locator('#root > main'))
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  const submitBox = await page.getByRole('button', { name: '完成初始化' }).boundingBox()
  expect(submitBox?.height).toBeGreaterThanOrEqual(40)
  expect(
    (await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze()).violations,
  ).toEqual([])
})

test.describe('without an Admin session', () => {
  test.use({ initialToken: null })

  for (const { width, height } of [
    { width: 390, height: 844 },
    { width: 390, height: 430 },
    { width: 1440, height: 900 },
  ]) {
    test(`light Login uses a restrained single-column canvas at ${width}x${height}`, async ({ page }) => {
      await page.setViewportSize({ width, height })
      await setUiPreferences(page, 'light', 'zh')
      await page.goto('/admin')

      await expect(page).toHaveURL(/\/admin\/login$/)
      await expect(page.getByRole('heading', { name: '管理后台' })).toBeVisible()

      const portalLink = page.getByRole('link', { name: '返回门户' })
      const portalLinkBox = await portalLink.boundingBox()
      expect(portalLinkBox?.height).toBeGreaterThanOrEqual(40)
      const loginSurface = page.getByRole('main').locator(':scope > div')
      const surfaceBox = await loginSurface.boundingBox()
      expect(surfaceBox?.width).toBeLessThanOrEqual(420)
      await expect(page.getByRole('button', { name: '登录' })).toBeVisible()

      await expectPureWhiteCanvas(page)
      await expectPureWhiteSurface(page.locator('#root > .min-h-screen'))
      expect(
        (await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa'])
          .analyze()).violations,
      ).toEqual([])
    })
  }
})

test('dark Portal keeps one subtle grain layer', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'zh')
  await page.goto('/')

  await expect(page.locator('body')).toHaveCSS(
    'background-color',
    'rgb(11, 13, 15)',
  )
  const wash = page.locator('.page-wash')
  await expect(wash).toHaveCount(1)
  await expect.poll(() => wash.evaluate(element => (
    getComputedStyle(element, '::before').opacity
  ))).toBe('0.07')
})
