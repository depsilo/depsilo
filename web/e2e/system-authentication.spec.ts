import AxeBuilder from '@axe-core/playwright'
import type { Page, Request } from '@playwright/test'
import { test, expect, mockAdminApi as mockApi, setUiPreferences, authenticationSettings } from './fixtures/admin-api'
import type { AuthenticationImpact, AuthenticationSourceState, Principal } from '../src/lib/adminApi.types'

const localAdmin: Principal = { id: 1, username: 'admin', enabled: true, role: 'admin', auth_method: 'jwt', token_permissions: null, can_write: true, can_manage_credentials: true, permission_source: 'local', login_method: 'password' }
const config = { ...authenticationSettings, display_name: 'Company login', enabled: true, issuer: 'https://identity.example', client_id: 'depsilo', redirect_url: 'https://depsilo.example/api/v1/auth/oidc/company/callback', admin_groups: ['operators'], readonly_groups: ['readers'], client_secret_set: true }
const state: AuthenticationSourceState = { id: 'company', protocol: 'oidc', legacy: false, configured: config, effective: config, overrides: {}, pending_restart: false, config_writable: true }
const labConfig = { ...config, display_name: 'Homelab', issuer: 'https://lab.example', redirect_url: 'https://depsilo.example/api/v1/auth/oidc/lab/callback' }
const labSource: AuthenticationSourceState = { ...state, id: 'lab', configured: labConfig, effective: labConfig }

const impact: AuthenticationImpact = { source_id: 'company', bound_user_count: 3, dependent_managed_user_count: 1, active_session_count: 5, observed_at: '2026-10-23T08:00:00Z', users: [{ id: 1, username: 'local-operator', permission_source: 'local', enabled: true }, { id: 2, username: 'managed-user', permission_source: 'oidc', enabled: true }], users_truncated: false }
const mockAdminApi = (page: Page, overrides: Parameters<typeof mockApi>[1] = {}) => mockApi(page, {
  'GET /api/v1/admin/authentication/sources/company/impact': impact,
  ...overrides,
})

test('adding a source stays a local draft until the complete configuration is explicitly reviewed and saved', async ({ page, baseURL }) => {
  let payload: unknown
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'POST /api/v1/admin/authentication/sources': (request: Request) => {
      payload = request.postDataJSON()
      return { status: 201, body: { ...state, id: 'work', configured: { ...config, display_name: '公司统一登录', enabled: false, redirect_url: `${baseURL}/api/v1/auth/oidc/work/callback` }, effective: null, pending_restart: true } }
    },
  })
  await page.goto('/admin/authentication')
  await expect(page.getByRole('heading', { name: '尚未添加认证源' })).toBeVisible()
  await expect(page.getByText('Casdoor')).toHaveCount(0)
  await page.getByRole('button', { name: '添加认证源', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: /^OIDC/ }).click()
  await expect(page).toHaveURL(/\?new=1$/)
  await expect(page.getByLabel('认证源名称')).toHaveValue('')
  await expect(page.getByLabel('认证源标识')).toBeHidden()
  await expect(page.getByRole('button', { name: '检查并保存' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '试登录', exact: true })).toBeDisabled()
  await page.getByLabel('认证源名称').fill('公司统一登录')
  await page.getByLabel('客户端 ID', { exact: true }).fill(config.client_id)
  await page.getByLabel('发行方地址（Issuer）').fill(config.issuer)
  await page.getByLabel('客户端密钥', { exact: true }).fill('fixture-only-secret')
  await page.getByLabel('管理员组', { exact: true }).fill('operators')
  await page.getByText('高级选项', { exact: true }).click()
  await expect(page.getByLabel('认证源标识')).toHaveValue(/^source-/)
  await page.getByLabel('认证源标识').fill('work')
  await expect(page.getByLabel('登录回调地址')).toHaveValue(`${baseURL}/api/v1/auth/oidc/work/callback`)
  expect(payload).toBeUndefined()
  await page.getByRole('button', { name: '检查并保存' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('公司统一登录')
  await expect(dialog).not.toContainText('fixture-only-secret')
  expect(payload).toBeUndefined()
  await dialog.getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(payload).toMatchObject({ id: 'work', protocol: 'oidc', display_name: '公司统一登录', enabled: false, issuer: config.issuer, client_id: config.client_id, client_secret: 'fixture-only-secret', admin_groups: ['operators'] })
  expect(payload).not.toHaveProperty('client_secret_set')
  await expect(page).toHaveURL(/\?source=work$/)
  await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
  await expect(page.getByRole('switch', { name: '启用外部登录' })).not.toBeChecked()
  await expect(page.getByText('当前运行: 尚未加载', { exact: true })).toBeVisible()
})

