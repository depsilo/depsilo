import AxeBuilder from '@axe-core/playwright'
import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'
import type { Request } from '@playwright/test'
import type { LoginIdentitiesResponse, OIDCCompleteResponse, Principal } from '../src/lib/adminApi.types'

const localPrincipal: Principal = { id: 1, username: 'admin', enabled: true, role: 'admin', auth_method: 'jwt', token_permissions: null, can_write: true, permission_source: 'local', login_method: 'password', can_manage_credentials: true }
const provider = { id: 'oidc', name: 'Casdoor', enabled: true, issuer: 'http://127.0.0.1:18000', redirect_url: 'http://localhost:23333/api/v1/auth/oidc/callback', session_ttl_seconds: 1800 }
const identity = { id: 7, provider: 'oidc', provider_name: 'Casdoor', display_name: 'Test operator', created_at: '2026-10-22T08:00:00Z', last_login_at: null, can_unlink: true }
const localIdentities: LoginIdentitiesResponse = { permission_source: 'local', password_enabled: true, can_link: true, identities: [], provider }
const teamSource = { id: 'team', name: 'Team login', protocol: 'oidc' as const, enabled: true, can_link: true, session_ttl_seconds: 1800 }
const labSource = { id: 'lab', name: 'Homelab', protocol: 'oidc' as const, enabled: true, can_link: true, session_ttl_seconds: 3600 }
const multiIdentities: LoginIdentitiesResponse = { ...localIdentities, provider: null, providers: [teamSource, labSource] }
const choice: Extract<OIDCCompleteResponse, { mode: 'choice' }> = { mode: 'choice', provider: teamSource, display_name: 'External operator', role: 'readonly', return_to: '/admin/account', expires_at: 1_950_000_000 }

test('a consumed trial can return to its source after refresh without replacing the administrator session', async ({ page }) => {
  let exchanges = 0
  await mockAdminApi(page, {
    'POST /api/v1/auth/identity/complete': () => ++exchanges === 1
      ? { mode: 'test', provider: teamSource, outcome: 'success', authenticated: true, mapped_role: 'admin', checked_at: '2026-10-23T08:00:00Z', return_to: '/admin/authentication?source=team' }
      : { status: 400, body: { code: 'LOGIN_EXPIRED' } },
  })
  await page.goto('/admin/auth/complete')
  await expect(page.getByRole('heading', { name: '登录验证结果' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: '未能完成身份验证' })).toBeVisible()
  await expect(page.getByRole('link', { name: '返回认证源' })).toHaveAttribute('href', '/admin/authentication?source=team')
  await expect(page.getByText(/此提示不代表验证成功/)).toBeVisible()
  await expect(page.getByRole('link', { name: '返回登录' })).toHaveCount(0)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

test('a cancelled provider authorization is not presented as role rejection or provider outage', async ({ page }) => {
  await mockAdminApi(page, {
    'POST /api/v1/auth/identity/complete': { mode: 'test', provider: teamSource, outcome: 'failed', authenticated: false, mapped_role: null, code: 'AUTHENTICATION_NOT_COMPLETED', checked_at: '2026-10-23T08:00:00Z', return_to: '/admin/authentication?source=team' },
  })
  await page.goto('/admin/auth/complete')
  await expect(page.locator('[data-slot="alert"]')).toContainText('身份提供方未授权，本次认证尚未完成')
  await expect(page.getByText('未完成验证', { exact: true })).toBeVisible()
  await expect(page.getByText(/此身份不符合访问规则/)).toHaveCount(0)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

test('removed sources remain distinct when their linked identities have the same display name', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': { ...multiIdentities, identities: ['old-team', 'old-lab'].map((id, index) => ({ ...identity, id: 10 + index, provider: id, provider_matches: false, login_available: false })) },
  })
  await page.goto('/admin/account')
  for (const id of ['old-team', 'old-lab']) await expect(page.getByRole('heading', { name: `先前的身份提供方 · ${id}`, exact: true })).toBeVisible()
  await page.getByRole('button', { name: '解绑 先前的身份提供方 · old-lab' }).click()
  await expect(page.getByRole('dialog').getByRole('heading', { name: '解绑 先前的身份提供方 · old-lab' })).toBeVisible()
})

test('local and organization sign-in are distinct and source errors remain beside the source', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: [teamSource, labSource] },
    'POST /api/v1/auth/oidc/start': { status: 503, body: { code: 'OIDC_UNAVAILABLE' } },
  })
  await page.goto('/admin/login')
  const local = page.getByRole('form', { name: '本地账户登录' })
  const organization = page.getByRole('region', { name: '组织登录' })
  await expect(local.getByLabel('用户名')).toBeVisible()
  await expect(organization).toContainText('已有本地账户？建议先使用本地账户登录')
  await page.screenshot({ path: '/tmp/depsilo-auth-ux-ui/login-1440-dark-zh-api-fixture.png', fullPage: true, animations: 'disabled' })
  await organization.getByRole('button', { name: '使用 Team login 登录' }).click()
  await expect(organization.getByRole('alert')).toBeVisible()
  await expect(local.getByRole('alert')).toHaveCount(0)
})

