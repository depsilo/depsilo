import AxeBuilder from '@axe-core/playwright'
import type { Page, Request } from '@playwright/test'
import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'
import type { LDAPAuthenticationSourceState, LoginIdentity, Principal } from '../src/lib/adminApi.types'

const principal: Principal = { id: 1, username: 'admin', enabled: true, role: 'admin', auth_method: 'jwt', token_permissions: null, can_write: true, permission_source: 'local', login_method: 'password', can_manage_credentials: true }
const provider = { id: 'directory', name: 'Company directory', protocol: 'ldap' as const, enabled: true, can_link: true }
const config = { enabled: false, display_name: provider.name, admin_groups: ['cn=operators,dc=example'], readonly_groups: ['cn=readers,dc=example'], session_ttl: '30m', ldap: { url: 'ldaps://directory.example:636', bind_dn: 'cn=search,dc=example', bind_password_set: true, user_base_dn: 'ou=people,dc=example', user_filter: '(uid={username})', subject_attribute: 'entryUUID', display_name_attribute: 'displayName', groups_attribute: 'memberOf', ca_cert_pem: '' } }
const source: LDAPAuthenticationSourceState = { id: provider.id, protocol: 'ldap', legacy: false, configured: config, effective: config, overrides: {}, config_writable: true, pending_restart: false }

async function serveInsecureOrigin(page: Page, baseURL: string | undefined) {
  await page.route('http://depsilo.example.test/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.startsWith('/api/')) return route.fallback()
    const response = await route.fetch({ url: new URL(url.pathname + url.search, baseURL).toString() })
    await route.fulfill({ response })
  })
}

test('insecure remote origins cannot collect or submit LDAP login credentials while local sign-in remains available', async ({ page, baseURL }) => {
  let directoryRequests = 0
  await serveInsecureOrigin(page, baseURL)
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: [provider] },
    'POST /api/v1/auth/ldap/login': () => { directoryRequests++; return { status: 403, body: {} } },
  })
  await page.goto('http://depsilo.example.test/admin/login')
  await expect(page.getByLabel('密码', { exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '使用 Company directory 登录' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/目录凭据需要通过 HTTPS/)).toBeVisible()
  await expect(dialog.getByLabel('目录用户名')).toBeDisabled()
  await expect(dialog.getByLabel('目录密码')).toBeDisabled()
  await expect(dialog.getByRole('button', { name: '登录', exact: true })).toBeDisabled()
  for (const [label, value] of [['目录用户名', 'directory-user'], ['目录密码', 'must-not-send']]) {
    const input = dialog.getByLabel(label)
    await input.evaluate(element => element.removeAttribute('disabled'))
    await input.fill(value)
  }
  await dialog.locator('form').evaluate(form => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
  await expect(dialog.getByLabel('目录密码')).toHaveValue('')
  expect(directoryRequests).toBe(0)
})

test('insecure remote origins cannot submit either LDAP binding proof', async ({ page, baseURL }) => {
  let bindings = 0
  await serveInsecureOrigin(page, baseURL)
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/auth/identities': { permission_source: 'local', password_enabled: true, can_link: true, provider: null, providers: [provider], identities: [] },
    'POST /api/v1/auth/identities/ldap/link': () => { bindings++; return { status: 403, body: {} } },
  })
  await page.goto('http://depsilo.example.test/admin/account')
  await page.getByRole('button', { name: '绑定 Company directory' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText(/目录凭据需要通过 HTTPS/)).toBeVisible()
  for (const [label, value] of [['当前本地密码', 'local-proof'], ['目录用户名', 'directory-user'], ['目录密码', 'directory-proof']]) {
    const input = dialog.getByLabel(label)
    await expect(input).toBeDisabled()
    await input.evaluate(element => element.removeAttribute('disabled'))
    await input.fill(value)
  }
  await dialog.locator('form').evaluate(form => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
  await expect(dialog.getByLabel('目录密码')).toHaveValue('')
  await expect(dialog.getByLabel('当前本地密码')).toHaveValue('')
  expect(bindings).toBe(0)
})