test('source creation conflicts preserve the draft and show a coded error beside the source ID', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
    'POST /api/v1/admin/authentication/sources': { status: 409, body: { code: 'AUTH_SOURCE_CONFLICT', field_errors: { id: 'SOURCE_EXISTS' } } },
  })
  await page.goto('/admin/authentication')
  await page.getByRole('button', { name: '添加认证源', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: /^OIDC/ }).click()
  await page.getByLabel('认证源名称').fill('Another source')
  await page.getByText('高级选项', { exact: true }).click()
  await page.getByLabel('认证源标识').fill('company')
  await page.getByRole('button', { name: '检查并保存' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.locator('[data-slot="alert"]')).toContainText('认证源名称或标识无效，或标识已存在')
  await expect(page.getByText('此认证源标识已存在，请更换。')).toBeVisible()
  await expect(page.getByLabel('认证源标识')).toHaveAttribute('aria-invalid', 'true')
  await page.getByLabel('认证源标识').fill('')
  await page.getByLabel('认证源标识').pressSequentially('another-source')
  await expect(page.getByLabel('认证源标识')).toBeVisible()
  await expect(page.getByLabel('认证源标识')).toHaveValue('another-source')
  await expect(page.getByLabel('认证源标识')).toHaveAttribute('aria-invalid', 'false')
  await expect(page.getByLabel('认证源名称')).toHaveValue('Another source')
  await expect(page).toHaveURL(/\?new=1$/)
})

test('authentication editor saves only reviewed changes and shows the pending restart separately from active status', async ({ page }) => {
  let payload: unknown
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [state, labSource], config_writable: true },
    'PUT /api/v1/admin/authentication/sources/company': (request: Request) => {
      payload = request.postDataJSON()
      return { ...state, configured: { ...config, display_name: 'Team SSO' }, pending_restart: true }
    },
  })
  await page.goto('/admin/authentication?source=company')
  await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
  await page.getByLabel('认证源名称').fill('Team SSO')
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '检查并保存' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('认证源名称')
  await dialog.getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(payload).toEqual({ display_name: 'Team SSO' })
  await expect(page.getByText(/此认证源的保存配置与运行状态不同/)).toBeVisible()
  await expect(page.getByText('通过 Company login 登录；下方编辑不会立即改变运行中的配置。')).toBeVisible()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Team SSO')
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await expect(page.getByRole('heading', { name: 'Company login' })).toBeVisible()
  await expect(page.getByText('Team SSO · https://identity.example', { exact: true })).toBeVisible()
  await expect(page.getByText('待重启', { exact: true })).toHaveCount(1)
  await page.getByRole('button', { name: '配置 Homelab', exact: true }).click()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Homelab')
  await expect(page.getByLabel('发行方地址（Issuer）')).toHaveValue(labConfig.issuer)
})

test('removing one active source preserves other sources and records pending removal until restart', async ({ page }) => {
  let sources = [state, labSource]
  const writes: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': () => ({ sources, config_writable: true }),
    'DELETE /api/v1/admin/authentication/sources/company': (request: Request) => {
      writes.push(request.url())
      sources = [{ ...state, configured: null, pending_restart: true }, labSource]
      return { status: 204, body: null }
    },
  })
  await page.goto('/admin/authentication')
  await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
  await page.getByRole('button', { name: '移除认证源', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: '移除 Company login？' })).toBeVisible()
  await expect(dialog).toContainText('用户和绑定记录保留')
  await dialog.getByRole('button', { name: '确认移除' }).click()
  await expect(page).toHaveURL(/\/admin\/authentication$/)
  await expect(page.getByText('已保存配置: 待移除', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Homelab' })).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0]).toMatch(/\/authentication\/sources\/company$/)
  await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
  await expect(page.getByText('此认证源已从配置中移除，重启后停止提供登录。原有账户和绑定记录保留。')).toBeVisible()
  await expect(page.getByLabel('认证源名称')).toHaveCount(0)
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  sources = [labSource]
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Company login' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '配置 Homelab', exact: true })).toBeVisible()
})

