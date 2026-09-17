import { adminApiDefaults, test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

/**
 * Resolve the shell's surface colours from the semantic tokens themselves.
 * The shell is one token set: the canvas is shared by the shell, main region,
 * and topbar, and the navigation rail is the only surface that differs.
 */
async function readShellSurfaces(page: import('@playwright/test').Page) {
  return page.evaluate(() => {
    const probe = document.createElement('span')
    document.body.appendChild(probe)
    const resolve = (name: string) => {
      probe.style.color = `var(${name})`
      return getComputedStyle(probe).color
    }
    const tokens = {
      canvas: resolve('--background'),
      rail: resolve('--sidebar'),
    }
    probe.remove()
    const background = (selector: string) => {
      const element = document.querySelector(selector)
      if (!element) throw new Error(`missing shell region: ${selector}`)
      return getComputedStyle(element).backgroundColor
    }
    return {
      tokens,
      shell: background('[data-admin-shell]'),
      main: background('[data-admin-main] > main'),
      topbar: background('[data-admin-topbar]'),
      sidebar: background('aside'),
    }
  })
}

const legacyAdminHrefs = [
  '/admin',
  '/admin/attention',
  '/admin/bandwidth',
  '/admin/logs',
  '/admin/audit',
  '/admin/cache',
  '/admin/indexes',
  '/admin/compile-cache',
  '/admin/upstreams',
  '/admin/upstream-updates',
  '/admin/quarantine',
  '/admin/rules',
  '/admin/security',
  '/admin/projects',
  '/admin/users',
  '/admin/license',
  '/admin/settings',
] as const

const workspaceNavigation = [
  { id: 'overview', label: 'Overview', href: '/admin', routes: ['/admin', '/admin/bandwidth'] },
  { id: 'upstreams', label: 'Upstreams', href: '/admin/upstreams', routes: ['/admin/upstreams'] },
  { id: 'cache', label: 'Cache', href: '/admin/cache', routes: ['/admin/cache', '/admin/indexes', '/admin/compile-cache'] },
  { id: 'logs', label: 'Logs', href: '/admin/logs', routes: ['/admin/logs', '/admin/upstream-updates', '/admin/audit'] },
  { id: 'security', label: 'Security', href: '/admin/security', routes: ['/admin/security', '/admin/quarantine', '/admin/rules'] },
  { id: 'projects', label: 'Projects', href: '/admin/projects', routes: ['/admin/projects'] },
] as const

test('pending principal check shows an accessible branded loading state', async ({ page }) => {
  let releasePrincipal: (value: unknown) => void = () => undefined
  const pendingPrincipal = new Promise<unknown>(resolve => {
    releasePrincipal = resolve
  })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': () => pendingPrincipal,
  })
  await page.goto('/admin')

  const pending = page.locator('[data-admin-auth-state="pending"]')
  await expect(pending).toBeVisible()
  await expect(pending).toHaveAttribute('aria-busy', 'true')
  await expect(pending.getByRole('status')).toContainText('Depsilo')
  await expect(pending.getByRole('status')).toContainText('正在验证会话')
  await expect(pending.getByRole('status').locator('[data-brand-mark]')).toBeVisible()
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)

  releasePrincipal(adminApiDefaults['GET /api/v1/auth/me'])
  await expect(pending).toBeHidden()
  await expect(page.locator('[data-admin-outlet]')).toBeVisible()
})

test('closed mobile drawer has no focusable offscreen links', async ({ page }) => {
  const longVersion = '0.2.0-126-g43ca7fe-dirty'
  await mockAdminApi(page, {
    'GET /api/v1/stats': { service: { version: longVersion, status: 'healthy' }, week: {}, upstreams: [] },
  })
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeVisible()
  await page.keyboard.press('Tab')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
  await page.keyboard.press('Enter')
  const drawer = page.getByRole('dialog', { name: /管理导航/ })
  await expect(drawer).toBeVisible()
  const version = drawer.getByTitle(longVersion)
  const close = drawer.getByRole('button', { name: '关闭' })
  await expect(version).toBeVisible()
  await expect(close).toBeVisible()
  const boxes = await Promise.all([version.boundingBox(), close.boundingBox()])
  expect(boxes[0]).not.toBeNull()
  expect(boxes[1]).not.toBeNull()
  const [versionBox, closeBox] = boxes as [NonNullable<typeof boxes[0]>, NonNullable<typeof boxes[1]>]
  const intersects = !(
    versionBox.x + versionBox.width <= closeBox.x
    || closeBox.x + closeBox.width <= versionBox.x
    || versionBox.y + versionBox.height <= closeBox.y
    || closeBox.y + closeBox.height <= versionBox.y
  )
  expect(intersects).toBe(false)
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: /打开导航/ })).toBeFocused()
})

