// Real Depsilo + Casdoor browser integration. Run `make build` first.
// All generated configuration, credentials and logs stay in a private directory.
import { randomBytes } from 'node:crypto'
import { execFileSync, spawn } from 'node:child_process'
import { createWriteStream } from 'node:fs'
import { mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { homedir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const require = createRequire(join(root, 'web/package.json'))
const { chromium } = require('playwright')
const base = 'http://127.0.0.1:18081'
const state = join(homedir(), '.cache/depsilo/oidc-casdoor')
const results = []
let stage = 'start isolated application'
let server, browser, log, run
function check(ok, message) { if (!ok) { const e = new Error(message); e.name = 'ProbeAssertion'; throw e } }
const pause = ms => new Promise(resolve => setTimeout(resolve, ms))
const isRunning = child => child && child.exitCode === null && child.signalCode === null
async function stopServer() {
  if (!isRunning(server)) return
  server.kill('SIGTERM')
  for (let attempt = 0; attempt < 100 && isRunning(server); attempt++) await pause(100)
  if (isRunning(server)) {
    server.kill('SIGKILL')
    for (let attempt = 0; attempt < 50 && isRunning(server); attempt++) await pause(100)
  }
  check(!isRunning(server), 'Isolated Depsilo did not stop')
}

try {
  const credentials = JSON.parse(await readFile(join(state, 'credentials.json'), 'utf8'))
  check(credentials.issuer === 'http://127.0.0.1:18000' && credentials.redirect_uris.includes(`${base}/api/v1/auth/oidc/callback`), 'Unexpected isolated fixture configuration')
  try { await fetch(`${base}/ready`, { signal: AbortSignal.timeout(1000) }); throw new Error('occupied') } catch (error) { check(error.message !== 'occupied', 'Port 18081 is occupied; no existing service was changed') }
  await mkdir(join(state, 'runs'), { recursive: true, mode: 0o700 })
  run = await mkdtemp(join(state, 'runs/depsilo-'))
  const password = randomBytes(24).toString('base64url') + '9!aA'
  const configPath = join(run, 'config.toml')
  const config = `config_version = 1
[server]
host = "127.0.0.1"
port = 18081
log_level = "warn"
[database]
driver = "sqlite"
dsn = ${JSON.stringify(join(run, 'depsilo.db'))}
[storage]
type = "local"
path = ${JSON.stringify(join(run, 'cache'))}
[auth]
enabled = true
token_ttl = "168h"
[auth.oidc]
enabled = true
display_name = "Casdoor"
issuer = ${JSON.stringify(credentials.issuer)}
client_id = ${JSON.stringify(credentials.client_id)}
redirect_url = "${base}/api/v1/auth/oidc/callback"
admin_groups = ["depsilo-test/depsilo-admins"]
readonly_groups = ["depsilo-test/depsilo-readers"]
groups_claim = "groups"
session_ttl = "30m"
allow_insecure_loopback = true
`
  await writeFile(configPath, config, { mode: 0o600 })
  log = createWriteStream(join(run, 'server.log'), { mode: 0o600 })
  // Never inherit application overrides from the caller's normal instance.
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('DEPSILO_')))
  async function startServer() {
    let spawnFailed = false
    server = spawn(join(root, 'bin/depsilo'), ['serve', '--config', configPath], { cwd: run,
      env: { ...env, DEPSILO_ADMIN_USERNAME: 'recovery', DEPSILO_ADMIN_PASSWORD: password, DEPSILO_AUTH_OIDC_CLIENT_SECRET: credentials.client_secret }, stdio: ['ignore', 'pipe', 'pipe'] })
    server.on('error', () => { spawnFailed = true })
    // A restart appends to the same private log; ending either pipe must not close it.
    server.stdout.pipe(log, { end: false }); server.stderr.pipe(log, { end: false })
    let ready = false
    for (let attempt = 0; attempt < 100; attempt++) {
      check(!spawnFailed && isRunning(server), 'Isolated Depsilo exited before readiness')
      try { ready = (await fetch(`${base}/ready`, { signal: AbortSignal.timeout(1000) })).ok } catch { /* startup */ }
      if (ready) break
      await pause(200)
    }
    check(ready, 'Isolated Depsilo did not become ready')
  }
  await startServer()
  browser = await chromium.launch()
  async function fresh(token) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 } })
    await context.addInitScript(({ origin }) => {
      if (location.origin !== origin) return
      localStorage.setItem('lang', 'zh'); localStorage.setItem('depsilo-theme', 'dark')
    }, { origin: base })
    const page = await context.newPage()
    await page.goto(`${base}/admin/login`)
    if (token) await page.evaluate(value => localStorage.setItem('token', value), token)
    return { context, page }
  }
  async function api(context, path, token, data, method = data === undefined ? 'GET' : 'POST') {
    const response = await context.request.fetch(`${base}/api/v1/${path}`, { method, headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), Origin: base }, ...(data === undefined ? {} : { data }) })
    let body = null
    try { body = await response.json() } catch { /* 204 */ }
    return { status: response.status(), body }
  }
  async function casdoor(page, username) {
    await page.locator('#username').waitFor()
    await page.locator('#username').fill(credentials.users[username].username)
    await page.locator('#password').fill(credentials.users[username].password)
    const completed = page.waitForResponse(response => [`${base}/api/v1/auth/identity/complete`, `${base}/api/v1/auth/oidc/complete`].includes(response.url()), { timeout: 25000 })
    await page.getByRole('button', { name: 'Sign In', exact: true }).click()
    const response = await completed
    return { status: response.status(), body: await response.json() }
  }
  function authenticationCounts() {
    const query = 'import sqlite3,json,sys; c=sqlite3.connect("file:"+sys.argv[1]+"?mode=ro",uri=True); print(json.dumps([c.execute("SELECT COUNT(*) FROM "+t).fetchone()[0] for t in ["users","external_identities","identity_sessions"]]))'
    return JSON.parse(execFileSync('python3', ['-c', query, join(run, 'depsilo.db')], { encoding: 'utf8' }))
  }
  async function chooseAccount(page, action) {
    await page.getByRole('heading', { name: '选择如何继续', exact: true }).waitFor()
    const resolved = page.waitForResponse(response => [`${base}/api/v1/auth/identity/resolve`, `${base}/api/v1/auth/oidc/resolve`].includes(response.url()))
    if (action === 'link') {
      await page.getByLabel('用户名', { exact: true }).fill('recovery')
      await page.getByLabel('当前本地密码', { exact: true }).fill(password)
      await page.getByRole('button', { name: '验证、绑定并登录', exact: true }).click()
    } else {
      await page.getByText('没有本地账户？创建新账户', { exact: true }).click()
      await page.getByRole('button', { name: '创建独立账户并登录', exact: true }).click()
    }
    const response = await resolved
    return { status: response.status(), body: await response.json() }
  }
  async function external(username, destination = '/admin/account', action = 'create') {
    const { context, page } = await fresh()
    await page.goto(`${base}/admin/login?next=${encodeURIComponent(destination)}`)
    await page.getByRole('button', { name: '使用 Casdoor 登录', exact: true }).click()
    let completion = await casdoor(page, username)
    if (completion.body?.mode === 'choice' && action !== 'pending') completion = await chooseAccount(page, action)
    return { context, page, ...completion, token: completion.body?.token }
  }

  stage = 'local recovery and explicit linking'
  const local = await fresh()
  const login = await api(local.context, 'auth/login', null, { username: 'recovery', password })
  check(login.status === 200, 'Local recovery login failed')
  const localToken = login.body.token
  const localMe = await api(local.context, 'auth/me', localToken)
  await local.page.evaluate(token => localStorage.setItem('token', token), localToken)

  stage = 'protocol-based source creation and removal preserve the active legacy source'
  await local.page.goto(`${base}/admin/authentication`)
  await local.page.getByRole('button', { name: '添加认证源', exact: true }).click()
  await local.page.getByRole('dialog').getByRole('button', { name: /^OIDC\b/ }).click()
  await local.page.waitForURL(`${base}/admin/authentication?new=1`)
  check(await local.page.getByLabel('认证源名称', { exact: true }).inputValue() === '', 'New authentication source has an invented default name')
  await local.page.getByLabel('认证源名称', { exact: true }).fill('测试目录')
  await local.page.getByText('高级选项', { exact: true }).click()
  await local.page.getByLabel('认证源标识', { exact: true }).fill('additional-oidc')
  await local.page.getByRole('switch', { name: '允许本机 HTTP 测试', exact: true }).check()
  await local.page.screenshot({ path: join(run, 'authentication-add-source.png'), fullPage: true })
  await local.page.getByRole('button', { name: '检查并保存', exact: true }).click()
  const create = local.page.getByRole('dialog')
  const createdResponse = local.page.waitForResponse(response => response.url() === `${base}/api/v1/admin/authentication/sources` && response.request().method() === 'POST')
  await create.getByRole('button', { name: '保存配置', exact: true }).click()
  stage = 'receive created source response'
  const created = await createdResponse
  check(created.status() === 201, 'Protocol-based authentication source creation failed')
  const createdSource = await created.json()
  check(createdSource.id === 'additional-oidc' && createdSource.protocol === 'oidc' && !createdSource.legacy && !createdSource.configured.enabled && createdSource.effective === null && createdSource.pending_restart, 'New source unexpectedly inherited legacy or active authentication state')
  stage = 'open newly created source editor'
  await local.page.waitForURL(`${base}/admin/authentication?source=additional-oidc`)
  await create.waitFor({ state: 'hidden' })
  await local.page.getByLabel('认证源名称', { exact: true }).waitFor()
  check(await local.page.getByLabel('认证源名称', { exact: true }).inputValue() === '测试目录', 'Source name did not survive creation')
  check(await local.page.getByLabel('客户端 ID', { exact: true }).inputValue() === '', 'New source inherited another source client')
  stage = 'inspect created source metadata'
  const listedSources = await api(local.context, 'admin/authentication/sources', localToken)
  check(listedSources.status === 200 && listedSources.body.sources.length === 2 && listedSources.body.sources.some(source => source.id === 'oidc' && source.legacy && source.effective.enabled), 'Source list lost the running legacy source')
  const availableProviders = await api(local.context, 'auth/providers')
  check(availableProviders.status === 200 && availableProviders.body.providers.length === 1 && availableProviders.body.providers[0].id === 'oidc' && availableProviders.body.providers[0].protocol === 'oidc', 'Saving an inactive source changed the available login methods')
  stage = 'show authentication source list'
  await local.page.goto(`${base}/admin/authentication`)
  await local.page.getByRole('heading', { name: '测试目录', exact: true }).waitFor()
  await local.page.screenshot({ path: join(run, 'authentication-source-list.png'), fullPage: true })
  stage = 'remove unused authentication source'
  await local.page.goto(`${base}/admin/authentication?source=additional-oidc`)
  await local.page.getByRole('button', { name: '移除认证源', exact: true }).click()
  const deletedResponse = local.page.waitForResponse(response => response.url() === `${base}/api/v1/admin/authentication/sources/additional-oidc` && response.request().method() === 'DELETE')
  await local.page.getByRole('dialog').getByRole('button', { name: '确认移除', exact: true }).click()
  check((await deletedResponse).status() === 204, 'Removing the inactive test source failed')
  await local.page.waitForURL(`${base}/admin/authentication`)
  const remainingSources = await api(local.context, 'admin/authentication/sources', localToken)
  check(remainingSources.body.sources.length === 1 && remainingSources.body.sources[0].id === 'oidc', 'Removing an inactive source affected legacy authentication')
  results.push('protocol-based source creation, independent disabled draft and removal; legacy source and available login methods unchanged')

  stage = 'system authentication form persists configuration without activating it'
  await local.page.goto(`${base}/admin/authentication?source=oidc`)
  stage = 'check and isolated browser trial do not create accounts or sessions'
  const beforeTrial = authenticationCounts()
  const checkedResponse = local.page.waitForResponse(response => response.url() === `${base}/api/v1/admin/authentication/check`)
  await local.page.getByRole('button', { name: '检查配置', exact: true }).click()
  const checked = await checkedResponse
  check(checked.status() === 200 && (await checked.json()).jwks_readable, 'Real provider discovery/JWKS check failed')
  await local.page.getByRole('button', { name: '试登录', exact: true }).click()
  const trial = await casdoor(local.page, 'admin-test')
  check(trial.status === 200 && trial.body.mode === 'test' && trial.body.outcome === 'success' && trial.body.authenticated && trial.body.mapped_role === 'admin' && !trial.body.token, 'Isolated trial failed or issued a session')
  await local.page.getByRole('heading', { name: '登录验证结果', exact: true }).waitFor()
  check(await local.page.evaluate(() => localStorage.getItem('token')) === localToken, 'Trial replaced the local administrator token')
  check(JSON.stringify(authenticationCounts()) === JSON.stringify(beforeTrial), 'Trial changed durable account, binding or session rows')
  await local.page.screenshot({ path: join(run, 'authentication-trial.png'), fullPage: true })
  results.push('real discovery/JWKS check and browser trial verify admin mapping without creating accounts, bindings, sessions or replacing administrator token')
  // Start the later binding with a fresh IdP browser login; Depsilo uses the
  // unchanged localStorage JWT, so clearing fixture cookies does not log it out.
  await local.context.clearCookies()
  await local.page.goto(`${base}/admin/authentication?source=oidc`)
  const secretField = local.page.getByLabel('客户端密钥', { exact: true })
  await secretField.waitFor()
  check(await secretField.isDisabled(), 'Environment-owned client secret is editable')
  check(await secretField.inputValue() === '', 'Saved client secret was exposed in the form')
  const initialAuthentication = await api(local.context, 'admin/authentication', localToken)
  check(initialAuthentication.status === 200 && initialAuthentication.body.configured.session_ttl === '30m' && initialAuthentication.body.effective.session_ttl === '30m', 'Initial authentication state differs from the isolated fixture')
  check(initialAuthentication.body.overrides.client_secret === 'DEPSILO_AUTH_OIDC_CLIENT_SECRET', 'Environment-owned client secret is not identified')
  check(!JSON.stringify(initialAuthentication.body).includes(credentials.client_secret), 'Authentication response exposed the client secret')
  await local.page.getByLabel('外部会话最长时长', { exact: true }).fill('20m')
  await local.page.getByRole('button', { name: '检查并保存', exact: true }).click()
  const review = local.page.getByRole('dialog')
  const savedResponse = local.page.waitForResponse(response => response.url() === `${base}/api/v1/admin/authentication/sources/oidc` && response.request().method() === 'PUT')
  await review.getByRole('button', { name: '保存配置', exact: true }).click()
  const savedAuthenticationResponse = await savedResponse
  check(savedAuthenticationResponse.status() === 200, 'Authentication form save failed')
  const savedAuthentication = await savedAuthenticationResponse.json()
  check(savedAuthentication.configured.session_ttl === '20m' && savedAuthentication.effective.session_ttl === '30m' && savedAuthentication.pending_restart && savedAuthentication.sessions_will_reset, 'Saving security configuration lost its pending session-reset impact or changed active authentication before restart')
  check(!JSON.stringify(savedAuthentication).includes(credentials.client_secret), 'Authentication save response exposed the client secret')
  const savedConfig = await readFile(configPath, 'utf8')
  check(!savedConfig.includes(credentials.client_secret) && !/^\s*client_secret\s*=/m.test(savedConfig), 'Saving configuration materialized the environment client secret')
  await review.waitFor({ state: 'hidden' })
  await local.page.getByText(/保存配置与运行状态不同/).waitFor()
  await local.page.screenshot({ path: join(run, 'authentication-configured.png'), fullPage: true })
  results.push('system authentication form saves 20m TTL with 30m still active; environment secret remains hidden and outside the config file')

  stage = 'local recovery and explicit linking'
  await local.page.goto(`${base}/admin/account`)
  await local.page.getByRole('button', { name: '绑定 Casdoor', exact: true }).click()
  await local.page.getByLabel('当前本地密码').fill(password)
  await local.page.getByRole('button', { name: '验证并前往绑定' }).click()
  const linked = await casdoor(local.page, 'admin-test')
  check(linked.status === 200 && linked.body.mode === 'link', `Explicit link failed (HTTP ${linked.status}, code ${linked.body.code || 'none'})`)
  await local.page.waitForURL(`${base}/admin/account`)
  check(await local.page.evaluate(() => localStorage.getItem('token')) === localToken, 'Link replaced local session')
  const methods = await api(local.context, 'auth/identities', localToken)
  check(methods.body.password_enabled && methods.body.identities.length === 1 && methods.body.permission_source === 'local', 'Link changed local authority')
  check(methods.body.providers.length === 1 && methods.body.providers[0].id === 'oidc' && methods.body.identities[0].provider === 'oidc' && methods.body.identities[0].protocol === 'oidc', 'Legacy binding lost its stable source identity')
  await local.page.getByRole('button', { name: '解绑', exact: true }).waitFor()
  await local.page.screenshot({ path: join(run, 'account-linked-desktop.png'), fullPage: true })
  await local.page.setViewportSize({ width: 390, height: 844 })
  check(await local.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Account overflow at mobile width')
  await local.page.screenshot({ path: join(run, 'account-linked-mobile.png'), fullPage: true })
  results.push('password + OIDC explicit link, stable account ID and local session, desktop/mobile UI')

  stage = 'external login to linked local account'
  const bound = await external('admin-test')
  check(bound.status === 200 && !!bound.token, 'Bound external login failed')
  const boundMe = await api(bound.context, 'auth/me', bound.token)
  check(boundMe.body.id === localMe.body.id && boundMe.body.permission_source === 'local' && boundMe.body.login_method === 'oidc' && !boundMe.body.can_manage_credentials, 'Bound login gained credential authority or changed account')
  const denied = await api(bound.context, 'admin/tokens', bound.token, { name: 'must-not-create', permissions: 'readwrite' })
  check(denied.status === 403, 'External session could create durable API credentials')
  results.push('linked OIDC login uses same local account; durable credential creation denied')

  stage = 'unlink revokes external session and retains password'
  const removed = await api(local.context, `auth/identities/${methods.body.identities[0].id}/unlink`, localToken, { password })
  check(removed.status === 204, 'Unlink failed')
  check((await api(bound.context, 'auth/me', bound.token)).status === 401, 'Unlinked session still valid')
  check((await api(local.context, 'auth/me', localToken)).status === 200, 'Unlink revoked independent local login')
  check((await api(local.context, 'auth/login', null, { username: 'recovery', password })).status === 200, 'Unlink lost password login')
  results.push('unlink immediately revokes external session and preserves local password')

  stage = 'wait for login rate limit before testing first-login choices'
  await pause(61000)
  stage = 'unassociated login cancellation does not create an account'
  const beforeChoice = authenticationCounts()
  const cancelled = await external('admin-test', '/admin/account', 'pending')
  check(cancelled.body.mode === 'choice' && !cancelled.token, 'Unassociated login did not request an explicit account choice')
  check(JSON.stringify(authenticationCounts()) === JSON.stringify(beforeChoice), 'Unassociated login created state before choice')
  await cancelled.page.screenshot({ path: join(run, 'first-login-choice.png'), fullPage: true })
  const cancelResponse = cancelled.page.waitForResponse(response => [`${base}/api/v1/auth/identity/cancel`, `${base}/api/v1/auth/oidc/cancel`].includes(response.url()))
  await cancelled.page.getByRole('button', { name: '取消并返回登录', exact: true }).click()
  check((await cancelResponse).status() === 204, 'Choice cancellation failed')
  check(JSON.stringify(authenticationCounts()) === JSON.stringify(beforeChoice), 'Cancellation created authentication state')
  stage = 'first login can explicitly join the existing local account'
  const joined = await external('admin-test', '/admin/account', 'link')
  check(joined.status === 200 && !!joined.token, 'Proven existing account login failed')
  const joinedMe = await api(joined.context, 'auth/me', joined.token)
  check(joinedMe.body.id === localMe.body.id && joinedMe.body.permission_source === 'local' && joinedMe.body.login_method === 'oidc' && joinedMe.body.current_identity_id && joinedMe.body.current_provider_id === 'oidc', 'First-login binding changed owner or lost actual identity')
  const joinedMethods = await api(joined.context, 'auth/identities', joined.token)
  check(joinedMethods.body.current_identity_id === joinedMethods.body.identities[0].id, 'Account methods did not identify this login')
  check((await api(local.context, `auth/identities/${joinedMethods.body.identities[0].id}/unlink`, localToken, { password })).status === 204, 'Could not unlink first-login binding')
  check((await api(joined.context, 'auth/me', joined.token)).status === 401, 'Removing first-login binding left its session active')
  results.push('first unassociated identity requires a choice; cancel creates nothing; fresh local proof binds the existing account and reports current identity; unlink revokes it')
  stage = 'wait for rate limit before explicit managed-account creation'
  await pause(61000)
  stage = 'managed administrator JIT, refresh and logout'
  const managed = await external('admin-test')
  check(managed.status === 200, 'Managed JIT login failed')
  await managed.page.waitForURL(`${base}/admin/account`)
  const managedMe = await api(managed.context, 'auth/me', managed.token)
  check(managedMe.body.permission_source === 'oidc' && managedMe.body.role === 'admin' && managedMe.body.id !== localMe.body.id && !managedMe.body.can_manage_credentials, 'Managed authority incorrect')
  const managedMethods = await api(managed.context, 'auth/identities', managed.token)
  check(!managedMethods.body.password_enabled && !managedMethods.body.can_link && !managedMethods.body.identities[0].can_unlink, 'Managed account can detach authority')
  await managed.page.getByText('Casdoor', { exact: true }).first().waitFor()
  await managed.page.screenshot({ path: join(run, 'account-managed.png'), fullPage: true })
  const refreshed = await api(managed.context, 'auth/refresh', managed.token, {})
  check(refreshed.status === 200 && refreshed.body.expires_at <= managed.body.expires_at, 'Refresh extended external deadline')
  check((await api(managed.context, 'auth/logout', refreshed.body.token, {})).status === 200, 'Logout failed')
  check((await api(managed.context, 'auth/me', managed.token)).status === 401, 'Logout did not revoke original token')
  results.push('explicit managed-account creation, no password/authority unlink, bounded refresh and logout revocation')

  stage = 'readonly mapping'
  const readerDestination = '/admin/account?q=hello%20world&name=%E6%B5%8B%E8%AF%95#methods'
  const reader = await external('reader-test', readerDestination)
  check(reader.status === 200, 'Reader login failed')
  check(reader.body.return_to === readerDestination, 'Encoded deep link was lost')
  const readerMe = await api(reader.context, 'auth/me', reader.token)
  check(readerMe.body.role === 'readonly' && !readerMe.body.can_write, 'Reader mapping incorrect')
  check((await api(reader.context, 'admin/users', reader.token, { username: 'forbidden' })).status === 403, 'Reader could mutate users')
  results.push('readonly mapped, write denied and encoded deep link retained')

  stage = 'wait for production login rate-limit window'
  await pause(61000)
  stage = 'unmapped rejection'
  const unmapped = await external('unmapped-test')
  check(unmapped.status === 403 && unmapped.body.code === 'ACCESS_DENIED', 'Unmapped identity gained access')
  check(!await unmapped.page.evaluate(() => localStorage.getItem('token')), 'Denied login installed a token')
  results.push('unmapped identity denied without a local session')

  stage = 'local disable takes priority over provider login'
  check((await api(local.context, `admin/users/${readerMe.body.id}`, localToken, { enabled: false }, 'PUT')).status === 200, 'Local disable failed')
  check((await api(reader.context, 'auth/me', reader.token)).status === 401, 'Disabled managed user retained session')
  const disabled = await external('reader-test')
  check(disabled.status === 403 && disabled.body.code === 'ACCESS_DENIED', 'Provider login recreated disabled account')
  results.push('local disable immediately revokes access and survives provider re-login')

  stage = 'restart applies authentication settings and revokes previous external sessions'
  // Create a valid session immediately before the restart, after the existing rate-limit pause.
  const beforeRestart = await external('admin-test')
  check(beforeRestart.status === 200 && !!beforeRestart.token, 'Could not establish the pre-restart external session')
  check((await api(beforeRestart.context, 'auth/me', beforeRestart.token)).status === 200, 'Pre-restart external session was not valid')
  check(beforeRestart.body.expires_at > Math.floor(Date.now() / 1000) + 20 * 60, 'Pending 20m TTL was activated before restart')
  await stopServer()
  await startServer()
  check((await api(local.context, 'auth/me', localToken)).status === 200, 'Restart invalidated the independent local recovery session')
  check((await api(beforeRestart.context, 'auth/me', beforeRestart.token)).status === 401, 'Authentication configuration change did not revoke the old external session')
  const restartedAuthentication = await api(local.context, 'admin/authentication', localToken)
  check(restartedAuthentication.status === 200 && restartedAuthentication.body.configured.session_ttl === '20m' && restartedAuthentication.body.effective.session_ttl === '20m' && !restartedAuthentication.body.pending_restart, 'Restart did not activate the saved authentication configuration')
  check(!JSON.stringify(restartedAuthentication.body).includes(credentials.client_secret), 'Restarted authentication response exposed the client secret')
  const restartedConfig = await readFile(configPath, 'utf8')
  check(!restartedConfig.includes(credentials.client_secret) && !/^\s*client_secret\s*=/m.test(restartedConfig), 'Restart materialized the environment client secret')
  await local.page.setViewportSize({ width: 1440, height: 900 })
  await local.page.goto(`${base}/admin/authentication?source=oidc`)
  await local.page.getByLabel('外部会话最长时长', { exact: true }).waitFor()
  check(await local.page.getByLabel('外部会话最长时长', { exact: true }).inputValue() === '20m', 'Restarted form did not show the effective 20m TTL')
  check(await local.page.getByText(/保存配置与运行状态不同/).count() === 0, 'Restarted form still reports a pending configuration change')
  await local.page.screenshot({ path: join(run, 'authentication-restarted.png'), fullPage: true })
  const afterRestart = await external('admin-test')
  check(afterRestart.status === 200 && !!afterRestart.token, 'External sign-in failed after configuration restart')
  const remaining = afterRestart.body.expires_at - Math.floor(Date.now() / 1000)
  check(remaining > 19 * 60 && remaining <= 20 * 60, 'New external session did not use the effective 20m deadline')
  check((await api(afterRestart.context, 'auth/me', afterRestart.token)).status === 200, 'New external session is invalid after restart')
  results.push('restart activates 20m TTL, clears pending status, preserves local recovery and revokes old external sessions; fresh Casdoor login uses 20m deadline')

  stage = 'rename-only settings preserve the same external session across restart'
  const renamedProvider = '团队登录'
  const beforeRenameMe = await api(afterRestart.context, 'auth/me', afterRestart.token)
  await local.page.getByLabel('认证源名称', { exact: true }).fill(renamedProvider)
  await local.page.getByRole('button', { name: '检查并保存', exact: true }).click()
  const renameReview = local.page.getByRole('dialog')
  const renamedResponse = local.page.waitForResponse(response => response.url() === `${base}/api/v1/admin/authentication/sources/oidc` && response.request().method() === 'PUT')
  await renameReview.getByRole('button', { name: '保存配置', exact: true }).click()
  const renamed = await renamedResponse
  check(renamed.status() === 200, 'Renaming the authentication source failed')
  const pendingRename = await renamed.json()
  check(pendingRename.configured.display_name === renamedProvider && pendingRename.effective.display_name === 'Casdoor' && pendingRename.pending_restart && pendingRename.sessions_will_reset === false, 'Rename-only save reported an incorrect runtime or session-reset impact')
  check((await api(afterRestart.context, 'auth/me', afterRestart.token)).status === 200, 'Saving a source name revoked the current external session')
  const pendingRenameProviders = await api(local.context, 'auth/providers')
  check(pendingRenameProviders.body.providers.some(provider => provider.id === 'oidc' && provider.name === 'Casdoor'), 'Saving a source name changed the running login label before restart')
  await renameReview.waitFor({ state: 'hidden' })
  await local.page.screenshot({ path: join(run, 'authentication-rename-pending.png'), fullPage: true })
  await stopServer()
  await startServer()
  check((await api(local.context, 'auth/me', localToken)).status === 200, 'Rename restart invalidated local recovery')
  const afterRenameMe = await api(afterRestart.context, 'auth/me', afterRestart.token)
  check(afterRenameMe.status === 200 && afterRenameMe.body.id === beforeRenameMe.body.id && afterRenameMe.body.permission_source === beforeRenameMe.body.permission_source, 'Rename restart revoked the original external JWT or changed account authority')
  const renamedSources = await api(local.context, 'admin/authentication/sources', localToken)
  const renamedSource = renamedSources.body.sources.find(source => source.id === 'oidc')
  check(renamedSources.status === 200 && renamedSource?.configured.display_name === renamedProvider && renamedSource.effective.display_name === renamedProvider && !renamedSource.pending_restart && renamedSource.sessions_will_reset === false, 'Rename restart did not activate the saved label and clear pending state')
  const renamedProviders = await api(local.context, 'auth/providers')
  check(renamedProviders.status === 200 && renamedProviders.body.providers.some(provider => provider.id === 'oidc' && provider.name === renamedProvider), 'Public login methods did not receive the renamed provider label')
  const renamedIdentities = await api(afterRestart.context, 'auth/identities', afterRestart.token)
  check(renamedIdentities.status === 200 && renamedIdentities.body.identities.some(identity => identity.provider === 'oidc' && identity.provider_name === renamedProvider && identity.login_available), 'Existing account binding did not receive the active provider label')
  await afterRestart.page.goto(`${base}/admin/account`)
  await afterRestart.page.getByRole('heading', { name: renamedProvider, exact: true }).waitFor()
  check(await afterRestart.page.evaluate(() => localStorage.getItem('token')) === afterRestart.token, 'Account page replaced the preserved external JWT')
  await afterRestart.page.screenshot({ path: join(run, 'account-provider-renamed.png'), fullPage: true })
  results.push('rename-only save stays pending until restart; the original external JWT, account and authority survive while login and account labels update')

  stage = 'security settings still revoke the preserved external session'
  const securityChange = await api(local.context, 'admin/authentication/sources/oidc', localToken, { session_ttl: '15m' }, 'PUT')
  check(securityChange.status === 200 && securityChange.body.configured.session_ttl === '15m' && securityChange.body.effective.session_ttl === '20m' && securityChange.body.pending_restart && securityChange.body.sessions_will_reset === true, 'Security change did not report the pending session-reset impact')
  check((await api(afterRestart.context, 'auth/me', afterRestart.token)).status === 200, 'Saving security settings activated revocation before restart')
  await stopServer()
  await startServer()
  check((await api(afterRestart.context, 'auth/me', afterRestart.token)).status === 401, 'A security change failed to revoke the same JWT preserved by the rename')
  check((await api(local.context, 'auth/me', localToken)).status === 200, 'Security restart invalidated independent local recovery')
  const securedSources = await api(local.context, 'admin/authentication/sources', localToken)
  const securedSource = securedSources.body.sources.find(source => source.id === 'oidc')
  check(securedSources.status === 200 && securedSource?.effective.session_ttl === '15m' && securedSource.effective.display_name === renamedProvider && !securedSource.pending_restart && securedSource.sessions_will_reset === false, 'Security restart did not apply the new TTL and retain the renamed provider')
  results.push('subsequent TTL change reports session revocation, takes effect only after restart, revokes the rename-preserved JWT and retains local recovery')

  const report = { timestamp: new Date().toISOString(), realDepsilo: true, issuer: credentials.issuer, results, evidence: run }
  await writeFile(join(run, 'result.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 })
  console.log(JSON.stringify(report, null, 2))
} catch (error) {
  if (run) await writeFile(join(run, 'failure.txt'), String(error.stack || error), { mode: 0o600 })
  console.error(`Depsilo/Casdoor integration failed at: ${stage}. Error type: ${error.name}.`)
  if (error.name === 'ProbeAssertion') console.error(error.message)
  if (run) console.error(`Private evidence: ${run}`)
  process.exitCode = 1
} finally {
  if (browser) await browser.close()
  await stopServer()
  log?.end()
}