test('unknown source deep links offer recovery without showing an invented configuration', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/auth/me': localAdmin })
  await page.goto('/admin/authentication?source=missing')
  await expect(page.getByText('找不到此认证源。请返回列表刷新后重试。')).toBeVisible()
  await expect(page.getByLabel('认证源名称')).toHaveCount(0)
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await expect(page.getByRole('heading', { name: '尚未添加认证源' })).toBeVisible()
})

test('replacement secrets are cleared after submission and never persisted or echoed in an error', async ({ page }) => {
  const secret = 'test-only-client-secret-not-a-real-credential'
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
    'PUT /api/v1/admin/authentication/sources/company': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ client_secret: secret })
      return { status: 422, body: { code: 'INVALID_SETTING', message: secret } }
    },
  })
  await page.goto('/admin/authentication?source=company')
  await page.getByLabel('客户端密钥', { exact: true }).fill(secret)
  await page.getByRole('button', { name: '检查并保存' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByRole('alert')).toContainText('配置未通过验证')
  await expect(page.getByText(secret, { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
  const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))
  expect(storage).not.toContain(secret)
})

for (const outcome of ['success', 'failure'] as const) {
  test(`a pending secret-only save guards refresh and browser Back until ${outcome} is known`, async ({ page }) => {
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    let submissions = 0
    await mockAdminApi(page, {
      'GET /api/v1/auth/me': localAdmin,
      'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
      'PUT /api/v1/admin/authentication/sources/company': async (request: Request) => {
        submissions++
        expect(request.postDataJSON()).toEqual({ client_secret: 'pending-fixture-secret' })
        await pending
        return outcome === 'success' ? { ...state, pending_restart: true } : { status: 503, body: { code: 'CONFIG_WRITE_FAILED' } }
      },
    })
    try {
      await page.goto('/admin/authentication')
      await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
      await page.getByLabel('客户端密钥', { exact: true }).fill('pending-fixture-secret')
      await page.getByRole('button', { name: '检查并保存' }).click()
      await page.getByRole('dialog', { name: '保存身份认证配置', exact: true }).getByRole('button', { name: '保存配置', exact: true }).click()
      await expect.poll(() => submissions).toBe(1)
      // Clearing the submitted secret must not make this in-flight write safe to leave.
      await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
      const refreshing = page.waitForEvent('dialog')
      await page.evaluate(() => { window.setTimeout(() => window.location.reload(), 0) })
      const refreshDialog = await refreshing
      expect(refreshDialog.type()).toBe('beforeunload')
      await refreshDialog.dismiss()
      await expect(page).toHaveURL(/\?source=company$/)
      await page.evaluate(() => history.back())
      const leave = page.getByRole('dialog', { name: /等待请求完成|放弃未保存的更改/ })
      await expect(leave).toBeVisible()
      await expect(leave.getByRole('heading', { name: '等待请求完成', exact: true })).toBeVisible()
      await expect(leave.getByRole('button', { name: '放弃并离开', exact: true })).toBeDisabled()
      await expect(leave.getByRole('button', { name: '取消', exact: true })).toBeEnabled()
      release()
      if (outcome === 'success') {
        await expect(page).toHaveURL(/\/admin\/authentication$/)
        await expect(leave).toBeHidden()
        await expect(page.getByRole('heading', { name: 'Company login', exact: true })).toBeVisible()
      } else {
        await expect(leave.getByRole('button', { name: '放弃并离开', exact: true })).toBeEnabled()
        await expect(page).toHaveURL(/\?source=company$/)
        await leave.getByRole('button', { name: '取消', exact: true }).click()
        await expect(page.getByRole('alert')).toContainText('无法确认配置是否已保存')
        await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
      }
      expect(submissions).toBe(1)
    } finally {
      release()
    }
  })
}

test('environment-controlled values and OIDC administrator sessions are readable but not editable', async ({ page }) => {
  const api = await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [{ ...state, effective: { ...config, client_id: 'from-environment' }, overrides: { client_id: 'DEPSILO_AUTH_SOURCES_COMPANY_CLIENT_ID' } }], config_writable: true },
  })
  await page.goto('/admin/authentication?source=company')
  await expect(page.getByLabel('客户端 ID', { exact: true })).toHaveValue('from-environment')
  await expect(page.getByLabel('客户端 ID', { exact: true })).toBeDisabled()
  await expect(page.getByLabel('认证源名称')).toBeEnabled()
  await expect(page.getByRole('button', { name: '移除认证源', exact: true })).toBeDisabled()
  await expect(page.getByText(/此认证源有环境变量覆盖/)).toBeVisible()
  api.overrides['GET /api/v1/auth/me'] = { ...localAdmin, login_method: 'oidc', can_manage_credentials: false }
  await page.reload()
  await expect(page.getByLabel('认证源名称')).toBeDisabled()
  await expect(page.getByText(/修改需使用本地管理员密码登录/)).toBeVisible()
  await expect(page.getByRole('button', { name: '检查并保存' })).toBeDisabled()
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await expect(page.getByRole('button', { name: '添加认证源', exact: true })).toHaveCount(0)
})

