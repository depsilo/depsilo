import AxeBuilder from '@axe-core/playwright'

import { adminApiDefaults, expect, mockAdminApi, test } from './fixtures/admin-api'

test('setup status failure blocks every app branch until a validated retry succeeds', async ({ page }) => {
  let setupAttempts = 0
  let authRequests = 0
  let statsRequests = 0
  let adminCacheRequests = 0
  let releaseConfigured!: (value: unknown) => void
  const configured = new Promise<unknown>(resolve => { releaseConfigured = resolve })
  const appModuleRequests: string[] = []

  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (/\/src\/(?:admin\/AdminApp|portal\/PortalApp|setup\/SetupWizard)\.tsx$/.test(pathname)) {
      appModuleRequests.push(pathname)
    }
  })
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': () => {
      setupAttempts += 1
      return setupAttempts === 1
        ? { status: 503, body: { code: 'UNAVAILABLE', message: 'setup status unavailable' } }
        : configured
    },
    'GET /api/v1/auth/me': () => {
      authRequests += 1
      return adminApiDefaults['GET /api/v1/auth/me']
    },
    'GET /api/v1/stats': () => {
      statsRequests += 1
      return adminApiDefaults['GET /api/v1/stats']
    },
    'GET /api/v1/admin/cache/distribution': () => {
      adminCacheRequests += 1
      return adminApiDefaults['GET /api/v1/admin/cache/distribution']
    },
  })

  await page.goto('/admin/cache?search=requests#results')

  const failure = page.getByRole('alert')
  await expect(failure).toContainText('无法确认 Depsilo 是否已完成初始化')
  await expect(failure).toBeFocused()
  await expect(page).toHaveURL(/\/admin\/cache\?search=requests#results$/)
  expect(setupAttempts).toBe(1)
  expect(authRequests).toBe(0)
  expect(statsRequests).toBe(0)
  expect(adminCacheRequests).toBe(0)
  expect(appModuleRequests).toEqual([])

  const retry = failure.getByRole('button')
  await retry.click()
  await expect(page.locator('[data-setup-gate-state]')).toHaveAttribute(
    'data-setup-gate-state',
    'unavailable',
  )
  await expect(retry).toBeDisabled()
  await expect(retry).toHaveAttribute('aria-busy', 'true')
  await expect(retry).toHaveText('检查中...')
  await expect.poll(() => setupAttempts).toBe(2)

  releaseConfigured({ needs_setup: false, token_required: false, future_field: true })

  await expect(page.locator('[data-admin-shell]')).toBeVisible()
  await expect(page).toHaveURL(/\/admin\/cache\?search=requests#results$/)
  await expect.poll(() => authRequests).toBeGreaterThan(0)
  await expect.poll(() => adminCacheRequests).toBeGreaterThan(0)
})

const malformedSetupStatuses: Array<[string, unknown]> = [
  ['null', null],
  ['object without fields', {}],
  ['missing token_required', { needs_setup: false }],
  ['string needs_setup', { needs_setup: 'false', token_required: false }],
  ['array', []],
]

for (const [name, response] of malformedSetupStatuses) {
  test(`malformed setup status (${name}) fails closed`, async ({ page }) => {
    let statsRequests = 0
    await mockAdminApi(page, {
      'GET /api/v1/setup/status': response,
      'GET /api/v1/stats': () => {
        statsRequests += 1
        return adminApiDefaults['GET /api/v1/stats']
      },
    })

    await page.goto('/')

    await expect(page.getByRole('alert')).toContainText('无法确认 Depsilo 是否已完成初始化')
    await expect(page.getByRole('heading', { name: '快速开始' })).toHaveCount(0)
    expect(statsRequests).toBe(0)
  })
}

test('setup-required status owns an Admin deep link before auth or page data can load', async ({ page }) => {
  let authRequests = 0
  let statsRequests = 0
  const applicationModuleRequests: string[] = []
  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (/\/src\/(?:admin\/AdminApp|portal\/PortalApp)\.tsx$/.test(pathname)) {
      applicationModuleRequests.push(pathname)
    }
  })
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
    'GET /api/v1/auth/me': () => {
      authRequests += 1
      return adminApiDefaults['GET /api/v1/auth/me']
    },
    'GET /api/v1/stats': () => {
      statsRequests += 1
      return adminApiDefaults['GET /api/v1/stats']
    },
  })

  await page.goto('/admin/cache?source=setup#blocked')

  await expect(page.getByRole('button', { name: '开始' })).toBeVisible()
  await expect(page).toHaveURL(/\/admin\/cache\?source=setup#blocked$/)
  expect(authRequests).toBe(0)
  expect(statsRequests).toBe(0)
  expect(applicationModuleRequests).toEqual([])
})

