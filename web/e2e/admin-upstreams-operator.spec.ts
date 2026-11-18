import AxeBuilder from '@axe-core/playwright'
import type { Locator, Page, Request } from '@playwright/test'
import type {
  AdminUpstream,
  AdminUpstreamListResponse,
  CheckUpstreamResponse,
  UpstreamMutationRequest,
} from '../src/lib/adminApi.types'
import {
  expect,
  mockAdminApi,
  setUiPreferences,
  test,
} from './fixtures/admin-api'

const timestamp = '2026-07-28T08:00:00Z'

function makeUpstream(
  id: number,
  overrides: Partial<AdminUpstream> = {},
): AdminUpstream {
  return {
    id,
    adapter_type: 'pypi',
    name: `source-${id}`,
    url: `https://source-${id}.example/simple`,
    proxy: '',
    priority: id,
    probe_mode: 'active',
    probe_interval: '30m0s',
    via: '',
    healthy: true,
    has_observations: true,
    runtime_sample_count: 12,
    avg_latency_ms: 20 + id,
    success_rate: 1,
    last_checked_at: timestamp,
    worker_running: true,
    proxy_source: '',
    last_failure: null,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  }
}

function listResponse(items: AdminUpstream[]): AdminUpstreamListResponse {
  return { items, total: items.length }
}

function checkResponse(
  upstream: AdminUpstream,
  healthy: boolean,
  latency = healthy ? 42 : 0,
): CheckUpstreamResponse {
  return {
    upstream: {
      ...upstream,
      healthy,
      avg_latency_ms: latency,
      success_rate: healthy ? 1 : 0.4,
      last_checked_at: timestamp,
    },
    check: {
      healthy,
      latency_ms: latency,
      checked_at: timestamp,
      error: healthy ? null : 'probe returned 503',
      failure: null,
    },
  }
}

async function expectNoDocumentOverflow(page: Page) {
  expect(await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }))).toEqual({
    clientWidth: page.viewportSize()?.width,
    scrollWidth: page.viewportSize()?.width,
  })
}

async function expectAssociatedError(input: Locator, expectedText: string) {
  await expect(input).toHaveAttribute('aria-invalid', 'true')
  const association = await input.evaluate((element) => {
    const describedBy = element.getAttribute('aria-describedby') ?? ''
    return {
      invalid: element.getAttribute('aria-invalid'),
      descriptions: describedBy
        .split(/\s+/)
        .filter(Boolean)
        .map(id => document.getElementById(id)?.textContent?.trim() ?? ''),
    }
  })
  expect(association.invalid).toBe('true')
  expect(association.descriptions).toContain(expectedText)
}

test('an empty upstream list has one working create action', async ({ page }) => {
  await mockAdminApi(page, { 'GET /api/v1/admin/upstreams': listResponse([]) })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/upstreams')

  await expect(page.getByRole('button', { name: '添加上游源', exact: true })).toHaveCount(1)
  await expect(page.getByRole('button', { name: '添加第一个上游源' })).toHaveCount(0)
  await page.getByRole('button', { name: '添加上游源', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '添加上游源' })).toBeVisible()
})

test('desktop adaptive layout keeps a large ecosystem operable and exposes routing metadata', async ({ page }) => {
  const upstreams = Array.from({ length: 27 }, (_, index) => makeUpstream(index + 1))
  await page.setViewportSize({ width: 1440, height: 1000 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse(upstreams),
  })

  await page.goto('/admin/upstreams')

  await expect(page.locator('[data-upstream-row]')).toHaveCount(27)
  const group = page.locator('[data-upstream-group="pypi"]')
  await expect(group).toHaveAttribute('data-upstream-group-layout', 'full')
  const itemGrid = group.locator('[data-upstream-item-grid]')
  await expect(itemGrid).toHaveAttribute('data-upstream-item-grid', 'auto-fill')
  expect(await itemGrid.evaluate((element) => (
    getComputedStyle(element).gridTemplateColumns.split(/\s+/).filter(Boolean).length
  ))).toBeGreaterThanOrEqual(2)

  const firstRow = page.locator('[data-upstream-row]').filter({
    has: page.getByText('source-1', { exact: true }),
  })
  await expect(firstRow).toContainText('https://source-1.example/simple')
  await expect(firstRow).toContainText('Priority 1')
  await expect(firstRow).toContainText('Active probe · 30m')
  await firstRow.getByText('Connection details and metrics', { exact: true }).click()
  await expect(firstRow).toContainText('100%')
  await expectNoDocumentOverflow(page)
})

