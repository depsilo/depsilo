import AxeBuilder from '@axe-core/playwright'
import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'
import type { Request } from '@playwright/test'

test('expired session and failed login stay in-app, then return to the deep link', { tag: '@smoke' }, async ({ page }) => {
  let authenticated = false
  await page.addInitScript(() => {
    const count = Number(sessionStorage.getItem('document-load-count') || '0')
    sessionStorage.setItem('document-load-count', String(count + 1))
  })
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': () => authenticated
      ? { id: 1, username: 'admin', role: 'admin', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: true }
      : { status: 401, body: { code: 'UNAUTHORIZED', message: 'session expired' } },
    'POST /api/v1/auth/login': async (request: Request) => {
      expect(request.headers().authorization).toBeUndefined()
      const body = request.postDataJSON() as { username: string; password: string }
      if (body.password !== 'correct-password') {
        return { status: 401, body: { code: 'UNAUTHORIZED', message: 'invalid credentials' } }
      }
      authenticated = true
      return {
        token: 'fresh-session-token',
        expires_at: 1_900_000_000,
        user: { id: 1, username: body.username, role: 'admin' },
      }
    },
  })

  await page.goto('/admin/cache?search=requests#results')
  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('document-load-count'))).toBe('1')

  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('wrong-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page.getByRole('alert')).toContainText('登录失败，请检查用户名和密码')
  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('document-load-count'))).toBe('1')

  await page.getByLabel('密码').fill('correct-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/admin\/cache\?search=requests#results$/)
  await expect.poll(() => page.evaluate(() => localStorage.getItem('token'))).toBe('fresh-session-token')
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('document-load-count'))).toBe('1')
})

test('login stays in place when the browser cannot persist the session', async ({ page }) => {
  await page.addInitScript(() => {
    const setItem = Storage.prototype.setItem
    Storage.prototype.setItem = function (key: string, value: string) {
      if (key === 'token' && value === 'cannot-persist-token') {
        throw new DOMException('Storage is disabled', 'SecurityError')
      }
      return setItem.call(this, key, value)
    }
  })
  await mockAdminApi(page, {
    'POST /api/v1/auth/login': {
      token: 'cannot-persist-token',
      expires_at: 1_900_000_000,
      user: { id: 1, username: 'admin', role: 'admin' },
    },
  })

  await page.goto('/admin/login')
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('correct-password')
  await page.getByRole('button', { name: '登录' }).click()

  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect(page.getByRole('alert')).toContainText('浏览器无法保存登录状态')
})

test('leaving Login cancels the pending request without pulling the user back', async ({ page }) => {
  let releaseLogin!: (value: unknown) => void
  const pendingLogin = new Promise(resolve => { releaseLogin = resolve })
  await mockAdminApi(page, {
    'POST /api/v1/auth/login': async () => pendingLogin,
  })

  await page.goto('/admin/login')
  const tokenBefore = await page.evaluate(() => localStorage.getItem('token'))
  await page.getByLabel('用户名').fill('admin')
  await page.getByLabel('密码').fill('correct-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page.getByRole('button', { name: '登录中...' })).toHaveAttribute('aria-busy', 'true')

  await page.getByRole('link', { name: '返回门户' }).click()
  await expect(page).toHaveURL('/')

  releaseLogin({
    token: 'late-session-token',
    expires_at: 1_900_000_000,
    user: { id: 1, username: 'admin', role: 'admin' },
  })
  await page.evaluate(() => new Promise<void>(resolve => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  }))

  await expect(page).toHaveURL('/')
  await expect.poll(() => page.evaluate(() => localStorage.getItem('token'))).toBe(tokenBefore)
})

