import { test, expect, mockAdminApi } from './fixtures/admin-api'
import type { Request } from '@playwright/test'

test('expired session and failed login stay in-app, then return to the deep link', async ({ page }) => {
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
  await expect(page.getByRole('alert')).toContainText('invalid credentials')
  await expect(page).toHaveURL(/\/admin\/login$/)
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('document-load-count'))).toBe('1')

  await page.getByLabel('密码').fill('correct-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/admin\/cache\?search=requests#results$/)
  await expect.poll(() => page.evaluate(() => localStorage.getItem('token'))).toBe('fresh-session-token')
  await expect.poll(() => page.evaluate(() => sessionStorage.getItem('document-load-count'))).toBe('1')
})

test('setup exposes named delete actions and times out with a retryable reconnect state', async ({ page }) => {
  let healthReady = false
  let healthRequests = 0
  const healthURLs: string[] = []
  await page.clock.install()
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': { needs_setup: true, token_required: true },
    'POST /api/v1/setup/complete': async (request: Request) => {
      expect(request.headers()['x-depsilo-bootstrap-token']).toBe('bootstrap-token-from-log')
      expect((request.postDataJSON() as { server: { port: number } }).server.port).toBe(24444)
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
  await page.getByRole('button', { name: '开始' }).click()
  await page.getByLabel('端口').fill('24444')
  await page.getByLabel('管理员密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('确认密码').fill('Tr0ub4dor&Correct')
  await page.getByLabel('首启验证令牌').fill('bootstrap-token-from-log')
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '下一步' }).click()

  await page.getByRole('button', { name: /npm.*2 个上游/i }).click()
  await expect(page.getByRole('button', { name: '删除上游源 npmmirror' })).toBeVisible()
  await page.getByRole('button', { name: '下一步' }).click()
  await page.getByRole('button', { name: '保存并启动' }).click()
  await expect(page.getByText('正在重启服务')).toBeVisible()

  for (let elapsed = 0; elapsed < 35_000; elapsed += 1_000) {
    await page.clock.fastForward(1_000)
    await page.waitForTimeout(5)
  }
  await expect(page.getByRole('heading', { name: '无法连接重启后的服务' })).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('超时')
  expect(healthRequests).toBeGreaterThan(0)
  expect(healthURLs.length).toBeGreaterThan(0)
  expect(healthURLs.every(url => url === 'http://127.0.0.1:24444/health')).toBe(true)

  healthReady = true
  await page.getByRole('button', { name: '重试连接' }).click()
  await page.clock.fastForward(1_500)
  await page.waitForTimeout(20)
  await expect(page.getByText('服务已就绪')).toBeVisible()
})