test('insecure LDAP editor blocks credentials and diagnostics but permits a non-secret name update', async ({ page, baseURL }) => {
  let credentialRequests = 0
  let saved: unknown
  await serveInsecureOrigin(page, baseURL)
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/admin/authentication/sources': { sources: [source], config_writable: true },
    'POST /api/v1/admin/authentication/ldap/check': () => { credentialRequests++; return { status: 403, body: {} } },
    'POST /api/v1/admin/authentication/ldap/trial': () => { credentialRequests++; return { status: 403, body: {} } },
    'PUT /api/v1/admin/authentication/sources/directory': (request: Request) => {
      saved = request.postDataJSON()
      return { ...source, configured: { ...config, display_name: 'Renamed directory' }, pending_restart: true }
    },
  })
  await page.goto('http://depsilo.example.test/admin/authentication?source=directory')
  await expect(page.getByText(/目录凭据需要通过 HTTPS/)).toBeVisible()
  await expect(page.getByLabel('服务账户密码')).toBeDisabled()
  await expect(page.getByRole('button', { name: '检查配置', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '验证目录登录', exact: true })).toBeDisabled()
  for (const [label, value] of [['目录用户名', 'directory-user'], ['目录密码', 'must-not-send']]) {
    const input = page.getByLabel(label, { exact: true })
    await expect(input).toBeDisabled()
    await input.evaluate(element => element.removeAttribute('disabled'))
    await input.fill(value)
  }
  await page.getByLabel('目录密码', { exact: true }).evaluate(input => input.closest('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))
  await expect(page.getByLabel('目录密码', { exact: true })).toHaveValue('')
  expect(credentialRequests).toBe(0)
  await page.getByLabel('认证源名称').fill('Renamed directory')
  await page.getByRole('button', { name: '检查并保存' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  expect(saved).toEqual({ display_name: 'Renamed directory' })
})

test('LDAP sources are added through protocol choice with a typed draft and no password echo', async ({ page }) => {
  let payload: Record<string, unknown> | undefined
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/admin/authentication/sources': { sources: [], config_writable: true },
    'POST /api/v1/admin/authentication/sources': (request: Request) => { payload = request.postDataJSON(); return { status: 201, body: { ...source, effective: null, pending_restart: true } } },
  })
  await page.goto('/admin/authentication')
  await page.getByRole('button', { name: '添加认证源', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: /^LDAP \/ LDAPS/ }).click()
  await page.getByLabel('认证源名称').fill(provider.name)
  await page.getByLabel('目录地址').fill(config.ldap.url)
  await page.getByLabel('服务账户 Bind DN').fill(config.ldap.bind_dn)
  await page.getByLabel('服务账户密码').fill('service-fixture-secret')
  await page.getByLabel('用户搜索 Base DN').fill(config.ldap.user_base_dn)
  await page.getByLabel('管理员组', { exact: true }).fill(config.admin_groups[0])
  await page.getByLabel('认证源标识').fill(provider.id)
  expect(payload).toBeUndefined()
  await page.getByRole('button', { name: '检查并保存' }).click()
  await expect(page.getByRole('dialog')).not.toContainText('service-fixture-secret')
  await page.getByRole('dialog').getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(page).toHaveURL(/source=directory$/)
  expect(payload).toMatchObject({ id: 'directory', protocol: 'ldap', enabled: false, ldap: { bind_password: 'service-fixture-secret', url: config.ldap.url } })
  expect(payload).not.toHaveProperty('issuer')
  expect(payload?.ldap).not.toHaveProperty('bind_password_set')
  await expect(page.getByLabel('服务账户密码')).toHaveValue('')
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage }))).not.toContain('service-fixture-secret')
})