test('mobile actions, status filters, and search remain explicit and overflow-free', async ({ page }) => {
  const upstreams = [
    makeUpstream(1, {
      name: 'Fast primary',
      url: 'https://fast.example/simple',
    }),
    makeUpstream(2, {
      name: 'Slow backup',
      url: 'https://slow.example/simple',
      avg_latency_ms: 1200,
    }),
    makeUpstream(3, {
      name: 'Offline mirror',
      url: 'https://offline.example/simple',
      healthy: false,
      avg_latency_ms: 0,
      success_rate: 0,
    }),
    makeUpstream(4, {
      name: 'Unobserved mirror',
      has_observations: false,
      runtime_sample_count: 0,
      last_checked_at: null,
      avg_latency_ms: 0,
      success_rate: 1,
    }),
  ]
  await page.setViewportSize({ width: 390, height: 844 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse(upstreams),
  })

  await page.goto('/admin/upstreams')

  for (const name of ['Check All', 'Add Upstream']) {
    const bounds = await page.getByRole('button', { name, exact: true }).boundingBox()
    expect(bounds?.height).toBeGreaterThanOrEqual(40)
  }

  const filters = page.getByRole('group', { name: 'Filter upstreams by health' }).getByRole('button')
  await expect(filters).toHaveCount(5)
  await expect(filters.nth(0)).toHaveAccessibleName('All 4')
  await expect(filters.nth(1)).toHaveAccessibleName('healthy 1')
  await expect(filters.nth(2)).toHaveAccessibleName('degraded 1')
  await expect(filters.nth(3)).toHaveAccessibleName('failed 1')
  await expect(filters.nth(4)).toHaveAccessibleName('Not yet observed 1')
  for (const filter of await filters.all()) {
    await expect(filter).toHaveAttribute('aria-pressed', /^(true|false)$/)
  }

  await filters.nth(4).click()
  const unobserved = page.locator('[data-upstream-row]')
  await expect(unobserved).toHaveCount(1)
  await expect(unobserved).toHaveAttribute('data-upstream-status', 'unknown')
  await unobserved.getByText('Connection details and metrics', { exact: true }).click()
  await expect(unobserved).toContainText('Observation success rate —')
  await expect(unobserved).not.toContainText('100%')
  await filters.nth(0).click()

  const search = page.getByRole('textbox', { name: 'Search upstreams' })
  await search.fill('slow.example')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(1)
  await expect(page.getByText('Slow backup', { exact: true })).toBeVisible()
  await search.fill('Fast primary')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(1)
  await expect(page.getByText('Fast primary', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Clear upstream search' }).click()
  await page.getByRole('button', { name: 'failed 1' }).click()
  await expect(page.locator('[data-upstream-row]')).toHaveCount(1)
  await expect(page.getByText('Offline mirror', { exact: true })).toBeVisible()

  await search.fill('does-not-exist')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(0)
  await page.getByRole('button', { name: 'Clear filters' }).click()
  await expect(page.locator('[data-upstream-row]')).toHaveCount(4)
  await expect(filters.nth(0)).toHaveAttribute('aria-pressed', 'true')
  await expectNoDocumentOverflow(page)
})

test('editing probe intervals faithfully displays Go durations and preserves custom intervals', async ({ page }) => {
  const upstreams = [
    makeUpstream(1, { probe_interval: '30m0s' }),
    makeUpstream(2, { probe_interval: '1h0m0s' }),
    makeUpstream(3, { probe_interval: '45m30s' }),
  ]
  const submitted = new Map<number, UpstreamMutationRequest>()
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse(upstreams),
    ...Object.fromEntries(upstreams.map(upstream => [
      `PUT /api/v1/admin/upstreams/${upstream.id}`,
      (request: Request) => {
        const body: UpstreamMutationRequest = request.postDataJSON()
        submitted.set(upstream.id, body)
        return { ...upstream, ...body }
      },
    ])),
  })
  await page.goto('/admin/upstreams')

  for (const [id, expectedInterval] of [[1, '30m'], [2, '1h'], [3, '45m30s']] as const) {
    await page.getByRole('button', { name: `Edit source-${id}`, exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit Upstream' })
    const interval = dialog.getByRole('combobox', { name: 'Probe interval', exact: true })
    await expect(interval).toHaveValue(expectedInterval)
    await expect(interval.locator('option:checked')).toContainText(expectedInterval)
    await dialog.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(dialog).toBeHidden()
    expect(submitted.get(id)?.probe_interval).toBe(expectedInterval)
  }
})

test('health and history refresh together and refreshing status does not trigger a probe', async ({ page }) => {
  await page.clock.install()
  const upstream = makeUpstream(1)
  let healthy = true
  let listCalls = 0
  let historyCalls = 0
  let probeCalls = 0
  page.on('request', request => {
    if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/check')) {
      probeCalls += 1
    }
  })
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': () => {
      listCalls += 1
      return listResponse([{ ...upstream, healthy }])
    },
    'GET /api/v1/admin/upstreams/latency': () => {
      historyCalls += 1
      return {
        series: [{ upstream_id: 1, points: [{ time: timestamp, latency_ms: healthy ? 21 : 0, healthy }] }],
      }
    },
  })
  await page.goto('/admin/upstreams')
  const row = page.locator('[data-upstream-row]')
  await expect(row).toHaveAttribute('data-upstream-status', 'healthy')
  await expect.poll(() => historyCalls).toBe(1)

  healthy = false
  await page.clock.fastForward(61_000)
  await expect.poll(() => listCalls).toBeGreaterThan(1)
  await expect.poll(() => historyCalls).toBeGreaterThan(1)
  await expect(row).toHaveAttribute('data-upstream-status', 'failed')
  await expect(row.locator('[data-heartbeat-state="down"]')).toHaveCount(1)
  await expect(page.getByRole('button', { name: 'failed 1', exact: true })).toBeVisible()

  const previousListCalls = listCalls
  const previousHistoryCalls = historyCalls
  healthy = true
  await page.getByRole('button', { name: 'Refresh status', exact: true }).click()
  await expect.poll(() => listCalls).toBeGreaterThan(previousListCalls)
  await expect.poll(() => historyCalls).toBeGreaterThan(previousHistoryCalls)
  await expect(row).toHaveAttribute('data-upstream-status', 'healthy')
  await expect(row.locator('[data-heartbeat-state="down"]')).toHaveCount(0)
  expect(probeCalls).toBe(0)
})