test('desktop navigation shows only workspaces and keeps destinations in page tabs', async ({ page }) => {
  await mockAdminApi(page)
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin')

  const navigation = page.locator('[data-admin-nav-surface="sidebar"]')
  const groups = navigation.locator('[data-admin-nav-group]')
  await expect(groups).toHaveCount(6)
  expect(await groups.evaluateAll(elements => elements.map(element => element.getAttribute('data-admin-nav-group')))).toEqual(
    workspaceNavigation.map(workspace => workspace.id),
  )

  for (const workspace of workspaceNavigation) {
    const group = navigation.locator(`[data-admin-nav-group="${workspace.id}"]`)
    const workspaceLink = group.locator('[data-admin-workspace-row]').getByRole('link', { name: workspace.label, exact: true })
    await expect(workspaceLink).toContainText(workspace.label)
    await expect(workspaceLink).toHaveAttribute('href', workspace.href)
  }

  await expect(navigation.getByRole('link')).toHaveCount(6)
  await expect(navigation.getByRole('button')).toHaveCount(0)
  await expect(navigation.locator('a[aria-current="page"]')).toHaveAttribute('href', '/admin')

  for (const workspace of workspaceNavigation) {
    await navigation.getByRole('link', { name: workspace.label, exact: true }).click()
    const group = navigation.locator(`[data-admin-nav-group="${workspace.id}"]`)
    const tabs = page.locator(`[data-admin-page-navigation="${workspace.id}"]`)
    if (workspace.routes.length === 1) {
      await expect(page).toHaveURL(new RegExp(`${workspace.href}$`))
      await expect(tabs).toHaveCount(0)
      await expect(navigation.getByRole('link')).toHaveCount(6)
      continue
    }
    await expect(tabs).toBeVisible()
    expect(await tabs.getByRole('link').evaluateAll(links => (
      links.map(link => link.getAttribute('href'))
    ))).toEqual(workspace.routes)

    for (const href of workspace.routes) {
      await tabs.locator(`a[href="${href}"]`).click()
      await expect(page).toHaveURL(new RegExp(`${href}$`))
      await expect(tabs.locator('a[aria-current="page"]')).toHaveAttribute('href', href)
      await expect(group.locator('[data-admin-workspace-current="true"]')).toBeVisible()
      await expect(group.getByRole('link')).toHaveAttribute('aria-current', href === workspace.href ? 'page' : 'location')
      await expect(navigation.getByRole('link')).toHaveCount(6)
    }
  }

  await page.locator('[data-admin-sidebar-footer]').getByRole('link', { name: 'Instance management', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/users$/)
  const instanceNavigation = page.locator('[data-admin-page-navigation="instance"]')
  await expect(instanceNavigation).toBeVisible()
  await expect(instanceNavigation.locator('a')).toHaveCount(3)

  const visibleRouteHrefs = [
    ...workspaceNavigation.flatMap(workspace => workspace.routes),
    '/admin/users',
    '/admin/settings',
    '/admin/license',
  ]
  expect([...visibleRouteHrefs].sort()).toEqual(
    legacyAdminHrefs.filter(href => href !== '/admin/attention').sort(),
  )

  await page.goto('/admin/attention')
  await expect(page.locator('[data-route-state="not-found"]')).toHaveCount(0)
  await expect(page.locator('main').getByRole('heading', { level: 1, name: 'Needs Attention' })).toBeVisible()
  await expect(navigation.locator('a[href="/admin/attention"]')).toHaveCount(0)

  await page.goto('/admin/security/unknown')
  await expect(page.locator('[data-route-state="not-found"]')).toBeVisible()
  await expect(navigation.locator('a[aria-current]')).toHaveCount(0)
})

test('desktop sign-out control becomes visibly focused for keyboard users', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin')

  const sidebar = page.locator('aside')
  const signOut = sidebar.getByRole('button', { name: /退出登录|Sign out/ })
  await expect(signOut).toHaveCSS('opacity', '0')
  await signOut.focus()
  await expect(signOut).toBeFocused()
  await expect(signOut).toHaveCSS('opacity', '1')
  await expect(signOut).toHaveCSS('outline-style', /solid|auto/)
})