test('single-page setup explains invalid credentials without disabling the primary action', async ({ page }) => {
  let setupRequests = 0
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: false },
    'POST /api/v1/setup/complete': () => {
      setupRequests += 1
      return { status: 500, body: { code: 'UNEXPECTED_REQUEST' } }
    },
  })

  await page.goto('/')
  await expect(page.getByRole('region', { name: '获取初始化令牌' })).toHaveCount(0)
  await expect(page.getByLabel('初始化令牌', { exact: true })).toHaveCount(0)
  // A server that predates listen_port and config_exists: the default port
  // and no notice about an existing configuration file.
  await expect(page.locator('summary').filter({ hasText: '高级设置' })).toContainText('端口 23333')
  await expect(page.getByRole('note')).toHaveCount(0)
  // The test server is on loopback, where plain HTTP never leaves the machine.
  await expect(page.locator('[data-setup-notice="unencrypted"]')).toHaveCount(0)
  const submit = page.getByRole('button', { name: '完成初始化' })
  await expect(submit).toBeEnabled()

  // The field applies the server's rules (internal/credential/policy.go).
  const password = page.getByLabel('管理员密码')
  await expect(password).toHaveAccessibleDescription(/至少 12 个字符.*三类字符.*至少 20 个字符.*不能包含用户名/)
  await password.fill('Ab1!')
  await expect(page.getByRole('alert')).toHaveText('密码至少需要 12 个字符。')
  await password.fill('abcdefghijk²!')
  await expect(page.getByRole('alert')).toHaveText('请使用至少三类字符，或使用不少于 20 个字符的长口令。')
  await password.fill('abcdefgh1234密码')
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(password).not.toHaveAttribute('aria-invalid', 'true')

  await page.getByLabel('管理员密码').fill('admin-Strong-Pass123!')
  await page.getByLabel('确认密码').fill('admin-Strong-Pass123!')
  await submit.click()

  await expect(page.getByRole('alert')).toContainText('密码不能包含管理员用户名')
  await expect(page.getByLabel('管理员密码')).toBeFocused()
  expect(setupRequests).toBe(0)
})