test('an upstream with a removed cascade peer can be switched back to direct egress', async ({ page }) => {
  const upstream = makeUpstream(1, { via: 'removed-parent' })
  let submitted: UpstreamMutationRequest | undefined
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([upstream]),
    'PUT /api/v1/admin/upstreams/1': (request: Request) => {
      submitted = request.postDataJSON()
      return { ...upstream, ...submitted }
    },
  })
  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Edit source-1', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Edit Upstream' })
  const egress = dialog.getByRole('combobox', { name: 'Cascade egress', exact: true })
  await expect(egress).toHaveValue('removed-parent')
  await expect(dialog).toContainText('removed-parent')
  await egress.selectOption({ label: 'Direct (default)' })
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(dialog).toBeHidden()
  expect(submitted?.via).toBe('')
})

test('changing the URL, proxy or name of an existing npm upstream warns that lockfiles generated through Depsilo stop working', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([
      makeUpstream(1, { adapter_type: 'npm', name: 'npmjs', url: 'https://registry.npmjs.org' }),
      makeUpstream(2),
    ]),
  })
  await page.goto('/admin/upstreams')
  const dialog = page.getByRole('dialog', { name: 'Edit Upstream' })
  const hint = dialog.locator('[data-npm-source-change]')
  const message = 'Changing the name, URL or proxy of this npm upstream invalidates lockfiles generated through Depsilo: '
    + 'the tarball URLs they record return 404 afterwards, so regenerate them after saving.'

  await page.getByRole('button', { name: 'Edit npmjs', exact: true }).click()
  const address = dialog.getByLabel('URL', { exact: true })
  await expect(address).toHaveValue('https://registry.npmjs.org')
  await expect(hint).toHaveCount(0)
  await address.fill('https://registry.npmmirror.com')
  await expect(hint).toHaveText(message)
  await expect(address).toHaveAccessibleDescription(/invalidates lockfiles generated through Depsilo/)
  // Restoring the saved URL changes nothing, so the warning goes away.
  await address.fill(' https://registry.npmjs.org ')
  await expect(hint).toHaveCount(0)
  // Signed tarball URLs also carry the upstream's own proxy and its name.
  const proxy = dialog.getByLabel('HTTP Proxy (optional)', { exact: true })
  await proxy.fill('http://10.0.0.9:3128')
  await expect(hint).toHaveText(message)
  await expect(proxy).toHaveAccessibleDescription(/invalidates lockfiles generated through Depsilo/)
  await proxy.fill(' ')
  await expect(hint).toHaveCount(0)
  const name = dialog.getByLabel('Name', { exact: true })
  await name.fill('npm-official')
  await expect(hint).toHaveText(message)
  await expect(name).toHaveAccessibleDescription(/invalidates lockfiles generated through Depsilo/)
  // With both changed, the hint is shown once.
  await address.fill('https://registry.npmmirror.com')
  await expect(hint).toHaveCount(1)
  await name.fill(' npmjs ')
  await address.fill('https://registry.npmjs.org')
  await expect(hint).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(dialog).toBeHidden()

  // Other ecosystems do not issue instance-bound URLs.
  await page.getByRole('button', { name: 'Edit source-2', exact: true }).click()
  await dialog.getByLabel('URL', { exact: true }).fill('https://other.example/simple')
  await dialog.getByLabel('Name', { exact: true }).fill('renamed')
  await expect(hint).toHaveCount(0)
})