test('directory checks and trial use saved configuration, never create a session, and clear test credentials', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/admin/authentication/sources': { sources: [source], config_writable: true },
    'POST /api/v1/admin/authentication/ldap/check': { checked_at: '2026-10-23T10:00:00Z', configuration_valid: true, tls_valid: true, bind_valid: true, directory_readable: true },
    'POST /api/v1/admin/authentication/ldap/trial': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ id: 'directory', protocol: 'ldap', create: false, settings: {}, username: 'test-user', password: 'directory-fixture-password' })
      return { mode: 'test', provider, outcome: 'success', authenticated: true, mapped_role: 'readonly', display_name: 'Directory test user', checked_at: '2026-10-23T10:00:00Z', return_to: '/admin/authentication?source=directory' }
    },
  })
  await page.goto('/admin/authentication?source=directory')
  await page.getByRole('button', { name: '检查配置', exact: true }).click()
  await expect(page.getByText('目录连接、服务账户与读取检查通过')).toBeVisible()
  await page.getByLabel('目录用户名', { exact: true }).fill('test-user')
  await page.getByLabel('目录密码', { exact: true }).fill('directory-fixture-password')
  await page.getByRole('button', { name: '验证目录登录' }).click()
  await expect(page.getByText('Directory test user')).toBeVisible()
  await expect(page.getByLabel('目录密码', { exact: true })).toHaveValue('')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage }))).not.toContain('directory-fixture-password')
  await page.getByLabel('目录用户名', { exact: true }).fill('another-user')
  await expect(page.getByText('Directory test user')).toHaveCount(0)
})

test('directory environment ownership is visible and a name-only review preserves running sessions', async ({ page }) => {
  let impacts = 0
  let payload: unknown
  const running = { ...config, enabled: true }
  const managed = { ...source, configured: running, effective: { ...running, ldap: { ...running.ldap, url: 'ldaps://managed.example' } }, overrides: { 'ldap.url': 'DEPSILO_DIRECTORY_URL', 'ldap.bind_password': 'DEPSILO_DIRECTORY_PASSWORD' }, sessions_will_reset: false }
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/admin/authentication/sources': { sources: [managed], config_writable: true },
    'GET /api/v1/admin/authentication/sources/directory/impact': () => { impacts++; return { status: 500, body: {} } },
    'PUT /api/v1/admin/authentication/sources/directory': (request: Request) => { payload = request.postDataJSON(); return { ...managed, configured: { ...running, display_name: 'Renamed directory' }, pending_restart: true } },
  })
  await page.goto('/admin/authentication?source=directory')
  await expect(page.getByLabel('目录地址')).toHaveValue('ldaps://managed.example')
  await expect(page.getByLabel('目录地址')).toBeDisabled()
  await expect(page.getByLabel('服务账户密码')).toBeDisabled()
  await expect(page.getByText(/DEPSILO_DIRECTORY_URL/)).toBeVisible()
  await page.getByLabel('认证源名称').fill('Renamed directory')
  await page.getByRole('button', { name: '检查并保存' }).click()
  const review = page.getByRole('dialog')
  await expect(review).toContainText('Company directory')
  await expect(review).toContainText('Renamed directory')
  await review.getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(review).toBeHidden()
  expect(payload).toEqual({ display_name: 'Renamed directory' })
  expect(impacts).toBe(0)
})

test('invalid directory configuration explains the LDAP schemes without echoing server values', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/admin/authentication/sources': { sources: [source], config_writable: true },
    'POST /api/v1/admin/authentication/ldap/check': { status: 400, body: { code: 'INVALID_SETTING', field_errors: { 'ldap.url': 'INVALID_URL' }, message: 'untrusted-server-message' } },
  })
  await page.goto('/admin/authentication?source=directory')
  await page.getByRole('button', { name: '检查配置', exact: true }).click()
  await expect(page.getByText('请输入有效的 ldaps:// 或 ldap:// 目录地址；ldap:// 连接必须支持 StartTLS。')).toBeVisible()
  await expect(page.getByText('untrusted-server-message')).toHaveCount(0)
  await page.getByLabel('目录地址').fill('ldaps://updated.example')
  await expect(page.getByText('请输入有效的 ldaps:// 或 ldap:// 目录地址；ldap:// 连接必须支持 StartTLS。')).toHaveCount(0)
})

