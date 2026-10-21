import AxeBuilder from '@axe-core/playwright'
import { mkdir } from 'node:fs/promises'
import type { Page, Request } from '@playwright/test'
import type { AdminUpstream, OnboardingDeliveryEvent, OnboardingEvent, OnboardingStatusResponse } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const startedAt = '2026-08-20T10:00:00Z'

async function selectClientOption(page: Page, label: string, option: string) {
  await page.getByRole('combobox', { name: label, exact: true }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
  await expect(page.getByRole('combobox', { name: label, exact: true })).toContainText(option)
}

function event(id: number, outcome: string, statusCode = 200, overrides: Partial<OnboardingEvent> = {}): OnboardingEvent {
  return { id, request_id: `request-${id}`, action: 'download', ecosystem: 'pypi', package_name: 'six', version: '1.17.0', outcome,
    status_code: statusCode, created_at: '2026-08-20T10:00:01Z', ...overrides }
}

function delivery(id: number, outcome: string, overrides: Partial<OnboardingDeliveryEvent> = {}): OnboardingDeliveryEvent {
  return { ...event(id, outcome), request_id: `request-${id}`, cache_key: 'pypi/six-1.17.0.whl', method: 'GET', dependency_kind: 'artifact',
    delivery_result: 'completed', bytes_sent: 11050, successful_artifact_download: true, ...overrides }
}

function response(overrides: Partial<OnboardingStatusResponse> = {}): OnboardingStatusResponse {
  return { status: 'completed', started_at: startedAt, next_after_id: 10, events: [], has_more: false,
    next_delivery_after_id: 'delivery-baseline', delivery_events: [], deliveries_has_more: false, ...overrides }
}

function upstream(id: number, ecosystem: string, name: string): AdminUpstream {
  return { id, adapter_type: ecosystem, name, url: 'https://packages.example.test', proxy: '', priority: 1,
    probe_mode: 'active', probe_interval: '1m', healthy: false, avg_latency_ms: 0, success_rate: 0,
    last_checked_at: null, worker_running: true, created_at: startedAt, updated_at: startedAt }
}

for (const shell of ['macOS / Linux (sh)', 'PowerShell']) {
  test(`pip one-command source survives reload and switches back to persistent settings in ${shell}`, async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await setUiPreferences(page, 'dark', 'en')
    let baselines = 0
    await mockAdminApi(page, {
      'GET /api/v1/admin/onboarding/status': (request: Request) => {
        if (!new URL(request.url()).searchParams.has('after_id')) baselines += 1
        return response()
      },
    })
    await page.goto('/admin/connect')
    await expect(page.getByRole('button', { name: 'Copy test command', exact: true })).toBeEnabled()
    if (shell === 'PowerShell') await selectClientOption(page, 'Shell', shell)
    const previousBaselines = baselines
    await selectClientOption(page, 'Configuration scope', 'This command only')
    await expect.poll(() => baselines).toBeGreaterThan(previousBaselines)
    const config = page.getByRole('region', { name: 'Use this source for one command', exact: true })
    await expect(config).toContainText('without changing pip configuration')
    await expect(config).not.toContainText('pip config --user')
    await expect(page.getByRole('button', { name: 'Copy configuration', exact: true })).toHaveCount(0)
    const download = page.getByRole('region', { name: 'Run a dependency download', exact: true })
    await expect(download.locator('pre')).toContainText(`--index-url '${new URL(page.url()).origin}/pypi/simple/'`)
    await page.getByRole('button', { name: 'Copy test command', exact: true }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(await download.locator('pre').textContent())
    await expect.poll(() => page.evaluate(() => JSON.parse(sessionStorage.getItem('depsilo.admin.onboarding-session.v2') ?? '{}').configurationScope)).toBe('temporary')
    await page.reload()
    await expect(page.getByRole('combobox', { name: 'Configuration scope', exact: true })).toContainText('This command only')
    await expect(page.getByRole('combobox', { name: 'Shell', exact: true })).toContainText(shell)
    await expect(page.getByRole('button', { name: 'Copy test command', exact: true })).toBeEnabled()
    await expect(download.locator('pre')).toContainText('--index-url')
    for (const [scope, flag] of [['Current user', '--user'], ['Current virtual environment', '--site']]) {
      const beforeSwitch = baselines
      await selectClientOption(page, 'Configuration scope', scope)
      await expect.poll(() => baselines).toBeGreaterThan(beforeSwitch)
      await expect(page.getByRole('region', { name: 'Configure pip', exact: true }).locator('pre').first()).toContainText(`config ${flag} set global.index-url`)
      await expect(download.locator('pre')).not.toContainText('--index-url')
    }
  })
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

test('an existing completed deployment goes straight to the Dashboard', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': {
      status: 'completed', started_at: startedAt, next_after_id: 200, events: [], has_more: false,
    },
  })

  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
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
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
})

