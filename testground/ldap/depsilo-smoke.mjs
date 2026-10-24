// Real OpenLDAP + Depsilo. Run fixture.py start and make build-dev-server first.
// Credentials, databases, logs and browser artifacts stay outside the repository.
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
const state = join(homedir(), '.cache/depsilo/ldap')
const base = 'http://127.0.0.1:18083'
const results = []
let stage = 'start isolated application', run, server, browser, log
const pause = ms => new Promise(resolve => setTimeout(resolve, ms))
const running = () => server && server.exitCode === null && server.signalCode === null
function check(ok, message) { if (!ok) { const error = new Error(message); error.name = 'ProbeAssertion'; throw error } }
async function stop() {
  if (!running()) return
  server.kill('SIGTERM')
  for (let count = 0; count < 100 && running(); count++) await pause(100)
  if (running()) server.kill('SIGKILL')
  for (let count = 0; count < 50 && running(); count++) await pause(100)
  check(!running(), 'Isolated application did not stop')
}

try {
  const credentials = JSON.parse(await readFile(join(state, 'credentials.json'), 'utf8'))
  const ca = await readFile(join(state, 'certs/ldap.crt'), 'utf8')
  check(credentials.url === 'ldaps://127.0.0.1:18636', 'Unexpected fixture URL')
  try { await fetch(`${base}/ready`, { signal: AbortSignal.timeout(1000) }); throw new Error('occupied') } catch (error) { check(error.message !== 'occupied', 'Port 18083 occupied; existing service unchanged') }
  await mkdir(join(state, 'runs'), { recursive: true, mode: 0o700 })
  run = await mkdtemp(join(state, 'runs/depsilo-'))
  const password = randomBytes(24).toString('base64url') + '9!aA'
  const configPath = join(run, 'config.toml')
  const settings = {
    enabled: true, display_name: '测试目录', admin_groups: [credentials.users['admin-test'].group],
    readonly_groups: [credentials.users['reader-test'].group], session_ttl: '30m',
    ldap: { url: credentials.url, bind_dn: credentials.bind_dn, bind_password: credentials.bind_password,
      user_base_dn: `ou=people,${credentials.base_dn}`, user_filter: '(uid={username})',
      subject_attribute: 'entryUUID', display_name_attribute: 'displayName', groups_attribute: 'memberOf', ca_cert_pem: ca },
  }
  const config = `config_version = 1
[server]
host = "127.0.0.1"
port = 18083
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
[auth.sources.directory]
protocol = "ldap"
${Object.entries(settings).filter(([key]) => key !== 'ldap').map(([key, value]) => `${key} = ${JSON.stringify(value)}`).join('\n')}
[auth.sources.directory.ldap]
${Object.entries(settings.ldap).map(([key, value]) => `${key} = ${JSON.stringify(value)}`).join('\n')}
`
  await writeFile(configPath, config, { mode: 0o600 })
  log = createWriteStream(join(run, 'server.log'), { mode: 0o600 })
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('DEPSILO_')))
  async function start() {
    server = spawn(join(root, 'bin/depsilo'), ['serve', '--config', configPath], { cwd: run,
      env: { ...env, DEPSILO_ADMIN_USERNAME: 'recovery', DEPSILO_ADMIN_PASSWORD: password }, stdio: ['ignore', 'pipe', 'pipe'] })
    server.stdout.pipe(log, { end: false }); server.stderr.pipe(log, { end: false })
    let ready = false
    for (let count = 0; count < 100; count++) {
      check(running(), 'Application exited before readiness; inspect private server.log')
      try { ready = (await fetch(`${base}/ready`, { signal: AbortSignal.timeout(1000) })).ok } catch { /* startup */ }
      if (ready) break
      await pause(200)
    }
    check(ready, 'Application readiness failed')
  }
  await start()
  browser = await chromium.launch()
  async function context(token) {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
    await ctx.addInitScript(({ token }) => { localStorage.setItem('lang', 'zh'); localStorage.setItem('depsilo-theme', 'dark'); if (token) localStorage.setItem('token', token) }, { token })
    return ctx
  }
  async function api(ctx, path, token, data, method = data === undefined ? 'GET' : 'POST', origin = base, retried = false) {
    const response = await ctx.request.fetch(`${base}/api/v1/${path}`, { method, headers: { Origin: origin, ...(token ? { Authorization: `Bearer ${token}` } : {}) }, ...(data === undefined ? {} : { data }) })
    if (response.status() === 429 && !retried) {
      const seconds = Number(response.headers()['retry-after'])
      check(Number.isFinite(seconds) && seconds > 0 && seconds <= 61, 'Unexpected rate limit retry deadline')
      console.log('Waiting for the normal authentication rate limit window')
      await pause((seconds + 1) * 1000)
      return api(ctx, path, token, data, method, origin, true)
    }
    let body = null
    try { body = await response.json() } catch { /* no content */ }
    return { status: response.status(), body }
  }
  const counts = () => JSON.parse(execFileSync('python3', ['-c', 'import sqlite3,json,sys; c=sqlite3.connect("file:"+sys.argv[1]+"?mode=ro",uri=True); print(json.dumps([c.execute("SELECT COUNT(*) FROM "+t).fetchone()[0] for t in ["users","external_identities","identity_sessions"]]))', join(run, 'depsilo.db')], { encoding: 'utf8' }))
  const local = await context()
  const login = await api(local, 'auth/login', null, { username: 'recovery', password })
  check(login.status === 200, 'Local recovery login failed')
  const token = login.body.token
  const draft = { id: 'directory', protocol: 'ldap', create: false, settings: {} }

  stage = 'directory diagnostics and secret redaction'
  const source = await api(local, 'admin/authentication/sources', token)
  check(source.status === 200 && source.body.sources[0].protocol === 'ldap', 'LDAP source missing')
  check(source.body.sources[0].configured.ldap.bind_password_set && !JSON.stringify(source.body).includes(credentials.bind_password), 'Directory bind secret leaked or lost')
  const before = counts()
  const diagnostic = await api(local, 'admin/authentication/ldap/check', token, draft)
  check(diagnostic.status === 200 && diagnostic.body.tls_valid && diagnostic.body.bind_valid, 'Verified LDAPS check failed')
  const startTLS = await api(local, 'admin/authentication/ldap/check', token, { ...draft, settings: { ldap: { url: credentials.starttls_url } } })
  check(startTLS.status === 200, 'StartTLS check failed')
  const noCA = await api(local, 'admin/authentication/ldap/check', token, { ...draft, settings: { ldap: { ca_cert_pem: '' } } })
  check(noCA.status === 503, 'Untrusted certificate was accepted')
  const trial = await api(local, 'admin/authentication/ldap/trial', token, { ...draft, username: 'admin-test', password: credentials.users['admin-test'].password })
  check(trial.status === 200 && trial.body.mode === 'test' && trial.body.authenticated && trial.body.mapped_role === 'admin' && !trial.body.token, 'Isolated LDAP trial failed')
  check(JSON.stringify(counts()) === JSON.stringify(before), 'Diagnostic wrote durable identity state')
  results.push('LDAPS and mandatory StartTLS; untrusted CA rejected; secret redacted; trial writes no identity state')

  stage = 'failed directory authentication leaves no account'
  for (const [username, supplied] of [['admin-test', 'wrong-password'], ['admin-test', ''], ['*)(uid=*)', 'wrong-password'], ['unmapped-test', credentials.users['unmapped-test'].password]]) {
    const response = await api(local, 'auth/ldap/login', null, { provider_id: 'directory', username, password: supplied })
    check(response.status === 403, 'Invalid or unmapped directory login admitted')
  }
  const cross = await api(local, 'auth/ldap/login', null, { provider_id: 'directory', username: 'admin-test', password: credentials.users['admin-test'].password }, 'POST', 'https://foreign.invalid')
  check(cross.status === 403 && JSON.stringify(counts()) === JSON.stringify(before), 'Cross-origin or denied login changed account state')
  results.push('Wrong/empty password, filter injection, unmapped groups and cross-origin login rejected')

  stage = 'explicit local binding and bounded directory login'
  const bound = await api(local, 'auth/identities/ldap/link', token, { provider_id: 'directory', password, username: 'admin-test', directory_password: credentials.users['admin-test'].password })
  check(bound.status === 200 && bound.body.mode === 'link' && !bound.body.token, 'Local directory binding failed or replaced login')
  const external = await context()
  const externalLogin = await api(external, 'auth/ldap/login', null, { provider_id: 'directory', username: 'admin-test', password: credentials.users['admin-test'].password, return_to: '/admin/account' })
  check(externalLogin.status === 200 && externalLogin.body.mode === 'login', 'Bound LDAP login failed')
  const externalToken = externalLogin.body.token
  const me = await api(external, 'auth/me', externalToken)
  check(me.status === 200 && me.body.permission_source === 'local' && me.body.login_method === 'ldap' && me.body.role === 'admin' && !me.body.can_manage_credentials, 'LDAP session escaped local account/credential boundaries')
  const blocked = await api(external, 'admin/authentication/sources/directory', externalToken, { display_name: 'should-not-save' }, 'PUT')
  check(blocked.status === 403, 'External session mutated trust configuration')
  results.push('Local password plus directory proof binds; LDAP login preserves local authority and cannot manage credentials')

  stage = 'first login requires explicit account choice'
  const reader = await context()
  const readerBefore = counts()
  const first = await api(reader, 'auth/ldap/login', null, { provider_id: 'directory', username: 'reader-test', password: credentials.users['reader-test'].password })
  check(first.status === 200 && first.body.mode === 'choice' && JSON.stringify(counts()) === JSON.stringify(readerBefore), 'First directory login created an account implicitly')
  const choice = await api(reader, 'auth/identity/complete', null, {})
  check(choice.status === 200 && choice.body.mode === 'choice', 'Directory choice cannot resume')
  const created = await api(reader, 'auth/identity/resolve', null, { action: 'create' })
  check(created.status === 200 && created.body.mode === 'login', 'Explicit directory account creation failed')
  const readerMe = await api(reader, 'auth/me', created.body.token)
  check(readerMe.body.permission_source === 'ldap' && readerMe.body.role === 'readonly' && !readerMe.body.can_manage_credentials, 'Directory role mapping failed')
  results.push('Unknown identity requires explicit choice; directory-managed reader retains bounded permissions')

  stage = 'rename preserves sessions; authentication change revokes'
  const renamed = await api(local, 'admin/authentication/sources/directory', token, { display_name: '团队目录' }, 'PUT')
  check(renamed.status === 200 && !renamed.body.sessions_will_reset, 'Rename reports a security reset')
  await stop(); await start()
  check((await api(external, 'auth/me', externalToken)).status === 200, 'Directory rename revoked session')
  const changed = await api(local, 'admin/authentication/sources/directory', token, { session_ttl: '15m' }, 'PUT')
  check(changed.status === 200 && changed.body.sessions_will_reset, 'Security change missed session impact')
  await stop(); await start()
  check((await api(external, 'auth/me', externalToken)).status === 401 && (await api(local, 'auth/me', token)).status === 200, 'Restart did not revoke external session or broke local recovery')
  results.push('Rename restart retains JWT; security change restart revokes it and preserves local recovery')

  stage = 'personal unlink and rebound login protection'
  const identities = await api(local, 'auth/identities', token)
  const identity = identities.body.identities.find(item => item.provider === 'directory')
  check(identity && identity.protocol === 'ldap', 'Directory binding details missing')
  const unlinked = await api(local, `auth/identities/${identity.id}/unlink`, token, { password })
  check(unlinked.status === 204, 'Directory unlink failed')
  check((await api(local, 'auth/identities', token)).body.identities.length === 0, 'Unlink retained directory identity')
  results.push('Personal unlink preserves local password and removes the selected directory identity')

  stage = 'real browser editor and directory login'
  const adminContext = await context(token)
  const adminPage = await adminContext.newPage()
  await adminPage.goto(`${base}/admin/authentication?source=directory`)
  await adminPage.getByRole('heading', { name: '目录连接', exact: true }).waitFor()
  check(await adminPage.locator('input[type=password]').first().inputValue() === '', 'Saved bind secret rendered in editor')
  await adminPage.screenshot({ path: join(run, 'ldap-source-real.png'), fullPage: true })
  check(await adminPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'Desktop editor overflow')
  await adminPage.setViewportSize({ width: 320, height: 844 })
  check(await adminPage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'Narrow editor overflow')
  await adminPage.screenshot({ path: join(run, 'ldap-source-320-real.png'), fullPage: true })
  const browserLogin = await context()
  const page = await browserLogin.newPage()
  await page.goto(`${base}/admin/login`)
  await page.getByRole('button', { name: '使用 团队目录 登录', exact: true }).click()
  await page.getByLabel('目录用户名', { exact: true }).fill('reader-test')
  await page.getByLabel('目录密码', { exact: true }).fill(credentials.users['reader-test'].password)
  await page.screenshot({ path: join(run, 'ldap-login-real.png'), fullPage: true })
  await page.getByRole('dialog').getByRole('button', { name: '登录', exact: true }).click()
  await page.waitForURL(url => url.pathname === '/admin' || url.pathname === '/admin/account')
  await page.goto(`${base}/admin/account`)
  await page.getByRole('heading', { level: 1, name: '账户安全' }).waitFor()
  await page.screenshot({ path: join(run, 'ldap-account-real.png'), fullPage: true })
  results.push('Real browser LDAP editor, desktop/narrow layout, directory password login and account view')
  await writeFile(join(run, 'result.json'), JSON.stringify({ passed: results, completed_at: new Date().toISOString() }, null, 2), { mode: 0o600 })
  console.log(JSON.stringify({ result: 'passed', checks: results, artifacts: run }, null, 2))
} catch (error) {
  console.error(JSON.stringify({ result: 'failed', stage, message: error.name === 'ProbeAssertion' ? error.message : 'Inspect private artifacts; raw transport/provider details suppressed', artifacts: run }))
  process.exitCode = 1
} finally {
  await browser?.close()
  await stop()
  log?.end()
}
