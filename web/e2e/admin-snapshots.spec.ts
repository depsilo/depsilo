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

  await page.goto('/admin/cache')
  await page.getByLabel('快照名称').fill('golden-1')
  await page.getByRole('button', { name: '从当前缓存创建' }).click()

  await expect(page.getByText('golden-1', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: '快照表格' })).toContainText('42')
  await page.getByRole('button', { name: '用于快照模式' }).click()

  await expect(page.getByText(/快照模式已启用/)).toBeVisible()
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭快照模式' }).first().click()
  await expect(page.getByText(/快照模式已启用/)).toHaveCount(0)
})