test('many organization sources can be filtered without changing the chosen source ID', async ({ page, baseURL }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: Array.from({ length: 6 }, (_, index) => ({ id: `source-${index}`, name: `Organization ${index}`, protocol: 'oidc' })) },
    'POST /api/v1/auth/oidc/start': (request: Request) => {
      expect(request.postDataJSON().provider_id).toBe('source-5')
      return { authorization_url: `${baseURL}/selected-organization` }
    },
  })
  await page.route('**/selected-organization', route => route.fulfill({ contentType: 'text/html', body: '<h1>Organization 5 sign-in</h1>' }))
  await page.goto('/admin/login')
  await page.getByLabel('搜索组织登录方式').fill('no-match')
  await expect(page.getByText('没有匹配的登录方式。')).toBeVisible()
  await page.getByLabel('搜索组织登录方式').fill('Organization 5')
  await expect(page.getByRole('button', { name: /^使用 Organization/ })).toHaveCount(1)
  await page.getByRole('button', { name: '使用 Organization 5 登录' }).click()
  await expect(page.getByRole('heading', { name: 'Organization 5 sign-in' })).toBeVisible()
})

test('a newly verified identity requires an explicit choice before creating a session', async ({ page }) => {
  let resolutions = 0
  await mockAdminApi(page, {
    'POST /api/v1/auth/identity/complete': choice,
    'POST /api/v1/auth/identity/resolve': () => { resolutions++; return { status: 500, body: {} } },
  })
  await page.goto('/admin/auth/complete')
  await expect(page.getByRole('heading', { name: '选择如何继续' })).toBeVisible()
  await expect(page.getByText('External operator', { exact: true })).toBeVisible()
  await expect(page.getByLabel('用户名')).toHaveValue('')
  await expect(page.getByRole('button', { name: '验证、绑定并登录' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '创建独立账户并登录' })).toBeHidden()
  expect(resolutions).toBe(0)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  await page.reload()
  await expect(page.getByRole('heading', { name: '选择如何继续' })).toBeVisible()
  expect(resolutions).toBe(0)
})

test('first sign-in can prove a local account, retry a bad password, and link without creating another account', async ({ page }) => {
  let attempts = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'POST /api/v1/auth/identity/complete': choice,
    'POST /api/v1/auth/identity/resolve': (request: Request) => {
      attempts++
      expect(request.headers().authorization).toBeUndefined()
      const body = request.postDataJSON()
      expect(body.action).toBe('link')
      expect(body.username).toBe('my-local-account')
      if (body.password === 'incorrect') return { status: 403, body: { code: 'REAUTHENTICATION_FAILED' } }
      expect(body.password).toBe('correct-test-password')
      return { mode: 'login', token: 'linked-oidc-session', expires_at: 1_950_000_000, return_to: '/admin/account' }
    },
  })
  await page.goto('/admin/auth/complete')
  await page.getByLabel('用户名').fill('my-local-account')
  await page.getByLabel('当前本地密码').fill('incorrect')
  await page.getByRole('button', { name: '验证、绑定并登录' }).click()
  await expect(page.getByRole('alert')).toContainText('本地密码验证失败')
  await expect(page.getByLabel('当前本地密码')).toHaveValue('')
  await expect(page.getByLabel('用户名')).toHaveValue('my-local-account')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  await page.getByLabel('当前本地密码').fill('correct-test-password')
  await page.getByRole('button', { name: '验证、绑定并登录' }).click()
  await expect(page).toHaveURL('/admin/account')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('linked-oidc-session')
  expect(attempts).toBe(2)
  expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain('correct-test-password')
})

test('creating an external account is explicit and communicates its permission authority', async ({ page }) => {
  let created = false
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, permission_source: 'oidc', login_method: 'oidc', can_manage_credentials: false },
    'POST /api/v1/auth/identity/complete': choice,
    'POST /api/v1/auth/identity/resolve': (request: Request) => {
      expect(request.headers().authorization).toBeUndefined()
      expect(request.postDataJSON()).toEqual({ action: 'create' })
      created = true
      return { mode: 'login', token: 'managed-oidc-session', expires_at: 1_950_000_000, return_to: '/admin/account' }
    },
  })
  await page.goto('/admin/auth/complete')
  await page.locator('summary').filter({ hasText: '没有本地账户？创建新账户' }).click()
  await expect(page.getByText(/创建由 Team login 管理的独立账户/)).toBeVisible()
  await expect(page.getByText(/不会合并已有账户，也不会新增本地密码/)).toBeVisible()
  expect(created).toBe(false)
  await page.getByRole('button', { name: '创建独立账户并登录' }).click()
  await expect(page).toHaveURL('/admin/account')
  expect(created).toBe(true)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('managed-oidc-session')
})