test('pending or failed cascade reads do not mislabel an existing peer as removed', async ({ page }) => {
  const upstream = makeUpstream(1, { via: 'existing-parent' })
  let releaseCascade: (() => void) | undefined
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([upstream]),
    'GET /api/v1/admin/cascade': async () => {
      await new Promise<void>(resolve => { releaseCascade = resolve })
      return { status: 503, body: { code: 'UNAVAILABLE', message: 'temporary failure' } }
    },
  })
  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Edit source-1', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Edit Upstream' })
  const egress = dialog.getByRole('combobox', { name: 'Cascade egress', exact: true })
  await expect(egress).toHaveValue('existing-parent')
  await expect(dialog).not.toContainText('removed from config')
  await expect(dialog).not.toContainText('no longer defines it')
  await expect.poll(() => Boolean(releaseCascade)).toBe(true)
  releaseCascade?.()
  await expect(dialog).toContainText('Cascade information is unavailable')
  await expect(dialog).not.toContainText('removed from config')
  await expect(dialog).not.toContainText('no longer defines it')
  await expect(egress).toHaveValue('existing-parent')
  await egress.selectOption({ label: 'Direct (default)' })
  await expect(egress).toHaveValue('')
})

test('a successful probe and an unhealthy runtime state remain separate in durable check results', async ({ page }) => {
  const upstream = makeUpstream(1, { name: 'recovering-mirror', healthy: false, success_rate: 0.5 })
  const result = checkResponse(upstream, true, 42)
  result.upstream.healthy = false
  result.upstream.success_rate = 0.5
  await setUiPreferences(page, 'dark', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([upstream]),
    'POST /api/v1/admin/upstreams/1/check': result,
  })
  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Check recovering-mirror', exact: true }).click()
  await expect(page.getByText('“recovering-mirror” passed its check at 42 ms', { exact: true })).toBeVisible()
  await expect(page.locator('[data-upstream-row]')).toHaveAttribute('data-upstream-status', 'failed')
  await page.getByRole('button', { name: /^Check results/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Check results', exact: true })
  const check = dialog.locator('[data-upstream-check-result="1"]')
  await expect(check).toContainText('Probe passed')
  await expect(check).toContainText('42')
  await expect(check).toContainText(/Runtime status.*failed/i)
  await expect(check).not.toContainText('Could not reach the upstream')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await page.getByRole('button', { name: /^Check results/ }).click()
  await expect(check).toContainText('Probe passed')
})

test('a fake-IP egress failure is explained on the card and in check results', async ({ page }) => {
  const fakeIP = { code: 'egress_fake_ip' as const, address: '198.18.0.65', range: '198.18.0.0/15' }
  const failed = makeUpstream(1, {
    name: 'official', url: 'https://pypi.org', healthy: false, success_rate: 0, avg_latency_ms: 5, last_failure: fakeIP,
  })
  const proxied = makeUpstream(2, { name: 'tuna', url: 'https://pypi.tuna.tsinghua.edu.cn', proxy_source: 'default' })
  const result = checkResponse(failed, false)
  result.upstream.last_failure = fakeIP
  result.check.error = 'upstream check failed'
  result.check.failure = fakeIP
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([failed, proxied]),
    'POST /api/v1/admin/upstreams/1/check': result,
  })
  await page.goto('/admin/upstreams')

  const card = page.locator('[data-upstream-row][data-upstream-status="failed"]')
  // Depsilo connects to fake-IP addresses by default, so the code means that
  // nothing carried the connection, that allow_fake_ip is off, or that the
  // guard always refuses the answer.
  await expect(card.locator('[data-upstream-failure="egress_fake_ip"]')).toContainText(
    'DNS resolved this upstream to 198.18.0.65, in the fake-IP range 198.18.0.0/15 that proxy software in fake-IP mode answers from, '
    + 'and Depsilo could not connect to it: nothing carried the connection, [egress] allow_fake_ip is off, '
    + 'or the answer mixed fake-IP and other addresses or held a unique-local IPv6 fake-IP address, which Depsilo always refuses.')
  await expect(card.locator('[data-upstream-failure="egress_fake_ip"]')).toContainText(
    'Turn on the proxy software\'s TUN mode, which carries connections to fake-IP addresses')
  // A container operator sets the proxy in the environment, not in config.toml.
  await expect(card.locator('[data-upstream-failure="egress_fake_ip"]')).toContainText(
    'upstream_proxy under [egress] in config.toml (or the environment variable DEPSILO_EGRESS_UPSTREAM_PROXY)')
  // The loopback example is wrong inside a container, and a container keeps
  // the environment it was created with, so the remedy says to run it again.
  await expect(card.locator('[data-upstream-failure="egress_fake_ip"]')).toContainText(
    'Inside a container, 127.0.0.1 is the container itself: remove the container and run it again with '
    + 'DEPSILO_EGRESS_UPSTREAM_PROXY=http://host.docker.internal:7890 (docker restart keeps the old environment), '
    + 'adding --add-host=host.docker.internal:host-gateway on Docker Engine for Linux. '
    + 'If [egress] allow_fake_ip or DEPSILO_EGRESS_ALLOW_FAKE_IP is false, Depsilo does not connect to fake-IP addresses itself: '
    + 'check whether it was turned off on purpose, for a network that really uses these ranges; '
    + 'if not, set it to true, or use the proxy port. '
    + 'If DNS also returns IPv6 fake-IP addresses from a unique-local range such as fdfe:dcba:9876::/48, '
    + 'set the proxy software\'s IPv6 fake-IP range to 2001:2::/48. '
    + 'Details: "Upstream network egress" in the deployment guide.',
  )
  // A failed upstream's average is not a response latency.
  await expect(card).not.toContainText('5 ms')
  await page.getByText('Connection details').nth(1).click()
  await expect(page.getByText('Uses the default egress proxy ([egress] upstream_proxy)', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Check official', exact: true }).click()
  await page.getByRole('button', { name: /^Check results/ }).click()
  const check = page.getByRole('dialog', { name: 'Check results', exact: true }).locator('[data-upstream-check-result="1"]')
  await expect(check.locator('[data-upstream-failure="egress_fake_ip"]')).toContainText('198.18.0.0/15')
  // The withheld raw error is not repeated as a meaningless detail.
  await expect(check).not.toContainText('upstream check failed')
  await expect(check.getByText('Error details')).toHaveCount(0)
})

test('fake-IP DNS failing every upstream is explained once for the page', async ({ page }) => {
  const items = [1, 2, 3].map(id => makeUpstream(id, {
    healthy: false, success_rate: 0, last_failure: { code: 'egress_fake_ip', address: `198.18.0.${id}`, range: '198.18.0.0/15' },
  }))
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, { 'GET /api/v1/admin/upstreams': listResponse(items) })
  await page.goto('/admin/upstreams')
  const notice = page.locator('[data-upstream-shared-failure="egress_fake_ip"]')
  await expect(notice).toContainText('3 upstreams cannot connect for the same reason')
  await expect(notice).toContainText('and Depsilo could not connect to those addresses: nothing carried the connections, '
    + '[egress] allow_fake_ip is off, or the answers mixed fake-IP and other addresses or held unique-local IPv6 fake-IP addresses, '
    + 'which Depsilo always refuses.')
  await expect(notice).toContainText('Turn on the proxy software\'s TUN mode')
  await expect(notice).toContainText('upstream_proxy under [egress]')
  const cardReasons = page.locator('[data-upstream-row] [data-upstream-failure="egress_fake_ip"]')
  await expect(cardReasons).toHaveCount(3)
  // The card line holds for every cause that the shared notice names.
  await expect(cardReasons.first()).toHaveText('DNS returned the fake-IP address 198.18.0.1; not connected.')

  // The remedy's variable cannot break at a space; it must still wrap on a phone.
  await page.setViewportSize({ width: 320, height: 844 })
  const remedy = await notice.getByText('DEPSILO_EGRESS_UPSTREAM_PROXY=http://host.docker.internal:7890').boundingBox()
  expect(remedy!.x + remedy!.width).toBeLessThanOrEqual(320)

  await setUiPreferences(page, 'light', 'zh')
  await page.reload()
  await expect(notice).toContainText('Depsilo 无法连接这些地址：可能是连接没有被接管，或者 [egress] allow_fake_ip 已关闭，'
    + '也可能是应答里混有其他地址或唯一本地地址段的 IPv6 fake-IP 地址，这类应答 Depsilo 一律拒绝。')
  await expect(notice).toContainText('打开代理软件的 TUN 模式，由它接管到 fake-IP 地址的连接')
  await expect(notice).toContainText('把 upstream_proxy（或环境变量 DEPSILO_EGRESS_UPSTREAM_PROXY）设为代理软件的 HTTP 端口')
  await expect(notice).toContainText('在容器里 127.0.0.1 指的是容器自己：要删除容器，再带上 DEPSILO_EGRESS_UPSTREAM_PROXY=http://host.docker.internal:7890 重新运行（docker restart 沿用旧的环境变量），'
    + 'Linux 上的 Docker Engine 还要加 --add-host=host.docker.internal:host-gateway。'
    + '如果 [egress] allow_fake_ip 或 DEPSILO_EGRESS_ALLOW_FAKE_IP 设成了 false，Depsilo 不会自己连接 fake-IP 地址：'
    + '先确认它是不是因为网络里确实在用这些地址段而特意关闭的；如果不是，改回 true，或改用代理端口。'
    + '如果 DNS 还返回了 fdfe:dcba:9876::/48 这类唯一本地地址段里的 IPv6 fake-IP 地址，请把代理软件的 IPv6 fake-IP 地址段改为 2001:2::/48。'
    + '详见部署指南的“Upstream network egress”一节。')
  await expect(cardReasons.first()).toHaveText('DNS 返回了 fake-IP 地址 198.18.0.1，未连接。')
})