test('mobile drawer selects a workspace and page tabs select its destination', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 320, height: 320 })
  await page.goto('/admin')
  const trigger = page.getByRole('button', { name: /打开导航/ })
  await trigger.click()

  const drawer = page.getByRole('dialog', { name: /管理导航/ })
  const navigation = drawer.locator('[data-admin-nav-surface="drawer"]')
  await expect(drawer.locator('[data-admin-sidebar-header]')).toBeInViewport()
  await expect(drawer.locator('[data-admin-sidebar-footer]')).toBeInViewport()
  await expect(navigation.getByRole('link', { name: '总览', exact: true })).toBeFocused()

  await expect(navigation.getByRole('link')).toHaveCount(6)
  await expect(navigation.getByRole('button')).toHaveCount(0)
  await navigation.getByRole('link', { name: '日志', exact: true }).click()
  await expect(drawer).toBeHidden()
  await expect(page).toHaveURL(/\/admin\/logs$/)

  const auditLogs = page.locator('[data-admin-page-navigation="logs"]').getByRole('link', { name: '审计日志', exact: true })
  await auditLogs.scrollIntoViewIfNeeded()
  await auditLogs.click()
  await expect(page).toHaveURL(/\/admin\/audit$/)
  await trigger.click()
  await expect(navigation.getByRole('link', { name: '日志', exact: true })).toBeFocused()
  await expect(navigation.getByRole('link', { name: '日志', exact: true })).toHaveAttribute('aria-current', 'location')
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('desktop shell uses a 232px rail and separates breadcrumb from the page heading', async ({ page }) => {
  await mockAdminApi(page)
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin/upstream-updates')

  const sidebar = page.locator('aside')
  const mainColumn = page.locator('[data-admin-main]')
  const topbar = page.locator('[data-admin-topbar]')
  const breadcrumb = topbar.locator('[data-admin-breadcrumb]')

  await expect(sidebar).toHaveCSS('width', '232px')
  await expect(mainColumn).toHaveCSS('margin-left', '232px')
  await expect(topbar).toHaveCSS('left', '232px')
  await expect(topbar.getByRole('heading')).toHaveCount(0)
  await expect(breadcrumb).toContainText('Logs')
  await expect(breadcrumb).toContainText('Metadata Refreshes')
  await expect(page.locator('main').getByRole('heading', { level: 1, name: 'Metadata Refreshes' })).toHaveCount(1)
})

test('full live status stays on Dashboard and is removed from the topbar', async ({ page }) => {
  await mockAdminApi(page)
  await page.setViewportSize({ width: 390, height: 844 })

  await page.goto('/admin')
  await expect(page.locator('main [data-query-key="now"]')).toBeVisible()
  const topbar = page.locator('[data-admin-topbar]')
  await expect(topbar).toHaveCSS('height', '48px')
  await expect(topbar.locator('[data-admin-service-status]')).toHaveCount(0)
  await expect(topbar).not.toContainText(/部分降级|性能下降|degraded|请求\/分钟|req\/min|出口|egress/i)

  await page.goto('/admin/security')
  await expect(page.locator('main [data-query-key="now"]')).toHaveCount(0)
  await expect(page.locator('[data-admin-service-status]')).toHaveCount(0)
})

