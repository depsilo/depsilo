import AxeBuilder from '@axe-core/playwright'
import type { Request } from '@playwright/test'

import {
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

const startedAt = '2026-08-20T10:00:00Z'

function event(id: number, outcome: string, statusCode = 200) {
  return {
    id,
    ecosystem: 'pypi',
    package_name: 'six',
    version: '1.17.0',
    outcome,
    status_code: statusCode,
    created_at: '2026-08-20T10:00:01Z',
  }
}

test('setup signs the new administrator in on the same origin and continues to onboarding', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'light', 'en')
  let configured = false
  let loginBody: unknown
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': () => ({ needs_setup: !configured, token_required: false }),
    'POST /api/v1/setup/complete': (request: Request) => {
      configured = true
      return {
        status: 'ok',
        message: 'Configuration saved. Server restarting...',
        reconnect_url: `${new URL(request.url()).origin}/`,
        restart_strategy: 'exec',
      }
    },
    'POST /api/v1/auth/login': (request: Request) => {
      loginBody = request.postDataJSON()
      return {
        token: 'new-admin-token',
        expires_at: 1_900_000_000,
        user: { id: 1, username: 'admin', role: 'admin' },
      }
    },
    'GET /api/v1/admin/onboarding/status': {
      status: 'not_started', started_at: startedAt, next_after_id: 0, events: [], has_more: false,
    },
  })
  await page.route('**/health', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ status: 'healthy' }),
  }))

  await page.goto('/')
  await page.getByLabel('Administrator password').fill('Tr0ub4dor&Correct')
  await page.getByLabel('Confirm password').fill('Tr0ub4dor&Correct')
  await page.getByRole('button', { name: 'Complete setup' }).click()
  await page.clock.fastForward(2_000)
  await expect(page).toHaveURL(/\/admin\/connect\?new=1$/)
  expect(loginBody).toEqual({ username: 'admin', password: 'Tr0ub4dor&Correct' })
})

test('fresh admin is routed through copyable config and a real miss-to-hit milestone', async ({ page, context }) => {
  await page.clock.install()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await setUiPreferences(page, 'light', 'en')
  let polls = 0
  const writes: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => {
      const url = new URL(request.url())
      if (!url.searchParams.has('after_id')) {
        return { status: 'not_started', started_at: startedAt, next_after_id: 10, events: [], has_more: false }
      }
      polls += 1
      return polls === 1
        ? { status: 'not_started', started_at: startedAt, next_after_id: 11, events: [event(11, 'miss')], has_more: false }
        : { status: 'completed', started_at: startedAt, next_after_id: 12, events: [event(12, 'hit')], has_more: false }
    },
    'PUT /api/v1/admin/onboarding': (request: Request) => {
      const status = (request.postDataJSON() as { status: string }).status
      writes.push(status)
      return { status }
    },
  })

  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin\/connect\?new=1$/)
  await expect(page.getByRole('heading', { name: 'Connect your first project' })).toBeVisible()
  await page.getByRole('button', { name: 'Copy code for Package manager configuration' }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain(`pip config set global.index-url ${new URL(page.url()).origin}/pypi/simple/`)

  await expect(page.getByRole('heading', { name: 'First request received' })).toBeVisible()
  await expect(page.getByText('PyPI', { exact: true })).toBeVisible()
  await expect(page.getByText('CACHE MISS', { exact: true })).toBeVisible()
  await expect.poll(() => writes).toContain('completed')

  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'First cache hit' })).toBeVisible()
  await expect(page.getByText('Served from local cache.')).toBeVisible()
})

test('an existing completed deployment goes straight to the Dashboard', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': {
      status: 'completed', started_at: startedAt, next_after_id: 200, events: [], has_more: false,
    },
  })

  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
})

test('setup recovery resumes the Dashboard instead of starting onboarding again', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'light', 'en')
  let configured = false
  await mockAdminApi(page, {
    'GET /api/v1/setup/status': () => ({ needs_setup: !configured, token_required: false }),
    'POST /api/v1/setup/complete': (request: Request) => {
      configured = true
      return {
        status: 'ok',
        message: 'Configuration recovered. Server restarting...',
        reconnect_url: `${new URL(request.url()).origin}/`,
        restart_strategy: 'exec',
      }
    },
    'POST /api/v1/auth/login': {
      token: 'recovered-admin-token',
      expires_at: 1_900_000_000,
      user: { id: 1, username: 'admin', role: 'admin' },
    },
    'GET /api/v1/admin/onboarding/status': {
      status: 'completed', started_at: startedAt, next_after_id: 200, events: [], has_more: false,
    },
  })
  await page.route('**/health', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ status: 'healthy' }),
  }))

  await page.goto('/')
  await page.getByLabel('Administrator password').fill('Tr0ub4dor&Correct')
  await page.getByLabel('Confirm password').fill('Tr0ub4dor&Correct')
  await page.getByRole('button', { name: 'Complete setup' }).click()
  await page.clock.fastForward(2_000)
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
})