for (const labels of [
  {
    locale: 'zh' as const,
    help: '获取初始化令牌',
    dockerCopy: /^复制Docker代码/,
    composeCopy: /复制Docker Compose代码/,
    startupOutput: '服务的启动输出里也会打印这个令牌；容器部署可运行 docker logs depsilo 查看。',
    localCopy: /复制本机运行代码/,
    copied: '已复制',
    token: '初始化令牌',
    helpHint: '在服务器的部署或配置目录运行命令，获取初始化令牌。',
    tokenMissing: '请获取并填写初始化令牌。',
    tokenRejected: '服务器没有接受这个初始化令牌。除非用 DEPSILO_BOOTSTRAP_TOKEN 指定了令牌，否则每次重启 Depsilo 都会更换令牌，请重新获取当前的令牌并再次粘贴。',
    passwordHint: /不能包含用户名/,
    password: '管理员密码',
    confirm: '确认密码',
    submit: '完成初始化',
  },
  {
    locale: 'en' as const,
    help: 'Get your bootstrap token',
    dockerCopy: /^Copy code for Docker$/,
    composeCopy: 'Copy code for Docker Compose',
    startupOutput: 'The token is also printed in the server\'s startup output; for a container, run docker logs depsilo.',
    localCopy: 'Copy code for Local installation',
    copied: 'Copied',
    token: 'Bootstrap token',
    helpHint: 'Run in the server deployment or configuration directory to get the bootstrap token.',
    tokenMissing: 'Get and enter the bootstrap token.',
    tokenRejected: 'The server did not accept this bootstrap token. It changes every time Depsilo restarts unless DEPSILO_BOOTSTRAP_TOKEN sets it, so print the current one and paste it again.',
    passwordHint: /must not contain the username/,
    password: 'Administrator password',
    confirm: 'Confirm password',
    submit: 'Complete setup',
  },
]) {
  test(`setup token commands copy with the keyboard and fit a narrow screen in ${labels.locale}`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.setViewportSize({ width: 320, height: 800 })
    await setUiPreferences(page, 'light', labels.locale)
    await mockAdminApi(page, {
      'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
      'POST /api/v1/setup/complete': {
        status: 403,
        body: { code: 'BOOTSTRAP_TOKEN_REQUIRED', message: 'A valid bootstrap token is required to complete initial setup' },
      },
    })

    await page.goto('/')
    const help = page.getByRole('region', { name: labels.help })
    await expect(help).toBeVisible()
    await expect(help).toContainText(labels.helpHint)
    await expect(page.getByText(/jwt\.key/)).toBeVisible()
    await expect(help).toContainText(labels.startupOutput)
    // Plain `docker run`, as in the README quick start, has no Compose project.
    const dockerCopy = help.getByRole('button', { name: labels.dockerCopy })
    await dockerCopy.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('docker exec depsilo depsilo setup token')
    await expect(dockerCopy).toHaveText(labels.copied)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    const composeCopy = help.getByRole('button', { name: labels.composeCopy })
    await composeCopy.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('docker compose exec depsilo depsilo setup token')
    await expect(composeCopy).toHaveText(labels.copied)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    const localCopy = help.getByRole('button', { name: labels.localCopy })
    await localCopy.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('depsilo setup token')
    await expect(localCopy).toHaveText(labels.copied)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    // The token is a pasted one-time value and stays readable; passwords stay masked.
    const token = page.getByLabel(labels.token, { exact: true })
    await expect(token).toHaveAttribute('type', 'text')
    await expect(token).toHaveAttribute('autocomplete', 'off')
    await expect(token).toHaveAttribute('autocapitalize', 'none')
    await expect(token).toHaveAttribute('spellcheck', 'false')
    await expect(token).toHaveCSS('font-family', /mono/i)
    await expect(page.getByLabel(labels.password, { exact: true })).toHaveAttribute('type', 'password')
    await expect(page.getByLabel(labels.confirm)).toHaveAttribute('type', 'password')
    await expect(page.getByLabel(labels.password, { exact: true })).toHaveAccessibleDescription(labels.passwordHint)

    await page.getByLabel(labels.password, { exact: true }).fill('Tr0ub4dor&Correct')
    await page.getByLabel(labels.confirm).fill('Tr0ub4dor&Correct')
    await page.getByRole('button', { name: labels.submit }).click()
    await expect(page.getByRole('alert')).toHaveText(labels.tokenMissing)
    await expect(page.getByLabel(labels.token, { exact: true })).toBeFocused()
    await page.getByLabel(labels.token, { exact: true }).fill('incorrect-token')
    await page.getByRole('button', { name: labels.submit }).click()
    // The refusal is explained in the page language on the token field itself.
    await expect(page.getByRole('alert')).toHaveText(labels.tokenRejected)
    await expect(page.getByText('A valid bootstrap token is required')).toHaveCount(0)
    await expect(token).toHaveAttribute('aria-invalid', 'true')
    await expect(token).toBeFocused()
    await expect(page.getByLabel(labels.password, { exact: true })).toHaveValue('Tr0ub4dor&Correct')
    await expect(page.getByLabel(labels.confirm)).toHaveValue('Tr0ub4dor&Correct')
    await expect(token).toHaveValue('incorrect-token')
    await expect(help).toBeVisible()

    await token.fill('another-token')
    await expect(page.getByRole('alert')).toHaveCount(0)
    await expect(token).not.toHaveAttribute('aria-invalid', 'true')
  })
}