test('read-only configuration keeps source details accessible while preventing add, edit, or remove', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [{ ...state, config_writable: false }], config_writable: false },
  })
  await page.goto('/admin/authentication')
  await expect(page.getByRole('button', { name: '添加认证源', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Company login')
  await expect(page.getByLabel('认证源名称')).toBeDisabled()
  await expect(page.getByRole('button', { name: '移除认证源', exact: true })).toBeDisabled()
  await expect(page.getByText(/当前配置不能通过页面写入/)).toBeVisible()
})

test('a writable local administrator API Token can edit and check but cannot start a browser sign-in trial', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localAdmin, auth_method: 'api_token', login_method: 'api_token', token_permissions: 'readwrite' },
    'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
  })
  await page.goto('/admin/authentication?source=company')
  await expect(page.getByLabel('认证源名称')).toBeEnabled()
  await expect(page.getByRole('button', { name: '检查配置', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: '试登录', exact: true })).toBeDisabled()
  await expect(page.getByText('试登录需要当前浏览器使用本地管理员密码登录。API Token 可管理配置，但不能执行此浏览器验证。')).toBeVisible()
})

test('initial load failure and stale configuration do not become a disabled provider', async ({ page }) => {
  const api = await mockAdminApi(page, { 'GET /api/v1/admin/authentication/sources': { status: 503, body: { code: 'CONFIG_READ_FAILED' } } })
  await page.goto('/admin/authentication?source=company')
  await expect(page.getByText('身份认证配置暂时无法读取')).toBeVisible()
  await expect(page.getByText('尚未添加认证源', { exact: true })).toHaveCount(0)
  api.overrides['GET /api/v1/admin/authentication/sources'] = { sources: [state], config_writable: true }
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByLabel('认证源名称')).toBeEnabled()
  api.overrides['GET /api/v1/admin/authentication/sources'] = { status: 503, body: { code: 'CONFIG_READ_FAILED' } }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText(/刷新失败，当前显示上次读取的配置/)).toBeVisible()
  await expect(page.getByLabel('认证源名称')).toBeDisabled()
})

test('dirty source edits survive reconnect refresh and leaving requires an explicit discard', async ({ page }) => {
  let payload: unknown
  let reads = 0
  const refreshed = { ...state, configured: { ...config, client_id: 'new-client-id', session_ttl: '15m' }, pending_restart: true }
  const api = await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': () => { reads++; return { sources: [state], config_writable: true } },
    'PUT /api/v1/admin/authentication/sources/company': (request: Request) => {
      payload = request.postDataJSON()
      return { ...refreshed, configured: { ...refreshed.configured, display_name: 'My edit' } }
    },
  })
  await page.goto('/admin/authentication?source=company')
  await page.getByLabel('认证源名称').fill('My edit')
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await expect(page.getByRole('dialog').getByRole('heading', { name: '放弃未保存的更改？' })).toBeVisible()
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByLabel('认证源名称')).toHaveValue('My edit')
  const before = reads
  api.overrides['GET /api/v1/admin/authentication/sources'] = () => { reads++; return { sources: [refreshed], config_writable: true } }
  await page.evaluate(() => window.dispatchEvent(new Event('offline')))
  await page.evaluate(() => window.dispatchEvent(new Event('online')))
  await expect.poll(() => reads).toBeGreaterThan(before)
  await expect(page.getByLabel('客户端 ID', { exact: true })).toHaveValue('new-client-id')
  await expect(page.getByLabel('外部会话最长时长')).toHaveValue('15m')
  await expect(page.getByLabel('认证源名称')).toHaveValue('My edit')
  await page.getByRole('button', { name: '检查并保存' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '保存配置', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
  expect(payload).toEqual({ display_name: 'My edit' })
  await page.getByLabel('客户端密钥', { exact: true }).fill('discard-this-test-secret')
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '放弃并离开' }).click()
  await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
  await expect(page.getByLabel('客户端密钥', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('认证源名称')).toHaveValue('My edit')
})