test('a failure at the egress proxy is explained on the card and in check results', async ({ page }) => {
  const action = 'Check that the proxy runs and that [egress] upstream_proxy in config.toml, or this upstream\'s HTTP proxy, '
    + 'points at it. If the proxy cannot reach this upstream because it is on your own network, list its host under '
    + '[egress] no_proxy and restart, so Depsilo connects to it directly.'
  // The proxy answered CONNECT with 502; then the check could not reach it.
  const refused = { code: 'egress_proxy' as const, status: 502 }
  const unreachable = { code: 'egress_proxy' as const }
  const failed = makeUpstream(1, {
    name: 'internal', url: 'https://pypi.internal.example', healthy: false, success_rate: 0, proxy_source: 'default', last_failure: refused,
  })
  const result = checkResponse(failed, false)
  result.upstream.last_failure = unreachable
  result.check.error = 'upstream check failed'
  result.check.failure = unreachable
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([failed]),
    'POST /api/v1/admin/upstreams/1/check': result,
  })
  await page.goto('/admin/upstreams')

  const reason = page.locator('[data-upstream-row][data-upstream-status="failed"] [data-upstream-failure="egress_proxy"]')
  await expect(reason).toContainText('The egress proxy refused to open a connection for this upstream (CONNECT answered 502).')
  await expect(reason).toContainText(action)
  // It is not a fake-IP answer, so the page has no shared fake-IP notice.
  await expect(page.locator('[data-upstream-shared-failure]')).toHaveCount(0)

  await page.getByRole('button', { name: 'Check internal', exact: true }).click()
  await page.getByRole('button', { name: /^Check results/ }).click()
  const check = page.getByRole('dialog', { name: 'Check results', exact: true }).locator('[data-upstream-check-result="1"]')
  await expect(check.locator('[data-upstream-failure="egress_proxy"]')).toContainText(
    'Depsilo could not connect to the egress proxy that carries this upstream.')
  await expect(check.locator('[data-upstream-failure="egress_proxy"]')).toContainText(action)
  await expect(check).not.toContainText('CONNECT answered')
  await page.keyboard.press('Escape')

  await setUiPreferences(page, 'light', 'zh')
  await page.reload()
  await expect(reason).toContainText('出站代理拒绝为这个上游建立连接（CONNECT 请求返回了 502）。')
  // An IP entry in no_proxy matches only a URL written with that address.
  await expect(reason).toContainText('把它 URL 里的主机名（URL 写的是 IP 时就写 IP）加入 [egress] no_proxy 后重启，Depsilo 就会直接连接它。')
})

