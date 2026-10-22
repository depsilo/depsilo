import { expect, mockAdminApi, test } from './fixtures/admin-api'
import type { Request } from '@playwright/test'

const advisory = {
  id: 1, osv_id: 'GHSA-review-fixture', ecosystem: 'pypi', package_name: 'review-package',
  affected_ranges: '[{"introduced":"0","fixed":"2.0.0"}]', severity: 'high', cvss_score: 8.1,
  summary: 'Fixture advisory summary', details: 'Evidence for a deliberate operator decision.',
  aliases: 'CVE-FIXTURE', references: '[{"url":"https://example.org/advisory"}]',
  published_at: '2026-10-20T00:00:00Z', modified_at: '2026-10-20T00:00:00Z',
  created_at: '2026-10-20T00:00:00Z', updated_at: '2026-10-20T00:00:00Z',
}
const event = {
  id: 1, ecosystem: 'pypi', package: 'review-package', version: '1.2.0', action: 'blocked',
  reason: 'Minimum release age not reached', threshold_seconds: 86400, age_at_call_seconds: 3600,
  actor_id: 0, client_ip: '192.0.2.1', created_at: '2026-10-20T08:00:00Z',
}

test('dismissing the last suggestion on a later page returns to remaining results', async ({ page }) => {
  let dismissed = false
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/suggestions': (request: Request) => {
      const currentPage = Number(new URL(request.url()).searchParams.get('page') || 1)
      return { items: currentPage === 2 && dismissed ? [] : [{ ...advisory, id: currentPage === 1 ? 1 : 21 }], total: dismissed ? 20 : 21, page: currentPage }
    },
    'POST /api/v1/admin/security/suggestions/21/dismiss': () => { dismissed = true; return { status: 'dismissed' } },
  })
  await page.goto('/admin/security?tab=suggestions&spage=2')
  await page.getByRole('button', { name: '忽略', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '忽略建议' }).click()
  await expect(page).not.toHaveURL(/spage=2/)
  await expect(page.getByText('review-package', { exact: true })).toBeVisible()
  await expect(page.getByText('暂无建议规则')).toHaveCount(0)
})

test('reviewing advisory evidence creates only an explicitly reviewed manual selector', async ({ page }) => {
  let created: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/security/vulnerabilities': { items: [advisory], total: 1, page: 1 },
    'POST /api/v1/admin/rules': (request: Request) => { created = request.postDataJSON(); return { ...request.postDataJSON(), id: 9 } },
  })
  await page.setViewportSize({ width: 320, height: 900 })
  await page.goto('/admin/security?tab=vulnerabilities&package=review-package')
  await page.getByRole('button', { name: /review-package · GHSA-review-fixture/ }).click()
  const detail = page.getByRole('dialog')
  await expect(detail).toContainText('Evidence for a deliberate operator decision.')
  await expect(detail).toContainText('introduced')
  await expect(detail.getByRole('link', { name: 'https://example.org/advisory' })).toHaveAttribute('href', 'https://example.org/advisory')
  await detail.getByRole('link', { name: '复核并起草包规则' }).click()
  const draft = page.getByRole('dialog', { name: '添加规则' })
  await expect(draft.getByLabel('包名', { exact: true })).toHaveValue('review-package')
  await expect(draft.getByLabel('版本', { exact: true })).toHaveValue('')
  await expect(draft).toContainText('尚未创建规则')
  await draft.getByRole('button', { name: '保存', exact: true }).click()
  expect(created).toBeUndefined()
  await draft.getByLabel('版本', { exact: true }).fill('<2.0.0')
  await draft.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => created).toEqual({ ecosystem: 'pypi', package_name: 'review-package', version: '<2.0.0', action: 'deny', reason: 'GHSA-review-fixture' })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
})

test('event details hand off an exact coordinate and explain permanent age approval', async ({ page }) => {
  let approved: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/quarantine/events': { items: [event], total: 1 },
    'GET /api/v1/admin/capabilities/summary': { capabilities: [{ name: 'minimum_release_age', ecosystem: 'pypi', support: 'supported', mode: 'block', data_status: 'source_bound' }] },
    'POST /api/v1/admin/quarantine/approve': (request: Request) => { approved = request.postDataJSON(); return { status: 'approved' } },
  })
  await page.goto('/admin/quarantine?tab=events&package=review-package&action=blocked')
  await page.getByRole('button', { name: 'review-package', exact: true }).click()
  const detail = page.getByRole('dialog')
  await expect(detail).toContainText('86400')
  await expect(detail).toContainText('192.0.2.1')
  await detail.getByRole('link', { name: '新建放行' }).click()
  await expect(page).toHaveURL(/tab=approvals/)
  const approval = page.getByRole('dialog', { name: '新建最小发布年龄放行' })
  await expect(approval.getByLabel('包名', { exact: true })).toHaveValue('review-package')
  await expect(approval.getByLabel('版本', { exact: true })).toHaveValue('1.2.0')
  await expect(approval).toContainText('持续至撤销')
  await expect(approval).toContainText('其他包规则仍然生效')
  await approval.getByLabel('原因').fill('Reviewed exact artifact')
  await approval.getByRole('button', { name: '新建放行', exact: true }).click()
  await expect.poll(() => approved).toEqual({ ecosystem: 'pypi', package: 'review-package', version: '1.2.0', reason: 'Reviewed exact artifact' })
})