test('Portal unknown paths keep the shell without loading a page route', async ({ page }) => {
  const pageModuleRequests: string[] = []
  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (/\/src\/portal\/pages\/(?:QuickStart|Monitor)\.tsx$/.test(pathname)) {
      pageModuleRequests.push(pathname)
    }
  })

  await page.goto('/does-not-exist?source=404#missing')

  await expect(page.locator('header')).toBeVisible()
  const navigation = page.getByRole('navigation', { name: '门户导航' })
  await expect(navigation).toBeVisible()
  await expect(navigation.locator('a[aria-current="page"]')).toHaveCount(0)
  await expect(navigation.locator('a button')).toHaveCount(0)
  const notFound = page.locator('[data-route-state="not-found"]')
  await expect(notFound.getByRole('heading', { name: '页面不存在' })).toBeVisible()
  await expect(notFound).toBeFocused()
  expect(pageModuleRequests).toEqual([])
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])

  await notFound.getByRole('link', { name: '返回快速开始' }).click()

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: '快速开始' })).toBeVisible()
  await expect(navigation.getByRole('link', { name: '快速开始' })).toHaveAttribute('aria-current', 'page')
  expect(pageModuleRequests.some(pathname => pathname.endsWith('/QuickStart.tsx'))).toBe(true)
})

test('authenticated Admin unknown paths render 404 inside the shell without dashboard work', async ({ page }) => {
  let dashboardRequests = 0
  const pageModuleRequests: string[] = []
  page.on('request', request => {
    const pathname = new URL(request.url()).pathname
    if (/\/src\/admin\/pages\/.+\.tsx$/.test(pathname)) pageModuleRequests.push(pathname)
  })
  await mockAdminApi(page, {
    'GET /api/v1/admin/dashboard': () => {
      dashboardRequests += 1
      return adminApiDefaults['GET /api/v1/admin/dashboard']
    },
  })

  await page.goto('/admin/does-not-exist?source=404#missing')

  await expect(page.locator('[data-admin-shell]')).toBeVisible()
  await expect(page.locator('header').getByRole('heading', { name: '页面不存在' })).toBeVisible()
  const notFound = page.locator('[data-route-state="not-found"]')
  await expect(notFound.getByRole('heading', { name: '页面不存在' })).toBeVisible()
  await expect(notFound).toBeFocused()
  expect(dashboardRequests).toBe(0)
  expect(pageModuleRequests).toEqual([])
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])

  await notFound.getByRole('link', { name: '返回管理总览' }).click()

  await expect(page).toHaveURL(/\/admin$/)
  await expect.poll(() => dashboardRequests).toBe(1)
})

test('Admin title derivation follows case-insensitive route matching', async ({ page }) => {
  await page.goto('/ADMIN/CACHE')

  await expect(page.locator('header').getByRole('heading', { name: '缓存管理' })).toBeVisible()
  await expect(page.locator('[data-route-state="not-found"]')).toHaveCount(0)
})

test.describe('unauthenticated Admin routing', () => {
  test.use({ initialToken: null })

  test('unknown paths reach login before the protected shell or 404', async ({ page }) => {
    const protectedModuleRequests: string[] = []
    page.on('request', request => {
      const pathname = new URL(request.url()).pathname
      if (/\/src\/admin\/(?:AdminShell|pages\/(?!Login)[^/]+)\.tsx$/.test(pathname)) {
        protectedModuleRequests.push(pathname)
      }
    })

    await page.goto('/admin/does-not-exist?source=404#missing')

    await expect(page).toHaveURL(/\/admin\/login$/)
    await expect(page.getByRole('heading', { name: '管理后台' })).toBeVisible()
    await expect(page.locator('[data-admin-shell]')).toHaveCount(0)
    await expect(page.locator('[data-route-state="not-found"]')).toHaveCount(0)
    expect(protectedModuleRequests).toEqual([])
  })
})