test('connection verification requires an artifact delivery and a matching cache hit', async ({ page, context }) => {
  await page.clock.install()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await setUiPreferences(page, 'light', 'en')
  let stage = 0
  const writes: string[] = []
  const deliveryCursors: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => {
      const url = new URL(request.url())
      if (!url.searchParams.has('after_id')) return response({ status: 'not_started' })
      deliveryCursors.push(url.searchParams.get('delivery_after_id') ?? '')
      if (stage === 0) return response({ status: 'not_started', next_after_id: 11, events: [event(11, 'hit', 200, { action: 'metadata', version: '' })] })
      return response({ status: 'not_started', next_after_id: 11 + stage, next_delivery_after_id: `delivery-${stage}`,
        events: [event(11 + stage, stage === 1 ? 'miss' : 'hit')],
        delivery_events: [delivery(100 + stage, stage === 1 ? 'miss' : 'hit')],
      })
    },
    'PUT /api/v1/admin/onboarding': (request: Request) => {
      const status = (request.postDataJSON() as { status: string }).status
      writes.push(status)
      return { status }
    },
  })
  await page.goto('/admin')
  await expect(page).toHaveURL(/\/admin\/connect\?new=1$/)
  await expect(page.getByRole('heading', { name: 'Connect a client' })).toBeVisible()
  await page.getByRole('button', { name: 'Copy configuration', exact: true }).click()
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain(`python -m pip config --user set global.index-url '${new URL(page.url()).origin}/pypi/simple/'`)
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Related request detected' })).toBeVisible()
  expect(writes).toEqual([])
  await expect(page.getByRole('heading', { name: 'Cache hit confirmed' })).toHaveCount(0)
  stage = 1
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Artifact delivered' })).toBeVisible()
  await expect.poll(() => writes).toEqual(['completed'])
  stage = 2
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Cache hit confirmed' })).toBeVisible()
  expect(deliveryCursors).toContain('delivery-1')
  expect(deliveryCursors.every(Boolean)).toBe(true)
  await page.getByRole('button', { name: 'View overview', exact: true }).click()
  await expect(page).toHaveURL(/\/admin$/)
})

test('Docker Hub activation remains restart-managed in the client connection page', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  let saved = false
  let restarted = false
  const state = () => ({ configured: saved, effective: restarted, docker_hub: saved && restarted,
    pending_restart: saved && !restarted, config_writable: true, blocked_by_override: false })
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': response(),
    'GET /api/v1/admin/docker/bootstrap': state,
    'POST /api/v1/admin/docker/bootstrap': () => { saved = true; return state() },
  })
  await page.goto('/admin/connect')
  await selectClientOption(page, 'Ecosystem', 'Container')
  await expect(page.getByText('Docker image proxying is off.', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Enable Docker Hub' }).click()
  await expect(page.getByText('Restart Depsilo to activate Docker proxying', { exact: false })).toBeVisible()
  await expect(page.getByText('Docker Hub proxy is active.', { exact: false })).toHaveCount(0)
  restarted = true
  await expect.poll(() => page.evaluate(() => JSON.parse(sessionStorage.getItem('depsilo.admin.onboarding-session.v2') ?? '{}').languageId)).toBe('container')
  await page.reload()
  await expect(page.getByRole('combobox', { name: 'Ecosystem', exact: true })).toContainText('Container')
  await expect(page.getByText('Docker Hub proxy is active.', { exact: false })).toBeVisible()
})

