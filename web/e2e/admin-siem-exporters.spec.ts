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
  await page.getByRole('button', { name: '外部投递' }).click()
  const section = page.locator('[data-siem-exporters]')
  await expect(section).toBeVisible()
  await expect(section.getByText('尚未配置审计导出器')).toBeVisible()

  await page.getByRole('button', { name: '添加投递目标', exact: true }).click()
  const form = page.getByRole('dialog')
  await form.getByLabel('名称').fill('生产 SIEM')
  await form.getByLabel('采集地址').fill('https://collector.example/audit')
  await form.getByLabel('令牌').fill('secret')
  await form.getByRole('button', { name: '添加导出器' }).click()

  await expect(section.getByText('生产 SIEM')).toBeVisible()
  await expect(section.getByRole('cell', { name: 'NDJSON' })).toBeVisible()
  await expect(section).toContainText('已启用')

  await section.getByRole('button', { name: '测试' }).click()
  await expect(section.getByText(/已向 "生产 SIEM" 投递测试事件/)).toBeVisible()

  await section.getByRole('button', { name: '停用' }).click()
  await expect(section).toContainText('已停用')
})

test('readonly operators can inspect exporters without write actions', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': { id: 2, username: 'reader', role: 'readonly', enabled: true, auth_method: 'jwt', token_permissions: null, can_write: false },
    'GET /api/v1/admin/audit/exporters': { audit_head: 1, items: [{ id: 1, name: 'Audit collector', kind: 'ndjson', url: 'https://collector.example/audit', token_set: true, events: '*', enabled: true, cursor: 1, lag: 0, delivered_count: 1, last_error: '', last_attempt_at: null, last_success_at: null, created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z' }] },
  })
  await page.goto('/admin/audit?view=delivery')
  const section = page.locator('[data-siem-exporters]')
  await expect(section.getByText('Audit collector')).toBeVisible()
  await expect(section.getByRole('button', { name: /测试|停用|删除|添加/ })).toHaveCount(0)
})