test('a failure at an upstream\'s own proxy is not sent to no_proxy, which cannot bypass that proxy', async ({ page }) => {
  const action = 'Check that the proxy runs and that this upstream\'s HTTP proxy points at it. [egress] no_proxy does not apply '
    + 'to an upstream with its own HTTP proxy: if the proxy cannot reach this upstream, or its cascade parent, because it is '
    + 'on your own network, clear this upstream\'s HTTP proxy. An upstream that is not a cascade then uses [egress] '
    + 'upstream_proxy when one is set, so also list its host under [egress] no_proxy and restart.'
  const refused = { code: 'egress_proxy' as const, status: 403 }
  const failed = makeUpstream(1, {
    name: 'corp', url: 'https://pypi.corp.example', proxy: 'http://proxy.example:3128', healthy: false, success_rate: 0,
    proxy_source: 'upstream', last_failure: refused,
  })
  const result = checkResponse(failed, false)
  result.upstream.last_failure = refused
  result.check.error = 'upstream check failed'
  result.check.failure = refused
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([failed]),
    'POST /api/v1/admin/upstreams/1/check': result,
  })
  await page.goto('/admin/upstreams')

  const reason = page.locator('[data-upstream-row][data-upstream-status="failed"] [data-upstream-failure="egress_proxy"]')
  await expect(reason).toContainText('The egress proxy refused to open a connection for this upstream (CONNECT answered 403).')
  await expect(reason).toContainText(action)
  await expect(reason).not.toContainText('so Depsilo connects to it directly')

  await page.getByRole('button', { name: 'Check corp', exact: true }).click()
  await page.getByRole('button', { name: /^Check results/ }).click()
  const check = page.getByRole('dialog', { name: 'Check results', exact: true }).locator('[data-upstream-check-result="1"]')
  await expect(check.locator('[data-upstream-failure="egress_proxy"]')).toContainText(action)
  await expect(check).not.toContainText('so Depsilo connects to it directly')
  await page.keyboard.press('Escape')

  await setUiPreferences(page, 'light', 'zh')
  await page.reload()
  await expect(reason).toContainText('[egress] no_proxy 对单独配置了 HTTP 代理的上游不起作用')
  await expect(reason).toContainText('请清空这个上游的 HTTP 代理。')
  await expect(reason).toContainText('非级联的上游会改用 [egress] upstream_proxy')
  await expect(reason).not.toContainText('Depsilo 就会直接连接它')
})

