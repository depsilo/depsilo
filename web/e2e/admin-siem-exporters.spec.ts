import type { AuditExporter, AuditExporterListResponse } from '../src/lib/adminApi.types'
import type { Request } from '@playwright/test'
import { expect, mockAdminApi, test } from './fixtures/admin-api'

test('audit exporters can be added, tested, and disabled from the audit page', async ({ page }) => {
  const exporters: AuditExporter[] = []
  let nextId = 1

  await mockAdminApi(page, {
    'GET /api/v1/admin/audit/exporters': () => ({
      items: exporters,
      audit_head: 120,
    } satisfies AuditExporterListResponse),
    'POST /api/v1/admin/audit/exporters': async (request: Request) => {
      const body = (await request.postDataJSON()) as { name: string; kind: AuditExporter['kind']; url: string }
      const created: AuditExporter = {
        id: nextId++,
        name: body.name,
        kind: body.kind,
        url: body.url,
        token_set: true,
        events: '*',
        enabled: true,
        cursor: 120,
        lag: 0,
        delivered_count: 0,
        last_error: '',
        last_attempt_at: null,
        last_success_at: null,
        created_at: '2026-10-06T10:00:00Z',
        updated_at: '2026-10-06T10:00:00Z',
      }
      exporters.push(created)
      return created
    },
    'PUT /api/v1/admin/audit/exporters/1': async (request: Request) => {
      const body = (await request.postDataJSON()) as { enabled?: boolean }
      if (body.enabled !== undefined) exporters[0].enabled = body.enabled
      return exporters[0]
    },
    'POST /api/v1/admin/audit/exporters/1/test': { delivered: true },
  })

  await page.goto('/admin/audit')
  const section = page.locator('[data-siem-exporters]')
  await expect(section).toBeVisible()
  await expect(section.getByText('尚未配置审计导出器')).toBeVisible()

  await section.getByLabel('名称').fill('生产 SIEM')
  await section.getByLabel('采集地址').fill('https://collector.example/audit')
  await section.getByLabel('令牌').fill('secret')
  await section.getByRole('button', { name: '添加导出器' }).click()

  await expect(section.getByText('生产 SIEM')).toBeVisible()
  await expect(section.getByRole('cell', { name: 'NDJSON' })).toBeVisible()
  await expect(section).toContainText('已启用')

  await section.getByRole('button', { name: '测试' }).click()
  await expect(section.getByText(/已向 "生产 SIEM" 投递测试事件/)).toBeVisible()

  await section.getByRole('button', { name: '停用' }).click()
  await expect(section).toContainText('已停用')
})
