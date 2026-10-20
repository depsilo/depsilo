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
  await expect(page.getByRole('region', { name: '获取验证令牌' })).toHaveCount(0)
  await expect(page.getByLabel('首启验证令牌')).toHaveCount(0)
  const submit = page.getByRole('button', { name: '完成初始化' })
  await expect(submit).toBeEnabled()
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
    help: '获取验证令牌',
    dockerCopy: /复制Docker Compose代码/,
    localCopy: /复制本机运行代码/,
    copied: '已复制',
    token: '首启验证令牌',
    password: '管理员密码',
    confirm: '确认密码',
    submit: '完成初始化',
  },
  {
    locale: 'en' as const,
    help: 'Get your bootstrap token',
    dockerCopy: 'Copy code for Docker Compose',
    localCopy: 'Copy code for Local installation',
    copied: 'Copied',
    token: 'Bootstrap token',
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
    await expect(page.getByText(/jwt\.key/)).toBeVisible()
    const dockerCopy = help.getByRole('button', { name: labels.dockerCopy })
    await dockerCopy.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('docker compose exec depsilo depsilo setup token')
    await expect(dockerCopy).toHaveText(labels.copied)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    const localCopy = help.getByRole('button', { name: labels.localCopy })
    await localCopy.focus()
    await page.keyboard.press('Enter')
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('depsilo setup token')
    await expect(localCopy).toHaveText(labels.copied)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    await page.getByLabel(labels.password, { exact: true }).fill('Tr0ub4dor&Correct')
    await page.getByLabel(labels.confirm).fill('Tr0ub4dor&Correct')
    await page.getByLabel(labels.token, { exact: true }).fill('incorrect-token')
    await page.getByRole('button', { name: labels.submit }).click()
    await expect(page.getByRole('alert')).toContainText('A valid bootstrap token is required')
    await expect(page.getByLabel(labels.password, { exact: true })).toHaveValue('Tr0ub4dor&Correct')
    await expect(page.getByLabel(labels.confirm)).toHaveValue('Tr0ub4dor&Correct')
    await expect(page.getByLabel(labels.token, { exact: true })).toHaveValue('incorrect-token')
    await expect(help).toBeVisible()
  })
}

test('single-page setup passes axe with advanced settings exposed', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
  })

  await page.goto('/')
  await expect(page.getByRole('heading', { name: '初始化 Depsilo' })).toBeVisible()
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
  await page.getByLabel('首启验证令牌').fill('bootstrap-token-from-log')
  await page.getByRole('button', { name: '完成初始化' }).click()
  await expect(page.getByText('正在重启服务')).toBeVisible()

  for (let elapsed = 0; elapsed < 35_000; elapsed += 1_000) {
    await page.clock.fastForward(1_000)
    await page.waitForTimeout(5)
  }
  await expect(page.getByRole('heading', { name: '无法连接重启后的服务' })).toBeVisible()
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