test('bulk checking is capped at four requests and leaves a durable outcome summary', async ({ page }) => {
  const upstreams = Array.from({ length: 6 }, (_, index) => makeUpstream(index + 1, {
    name: `bulk-${index + 1}`,
  }))
  const releases = new Map<number, () => void>()
  let activeRequests = 0
  let maxActiveRequests = 0
  let startedRequests = 0
  const overrides = Object.fromEntries(upstreams.map((upstream) => [
    `POST /api/v1/admin/upstreams/${upstream.id}/check`,
    async () => {
      activeRequests += 1
      startedRequests += 1
      maxActiveRequests = Math.max(maxActiveRequests, activeRequests)
      await new Promise<void>((resolve) => {
        releases.set(upstream.id, resolve)
      })
      activeRequests -= 1
      if (upstream.id === 3) {
        return {
          status: 500,
          body: { code: 'CHECK_FAILED', message: 'probe transport failed' },
        }
      }
      return checkResponse(
        upstream,
        upstream.id !== 2 && upstream.id !== 5,
        upstream.id === 4 ? 1200 : undefined,
      )
    },
  ]))
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse(upstreams),
    ...overrides,
  })

  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Check All', exact: true }).click()

  await expect.poll(() => startedRequests).toBe(4)
  await expect(page.getByRole('button', { name: 'Checking 0/6', exact: true })).toBeVisible()
  const rowCheckButtons = page.getByRole('button', { name: /^Check bulk-/ })
  await expect(rowCheckButtons).toHaveCount(6)
  expect(await rowCheckButtons.evaluateAll(buttons => (
    buttons.every(button => (button as HTMLButtonElement).disabled)
  ))).toBe(true)

  releases.get(1)?.()
  await expect(page.getByRole('button', { name: 'Checking 1/6', exact: true })).toBeVisible()
  await expect.poll(() => startedRequests).toBe(5)
  for (const id of [2, 3, 4, 5]) releases.get(id)?.()
  await expect.poll(() => startedRequests).toBe(6)
  releases.get(6)?.()

  const summary = page.locator('[data-upstream-check-summary]')
  await expect(summary).toContainText('2 healthy')
  await expect(summary).toContainText('1 degraded')
  await expect(summary).toContainText('2 failed')
  await expect(summary).toContainText('failed requests: 1')
  await expect(page.getByRole('button', { name: 'Check All', exact: true })).toBeEnabled()
  await expect(summary).toBeVisible()
  expect(maxActiveRequests).toBeLessThanOrEqual(4)
  await page.getByRole('button', { name: /^Check results/ }).click()
  const results = page.getByRole('dialog', { name: 'Check results', exact: true })
  await expect(results.locator('[data-upstream-check-result="2"]')).toContainText('probe returned 503')
  await expect(results.locator('[data-upstream-check-result="3"]')).toContainText('probe transport failed')
})