test('manual onboarding can be skipped and stays usable at mobile width', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  const writes: string[] = []
  let durableStatus = 'not_started'
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': () => ({
      status: durableStatus, started_at: startedAt, next_after_id: 10, events: [], has_more: false,
    }),
    'PUT /api/v1/admin/onboarding': (request: Request) => {
      const status = (request.postDataJSON() as { status: string }).status
      writes.push(status)
      durableStatus = status
      return { status }
    },
    'POST /api/v1/auth/login': {
      token: 'signed-in-again',
      expires_at: 1_900_000_000,
      user: { id: 1, username: 'admin', role: 'admin' },
    },
  })

  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Connect your first project' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])

  await page.getByRole('button', { name: 'Continue to Dashboard' }).last().click()
  await expect(page).toHaveURL(/\/admin$/)
  expect(writes).toContain('skipped')
  await page.evaluate(() => localStorage.removeItem('token'))
  await page.goto('/admin/login')
  await page.getByLabel('Username').fill('admin')
  await page.getByLabel('Password').fill('Tr0ub4dor&Correct')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
})

test('an onboarding status outage never blocks Admin or the Dashboard escape', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': {
      status: 503,
      body: { code: 'DB_ERROR', message: 'temporarily unavailable' },
    },
  })

  await page.goto('/admin')
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  await page.goto('/admin/connect')
  await expect(page.getByText('Unable to load first-run onboarding status.')).toBeVisible()
  await page.getByRole('button', { name: 'Go to Dashboard' }).click()
  await expect(page).toHaveURL(/\/admin$/)
})

test('a stale not-started gate cannot trap the operator when the baseline request fails', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  let reads = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': () => {
      reads += 1
      return reads === 1
        ? { status: 'not_started', started_at: startedAt, next_after_id: 10, events: [], has_more: false }
        : { status: 503, body: { code: 'DB_ERROR', message: 'temporarily unavailable' } }
    },
  })

  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin\/connect\?new=1$/)
  await expect(page.getByText('Unable to load first-run onboarding status.')).toBeVisible()
  await page.getByRole('button', { name: 'Go to Dashboard' }).click()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
})

test('a failed skip write never blocks Dashboard access', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': {
      status: 'not_started', started_at: startedAt, next_after_id: 10, events: [], has_more: false,
    },
    'PUT /api/v1/admin/onboarding': {
      status: 503,
      body: { code: 'DB_ERROR', message: 'temporarily unavailable' },
    },
  })

  await page.goto('/admin/connect')
  await page.getByRole('button', { name: 'Continue to Dashboard' }).last().click()
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
})

test('onboarding remains usable without page overflow at every acceptance viewport', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': {
      status: 'completed', started_at: startedAt, next_after_id: 10, events: [], has_more: false,
    },
  })
  await page.goto('/admin/connect')

  for (const width of [390, 430, 768, 1280, 1440]) {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 1000 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth), `${width}px viewport`).toBe(width)
    await expect(page.getByRole('heading', { name: 'Choose an ecosystem' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Choose a Python package manager' })).toBeVisible()
    await expect(page.getByText('Run this command to save the Depsilo endpoint.')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Waiting for your first dependency request' })).toBeVisible()
  }

  await page.getByRole('button', { name: 'Node.js' }).click()
  await page.getByRole('button', { name: 'Yarn' }).click()
  await expect(page.getByText('Run your project’s normal dependency command. Depsilo will detect the request automatically.')).toBeVisible()
  await expect(page.getByText('Run your project’s normal dependency command. This page updates automatically.')).toBeVisible()

  await page.getByRole('button', { name: 'View all ecosystems' }).click()
  await page.getByRole('button', { name: 'Container' }).click()
  await page.getByRole('button', { name: 'Show common ecosystems' }).click()
  await expect(page.getByRole('button', { name: 'Python' })).toHaveAttribute('aria-pressed', 'true')
})

for (const outcome of [
  { value: 'blocked', status: 403, label: 'BLOCK', hint: 'Depsilo handled the request correctly.' },
  { value: 'error', status: 502, label: 'UPSTREAM ERROR', hint: 'the upstream failed' },
]) {
  test(`${outcome.value} is a handled first request rather than an endless wait`, async ({ page }) => {
    await setUiPreferences(page, 'dark', 'en')
    await mockAdminApi(page, {
      'GET /api/v1/admin/onboarding/status': (request: Request) => {
        const url = new URL(request.url())
        return url.searchParams.has('after_id')
          ? { status: 'completed', started_at: startedAt, next_after_id: 11, events: [event(11, outcome.value, outcome.status)], has_more: false }
          : { status: 'completed', started_at: startedAt, next_after_id: 10, events: [], has_more: false }
      },
    })

    await page.goto('/admin/connect')
    await expect(page.getByRole('heading', { name: 'First request received' })).toBeVisible()
    await expect(page.getByText(outcome.label, { exact: true })).toBeVisible()
    await expect(page.getByText(outcome.hint, { exact: false })).toBeVisible()
  })
}