test('desktop Admin chrome uses a clean canvas, brand portal link, and labeled theme control', async ({ page }) => {
  await mockAdminApi(page)
  await setUiPreferences(page, 'light', 'zh')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin/security')

  const shell = page.locator('[data-admin-shell]')
  const topbar = page.locator('[data-admin-topbar]')
  await expect(shell).toBeVisible()
  // The Admin shell no longer forks the palette, and no decorative overlay sits
  // behind it: every chrome surface resolves from the single token cascade.
  await expect(page.locator('.page-wash')).toHaveCount(0)
  const light = await readShellSurfaces(page)
  expect(light.sidebar).toBe(light.tokens.rail)
  expect(light.shell).toBe(light.tokens.canvas)
  expect(light.main).toBe(light.tokens.canvas)
  expect(light.topbar).toBe(light.tokens.canvas)
  expect(light.tokens.rail).not.toBe(light.tokens.canvas)

  const brandLink = page.locator('[data-admin-nav-surface="sidebar"]')
    .locator('..')
    .getByRole('link', { name: '返回门户' })
  await expect(brandLink).toHaveAttribute('href', '/')
  await expect(topbar.getByRole('link', { name: '返回门户' })).toHaveCount(0)

  const themeToggle = topbar.locator('[data-theme-toggle="labeled"]')
  await expect(themeToggle).toContainText('外观：浅色')
  await themeToggle.click()
  await expect(themeToggle).toContainText('外观：深色')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  const dark = await readShellSurfaces(page)
  expect(dark.shell).toBe(dark.tokens.canvas)
  expect(dark.main).toBe(dark.tokens.canvas)
  expect(dark.topbar).toBe(dark.tokens.canvas)
  expect(dark.sidebar).toBe(dark.tokens.rail)
  expect(dark.tokens.canvas).not.toBe(light.tokens.canvas)
  expect(dark.tokens.rail).not.toBe(light.tokens.rail)
  expect(await page.evaluate(() => localStorage.getItem('depsilo-theme'))).toBe('dark')

  await themeToggle.click()
  await expect(themeToggle).toContainText('外观：跟随系统')
  expect(await page.evaluate(() => localStorage.getItem('depsilo-theme'))).toBe('system')
  const resolvedSystemTheme = await page.evaluate(() => (
    window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  ))
  await expect(page.locator('html')).toHaveAttribute('data-theme', resolvedSystemTheme)

  await themeToggle.click()
  await expect(themeToggle).toContainText('外观：浅色')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  expect(await page.evaluate(() => localStorage.getItem('depsilo-theme'))).toBe('light')
})

test('mobile Admin keeps the theme mode readable and exposes Portal through the drawer brand', async ({ page }) => {
  await mockAdminApi(page)
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/settings')

  const themeToggle = page.locator('[data-admin-topbar] [data-theme-toggle="labeled"]')
  await expect(themeToggle).toBeVisible()
  await expect(themeToggle).toHaveAttribute('aria-label', 'Appearance: Light')
  expect((await themeToggle.innerText()).trim()).toBe('Light')
  const themeToggleBox = await themeToggle.boundingBox()
  expect(themeToggleBox?.width).toBeGreaterThanOrEqual(40)
  expect(themeToggleBox?.height).toBeGreaterThanOrEqual(40)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)

  await page.getByRole('button', { name: 'Open navigation' }).click()
  const drawer = page.getByRole('dialog', { name: 'Admin navigation' })
  const brandLink = drawer.getByRole('link', { name: 'Back to Portal' })
  await expect(brandLink).toHaveAttribute('href', '/')
  await brandLink.click()
  await expect(page).toHaveURL(/\/$/)
  await expect(drawer).toBeHidden()
})

test('failed now request never displays healthy', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/now': { status: 500, body: { code: 'FAILED', message: 'down' } },
  })
  await page.goto('/admin')
  const fullStatus = page.locator('main [data-query-key="now"]')
  await expect(fullStatus.getByText(/状态不可用/)).toBeVisible()
  await expect(fullStatus.getByText(/健康|已就绪/)).toHaveCount(0)
})

test('stale cached status keeps its refresh action visible', async ({ page }) => {
  let calls = 0
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/now': () => {
      calls += 1
      return calls === 1
        ? adminApiDefaults['GET /api/v1/now']
        : { status: 500, body: { code: 'FAILED', message: 'down' } }
    },
  })
  await page.goto('/admin')
  const fullStatus = page.locator('main [data-query-key="now"]')
  const staleLabel = fullStatus.getByText('数据已过期').first()
  const refresh = fullStatus.getByRole('button', { name: '刷新' })
  await expect(staleLabel).toBeVisible({ timeout: 10_000 })
  await expect(refresh).toBeVisible()
  await expect(refresh).toBeInViewport()
  const strip = fullStatus
  expect(await refresh.evaluate((button, root) => {
    const buttonBox = button.getBoundingClientRect()
    const rootBox = (root as HTMLElement).getBoundingClientRect()
    return buttonBox.right <= rootBox.right && buttonBox.left >= rootBox.left
  }, await strip.elementHandle())).toBe(true)
})

test('principal failure gates the outlet until Retry succeeds', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': () => {
      calls += 1
      return calls === 1
        ? { status: 500, body: { code: 'FAILED', message: 'principal unavailable' } }
        : adminApiDefaults['GET /api/v1/auth/me']
    },
  })
  await page.goto('/admin')
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.locator('[data-admin-outlet]')).toHaveCount(0)
  await page.getByRole('button', { name: /重试/ }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.locator('[data-admin-outlet]')).toBeVisible()
})
