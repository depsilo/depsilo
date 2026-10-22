import type { Snapshot, SnapshotListResponse } from '../src/lib/adminApi.types'
import type { Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('freeze snapshot can be promoted and activated from the cache page', async ({ page }) => {
  const snapshots: Snapshot[] = []
  let activeId = 0
  let nextId = 1

  await mockAdminApi(page, {
    'GET /api/v1/admin/snapshots': () => ({
      items: snapshots,
      active_snapshot_id: activeId,
      active_snapshot: snapshots.find(snapshot => snapshot.id === activeId)?.name ?? '',
    } satisfies SnapshotListResponse),
    'GET /api/v1/admin/snapshots/1': () => ({ snapshot: snapshots[0], items: [{id:1,snapshot_id:1,ecosystem:'pypi',package:'six',version:'1.17.0',cache_key:'pypi/six',sha256:'abc',size:12000}], total:42, page:1, active:false }),
    'POST /api/v1/admin/snapshots': async (request: Request) => {
      const body = (await request.postDataJSON()) as { name: string; note?: string }
      const created: Snapshot = {
        id: nextId++,
        name: body.name,
        note: body.note ?? '',
        created_by: 'e2e-admin',
        artifact_count: 42,
        total_bytes: 5 * 1024 * 1024,
        created_at: '2026-10-06T12:00:00Z',
        updated_at: '2026-10-06T12:00:00Z',
      }
      snapshots.push(created)
      return created
    },
    'PUT /api/v1/admin/snapshots/active': async (request: Request) => {
      const body = (await request.postDataJSON()) as { snapshot_id: number }
      activeId = body.snapshot_id
      return {
        active_snapshot_id: activeId,
        active_snapshot: snapshots.find(snapshot => snapshot.id === activeId)?.name ?? '',
      }
    },
  })

  await page.goto('/admin/cache?view=snapshots')
  await page.getByLabel('快照名称').fill('golden-1')
  await page.getByRole('button', { name: '创建哈希清单' }).click()

  await expect(page.getByText('golden-1', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: '快照表格' })).toContainText('42')
  await page.getByRole('button', { name: '用于快照模式' }).click()
  await expect(page.getByRole('dialog')).toContainText('未列入该快照的包版本将被拒绝')
  await expect(page.getByRole('dialog')).toContainText('six@1.17.0')
  expect(activeId).toBe(0)
  await page.getByRole('button', { name: '确认启用快照模式' }).click()

  await expect(page.getByText(/快照模式已启用/)).toBeVisible()
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭快照模式' }).first().click()
  await expect(page.getByText(/快照模式已启用/)).toHaveCount(0)
})
