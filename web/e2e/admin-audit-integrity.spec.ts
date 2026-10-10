import type { AuditChainReport, AuditIntegrityResponse } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

function integrity(overrides: Partial<AuditChainReport>): AuditIntegrityResponse {
  return {
    integrity: {
      ok: true,
      rows_scanned: 1200,
      chained_rows: 1180,
      unchained_rows: 20,
      first_chained_id: 21,
      head_id: 1200,
      head_hash: 'abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789',
      scanned_at: '2026-10-06T10:00:00Z',
      ...overrides,
    },
    anchors: { configured: false, checkpoints: 0, ok: true },
    anchor_feed: { file_configured: false, remote_configured: false },
  }
}

test('audit page shows a verified chain including the pre-chain prefix', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/integrity': integrity({}),
  })
  await page.goto('/admin/audit')

  await expect(page.getByText(/审计链已校验：1,180 行/)).toBeVisible()
  await expect(page.getByText(/20 行早于哈希链/)).toBeVisible()
})

test('audit page surfaces a broken chain with the offending row', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/integrity': integrity({
      ok: false,
      broken_at_id: 842,
      reason: 'hash does not match the row content',
    }),
  })
  await page.goto('/admin/audit')

  await expect(page.getByText(/审计链在第 842 行断裂/)).toBeVisible()
  await expect(page.getByText(/hash does not match the row content/)).toBeVisible()
})

test('audit page warns when the remote anchor feed is behind', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/integrity': {
      ...integrity({}),
      anchor_feed: {
        file_configured: false,
        remote_configured: true,
        remote_last_error: 'anchor endpoint returned HTTP 503',
      },
    },
  })
  await page.goto('/admin/audit')

  await expect(page.getByText(/远程锚点投递失败/)).toBeVisible()
  await expect(page.getByText(/HTTP 503/)).toBeVisible()
})

test('audit page shows chain and anchor failures even when remote delivery also fails', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/integrity': {
      ...integrity({ ok: false, broken_at_id: 842, reason: 'hash mismatch' }),
      anchors: { configured: true, checkpoints: 2, ok: false, broken_at_id: 840, reason: 'anchor mismatch' },
      anchor_feed: { file_configured: false, remote_configured: true, remote_last_error: 'HTTP 503' },
    },
  })
  await page.goto('/admin/audit')

  const chain = page.getByText(/审计链在第 842 行断裂/)
  const anchor = page.getByText(/840.*anchor mismatch/)
  const remote = page.getByText(/远程锚点投递失败.*HTTP 503/)
  await expect(chain).toBeVisible()
  await expect(anchor).toBeVisible()
  await expect(remote).toBeVisible()
  expect((await chain.boundingBox())!.y).toBeLessThan((await remote.boundingBox())!.y)
})

test('audit page marks the previous verification stale when rechecking fails', async ({ page }) => {
  let calls = 0
  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/integrity': () => {
      calls += 1
      return calls === 1 ? integrity({}) : { status: 503, body: { code: 'UNAVAILABLE' } }
    },
  })
  await page.goto('/admin/audit')
  await expect(page.getByText(/审计链已校验/)).toBeVisible()
  await page.getByRole('button', { name: '重新校验' }).click()
  await expect.poll(() => calls).toBe(2)
  await expect(page.getByText(/校验失败|上次校验结果/)).toBeVisible()
})