test('runtime identity and saved identity remain separate until restart', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [{ ...state, configured: { ...config, enabled: false, display_name: 'Future login', issuer: 'https://future.example' }, pending_restart: true }], config_writable: true },
  })
  await page.goto('/admin/authentication')
  await expect(page.getByRole('heading', { name: 'Company login' })).toBeVisible()
  await expect(page.getByText('https://identity.example', { exact: true })).toBeVisible()
  await expect(page.getByText('当前运行: 已启用', { exact: true })).toBeVisible()
  await expect(page.getByText('已保存配置: 未启用', { exact: true })).toBeVisible()
  await expect(page.getByText('Future login · https://future.example', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Company login' })).toBeVisible()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Future login')
  await expect(page.getByLabel('发行方地址（Issuer）')).toHaveValue('https://future.example')
  await expect(page.getByRole('switch', { name: '启用外部登录' })).not.toBeChecked()
})

test('checking a draft sends no save, reports the limited check, and invalidates the report on edit', async ({ page, context }) => {
  const payloads: unknown[] = []
  const writes: string[] = []
  page.on('request', request => { if (request.method() !== 'GET' && /\/authentication\/sources(?:\/|$)/.test(request.url())) writes.push(request.url()) })
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
    'POST /api/v1/admin/authentication/check': (request: Request) => {
      payloads.push(request.postDataJSON())
      return { checked_at: '2026-10-23T08:00:00Z', configuration_valid: true, discovery_valid: true, jwks_readable: true }
    },
  })
  await page.goto('/admin/authentication?source=company')
  await page.getByRole('button', { name: '复制回调地址', exact: true }).click()
  await expect(page.getByText('回调地址已复制')).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(config.redirect_url)
  await expect(page.getByRole('button', { name: '试登录', exact: true })).toBeEnabled()
  await page.getByLabel('认证源名称').fill('Draft name')
  await expect(page.getByRole('button', { name: '试登录', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '检查配置', exact: true }).click()
  await expect(page.getByText('配置、发现文档与公钥检查通过', { exact: true })).toBeVisible()
  await expect(page.getByText('尚未验证客户端密钥、实际登录和角色映射。')).toBeVisible()
  expect(payloads).toEqual([{ id: 'company', protocol: 'oidc', create: false, settings: { display_name: 'Draft name' } }])
  expect(writes).toEqual([])
  await page.getByLabel('客户端 ID', { exact: true }).fill('different-client')
  await expect(page.getByText('配置、发现文档与公钥检查通过', { exact: true })).toHaveCount(0)
})

test('failed configuration checks show localized field errors and never reflect server values', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
    'POST /api/v1/admin/authentication/check': { status: 422, body: { code: 'INVALID_SETTING', message: 'server-private-value', field_errors: { issuer: 'DUPLICATE_ISSUER', redirect_url: 'CALLBACK_PATH' } } },
  })
  await page.goto('/admin/authentication?source=company')
  await page.getByRole('button', { name: '检查配置', exact: true }).click()
  await expect(page.getByText('此 Issuer 已用于其他认证源。')).toBeVisible()
  await expect(page.getByText('回调路径必须与此认证源的标识一致。')).toBeVisible()
  await expect(page.getByLabel('发行方地址（Issuer）')).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByLabel('登录回调地址')).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByText('server-private-value')).toHaveCount(0)
  await page.getByLabel('发行方地址（Issuer）').fill('https://different.example')
  await expect(page.getByLabel('发行方地址（Issuer）')).toHaveAttribute('aria-invalid', 'false')
})

test('trial uses the saved source and existing administrator session without saving or replacing it', async ({ page, baseURL }) => {
  const writes: string[] = []
  page.on('request', request => { if (request.method() !== 'GET' && /\/authentication\/sources(?:\/|$)/.test(request.url())) writes.push(request.url()) })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [{ ...state, configured: { ...config, enabled: false } }], config_writable: true },
    'POST /api/v1/admin/authentication/trial': (request: Request) => {
      expect(request.headers().authorization).toBe('Bearer e2e-token')
      expect(request.postDataJSON()).toEqual({ id: 'company', protocol: 'oidc', create: false, settings: {} })
      return { authorization_url: `${baseURL}/fixture-trial` }
    },
  })
  await page.route('**/fixture-trial', route => route.fulfill({ contentType: 'text/html', body: '<h1>OIDC sign-in trial</h1>' }))
  await page.goto('/admin/authentication?source=company')
  await page.getByRole('button', { name: '试登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'OIDC sign-in trial' })).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  expect(writes).toEqual([])
})

