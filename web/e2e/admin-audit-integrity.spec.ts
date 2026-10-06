import type { AuditChainReport } from '../src/lib/adminApi.types'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

function integrity(overrides: Partial<AuditChainReport>): { integrity: AuditChainReport } {
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