test('LDAP sign-in retries invalid credentials without changing an existing token, then offers an explicit account choice', async ({ page }) => {
  let attempts = 0
  const choice = { mode: 'choice', provider, display_name: 'Directory operator', role: 'readonly', return_to: '/admin/account', expires_at: 1_950_000_000 }
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: [provider] },
    'POST /api/v1/auth/ldap/login': (request: Request) => {
      expect(request.headers().authorization).toBeUndefined()
      expect(request.postDataJSON().provider_id).toBe('directory')
      return ++attempts === 1 ? { status: 403, body: { code: 'LDAP_AUTHENTICATION_FAILED' } } : choice
    },
    'POST /api/v1/auth/identity/complete': choice,
  })
  await page.goto('/admin/login')
  await page.getByRole('button', { name: '使用 Company directory 登录' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('目录用户名').fill('operator')
  await dialog.getByLabel('目录密码').fill('incorrect')
  await dialog.getByRole('button', { name: '登录', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('目录用户名或密码不正确')
  await expect(dialog.getByLabel('目录密码')).toHaveValue('')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  await dialog.getByLabel('目录密码').fill('directory-fixture-password')
  await dialog.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '选择如何继续' })).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

test('local accounts link a directory identity using two separate proofs without changing their local authority', async ({ page }) => {
  let linked = false
  const identity: LoginIdentity = { id: 12, provider: 'directory', protocol: 'ldap', provider_name: provider.name, provider_matches: true, login_available: true, display_name: 'Directory operator', created_at: '2026-10-23T10:00:00Z', last_login_at: null, can_unlink: true }
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': principal,
    'GET /api/v1/auth/identities': () => ({ permission_source: 'local', password_enabled: true, can_link: true, provider: null, providers: [provider], identities: linked ? [identity] : [] }),
    'POST /api/v1/auth/identities/ldap/link': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ provider_id: 'directory', password: 'local-proof', username: 'directory-user', directory_password: 'directory-proof' })
      linked = true
      return { mode: 'link', result: 'linked', return_to: '/admin/account' }
    },
  })
  await page.goto('/admin/account')
  await page.getByRole('button', { name: '绑定 Company directory' }).click()
  await page.getByLabel('当前本地密码').fill('local-proof')
  await page.getByLabel('目录用户名').fill('directory-user')
  await page.getByLabel('目录密码').fill('directory-proof')
  await page.getByRole('button', { name: '验证并绑定' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByText('Directory operator')).toBeVisible()
  await expect(page.getByText('权限由 Depsilo 本地管理员管理')).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  expect(await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage }))).not.toMatch(/local-proof|directory-proof/)
})

for (const sample of [{ width: 1440, theme: 'dark', lang: 'zh' }, { width: 320, theme: 'light', lang: 'en' }] as const) {
  test(`LDAP configuration is accessible without overflow at ${sample.width}px`, async ({ page }) => {
    await page.setViewportSize({ width: sample.width, height: 900 })
    await setUiPreferences(page, sample.theme, sample.lang)
    await mockAdminApi(page, { 'GET /api/v1/auth/me': principal, 'GET /api/v1/admin/authentication/sources': { sources: [source], config_writable: true } })
    await page.goto('/admin/authentication?source=directory')
    await expect(page.getByLabel(sample.lang === 'zh' ? '目录地址' : 'Directory URL')).toHaveValue(config.ldap.url)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/ldap-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
  })
}