test('other ecosystems cannot verify the selected client and a manager change starts a new check', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'dark', 'en')
  let matching = false
  let baselines = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => {
      if (!new URL(request.url()).searchParams.has('after_id')) { baselines += 1; return response() }
      return response({ next_after_id: 11,
        events: [event(11, 'hit', 200, { ecosystem: matching ? 'pypi' : 'npm' })],
        delivery_events: matching ? [] : [delivery(101, 'hit', { ecosystem: 'npm' })],
      })
    },
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  matching = true
  // The same audit ID was already consumed by the unrelated row; only a new
  // request in a new session may provide fresh connection evidence.
  await page.getByRole('button', { name: 'Start a new check' }).click()
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Related request detected' })).toBeVisible()
  matching = false
  const previousBaselines = baselines
  await selectClientOption(page, 'Package manager', 'uv')
  await expect.poll(() => baselines).toBeGreaterThan(previousBaselines)
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Cache hit confirmed' })).toHaveCount(0)
})

test('a late poll from the previous manager cannot replace the new observation or its cursors', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'dark', 'en')
  const nextStartedAt = '2026-08-20T10:01:00Z'
  let selectingNextManager = false
  let oldResponseReturned = false
  let markOldPollStarted!: () => void
  let releaseOldPoll!: () => void
  const oldPollStarted = new Promise<void>(resolve => { markOldPollStarted = resolve })
  const oldPollReleased = new Promise<void>(resolve => { releaseOldPoll = resolve })
  const nextPolls: { afterId: string | null; deliveryAfterId: string | null; startedAt: string | null }[] = []
  const writes: string[] = []
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': async (request: Request) => {
      const params = new URL(request.url()).searchParams
      // Both the route gate and observation baseline omit after_id. Select
      // the baseline by the user action, rather than their request order.
      if (!params.has('after_id')) return selectingNextManager
        ? response({ started_at: nextStartedAt, next_after_id: 50, next_delivery_after_id: 'uv-baseline' })
        : response({ next_delivery_after_id: 'pip-baseline' })
      if (params.get('delivery_after_id') === 'pip-baseline') {
        markOldPollStarted()
        await oldPollReleased
        oldResponseReturned = true
        return response({ next_after_id: 60, next_delivery_after_id: 'late-pip-hit',
          events: [event(60, 'hit', 200, { created_at: '2026-08-20T10:01:01Z' })],
          delivery_events: [delivery(160, 'hit', { created_at: '2026-08-20T10:01:01Z' })],
        })
      }
      nextPolls.push({ afterId: params.get('after_id'), deliveryAfterId: params.get('delivery_after_id'), startedAt: params.get('started_at') })
      return response({ started_at: nextStartedAt, next_after_id: 51, next_delivery_after_id: 'uv-metadata',
        events: [event(51, 'miss', 200, { action: 'metadata', version: '', created_at: '2026-08-20T10:01:02Z' })],
      })
    },
    'PUT /api/v1/admin/onboarding': (request: Request) => {
      const { status } = request.postDataJSON() as { status: string }
      writes.push(status)
      return { status }
    },
  })
  const storedSession = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('depsilo.admin.onboarding-session.v2') ?? '{}'))
  await page.goto('/admin/connect')
  await expect.poll(storedSession).toMatchObject({ managerId: 'pip', afterId: 10, deliveryAfterId: 'pip-baseline' })
  await page.clock.fastForward(2_600)
  await oldPollStarted
  try {
    selectingNextManager = true
    await selectClientOption(page, 'Package manager', 'uv')
    await expect(page.getByRole('heading', { name: 'Waiting for a related request', exact: true })).toBeVisible()
    await expect.poll(storedSession).toMatchObject({ managerId: 'uv', startedAt: nextStartedAt, afterId: 50, deliveryAfterId: 'uv-baseline' })
    expect(await storedSession()).not.toHaveProperty('firstCacheHit')
  } finally {
    releaseOldPoll()
  }
  await expect.poll(() => oldResponseReturned).toBe(true)
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Related request detected', exact: true })).toBeVisible()
  expect(nextPolls[0]).toEqual({ afterId: '50', deliveryAfterId: 'uv-baseline', startedAt: nextStartedAt })
  await expect.poll(storedSession).toMatchObject({ managerId: 'uv', startedAt: nextStartedAt, afterId: 51, deliveryAfterId: 'uv-metadata' })
  const session = await storedSession()
  expect(session).not.toHaveProperty('successfulDownload')
  expect(session).not.toHaveProperty('firstCacheHit')
  expect(writes).toEqual([])
  await expect(page.getByRole('heading', { name: 'Cache hit confirmed', exact: true })).toHaveCount(0)
})