test('cancelling a pending identity clears its server transaction without changing an existing session', async ({ page }) => {
  let cancelled = false
  await mockAdminApi(page, {
    'POST /api/v1/auth/identity/complete': choice,
    'POST /api/v1/auth/identity/cancel': (request: Request) => {
      expect(request.headers().authorization).toBeUndefined()
      expect(request.postDataJSON()).toEqual({})
      cancelled = true
      return { status: 204, body: null }
    },
  })
  await page.goto('/admin/auth/complete')
  await page.getByRole('button', { name: '取消并返回登录' }).click()
  await expect(page).toHaveURL('/admin/login')
  expect(cancelled).toBe(true)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

test('an expired identity choice offers recovery without repeatedly submitting an unusable form', async ({ page }) => {
  await mockAdminApi(page, {
    'POST /api/v1/auth/identity/complete': choice,
    'POST /api/v1/auth/identity/resolve': { status: 400, body: { code: 'LOGIN_EXPIRED' } },
  })
  await page.goto('/admin/auth/complete')
  await page.getByLabel('用户名').fill('local-account')
  await page.getByLabel('当前本地密码').fill('not-stored')
  await page.getByRole('button', { name: '验证、绑定并登录' }).click()
  await expect(page.getByRole('heading', { name: '未能完成身份验证' })).toBeVisible()
  await expect(page.getByRole('link', { name: '返回登录' })).toBeVisible()
  await expect(page.getByLabel('当前本地密码')).toHaveCount(0)
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

for (const outcome of ['success', 'denied', 'failed'] as const) {
  test(`source sign-in trial ${outcome} never creates or replaces the administrator session`, async ({ page }) => {
    const actions: string[] = []
    page.on('request', request => { if (/\/auth\/identity\/(resolve|cancel)$/.test(request.url())) actions.push(request.url()) })
    await mockAdminApi(page, {
      'POST /api/v1/auth/identity/complete': (request: Request) => {
        expect(request.headers().authorization).toBe('Bearer e2e-token')
        return { mode: 'test', provider: teamSource, outcome, authenticated: outcome !== 'failed', mapped_role: outcome === 'success' ? 'admin' : null, display_name: outcome !== 'failed' ? 'Verified operator' : undefined, checked_at: '2026-10-23T08:00:00Z', return_to: '/admin/authentication?source=team' }
      },
    })
    await page.goto('/admin/auth/complete')
    await expect(page.getByRole('heading', { name: '登录验证结果' })).toBeVisible()
    await expect(page.getByText(/你当前的管理员登录保持不变/)).toBeVisible()
    await expect(page.getByRole('link', { name: '返回认证源' })).toHaveAttribute('href', '/admin/authentication?source=team')
    await expect(page.locator('[data-slot="alert"]')).toContainText(outcome === 'success' ? '已通过本次身份验证与角色映射' : outcome === 'denied' ? '此身份不符合访问规则' : '本次登录验证未完成')
    expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
    expect(actions).toEqual([])
    if (outcome === 'success') await page.screenshot({ path: '/tmp/depsilo-auth-ux-ui/login-trial-success-api-fixture.png', fullPage: true, animations: 'disabled' })
  })
}

test('account marks only the precise current identity and separates bound from available sources', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, login_method: 'oidc', can_manage_credentials: false },
    'GET /api/v1/auth/identities': { ...multiIdentities, current_identity_id: 7, current_provider_id: 'team', identities: [{ ...identity, provider: 'team', provider_name: 'Team login' }] },
  })
  await page.goto('/admin/account')
  const current = page.getByRole('region', { name: '当前登录方式' })
  const available = page.getByRole('region', { name: '可绑定的登录方式' })
  await expect(current.getByText('本次登录', { exact: true })).toHaveCount(1)
  await expect(current.getByRole('heading', { name: 'Team login' })).toBeVisible()
  await expect(available.getByRole('button', { name: '绑定 Homelab' })).toBeVisible()
  await expect(current.getByText('认证源 ID：team', { exact: true })).toBeHidden()
  await page.screenshot({ path: '/tmp/depsilo-auth-ux-ui/account-current-and-available-1440-dark-zh-api-fixture.png', fullPage: true, animations: 'disabled' })
  await current.getByRole('button', { name: '解绑', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('解绑后当前会话将结束')
})

for (const sample of [{ width: 1440, theme: 'dark' as const, lang: 'zh' as const }, { width: 320, theme: 'light' as const, lang: 'en' as const }]) {
  test(`first identity choice is accessible at ${sample.width}px ${sample.theme}`, async ({ page }) => {
    await page.setViewportSize({ width: sample.width, height: 1000 })
    await setUiPreferences(page, sample.theme, sample.lang)
    await mockAdminApi(page, { 'POST /api/v1/auth/identity/complete': choice })
    await page.goto('/admin/auth/complete')
    await expect(page.getByRole('heading', { name: sample.lang === 'zh' ? '选择如何继续' : 'Choose how to continue' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.locator('summary').focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('button', { name: sample.lang === 'zh' ? '创建独立账户并登录' : 'Create a separate account and sign in' })).toBeVisible()
    await page.screenshot({ path: `/tmp/depsilo-auth-ux-ui/choice-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
  })
}

test('login selects a source explicitly and only that source shows its redirect state', async ({ page, baseURL }) => {
  let release!: () => void
  const waiting = new Promise<void>(resolve => { release = resolve })
  let selected: unknown
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: [teamSource, labSource] },
    'POST /api/v1/auth/oidc/start': async (request: Request) => {
      selected = request.postDataJSON()
      await waiting
      return { authorization_url: `${baseURL}/lab-login` }
    },
  })
  await page.route('**/lab-login', route => route.fulfill({ contentType: 'text/html', body: '<h1>Lab sign-in</h1>' }))
  await page.goto('/admin/login?next=%2Fadmin%2Faccount')
  await page.getByRole('button', { name: '使用 Homelab 登录' }).click()
  await expect(page.getByRole('button', { name: '正在前往登录…' })).toHaveCount(1)
  await expect(page.getByRole('button', { name: '使用 Team login 登录' })).toBeDisabled()
  expect(selected).toEqual({ return_to: '/admin/account', provider_id: 'lab' })
  release()
  await expect(page.getByRole('heading', { name: 'Lab sign-in' })).toBeVisible()
})

test('a local account binds a second source and unlinking one preserves the other', async ({ page, baseURL }) => {
  let bound = [{ ...identity, provider: 'team', provider_name: 'Team login', login_available: true, provider_matches: true }]
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': () => ({ ...multiIdentities, identities: bound, providers: [teamSource, labSource].map(source => ({ ...source, can_link: !bound.some(row => row.provider === source.id) })) }),
    'POST /api/v1/auth/identities/link': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ password: 'correct', provider_id: 'lab' })
      bound.push({ ...identity, id: 8, provider: 'lab', provider_name: 'Homelab', login_available: true, provider_matches: true })
      return { authorization_url: `${baseURL}/mock-link-complete` }
    },
    'POST /api/v1/auth/identities/7/unlink': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ password: 'correct' })
      bound = bound.filter(row => row.id !== 7)
      return { status: 204, body: null }
    },
  })
  await page.route('**/mock-link-complete', route => route.fulfill({ contentType: 'text/html', body: '<h1>Source linked</h1>' }))
  await page.goto('/admin/account')
  await expect(page.getByRole('button', { name: '绑定 Team login' })).toHaveCount(0)
  await page.getByRole('button', { name: '绑定 Homelab', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('heading', { name: '绑定 Homelab' })).toBeVisible()
  await page.getByLabel('当前本地密码').fill('correct')
  await page.getByRole('button', { name: '验证并前往绑定' }).click()
  await expect(page.getByRole('heading', { name: 'Source linked' })).toBeVisible()
  await page.goto('/admin/account')
  await expect(page.getByText('已绑定', { exact: true })).toHaveCount(2)
  await expect(page.getByRole('button', { name: /^绑定 / })).toHaveCount(0)
  await page.screenshot({ path: '/tmp/depsilo-multi-oidc-ui/account-two-bindings-api-fixture.png', fullPage: true, animations: 'disabled' })
  await page.getByRole('button', { name: '解绑', exact: true }).first().click()
  await expect(page.getByRole('dialog').getByRole('heading', { name: '解绑 Team login' })).toBeVisible()
  await page.getByLabel('当前本地密码').fill('correct')
  await page.getByRole('button', { name: '确认解绑' }).click()
  await expect(page.getByRole('button', { name: '绑定 Team login' })).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Homelab' })).toHaveCount(0)
  await expect(page.getByText('已绑定', { exact: true })).toHaveCount(1)
  expect(bound.map(row => row.provider)).toEqual(['lab'])
})

test('readonly local users can bind any eligible source with their own password', async ({ page, baseURL }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, role: 'readonly', can_write: false, can_manage_credentials: false },
    'GET /api/v1/auth/identities': multiIdentities,
    'POST /api/v1/auth/identities/link': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ password: 'own-password', provider_id: 'team' })
      return { authorization_url: `${baseURL}/team-login` }
    },
  })
  await page.route('**/team-login', route => route.fulfill({ contentType: 'text/html', body: '<h1>Team sign-in</h1>' }))
  await page.goto('/admin/account')
  await expect(page.getByRole('button', { name: /^绑定 / })).toHaveCount(2)
  await page.getByRole('button', { name: '绑定 Team login', exact: true }).click()
  await page.getByLabel('当前本地密码').fill('own-password')
  await page.getByRole('button', { name: '验证并前往绑定' }).click()
  await expect(page.getByRole('heading', { name: 'Team sign-in' })).toBeVisible()
})

test('managed accounts cannot bind another source or unlink their managing identity', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, permission_source: 'oidc', login_method: 'oidc', can_manage_credentials: false },
    'GET /api/v1/auth/identities': { ...multiIdentities, permission_source: 'oidc', password_enabled: false, can_link: false, identities: [{ ...identity, provider: 'team', provider_name: 'Team login', can_unlink: false }], providers: [teamSource, labSource].map(source => ({ ...source, can_link: false })) },
  })
  await page.goto('/admin/account')
  await expect(page.getByRole('heading', { name: '可绑定的登录方式' })).toHaveCount(0)
  await expect(page.getByText('此账户由外部认证源管理，不能绑定其他登录方式。')).toBeVisible()
  await expect(page.getByRole('button', { name: /^绑定 |^解绑$/ })).toHaveCount(0)
})

test('disabled and removed sources stay distinguishable without concealing available sources', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 900 })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': { ...multiIdentities, providers: [teamSource, { ...labSource, enabled: false, can_link: false }], identities: [{ ...identity, provider: 'removed', provider_name: 'OIDC', provider_matches: false, login_available: false }] },
  })
  await page.goto('/admin/account')
  await expect(page.getByText('已绑定 · 不可用', { exact: true })).toBeVisible()
  await expect(page.getByText('认证源 ID：removed', { exact: true })).toBeHidden()
  await page.locator('summary[aria-label="查看 先前的身份提供方 · removed 的详情"]').click()
  await expect(page.getByText('认证源 ID：removed', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '解绑 先前的身份提供方 · removed', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Team login' })).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Homelab' })).toHaveCount(0)
  await expect(page.getByText('此认证源尚未启用，暂时不能绑定或用于登录。')).toBeHidden()
  await page.locator('summary').filter({ hasText: '未启用的认证源（1）' }).click()
  await expect(page.getByText('此认证源尚未启用，暂时不能绑定或用于登录。')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
  await page.screenshot({ path: '/tmp/depsilo-multi-oidc-ui/account-320-disabled-removed-api-fixture.png', fullPage: true, animations: 'disabled' })
})

test('public source loading, failure, and an empty source list do not pretend a provider exists', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  const api = await mockAdminApi(page, {
    'GET /api/v1/auth/providers': async () => { await pending; return { status: 503, body: { code: 'UNAVAILABLE' } } },
  })
  await page.goto('/admin/login')
  await expect(page.getByText('正在读取其他登录方式…')).toBeVisible()
  await expect(page.getByRole('button', { name: /^使用 .* 登录$/ })).toHaveCount(0)
  release()
  await expect(page.getByText('外部登录方式暂时无法读取。仍可使用本地密码登录。')).toBeVisible()
  api.overrides['GET /api/v1/auth/providers'] = { providers: [] }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('外部登录方式暂时无法读取。仍可使用本地密码登录。')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /^使用 .* 登录$/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeVisible()
})

test('an unavailable selected source keeps local sign-in and other sources usable', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': { providers: [teamSource, labSource] },
    'POST /api/v1/auth/oidc/start': (request: Request) => {
      expect(request.postDataJSON().provider_id).toBe('lab')
      return { status: 503, body: { code: 'OIDC_UNAVAILABLE' } }
    },
  })
  await page.goto('/admin/login')
  await page.getByLabel('用户名').fill('local-admin')
  await page.getByLabel('密码', { exact: true }).fill('own-password')
  await page.getByRole('button', { name: '使用 Homelab 登录' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByRole('button', { name: '使用 Team login 登录' })).toBeEnabled()
  await expect(page.getByRole('button', { name: '使用 Homelab 登录' })).toBeEnabled()
  await expect(page.getByRole('button', { name: '登录', exact: true })).toBeEnabled()
})

test('account loading and a binding with unknown source availability never appear as an empty account', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': async () => {
      await pending
      return { ...localIdentities, provider: null, providers: [], can_link: false, identities: [{ ...identity, provider: 'unknown', provider_name: 'OIDC' }] }
    },
  })
  await page.goto('/admin/account')
  await expect(page.getByRole('heading', { name: '账户安全', exact: true })).toBeVisible()
  await expect(page.getByRole('status')).toBeVisible()
  await expect(page.getByText('外部登录尚未启用。启用后，可在这里绑定你的外部账户。')).toHaveCount(0)
  release()
  await page.locator('summary[aria-label="查看 OIDC 的详情"]').click()
  await expect(page.getByText('认证源 ID：unknown', { exact: true })).toBeVisible()
  await expect(page.getByText('已绑定 · 不可用', { exact: true })).toBeVisible()
  await expect(page.getByText('暂时无法确认此认证源的登录可用状态。请刷新状态，或联系管理员。')).toBeVisible()
  await expect(page.getByText('此登录方式已被管理员停用，现有绑定仍保留。')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '解绑', exact: true })).toBeVisible()
})

test('external sign-in preserves a safe deep link and does not attach an existing token to public auth', async ({ page, baseURL }) => {
  let startCalls = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/providers': (request: Request) => {
      expect(request.headers().authorization).toBeUndefined()
      return { providers: [{ id: 'oidc', name: 'Casdoor' }] }
    },
    'POST /api/v1/auth/oidc/start': (request: Request) => {
      startCalls++
      expect(request.headers().authorization).toBeUndefined()
      expect(request.postDataJSON()).toEqual({ return_to: '/admin/cache?q=six#results', provider_id: 'oidc' })
      return { authorization_url: `${baseURL}/test-provider-login` }
    },
  })
  await page.route('**/test-provider-login', route => route.fulfill({ contentType: 'text/html', body: '<h1>Test provider login</h1>' }))
  await page.goto('/admin/login?next=%2Fadmin%2Fcache%3Fq%3Dsix%23results')
  await page.getByRole('button', { name: '使用 Casdoor 登录' }).click()
  await expect(page.getByRole('heading', { name: 'Test provider login' })).toBeVisible()
  expect(startCalls).toBe(1)
})

test('login completion is single use under StrictMode and restores the deep link', async ({ page }) => {
  let exchanges = 0
  await page.addInitScript(() => localStorage.removeItem('token'))
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'POST /api/v1/auth/identity/complete': (request: Request) => {
      exchanges++
      expect(request.headers().authorization).toBeUndefined()
      expect(request.headers()['content-type']).toContain('application/json')
      expect(request.postDataJSON()).toEqual({})
      return { mode: 'login', token: 'oidc-new-session', expires_at: 1_900_000_000, return_to: '/admin/cache?q=six' }
    },
  })
  await page.goto('/admin/auth/complete')
  await expect(page).toHaveURL(/\/admin\/cache\?q=six$/)
  await expect.poll(() => page.evaluate(() => localStorage.getItem('token'))).toBe('oidc-new-session')
  expect(exchanges).toBe(1)
})

test('link completion keeps the existing local session and remains reachable before onboarding', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/admin/onboarding/status': { status: 'not_started' },
    'GET /api/v1/auth/identities': { ...localIdentities, can_link: false, identities: [identity] },
    'POST /api/v1/auth/identity/complete': { mode: 'link', result: 'linked', return_to: '/admin/account' },
  })
  await page.goto('/admin/auth/complete')
  await expect(page).toHaveURL('/admin/account')
  await expect(page.getByText('登录方式已绑定。原有账户权限保持不变。')).toBeVisible()
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
})

test('binding requires password proof, preserves login on a bad proof, and clears the password field', async ({ page, baseURL }) => {
  let attempts = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': localIdentities,
    'POST /api/v1/auth/identities/link': (request: Request) => {
      attempts++
      expect(request.headers().authorization).toBe('Bearer e2e-token')
      if (request.postDataJSON().password === 'incorrect') return { status: 403, body: { code: 'REAUTHENTICATION_FAILED' } }
      return { authorization_url: `${baseURL}/test-provider-login` }
    },
  })
  await page.route('**/test-provider-login', route => route.fulfill({ contentType: 'text/html', body: '<h1>Test provider login</h1>' }))
  await page.goto('/admin/account')
  await page.getByRole('button', { name: '绑定 Casdoor', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('button', { name: '验证并前往绑定' })).toBeDisabled()
  await dialog.getByLabel('当前本地密码').fill('incorrect')
  await dialog.getByRole('button', { name: '验证并前往绑定' }).click()
  await expect(dialog.getByRole('alert')).toContainText('本地密码验证失败')
  await expect(dialog.getByLabel('当前本地密码')).toHaveValue('')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  await dialog.getByLabel('当前本地密码').fill('correct')
  await dialog.getByRole('button', { name: '验证并前往绑定' }).click()
  await expect(page.getByRole('heading', { name: 'Test provider login' })).toBeVisible()
  expect(attempts).toBe(2)
})

test('unlink explains revocation and refreshes the remaining password method', async ({ page }) => {
  let linked = true
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': () => ({ ...localIdentities, can_link: !linked, identities: linked ? [identity] : [] }),
    'POST /api/v1/auth/identities/7/unlink': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ password: 'correct' })
      linked = false
      return { status: 204, body: null }
    },
  })
  await page.goto('/admin/account')
  await page.getByRole('button', { name: '解绑', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('保留本地密码登录')
  await dialog.getByLabel('当前本地密码').fill('correct')
  await dialog.getByRole('button', { name: '确认解绑' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByText('登录方式已解绑，相关外部登录会话已撤销。')).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Casdoor' })).toBeVisible()
  expect(linked).toBe(false)
})

for (const code of ['LOGIN_EXPIRED', 'ACCESS_DENIED', 'OIDC_UNAVAILABLE']) {
  test(`completion failure ${code} has recovery links without removing an existing session`, async ({ page }) => {
    await mockAdminApi(page, { 'POST /api/v1/auth/identity/complete': { status: 403, body: { code } } })
    await page.goto('/admin/auth/complete')
    await expect(page.getByRole('heading', { name: '未能完成身份验证' })).toBeVisible()
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(page.getByRole('link', { name: '返回登录' })).toBeVisible()
    expect(await page.evaluate(() => localStorage.getItem('token'))).toBe('e2e-token')
  })
}

test('managed identities cannot be unlinked or given local credentials; user operations require local credential authority', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, permission_source: 'oidc', login_method: 'oidc', can_manage_credentials: false },
    'GET /api/v1/auth/identities': { ...localIdentities, permission_source: 'oidc', password_enabled: false, can_link: false, identities: [{ ...identity, can_unlink: false }] },
  })
  await page.goto('/admin/account')
  await expect(page.getByText('此账户不使用本地密码。')).toBeVisible()
  await expect(page.getByRole('button', { name: '解绑', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '绑定 Casdoor' })).toHaveCount(0)
  await page.goto('/admin/users')
  await expect(page.getByRole('button', { name: '添加用户' })).toHaveCount(0)
  await expect(page.getByText('当前登录方式不能管理用户或长期凭据。请使用具备管理权限的本地账户密码重新登录。')).toBeVisible()
})

test('local administrators see external permission sources without password or role editing', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/admin/users': [{ id: 2, username: 'managed-operator', role: 'admin', enabled: true, permission_source: 'oidc', created_at: '2026-10-22T08:00:00Z', updated_at: '2026-10-22T08:00:00Z', last_login_at: null }],
  })
  await page.goto('/admin/users')
  await expect(page.getByText('外部身份提供方', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '编辑 managed-operator' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '禁用 managed-operator' })).toBeVisible()
})

test('account load failure remains recoverable rather than showing no identities', async ({ page }) => {
  const api = await mockAdminApi(page, { 'GET /api/v1/auth/identities': { status: 503, body: { code: 'UNAVAILABLE' } } })
  await page.goto('/admin/account')
  await expect(page.getByText('登录方式暂时无法读取')).toBeVisible()
  await expect(page.getByText('未绑定', { exact: true })).toHaveCount(0)
  api.overrides['GET /api/v1/auth/identities'] = localIdentities
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByRole('button', { name: '绑定 Casdoor' })).toBeVisible()
})

for (const role of ['admin', 'readonly'] as const) {
  test(`no configured source separates personal account help from ${role} system access`, async ({ page }) => {
    await page.setViewportSize({ width: role === 'admin' ? 1440 : 320, height: 900 })
    await setUiPreferences(page, 'light', 'zh')
    await mockAdminApi(page, {
      'GET /api/v1/auth/me': { ...localPrincipal, role, can_write: role === 'admin', can_manage_credentials: role === 'admin' },
      'GET /api/v1/auth/identities': { ...localIdentities, can_link: false, provider: null, providers: [] },
    })
    await page.goto('/admin/account')
    await expect(page.getByText('外部登录尚未启用。启用后，可在这里绑定你的外部账户。')).toBeVisible()
    await expect(page.getByRole('heading', { name: '外部登录', exact: true })).toBeVisible()
    await expect(page.getByText('Casdoor', { exact: true })).toHaveCount(0)
    await expect(page.getByText('Issuer', { exact: true })).toHaveCount(0)
    await expect(page.getByText('登录回调地址', { exact: true })).toHaveCount(0)
    await expect(page.getByText(/auth\.oidc/)).toHaveCount(0)
    await expect(page.getByText(/30 分钟/)).toHaveCount(0)
    const manageLink = page.getByRole('link', { name: '管理身份认证' })
    if (role === 'admin') {
      await expect(manageLink).toHaveAttribute('href', '/admin/authentication')
      await page.screenshot({ path: '/tmp/depsilo-system-auth-ui/account-disabled-admin-light-api-fixture.png', fullPage: true, animations: 'disabled' })
    } else {
      await expect(manageLink).toHaveCount(0)
      await expect(page.getByText('如需使用外部登录，请联系管理员。')).toBeVisible()
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
  })
}

test('a disabled provider preserves its linked identity and allows password-confirmed unlinking', async ({ page }) => {
  let linked = true
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': localPrincipal,
    'GET /api/v1/auth/identities': () => ({ ...localIdentities, can_link: false, identities: linked ? [identity] : [], provider: { ...provider, enabled: false } }),
    'POST /api/v1/auth/identities/7/unlink': (request: Request) => {
      expect(request.postDataJSON()).toEqual({ password: 'correct' })
      linked = false
      return { status: 204, body: null }
    },
  })
  await page.goto('/admin/account')
  await expect(page.getByText('此登录方式已被管理员停用，现有绑定仍保留。')).toBeVisible()
  await expect(page.getByText('已绑定 · 不可用', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '解绑', exact: true }).click()
  await page.getByLabel('当前本地密码').fill('correct')
  await page.getByRole('button', { name: '确认解绑' }).click()
  await expect(page.getByText('登录方式已解绑，相关外部登录会话已撤销。')).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Casdoor' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: '管理身份认证' })).toBeVisible()
})

for (const permissionSource of ['local', 'oidc'] as const) {
  test(`a replaced provider does not relabel or validate the previous ${permissionSource} identity`, async ({ page }) => {
    const local = permissionSource === 'local'
    await page.setViewportSize({ width: 320, height: 900 })
    await mockAdminApi(page, {
      'GET /api/v1/auth/me': { ...localPrincipal, permission_source: permissionSource, login_method: local ? 'password' : 'oidc', can_manage_credentials: local },
      'GET /api/v1/auth/identities': {
        ...localIdentities, permission_source: permissionSource, password_enabled: local, can_link: false,
        provider: { ...provider, name: 'Replacement provider', issuer: 'https://replacement.example' },
        identities: [{ ...identity, provider_name: 'OIDC', provider_matches: false, login_available: false, can_unlink: local }],
      },
    })
    await page.goto('/admin/account')
    await expect(page.getByRole('heading', { name: '先前的身份提供方 · oidc', exact: true })).toBeVisible()
    await expect(page.getByText('已绑定 · 不可用', { exact: true })).toBeVisible()
    await expect(page.getByText('此认证源已移除或发生变更，此绑定已无法用于登录。')).toBeVisible()
    await expect(page.getByText('Replacement provider', { exact: true })).toHaveCount(0)
    await expect(page.getByText('此登录方式已被管理员停用，现有绑定仍保留。')).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    if (local) {
      await page.getByRole('button', { name: '解绑 先前的身份提供方 · oidc', exact: true }).click()
      await expect(page.getByRole('dialog').getByRole('heading', { name: '解绑 先前的身份提供方 · oidc' })).toBeVisible()
      await expect(page.getByLabel('当前本地密码')).toBeVisible()
    } else {
      await expect(page.getByRole('button', { name: '解绑', exact: true })).toHaveCount(0)
      await expect(page.getByText('这是此账户的管理身份，不能解绑。访问权限由身份提供方管理，如需变更请联系管理员。')).toBeVisible()
    }
  })
}

test('readonly local users can manage their own login method while stale results prevent mutation', async ({ page }) => {
  const api = await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, role: 'readonly', can_write: false, can_manage_credentials: false },
    'GET /api/v1/auth/identities': localIdentities,
  })
  await page.goto('/admin/account')
  await page.getByRole('button', { name: '绑定 Casdoor', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  api.overrides['GET /api/v1/auth/identities'] = { status: 503, body: { code: 'UNAVAILABLE' } }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('刷新失败，以下为上次读取的登录方式。请刷新后再操作。')).toBeVisible()
  await expect(page.getByRole('button', { name: '绑定 Casdoor', exact: true })).toBeDisabled()
  await expect(page.getByRole('heading', { name: '可绑定的登录方式', exact: true })).toBeVisible()
})

for (const sample of [{ width: 1440, theme: 'dark' as const, lang: 'zh' as const }, { width: 320, theme: 'light' as const, lang: 'en' as const }]) {
  test(`account and proof dialog are accessible at ${sample.width}px ${sample.theme}`, async ({ page }) => {
    await page.setViewportSize({ width: sample.width, height: 900 })
    await setUiPreferences(page, sample.theme, sample.lang)
    await mockAdminApi(page, { 'GET /api/v1/auth/me': localPrincipal, 'GET /api/v1/auth/identities': { ...localIdentities, can_link: false, identities: [identity] } })
    await page.goto('/admin/account')
    await expect(page.getByRole('heading', { name: sample.lang === 'zh' ? '账户安全' : 'Account security', exact: true })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.screenshot({ path: `/tmp/depsilo-oidc-ui/account-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: true, animations: 'disabled' })
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    const details = page.locator('details').filter({ has: page.locator('summary[aria-label]') })
    await details.locator('summary').focus()
    await page.keyboard.press('Enter')
    await expect(details).toHaveAttribute('open', '')
    await expect(page.getByText(sample.lang === 'zh' ? /30 分钟/ : /30 minutes/)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.getByRole('button', { name: sample.lang === 'zh' ? '解绑' : 'Unlink', exact: true }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.screenshot({ path: `/tmp/depsilo-oidc-ui/unlink-${sample.width}-${sample.theme}-${sample.lang}-api-fixture.png`, fullPage: false, animations: 'disabled' })
    expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([])
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()
    await expect(page.getByRole('button', { name: sample.lang === 'zh' ? '解绑' : 'Unlink', exact: true })).toBeFocused()
  })
}

test('a local account signing in through OIDC cannot mint long-lived credentials or change cascade secrets', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/auth/me': { ...localPrincipal, login_method: 'oidc', can_manage_credentials: false } })
  await page.goto('/admin/compile-cache')
  await expect(page.getByRole('heading', { name: '编译缓存', exact: true }).first()).toBeAttached()
  await expect(page.getByRole('button', { name: '创建凭据', exact: true })).toHaveCount(0)
  await expect(page.getByText('当前登录方式不能管理用户或长期凭据。请使用具备管理权限的本地账户密码重新登录。')).toBeVisible()
  await page.goto('/admin/cascade')
  await expect(page.locator('[data-cascade-config-form]').getByRole('spinbutton').first()).toBeDisabled()
  await expect(page.getByRole('button', { name: '添加父节点', exact: true })).toBeDisabled()
})

test('API-token sessions explain the interactive sign-in requirement without requesting identities', async ({ page }) => {
  let reads = 0
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { ...localPrincipal, auth_method: 'api_token', login_method: 'api_token', token_permissions: 'readwrite' },
    'GET /api/v1/auth/identities': () => { reads++; return localIdentities },
  })
  await page.goto('/admin/account')
  await expect(page.getByText('当前使用 API Token 访问。请通过交互式登录进入此页面，查看与管理登录方式。')).toBeVisible()
  expect(reads).toBe(0)
})

test('an expired protected session returns to login with the account destination preserved', async ({ page }) => {
  const api = await mockAdminApi(page, { 'GET /api/v1/auth/identities': localIdentities })
  await page.goto('/admin/account')
  await expect(page.getByRole('button', { name: '绑定 Casdoor' })).toBeVisible()
  api.overrides['GET /api/v1/auth/identities'] = { status: 401, body: { code: 'UNAUTHORIZED' } }
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page).toHaveURL('/admin/login')
  expect(await page.evaluate(() => localStorage.getItem('token'))).toBeNull()
})