test('quarantine filters survive reload and readonly users can inspect evidence without approving', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { id: 2, username: 'reader', role: 'readonly', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: false },
    'GET /api/v1/admin/quarantine/events': { items: [event], total: 1 },
  })
  await page.setViewportSize({ width: 320, height: 900 })
  await page.goto('/admin/quarantine?tab=events&package=review-package&ecosystem=pypi&action=blocked')
  await page.reload()
  await expect(page.getByRole('textbox', { name: '按包名搜索' })).toHaveValue('review-package')
  await expect(page.getByRole('combobox', { name: '生态', exact: true })).toHaveValue('pypi')
  await page.getByRole('button', { name: '查看事件详情' }).click()
  await expect(page.getByRole('dialog')).toContainText('Minimum release age not reached')
  await expect(page.getByRole('dialog').getByRole('link', { name: '新建放行' })).toHaveCount(0)
  await page.keyboard.press('Escape')
  await page.getByRole('combobox', { name: '拦截视图' }).selectOption('approvals')
  await page.reload()
  await expect(page.getByRole('combobox', { name: '拦截视图' })).toHaveValue('approvals')
})

test('malware override requires an explicit version or deliberate all-version scope', async ({ page }) => {
  let submitted: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/blocklist/status': { enabled: true, mode: 'block', entry_count: 5, ecosystems: ['pypi'], last_success_at: '2026-10-20T00:00:00Z' },
    'POST /api/v1/admin/blocklist/overrides': (request: Request) => { submitted = request.postDataJSON(); return { ...request.postDataJSON(), id: 4, created_at: '2026-10-22T00:00:00Z', expires_at: '2026-10-23T00:00:00Z' } },
  })
  await page.goto('/admin/quarantine?tab=blocklist')
  await page.getByRole('button', { name: '添加豁免' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('包名', { exact: true }).fill('review-package')
  await dialog.getByLabel('原因').fill('Reviewed false positive')
  await expect(dialog.getByRole('button', { name: '创建豁免（24h）' })).toBeDisabled()
  await dialog.getByLabel('豁免版本范围').selectOption('all')
  await expect(dialog).toContainText('所有版本都将在 24 小时内')
  await dialog.getByRole('button', { name: '创建豁免（24h）' }).click()
  await expect.poll(() => submitted).toEqual({ ecosystem: 'pypi', package: 'review-package', version: '', reason: 'Reviewed false positive' })
})

test('attention uses canonical upstream evidence and retains unknown as a neutral state', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/upstreams': { items: [{ id: 1, name: 'unobserved', adapter_type: 'pypi', healthy: true, has_observations: false, avg_latency_ms: 0 }], total: 1 },
  })
  await page.goto('/admin/upstreams')
  await expect(page.getByText('unobserved', { exact: true })).toBeVisible()
  await page.goto('/admin/attention')
  await expect(page.getByText(/另有 1 个上游尚无观测/)).toBeVisible()
  await expect(page.getByText('上游源需要关注', { exact: true })).toHaveCount(0)
  await expect(page.getByText('当前没有需要处理的运行风险')).toBeVisible()
})

test('mobile rule review preserves filters, complete reasons, and explicit delete confirmation', async ({ page }) => {
  let deleted = false
  const reason = 'A reviewed policy reason that remains fully accessible even when it exceeds the table cell preview.'
  await mockAdminApi(page, {
    'GET /api/v1/admin/rules': { items: [{ id: 4, ecosystem: 'pypi', package_name: 'review-package', version: '<2.0.0', action: 'deny', reason, created_by: 'admin', created_at: '2026-10-20T00:00:00Z' }], total: 1 },
    'DELETE /api/v1/admin/rules/4': () => { deleted = true; return { status: 'deleted' } },
  })
  await page.setViewportSize({ width: 320, height: 900 })
  await page.goto('/admin/rules?q=review&filter_ecosystem=pypi&action=deny')
  await page.reload()
  await page.getByRole('button', { name: 'review-package', exact: true }).click()
  const detail = page.getByRole('dialog')
  await expect(detail).toContainText(reason)
  await detail.getByRole('button', { name: '删除', exact: true }).click()
  expect(deleted).toBe(false)
  const confirmation = page.getByRole('dialog')
  await expect(confirmation).toContainText('review-package')
  await confirmation.getByRole('button', { name: '删除', exact: true }).click()
  await expect.poll(() => deleted).toBe(true)
})
