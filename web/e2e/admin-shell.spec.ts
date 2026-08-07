import { adminApiDefaults, test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'

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
  { id: 'overview', label: 'Overview', href: '/admin', routes: [] },
  {
    id: 'history',
    label: 'History',
    href: '/admin/logs',
    routes: ['/admin/logs', '/admin/upstream-updates', '/admin/audit', '/admin/bandwidth'],
  },
  {
    id: 'sourcesCache',
    label: 'Sources & Cache',
    href: '/admin/upstreams',
    routes: ['/admin/upstreams', '/admin/cache', '/admin/indexes', '/admin/compile-cache'],
  },
  {
    id: 'governance',
    label: 'Security Governance',
    href: '/admin/security',
    routes: ['/admin/security', '/admin/quarantine', '/admin/rules', '/admin/projects'],
  },
  {
    id: 'system',
    label: 'System',
    href: '/admin/users',
    routes: ['/admin/users', '/admin/settings', '/admin/license'],
  },
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
  await expect(pending.locator('.depsilo-logo-mark')).toBeVisible()
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

test('desktop navigation defaults every workspace open and keeps disclosure independent from navigation', async ({ page }) => {
  await mockAdminApi(page)
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/admin')

  const navigation = page.locator('[data-admin-nav-surface="sidebar"]')
  const groups = navigation.locator('[data-admin-nav-group]')
  await expect(groups).toHaveCount(5)
  expect(await groups.evaluateAll(elements => elements.map(element => element.getAttribute('data-admin-nav-group')))).toEqual(
    workspaceNavigation.map(workspace => workspace.id),
  )

  for (const workspace of workspaceNavigation) {
    const group = navigation.locator(`[data-admin-nav-group="${workspace.id}"]`)
    const workspaceLink = group.locator('[data-admin-workspace-row]').getByRole('link', { name: workspace.label, exact: true })
    await expect(workspaceLink).toContainText(workspace.label)
    await expect(workspaceLink).toHaveAttribute('href', workspace.href)
  }

  await expect(navigation.locator('[data-admin-local-navigation]')).toHaveCount(4)
  await expect(navigation.locator('a[aria-current="page"]')).toHaveCount(1)
  await expect(navigation.locator('a[aria-current="page"]')).toHaveAttribute('href', '/admin')

  for (const workspace of workspaceNavigation.slice(1)) {
    const group = navigation.locator(`[data-admin-nav-group="${workspace.id}"]`)
    await expect(group).toHaveAttribute('data-admin-nav-expanded', 'true')
    await expect(group.getByRole('button', { name: `Collapse ${workspace.label}` })).toHaveAttribute('aria-expanded', 'true')
    expect(await group.locator('[data-admin-local-navigation] a').evaluateAll(links => (
      links.map(link => link.getAttribute('href'))
    ))).toEqual(workspace.routes)
  }

  const historyGroup = navigation.locator('[data-admin-nav-group="history"]')
  await historyGroup.getByRole('button', { name: 'Collapse History' }).click()
  await expect(historyGroup).toHaveAttribute('data-admin-nav-expanded', 'false')
  await expect(historyGroup.locator('[data-admin-local-navigation]')).toHaveCount(0)
  await expect(navigation.locator('[data-admin-local-navigation]')).toHaveCount(3)
  await historyGroup.getByRole('button', { name: 'Expand History' }).click()
  await expect(navigation.locator('[data-admin-local-navigation]')).toHaveCount(4)

  for (const workspace of workspaceNavigation.slice(1)) {
    await page.goto(workspace.href)
    await expect(navigation.locator(`[data-admin-nav-group="${workspace.id}"]`)).toHaveAttribute('data-admin-nav-active', 'true')
    await expect(navigation.locator('[data-admin-local-navigation]')).toHaveCount(4)
  }

  const visibleRouteHrefs = [
    workspaceNavigation[0].href,
    ...workspaceNavigation.slice(1).flatMap(workspace => workspace.routes),
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
  await expect(navigation.locator('a[aria-current="page"]')).toHaveCount(0)
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

test('mobile drawer expands a workspace on demand before selecting a local destination', async ({ page }) => {
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

  const historyWorkspace = navigation.locator('[data-admin-nav-group="history"]')
  await expect(historyWorkspace).toHaveAttribute('data-admin-nav-expanded', 'false')
  await historyWorkspace.getByRole('button', { name: '展开历史' }).click()
  await expect(drawer).toBeVisible()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(historyWorkspace).toHaveAttribute('data-admin-nav-expanded', 'true')
  await expect(navigation.locator('[data-admin-local-navigation]')).toHaveCount(1)

  const auditLogs = historyWorkspace.getByRole('link', { name: '审计日志', exact: true })
  await auditLogs.scrollIntoViewIfNeeded()
  await expect(auditLogs).toBeVisible()
  await auditLogs.click()
  await expect(drawer).toBeHidden()
  await expect(page).toHaveURL(/\/admin\/audit$/)
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
  await expect(breadcrumb).toContainText('History')
  await expect(breadcrumb).toContainText('Upstream Updates')
  await expect(page.locator('main').getByRole('heading', { level: 1, name: 'Upstream Updates' })).toHaveCount(1)
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
  const globalWash = page.locator('#root > .page-wash')
  const main = page.locator('[data-admin-main] > main')
  const topbar = page.locator('[data-admin-topbar]')
  const sidebar = page.locator('aside')
  await expect(shell.locator(':scope > .page-wash')).toHaveCount(0)
  await expect(globalWash).toHaveCSS('z-index', '0')
  await expect(shell).toHaveCSS('position', 'relative')
  await expect(shell).toHaveCSS('z-index', '1')
  await expect(shell).toHaveCSS('background-color', 'rgb(255, 255, 255)')
  await expect(main).toHaveCSS('background-color', 'rgb(255, 255, 255)')
  await expect(topbar).toHaveCSS('background-color', 'rgb(255, 255, 255)')
  await expect(sidebar).toHaveCSS('background-color', 'rgb(243, 248, 245)')

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
  await expect(shell).toHaveCSS('background-color', 'rgb(11, 13, 15)')
  await expect(main).toHaveCSS('background-color', 'rgb(11, 13, 15)')
  await expect(topbar).toHaveCSS('background-color', 'rgb(11, 13, 15)')
  await expect(sidebar).toHaveCSS('background-color', 'rgb(26, 31, 36)')
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