test('reload preserves the selected manager, shell, scope and verified result', async ({ page }) => {
  // Preserve the native NavigationTiming entry used to distinguish reloads
  // from a new visit. Playwright's fake performance returns no entries.
  await setUiPreferences(page, 'dark', 'en')
  let produceDelivery = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => new URL(request.url()).searchParams.has('after_id') && produceDelivery
      ? response({ next_after_id: 11, events: [event(11, 'miss')], delivery_events: [delivery(101, 'miss')] })
      : response(),
  })
  await page.goto('/admin/connect')
  await selectClientOption(page, 'Configuration scope', 'Current virtual environment')
  await selectClientOption(page, 'Shell', 'PowerShell')
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  produceDelivery = true
  await expect(page.getByRole('heading', { name: 'Artifact delivered' })).toBeVisible()
  await expect.poll(() => page.evaluate(() => JSON.parse(sessionStorage.getItem('depsilo.admin.onboarding-session.v2') ?? '{}').configurationScope)).toBe('venv')
  await page.reload()
  await expect(page.getByRole('combobox', { name: 'Package manager', exact: true })).toContainText('pip')
  await expect(page.getByRole('combobox', { name: 'Configuration scope', exact: true })).toContainText('Current virtual environment')
  await expect(page.getByRole('combobox', { name: 'Shell', exact: true })).toContainText('PowerShell')
  await expect(page.getByRole('heading', { name: 'Artifact delivered' })).toBeVisible()
})

test('manual connection can be left without verification and stays keyboard accessible on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  const writes: string[] = []
  let durableStatus: OnboardingStatusResponse['status'] = 'not_started'
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': () => response({ status: durableStatus }),
    'PUT /api/v1/admin/onboarding': (request: Request) => {
      durableStatus = (request.postDataJSON() as { status: 'completed' | 'skipped' }).status
      writes.push(durableStatus)
      return { status: durableStatus }
    },
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Connect a client' })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390)
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
  await page.getByRole('button', { name: 'Return without verification', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/admin$/)
  expect(writes).toContain('skipped')
})

for (const mode of ['outage', 'stale-gate', 'skip-write-failure'] as const) {
  test(`${mode} cannot trap the operator in connection verification`, async ({ page }) => {
    await setUiPreferences(page, 'light', 'en')
    let reads = 0
    await mockAdminApi(page, {
      'GET /api/v1/admin/onboarding/status': () => {
        reads += 1
        if (mode === 'skip-write-failure' || (mode === 'stale-gate' && reads === 1)) return response({ status: 'not_started' })
        return { status: 503, body: { code: 'DB_ERROR', message: 'temporarily unavailable' } }
      },
      'PUT /api/v1/admin/onboarding': { status: 503, body: { code: 'DB_ERROR', message: 'temporarily unavailable' } },
    })
    await page.goto(mode === 'stale-gate' ? '/admin' : '/admin/connect')
    if (mode !== 'skip-write-failure') {
      await expect(page.getByRole('heading', { name: 'Verification unavailable' })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Copy configuration', exact: true })).toBeVisible()
    }
    await page.getByRole('button', { name: 'Back to overview', exact: true }).click()
    await expect(page).toHaveURL(/\/admin$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
  })
}

test('client controls and code remain usable across acceptance widths without document overflow', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, { 'GET /api/v1/admin/onboarding/status': response() })
  await page.goto('/admin/connect')
  for (const width of [320, 390, 430, 768, 1280, 1440, 1920]) {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 1000 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth), `${width}px viewport`).toBe(width)
    await expect(page.getByRole('combobox', { name: 'Ecosystem', exact: true })).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Package manager', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Copy configuration', exact: true })).toBeVisible()
    await expect(page.getByRole('complementary', { name: 'Verification result', exact: true })).toBeVisible()
  }
  await selectClientOption(page, 'Ecosystem', 'Node.js')
  await selectClientOption(page, 'Package manager', 'Yarn')
  await expect(page.getByRole('button', { name: 'Copy test command', exact: true })).toHaveCount(0)
  await expect(page.getByRole('complementary', { name: 'Verification result', exact: true })).toBeVisible()
})