for (const action of ['save', 'remove'] as const) {
  test(`impact preview must load successfully before ${action} and shows observed counts`, async ({ page }) => {
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    let mutated = false
    const api = await mockAdminApi(page, {
      'GET /api/v1/auth/me': localAdmin,
      'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true },
      'GET /api/v1/admin/authentication/sources/company/impact': async () => { await pending; return { status: 503, body: { code: 'DATABASE_ERROR' } } },
      'PUT /api/v1/admin/authentication/sources/company': () => { mutated = true; return { ...state, configured: { ...config, display_name: 'Changed' }, pending_restart: true } },
      'DELETE /api/v1/admin/authentication/sources/company': () => { mutated = true; return { status: 204, body: null } },
    })
    await page.goto('/admin/authentication?source=company')
    if (action === 'save') {
      await page.getByLabel('认证源名称').fill('Changed')
      await page.getByRole('button', { name: '检查并保存' }).click()
    } else await page.getByRole('button', { name: '移除认证源', exact: true }).click()
    const dialog = page.getByRole('dialog')
    const confirm = dialog.getByRole('button', { name: action === 'save' ? '保存配置' : '确认移除', exact: true })
    await expect(dialog.getByText('正在读取受影响账户与会话…')).toBeVisible()
    await expect(confirm).toBeDisabled()
    release()
    await expect(dialog.getByText('无法读取影响范围，请重试后再确认。')).toBeVisible()
    await expect(confirm).toBeDisabled()
    expect(mutated).toBe(false)
    api.overrides['GET /api/v1/admin/authentication/sources/company/impact'] = impact
    await dialog.getByRole('button', { name: '重试', exact: true }).click()
    const preview = dialog.getByRole('region', { name: '重启后的影响范围' })
    for (const [label, count] of [['关联账户', '3'], ['依赖此源的外部账户', '1'], ['有效会话记录', '5']]) {
      await expect(preview.locator('dl > div').filter({ has: page.getByText(label, { exact: true }) }).locator('dd')).toHaveText(count)
    }
    await expect(preview).toContainText('并非在线人数')
    await preview.getByText('查看关联账户', { exact: true }).click()
    await expect(preview.getByText('managed-user', { exact: true })).toBeVisible()
    await expect(confirm).toBeEnabled()
    if (action === 'save') {
      await expect(dialog).toContainText('原值: Company login')
      await expect(dialog).toContainText('保存为: Changed')
    }
    await expect(dialog).toHaveCSS('opacity', '1')
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/${action}-impact-api-fixture.png`, fullPage: false, animations: 'disabled' })
    await confirm.click()
    await expect(dialog).toBeHidden()
    expect(mutated).toBe(true)
  })
}

test('pending removal can be restored without changing another source and failures remain recoverable', async ({ page }) => {
  let restores = 0
  const api = await mockAdminApi(page, {
    'GET /api/v1/auth/me': localAdmin,
    'GET /api/v1/admin/authentication/sources': { sources: [{ ...state, configured: null, can_restore: true, pending_restart: true }, labSource], config_writable: true },
    'POST /api/v1/admin/authentication/sources/company/restore': () => { restores++; return { status: 503, body: { code: 'CONFIG_WRITE_FAILED' } } },
  })
  await page.goto('/admin/authentication?source=company')
  await expect(page.getByText('当前运行: 已启用', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '撤销待移除', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('恢复未完成')
  await expect(page.getByRole('button', { name: '撤销待移除', exact: true })).toBeEnabled()
  api.overrides['POST /api/v1/admin/authentication/sources/company/restore'] = () => { restores++; return state }
  await page.getByRole('button', { name: '撤销待移除', exact: true }).click()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Company login')
  await page.getByRole('button', { name: '返回认证源列表' }).click()
  await expect(page.getByRole('heading', { name: 'Homelab' })).toBeVisible()
  await expect(page.getByText('已保存配置: 待移除', { exact: true })).toHaveCount(0)
  expect(restores).toBe(2)
})

for (const destination of ['sidebar', 'workspace tab', 'browser Back'] as const) {
  test(`unsaved edits guard ${destination} navigation and discard requires an explicit decision`, async ({ page }) => {
    await mockAdminApi(page, { 'GET /api/v1/auth/me': localAdmin, 'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true } })
    await page.goto('/admin/authentication')
    await page.getByRole('button', { name: '配置 Company login', exact: true }).click()
    await page.getByLabel('认证源名称').fill('Unsaved name')
    const leave = async () => {
      if (destination === 'sidebar') await page.locator('aside').getByRole('link', { name: '总览', exact: true }).click()
      else if (destination === 'workspace tab') await page.locator('[data-admin-page-navigation="instance"] a[href="/admin/settings"]').click()
      else await page.evaluate(() => history.back())
    }
    await leave()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: '放弃未保存的更改？' })).toBeVisible()
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await expect(page).toHaveURL(/\?source=company$/)
    await expect(page.getByLabel('认证源名称')).toHaveValue('Unsaved name')
    await leave()
    await dialog.getByRole('button', { name: '放弃并离开', exact: true }).click()
    await expect(page).toHaveURL(destination === 'sidebar' ? /\/admin$/ : destination === 'workspace tab' ? /\/admin\/settings$/ : /\/admin\/authentication$/)
  })
}

test('browser refresh warns before discarding an edited source', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/auth/me': localAdmin, 'GET /api/v1/admin/authentication/sources': { sources: [state], config_writable: true } })
  await page.goto('/admin/authentication?source=company')
  await page.getByLabel('认证源名称').fill('Unsaved refresh')
  const rejected = page.waitForEvent('dialog')
  // Browser-native reload obeys the user's cancellation; Playwright's CDP
  // Page.reload command can wait for a load that correctly never happens.
  await page.evaluate(() => { window.setTimeout(() => window.location.reload(), 0) })
  const dialog = await rejected
  expect(dialog.type()).toBe('beforeunload')
  await dialog.dismiss()
  await expect(page.getByLabel('认证源名称')).toHaveValue('Unsaved refresh')
  const accepted = page.waitForEvent('dialog')
  const reloaded = page.waitForEvent('domcontentloaded')
  await page.evaluate(() => { window.setTimeout(() => window.location.reload(), 0) })
  await (await accepted).accept()
  await reloaded
  await expect(page.getByLabel('认证源名称')).toHaveValue('Company login')
})

for (const sample of [{ width: 1440, theme: 'light' as const, lang: 'zh' as const }, { width: 2048, theme: 'dark' as const, lang: 'zh' as const }, { width: 320, theme: 'light' as const, lang: 'en' as const }]) {
  test(`system authentication has readable fields and no overflow at ${sample.width}px ${sample.theme}`, async ({ page }) => {
    await page.setViewportSize({ width: sample.width, height: 1000 })
    await setUiPreferences(page, sample.theme, sample.lang)
    await mockAdminApi(page, { 'GET /api/v1/auth/me': localAdmin, 'GET /api/v1/admin/authentication/sources': { sources: [state, labSource], config_writable: true } })
    await page.goto('/admin/authentication')
    await expect(page.getByRole('heading', { name: sample.lang === 'en' ? 'Authentication sources' : '认证源', exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/sources-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
    await page.getByRole('button', { name: sample.lang === 'en' ? 'Add authentication source' : '添加认证源', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: /^OIDC/ }).click()
    await expect(page).toHaveURL(/\?new=1$/)
    await page.getByLabel(sample.lang === 'en' ? 'Source name' : '认证源名称').fill(sample.lang === 'en' ? 'Company sign-in' : '公司统一登录')
    // The submit button transitions from disabled opacity after the first edit.
    await expect(page.getByRole('button', { name: sample.lang === 'en' ? 'Review and save' : '检查并保存' })).toHaveCSS('opacity', '1')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/add-source-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
    await page.getByRole('button', { name: sample.lang === 'en' ? 'Back to sources' : '返回认证源列表' }).click()
    await page.getByRole('dialog').getByRole('button', { name: sample.lang === 'en' ? 'Discard and leave' : '放弃并离开' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()
    await page.getByRole('button', { name: sample.lang === 'en' ? 'Configure Company login' : '配置 Company login', exact: true }).click()
    await expect(page.getByRole('heading', { name: sample.lang === 'en' ? 'OIDC connection settings' : 'OIDC 连接配置', exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/edit-source-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
  })
}