test('delete failure stays in the named dialog with routing context', async ({ page }) => {
  const upstream = makeUpstream(7, {
    name: 'only-pypi',
    url: 'https://only-pypi.example/simple',
    priority: 7,
  })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([upstream]),
    'DELETE /api/v1/admin/upstreams/7': {
      status: 409,
      body: {
        code: 'LAST_UPSTREAM',
        message: 'cannot delete the last upstream',
      },
    },
  })

  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Delete only-pypi', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Delete “only-pypi”?' })
  await expect(dialog).toContainText('https://only-pypi.example/simple')
  await expect(dialog).toContainText('Priority')
  await expect(dialog).toContainText('7')
  await dialog.getByRole('button', { name: 'Delete', exact: true }).click()

  await expect(dialog).toContainText(
    'This is the ecosystem’s last upstream. Add a replacement before deleting it.',
  )
  await expect(dialog).toContainText('https://only-pypi.example/simple')
  await expect(dialog).toContainText('7')
  await expect(dialog.getByRole('button', { name: 'Delete', exact: true })).toBeEnabled()
})

test('invalid upstream and proxy schemes expose associated errors without an API request', async ({ page }) => {
  let createRequests = 0
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'POST /api/v1/admin/upstreams': () => {
      createRequests += 1
      return makeUpstream(99)
    },
  })

  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Add Upstream', exact: true }).click()

  const dialog = page.getByRole('dialog', { name: 'Add Upstream' })
  const name = dialog.getByLabel('Name', { exact: true })
  await name.fill('源'.repeat(43))
  const upstreamUrl = dialog.getByLabel('URL', { exact: true })
  const proxyUrl = dialog.getByLabel('HTTP Proxy (optional)', { exact: true })
  await upstreamUrl.fill('https://registry.example/simple?token=secret')
  await proxyUrl.fill('socks5://127.0.0.1:1080')
  await dialog.getByRole('button', { name: 'Save', exact: true }).click()

  await expectAssociatedError(
    name,
    'Use a name no longer than 128 UTF-8 bytes',
  )
  await expectAssociatedError(
    upstreamUrl,
    'Enter an HTTP/HTTPS URL without a query or fragment',
  )
  await expectAssociatedError(
    proxyUrl,
    'Enter a valid HTTP/HTTPS proxy URL',
  )
  expect(createRequests).toBe(0)
  await expect(dialog).toBeVisible()
})

test('pending create and delete requests keep their dialogs locked until completion', async ({ page }) => {
  const existing = makeUpstream(1, { name: 'stable-source' })
  let releaseCreate: (() => void) | undefined
  let releaseDelete: (() => void) | undefined
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse([existing]),
    'POST /api/v1/admin/upstreams': async () => {
      await new Promise<void>((resolve) => { releaseCreate = resolve })
      return makeUpstream(2, { name: 'new-source' })
    },
    'DELETE /api/v1/admin/upstreams/1': async () => {
      await new Promise<void>((resolve) => { releaseDelete = resolve })
      return { deleted_id: 1 }
    },
  })

  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: 'Add Upstream', exact: true }).click()
  const createDialog = page.getByRole('dialog', { name: 'Add Upstream' })
  await createDialog.getByLabel('Name', { exact: true }).fill('new-source')
  await createDialog.getByLabel('URL', { exact: true }).fill('https://new-source.example/simple')
  await createDialog.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(createDialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled()
  await expect(createDialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(createDialog).toBeVisible()
  releaseCreate?.()
  await expect(createDialog).toBeHidden()

  await page.getByRole('button', { name: 'Delete stable-source', exact: true }).click()
  const deleteDialog = page.getByRole('dialog', { name: 'Delete “stable-source”?' })
  await deleteDialog.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(deleteDialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled()
  await expect(deleteDialog.getByRole('button', { name: 'Close', exact: true })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(deleteDialog).toBeVisible()
  releaseDelete?.()
  await expect(deleteDialog).toBeHidden()
})

test('a populated light-theme upstream workspace passes WCAG A and AA checks', async ({ page }) => {
  const upstreams = [
    makeUpstream(1),
    makeUpstream(2, { avg_latency_ms: 1200 }),
    makeUpstream(3, { healthy: false, avg_latency_ms: 0, success_rate: 0 }),
  ]
  await page.setViewportSize({ width: 1440, height: 1000 })
  await setUiPreferences(page, 'light', 'en')
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': listResponse(upstreams),
  })

  await page.goto('/admin/upstreams')
  await expect(page.locator('[data-upstream-row]')).toHaveCount(3)

  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa'])
    .analyze()
  expect(result.violations).toEqual([])
})