for (const outcome of [
  { value: 'blocked', status: 403, title: 'Request blocked' },
  { value: 'error', status: 502, title: 'Request failed' },
]) {
  test(`${outcome.value} is an observed failure, never successful verification`, async ({ page }) => {
    await setUiPreferences(page, 'dark', 'en')
    const writes: string[] = []
    await mockAdminApi(page, {
      'GET /api/v1/admin/onboarding/status': (request: Request) => new URL(request.url()).searchParams.has('after_id')
        ? response({ status: 'not_started', next_after_id: 11, events: [event(11, outcome.value, outcome.status)] })
        : response({ status: 'not_started' }),
      'PUT /api/v1/admin/onboarding': (request: Request) => {
        const { status } = request.postDataJSON() as { status: string }
        writes.push(status)
        return { status }
      },
    })
    await page.goto('/admin/connect')
    await expect(page.getByRole('heading', { name: outcome.title, exact: true })).toBeVisible()
    expect(writes).toEqual([])
    await expect(page.getByRole('heading', { name: 'Artifact delivered' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Return without verification', exact: true })).toBeVisible()
  })
}

test('a cancelled body remains a failed verification despite an audit MISS', async ({ page }) => {
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => new URL(request.url()).searchParams.has('after_id')
      ? response({ next_after_id: 11, events: [event(11, 'miss')], delivery_events: [delivery(101, 'miss', { delivery_result: 'cancelled', successful_artifact_download: false })] })
      : response(),
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Request failed', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Artifact delivered' })).toHaveCount(0)
})

test('a newer successful delivery replaces an earlier failed request diagnostic', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'dark', 'en')
  let recovered = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => {
      if (!new URL(request.url()).searchParams.has('after_id')) return response()
      return response({ next_after_id: 11,
        events: recovered ? [] : [event(11, 'error', 502)],
        delivery_events: recovered ? [delivery(102, 'miss', { request_id: 'request-12', created_at: '2026-08-20T10:00:02Z' })] : [],
      })
    },
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Waiting for a related request' })).toBeVisible()
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Request failed', exact: true })).toBeVisible()
  recovered = true
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Artifact delivered', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Request failed', exact: true })).toHaveCount(0)
})

test('Chinese connection guidance and verified results remain readable on desktop and mobile', async ({ page }) => {
  await page.clock.install()
  await setUiPreferences(page, 'dark', 'zh')
  let verified = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => verified && new URL(request.url()).searchParams.has('after_id')
      ? response({ next_after_id: 11, events: [event(11, 'hit')], delivery_events: [delivery(101, 'hit')] })
      : response(),
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: '等待相关请求', exact: true })).toBeVisible()
  for (const width of [1440, 1920, 320]) {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    await expect(page.getByRole('heading', { name: '接入客户端', exact: true })).toBeVisible()
    if (process.env.CONNECTION_REVIEW_DIR) {
      await mkdir(process.env.CONNECTION_REVIEW_DIR, { recursive: true })
      await page.screenshot({ path: `${process.env.CONNECTION_REVIEW_DIR}/connection-waiting-dark-zh-${width}-mock.png`, fullPage: true })
    }
  }
  verified = true
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: '已确认缓存命中', exact: true })).toBeVisible()
  await expect(page.locator('.connection-primary')).toHaveCSS('background-color', 'rgb(37, 99, 235)')
  for (const width of [1440, 320]) {
    await page.setViewportSize({ width, height: width < 768 ? 844 : 900 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width)
    if (process.env.CONNECTION_REVIEW_DIR) {
      await page.screenshot({ path: `${process.env.CONNECTION_REVIEW_DIR}/connection-verified-dark-zh-${width}-mock.png`, fullPage: true })
    }
  }
})