for (const viewport of [{ width: 1280, height: 800 }, { width: 390, height: 844 }]) {
  test(`a rejected token is focused with its label in view at ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await setUiPreferences(page, 'light', 'zh')
    await mockAdminApi(page, {
      // A reopened setup shows the config.toml notice above the fields.
      'GET /api/v1/setup/status': { needs_setup: true, token_required: true, listen_port: 23333, config_exists: true },
      'POST /api/v1/setup/complete': {
        status: 403,
        body: { code: 'BOOTSTRAP_TOKEN_REQUIRED', message: 'A valid bootstrap token is required to complete initial setup' },
      },
    })
    await page.goto('/')
    const token = page.getByLabel('初始化令牌', { exact: true })
    await token.fill('incorrect-token')
    await page.getByLabel('管理员密码', { exact: true }).fill('Tr0ub4dor&Correct')
    await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
    await page.getByRole('button', { name: '完成初始化' }).click()
    await expect(page.getByRole('alert')).toContainText('服务器没有接受这个初始化令牌。')
    await expect(token).toBeFocused()
    const label = page.locator('label').filter({ hasText: /^初始化令牌$/ })
    await expect.poll(async () => {
      const [labelBox, tokenBox] = [await label.boundingBox(), await token.boundingBox()]
      return labelBox !== null && tokenBox !== null && labelBox.y >= 0 && tokenBox.y + tokenBox.height <= viewport.height
    }).toBe(true)
  })
}

test('setup explains the failures an operator can act on in the page language', async ({ page }) => {
  let failure = { status: 500, body: { code: 'UNSET', message: 'unset' } }
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
    'POST /api/v1/setup/complete': () => failure,
  })

  await page.goto('/')
  await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('初始化令牌', { exact: true }).fill('bootstrap-token-from-log')

  for (const expected of [
    {
      status: 409, code: 'ALREADY_CONFIGURED', message: 'Server is already configured',
      shown: '这个 Depsilo 实例已经完成初始化。请刷新页面后登录。',
    },
    {
      status: 409, code: 'ADMIN_CREDENTIALS_MISMATCH',
      message: 'An administrator already exists; enter its current credentials to recover the configuration',
      shown: '这个实例已经有管理员账号。请输入该账号当前的用户名和密码，以恢复配置。',
    },
    {
      status: 500, code: 'WRITE_FAILED', message: 'Failed to save configuration',
      shown: '管理员账号已保存，但服务器未能写入配置文件。请查看服务日志，并确认 Depsilo 对配置目录有写入权限，然后用相同的用户名和密码再次提交。',
    },
    // A code without a translation keeps the server's sentence.
    {
      status: 500, code: 'ADMIN_CREATE_FAILED', message: 'Failed to create the initial administrator',
      shown: 'Failed to create the initial administrator',
    },
  ]) {
    failure = { status: expected.status, body: { code: expected.code, message: expected.message } }
    await page.getByRole('button', { name: '完成初始化' }).click()
    await expect(page.getByRole('alert')).toHaveText(expected.shown)
    await expect(page.getByLabel('初始化令牌', { exact: true })).not.toHaveAttribute('aria-invalid', 'true')
  }
})

test('setup starts from the listening port and warns before replacing a configuration file', async ({ page }) => {
  let submittedPort: number | undefined
  await page.clock.install()
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: false, listen_port: 24646, config_exists: true },
    'POST /api/v1/auth/login': { status: 401, body: { code: 'UNAUTHORIZED' } },
    'POST /api/v1/setup/complete': (request: Request) => {
      submittedPort = (request.postDataJSON() as { server: { port: number } }).server.port
      return { status: 'ok', message: 'saved', reconnect_url: 'http://127.0.0.1:24646/', restart_strategy: 'exec' }
    },
  })
  await page.route('**/health', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{}' }))

  await page.goto('/')
  await expect(page.getByRole('note')).toHaveText(
    '这个实例已经有配置文件。完成初始化会替换它，现有文件会作为备份保留在同一目录。',
  )
  const advanced = page.locator('summary').filter({ hasText: '高级设置' })
  await expect(advanced).toContainText('端口 24646')
  await advanced.click()
  await expect(page.getByLabel('端口')).toHaveValue('24646')

  await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
  await page.getByRole('button', { name: '完成初始化' }).click()
  await expect(page.getByText('正在重启服务')).toBeVisible()
  expect(submittedPort).toBe(24646)
})

test('the local token command names a listening port other than the default', async ({ page }) => {
  // `depsilo serve --port 24646`: the bare command would ask port 23333 and
  // print nothing, or the token of another instance pending setup there.
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true, listen_port: 24646 },
  })

  await page.goto('/')
  const commands = page.getByRole('region', { name: '获取初始化令牌' }).locator('pre')
  await expect(commands).toHaveText([
    // The port inside a container stays the default.
    'docker exec depsilo depsilo setup token',
    'docker compose exec depsilo depsilo setup token',
    'depsilo setup token --port 24646',
  ])
})

for (const labels of [
  {
    locale: 'zh' as const, advanced: '高级设置',
    summary: '端口 23333 · 13 个生态 · 默认上游优先使用中国大陆镜像',
    hint: '多数生态的默认上游以中国大陆镜像为首选、官方源为备用，可在“配置上游源”中修改。',
  },
  {
    locale: 'en' as const, advanced: 'Advanced settings',
    summary: 'Port 23333 · 13 ecosystems · default upstreams prefer mainland-China mirrors',
    hint: 'For most ecosystems the default upstreams put a mainland-China mirror first and the official registry second; change them under “Configure upstreams”.',
  },
]) {
  test(`the collapsed advanced settings say which mirrors the defaults prefer in ${labels.locale}`, async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 800 })
    await setUiPreferences(page, 'light', labels.locale)
    await mockAdminApi(page, {
      'GET /api/v1/setup/status': { needs_setup: true, token_required: true, listen_port: 23333 },
    })

    await page.goto('/')
    const advanced = page.locator('summary').filter({ hasText: labels.advanced })
    await expect(page.locator('details')).not.toHaveAttribute('open', '')
    // The whole line is readable while collapsed, even on a narrow screen.
    const summaryLine = advanced.getByText(labels.summary, { exact: true })
    await expect(summaryLine).toBeVisible()
    expect(await summaryLine.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    await advanced.click()
    await expect(page.locator('details')).toContainText(labels.hint)
  })
}

test('single-page setup passes axe with advanced settings exposed', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true, listen_port: 23333, config_exists: true },
  })

  await page.goto('/')
  await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
  await expect(page.getByRole('note')).toBeVisible()
  await page.locator('summary').filter({ hasText: '高级设置' }).click()
  await expect(page.locator('details')).toHaveAttribute('open', '')

  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
  expect(result.violations).toEqual([])
})

test('single-page setup submits secure defaults and recovers after a reconnect timeout', async ({ page }) => {
  let healthReady = false
  let healthRequests = 0
  const healthURLs: string[] = []
  await page.clock.install()
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
    'POST /api/v1/auth/login': { status: 401, body: { code: 'UNAUTHORIZED' } },
    'POST /api/v1/setup/complete': async (request: Request) => {
      expect(request.headers()['x-depsilo-bootstrap-token']).toBe('bootstrap-token-from-log')
      const body = request.postDataJSON() as {
        server: { port: number }
        storage: { path: string }
        admin: { username: string; password: string }
        ecosystems: Record<string, {
          enabled: boolean
          upstreams: Array<{ name: string; url: string; priority: number }>
        }>
      }
      expect(body.server.port).toBe(24444)
      expect(body.storage.path).toBe('./data/cache')
      expect(body.admin).toEqual({ username: 'admin', password: 'Tr0ub4dor&Correct' })
      expect(body.ecosystems.npm.enabled).toBe(true)
      expect(body.ecosystems.npm.upstreams.length).toBeGreaterThan(0)
      for (const expected of [
        { ecosystem: 'cargo', index: 0, upstream: { name: 'rsproxy', url: 'https://rsproxy.cn/index/', priority: 1 } },
        { ecosystem: 'maven', index: 1, upstream: { name: 'central', url: 'https://repo.maven.apache.org/maven2/', priority: 2 } },
        { ecosystem: 'rubygems', index: 0, upstream: { name: 'tuna', url: 'https://mirrors.tuna.tsinghua.edu.cn/rubygems/', priority: 1 } },
        { ecosystem: 'composer', index: 0, upstream: { name: 'aliyun', url: 'https://mirrors.aliyun.com/composer/', priority: 1 } },
      ]) {
        expect(body.ecosystems[expected.ecosystem].upstreams[expected.index]).toEqual(expected.upstream)
      }
      return {
        status: 'ok',
        message: 'Configuration saved. Server restarting...',
        reconnect_url: 'http://127.0.0.1:24444/',
        restart_strategy: 'exec',
      }
    },
  })
  page.on('request', request => {
    if (new URL(request.url()).pathname === '/health') healthURLs.push(request.url())
  })
  await page.route('**/health', async route => {
    healthRequests += 1
    await route.fulfill({
      status: healthReady ? 200 : 503,
      contentType: 'application/json',
      headers: { 'Access-Control-Allow-Origin': '*' },
      body: JSON.stringify({ status: healthReady ? 'healthy' : 'restarting' }),
    })
  })

  await page.goto('/')
  await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
  await page.locator('summary').filter({ hasText: '高级设置' }).click()
  await page.getByLabel('端口').fill('24444')
  await page.getByRole('button', { name: /npm.*2 个/i }).click()
  await expect(page.getByRole('button', { name: '删除上游源 npmmirror' })).toBeVisible()
  await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('初始化令牌', { exact: true }).fill('bootstrap-token-from-log')
  await page.getByRole('button', { name: '完成初始化' }).click()
  await expect(page.getByText('正在重启服务')).toBeVisible()

  for (let elapsed = 0; elapsed < 35_000; elapsed += 1_000) {
    await page.clock.fastForward(1_000)
    await page.waitForTimeout(5)
  }
  await expect(page.getByRole('heading', { name: '无法连接重启后的服务' })).toBeVisible()
  await expect(page.locator('[data-setup-state]')).toHaveAttribute('data-setup-state', 'failed')
  await expect(page.getByRole('alert')).toContainText('超时')
  expect(healthRequests).toBeGreaterThan(0)
  expect(healthURLs.length).toBeGreaterThan(0)
  const browserHealthURL = new URL('/health', page.url()).toString()
  expect(healthURLs.every(url => url === browserHealthURL)).toBe(true)

  healthReady = true
  await page.getByRole('button', { name: '重试连接' }).click()
  await page.clock.fastForward(1_500)
  await page.waitForTimeout(20)
  await expect(page.getByText('服务已就绪')).toBeVisible()
})

test('setup that needs a manual restart reports success and waits for the operator', async ({ page }) => {
  let healthRequests = 0
  await page.clock.install()
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: false },
    'POST /api/v1/auth/login': { status: 401, body: { code: 'UNAUTHORIZED' } },
    // Windows cannot replace the running process, so the server asks for a restart.
    'POST /api/v1/setup/complete': {
      status: 'ok',
      message: 'Configuration saved. Restart the server process to apply it.',
      reconnect_url: 'http://127.0.0.1:23333/',
      restart_strategy: 'supervisor_required',
    },
  })
  await page.route('**/health', async route => {
    healthRequests += 1
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{"status":"healthy"}' })
  })

  await page.goto('/')
  await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
  await page.getByRole('button', { name: '完成初始化' }).click()

  await expect(page.getByRole('heading', { name: '配置已保存，请重启 Depsilo' })).toBeVisible()
  await expect(page.getByRole('status')).toHaveText('配置已保存。请先手动重启 Depsilo 进程，再重试连接。')
  await expect(page.getByRole('heading', { name: '无法连接重启后的服务' })).toHaveCount(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('[data-setup-state]')).toHaveAttribute('data-setup-state', 'restart-required')
  // Nothing is probed until the operator says the process is back.
  await page.clock.fastForward(5_000)
  expect(healthRequests).toBe(0)

  await page.getByRole('button', { name: '重试连接' }).click()
  await page.clock.fastForward(1_500)
  await expect(page.getByText('服务已就绪')).toBeVisible()
  expect(healthRequests).toBeGreaterThan(0)
})

test('setup over plain HTTP from another machine says that the credentials are sent unencrypted', async ({ page, baseURL }) => {
  await page.setViewportSize({ width: 320, height: 800 })
  await page.route(/^http:\/\/depsilo\.example\.test\//, async route => {
    const url = new URL(route.request().url())
    if (url.pathname.startsWith('/api/')) {
      await route.fallback()
    } else {
      // Load the real SPA at a non-loopback HTTP address without DNS.
      const response = await route.fetch({ url: new URL(url.pathname + url.search, baseURL).toString() })
      await route.fulfill({ response })
    }
  })
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
  })

  await page.goto('http://depsilo.example.test/')
  await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
  await expect(page.locator('[data-setup-notice="unencrypted"]')).toHaveText(
    '此页面没有使用 HTTPS，初始化令牌和新密码会以未加密的方式发送。请在服务器本机完成初始化，或只在你信任的网络中操作。',
  )
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

for (const publicOrigin of ['https://depsilo.example.test', 'https://depsilo.example.test:8443']) {
  test(`setup reconnects through the browser HTTPS origin ${publicOrigin} behind a reverse proxy`, async ({ page, baseURL }) => {
    const healthURLs: string[] = []
    await page.clock.install()
    await page.route(/^https:\/\/depsilo\.example\.test(?::\d+)?\//, async route => {
      const url = new URL(route.request().url())
      if (url.pathname.startsWith('/api/')) {
        await route.fallback()
      } else if (url.pathname === '/health') {
        healthURLs.push(url.toString())
        if (url.origin === publicOrigin) {
          await route.fulfill({ status: 200, contentType: 'application/json', body: '{"status":"healthy"}' })
        } else {
          await route.abort('connectionrefused')
        }
      } else if (url.pathname === '/admin/login') {
        await route.fulfill({ status: 200, contentType: 'text/html', body: '<h1>Administrator login</h1>' })
      } else {
        // Load the real SPA at its public HTTPS address without requiring an
        // external DNS name, certificate, or reverse proxy in the offline gate.
        const response = await route.fetch({ url: new URL(url.pathname + url.search, baseURL).toString() })
        await route.fulfill({ response })
      }
    })
    await mockAdminApi(page, {
      'GET /api/v1/setup/status': { needs_setup: true, token_required: false },
      'POST /api/v1/setup/complete': {
        status: 'ok',
        reconnect_url: 'http://127.0.0.1:23333/',
        restart_strategy: 'exec',
      },
      'POST /api/v1/auth/login': { status: 401, body: { code: 'UNAUTHORIZED' } },
    })

    await page.goto(publicOrigin)
    await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
    await expect(page.locator('[data-setup-notice="unencrypted"]')).toHaveCount(0)
    await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
    await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
    await page.getByRole('button', { name: '完成初始化' }).click()
    await expect(page.getByText('正在重启服务')).toBeVisible()
    await page.clock.fastForward(1_500)
    await expect.poll(() => healthURLs).not.toEqual([])
    expect(healthURLs).toEqual([`${publicOrigin}/health`])
    await expect(page.getByText('服务已就绪')).toBeVisible()
    await page.clock.fastForward(300)
    await expect(page).toHaveURL(`${publicOrigin}/admin/login?next=%2Fadmin`)
    await expect(page.getByRole('heading', { name: 'Administrator login' })).toBeVisible()
  })
}
