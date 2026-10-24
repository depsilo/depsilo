import { adminApiDefaults, expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const reader = {
  id: 2, username: 'reader', role: 'readonly', enabled: true,
  auth_method: 'jwt', token_permissions: null, can_write: false,
  permission_source: 'local', login_method: 'password', can_manage_credentials: false,
}

for (const path of ['/admin/users', '/admin/authentication', '/admin/settings', '/admin/license']) {
  test(`readonly operators cannot open ${path} or request its protected page data`, async ({ page }) => {
    const protectedCalls: string[] = []
    page.on('request', request => {
      if (/\/api\/v1\/admin\/(users|tokens|authentication(?:\/sources(?:\/[^/?]+)?)?|settings|license\/status|webhooks)(?:\?|$)/.test(request.url())) protectedCalls.push(request.url())
    })
    await mockAdminApi(page, { 'GET /api/v1/auth/me': reader })
    await page.goto(path)
    const denial = page.locator('[data-admin-access="denied"]')
    await expect(denial.getByRole('heading', { name: '需要管理员权限' })).toBeVisible()
    await expect(page.locator('[data-admin-page-navigation="instance"]')).toHaveCount(0)
    await expect(page.getByRole('link', { name: '系统管理', exact: true })).toHaveCount(0)
    await expect(page.locator('aside').getByRole('link', { name: '账户安全', exact: true })).toBeVisible()
    expect(protectedCalls).toEqual([])
    await denial.getByRole('link', { name: '返回总览', exact: true }).click()
    await expect(page).toHaveURL(/\/admin$/)
  })
}

for (const session of [
  { auth_method: 'jwt', login_method: 'oidc', token_permissions: null, can_write: true },
  { auth_method: 'jwt', login_method: 'ldap', token_permissions: null, can_write: true },
  { auth_method: 'api_token', login_method: 'api_token', token_permissions: 'readonly', can_write: false },
]) {
  test(`an admin using ${session.login_method} can see system management without credential write permission`, async ({ page }) => {
    await mockAdminApi(page, {
      'GET /api/v1/auth/me': { id: 1, username: 'admin', role: 'admin', enabled: true, permission_source: 'local', can_manage_credentials: false, ...session },
      'GET /api/v1/admin/onboarding/status': { status: 'not_started' },
    })
    await page.goto('/admin/users')
    await expect(page.locator('aside').getByRole('link', { name: '系统管理', exact: true })).toBeVisible()
    await expect(page.locator('[data-admin-access="denied"]')).toHaveCount(0)
    await expect(page.locator('[data-admin-page-navigation="instance"]').getByRole('link', { name: '身份认证', exact: true })).toHaveAttribute('href', '/admin/authentication')
    await expect(page).toHaveURL(/\/admin\/users$/)
  })
}

test('a readonly user can still open personal account security on mobile', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await page.setViewportSize({ width: 320, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': reader,
    'GET /api/v1/admin/onboarding/status': { status: 'not_started' },
    'GET /api/v1/auth/identities': adminApiDefaults['GET /api/v1/auth/identities'],
  })
  await page.goto('/admin/account')
  await expect(page.getByRole('heading', { level: 1, name: 'Account security', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Open navigation', exact: true }).click()
  const drawer = page.locator('#admin-mobile-navigation')
  await expect(drawer.getByRole('link', { name: 'System management', exact: true })).toHaveCount(0)
  await expect(drawer.getByRole('link', { name: 'Account security', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})


test('local administrators can disable LDAP accounts without editing their directory roles or passwords', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/users': [{ id: 2, username: 'directory-operator', role: 'readonly', enabled: true, permission_source: 'ldap', created_at: '2026-10-23T08:00:00Z', updated_at: '2026-10-23T08:00:00Z', last_login_at: null }],
  })
  await page.goto('/admin/users')
  const row = page.getByRole('row').filter({ hasText: 'directory-operator' })
  await expect(row).toContainText('外部身份提供方')
  await expect(row.getByRole('button', { name: '编辑 directory-operator' })).toHaveCount(0)
  await expect(row.getByRole('button', { name: '禁用 directory-operator' })).toBeVisible()
})