test('upstream warnings follow the selected ecosystem even when check timestamps are absent', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': response(),
    'GET /api/v1/admin/upstreams': { items: [upstream(1, 'pypi', 'python-official'), upstream(2, 'npm', 'node-official')], total: 2 },
  })
  await page.goto('/admin/connect')
  await expect(page.getByText('Related upstreams need attention: python-official.', { exact: false })).toBeVisible()
  await expect(page.getByText('Related upstreams need attention: node-official.', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Copy configuration', exact: true })).toBeEnabled()
  await selectClientOption(page, 'Ecosystem', 'Node.js')
  await expect(page.getByText('Related upstreams need attention: node-official.', { exact: false })).toBeVisible()
  await expect(page.getByText('Related upstreams need attention: python-official.', { exact: false })).toHaveCount(0)
})

for (const upstreamUnavailable of [false, true]) {
  test(`a confirmed readiness failure stays actionable through the initial gate (upstream read failure: ${upstreamUnavailable})`, async ({ page }) => {
    await setUiPreferences(page, 'light', 'en')
    let durableStatus: OnboardingStatusResponse['status'] = 'not_started'
    const writes: string[] = []
    await mockAdminApi(page, {
      'GET /api/v1/admin/onboarding/status': () => response({ status: durableStatus }),
      'PUT /api/v1/admin/onboarding': (request: Request) => {
        durableStatus = (request.postDataJSON() as { status: 'completed' | 'skipped' }).status
        writes.push(durableStatus)
        return { status: durableStatus }
      },
      'GET /ready': { status: 503, body: { status: 'not_ready', version: 'dev', checks: { database: 'ready', storage: 'unavailable' } } },
      'GET /api/v1/admin/upstreams': upstreamUnavailable
        ? { status: 503, body: { code: 'UNAVAILABLE', message: 'temporarily unavailable' } }
        : { items: [], total: 0 },
    })
    await page.goto('/admin')
    await expect(page).toHaveURL(/\/admin\/connect\?new=1$/)
    const readinessAlert = page.getByRole('alert').filter({ hasText: 'A required database or storage check failed.' })
    await expect(readinessAlert).toBeVisible()
    await expect(page.getByRole('button', { name: 'Copy configuration', exact: true })).toBeEnabled()
    await readinessAlert.getByRole('button', { name: 'View service status', exact: true }).click()
    await expect(page).toHaveURL(/\/admin$/)
    await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible()
    expect(writes).toContain('skipped')
  })
}

test('English light-theme verified guidance keeps accessible contrast and primary action styling', async ({ page }) => {
  await page.clock.install()
  await page.setViewportSize({ width: 1440, height: 900 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/onboarding/status': (request: Request) => new URL(request.url()).searchParams.has('after_id')
      ? response({ next_after_id: 11, events: [event(11, 'hit')], delivery_events: [delivery(101, 'hit')] })
      : response(),
  })
  await page.goto('/admin/connect')
  await expect(page.getByRole('heading', { name: 'Waiting for a related request', exact: true })).toBeVisible()
  await page.clock.fastForward(2_600)
  await expect(page.getByRole('heading', { name: 'Cache hit confirmed', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'View overview', exact: true })).toHaveCSS('background-color', 'rgb(37, 99, 235)')
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([])
  if (process.env.CONNECTION_REVIEW_DIR) {
    await mkdir(process.env.CONNECTION_REVIEW_DIR, { recursive: true })
    await page.screenshot({ path: `${process.env.CONNECTION_REVIEW_DIR}/connection-verified-light-en-1440-mock.png`, fullPage: true })
  }
})
