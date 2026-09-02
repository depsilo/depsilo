import { test, expect, mockAdminApi, setUiPreferences } from './fixtures/admin-api'
import type { Request } from '@playwright/test'

const rule = (overrides: Record<string, unknown> = {}) => ({
  id: 7,
  ecosystem: 'pypi',
  package_name: 'requests',
  version: '>= 2.31.0',
  action: 'deny' as const,
  reason: 'Blocked by release review',
  created_by: 'admin',
  created_at: '2026-08-01T00:00:00Z',
  updated_at: '2026-08-01T00:00:00Z',
  ...overrides,
})

test('test policy shows candidate hierarchy, winner, and decision reason', async ({ page }) => {
  let requestBody: unknown
  await mockAdminApi(page, {
    'GET /api/v1/admin/rules': [],
    'POST /api/v1/admin/rules/test': async (request: Request) => {
      requestBody = request.postDataJSON()
      return {
        allowed: false,
        matched_rule: rule(),
        winning_rule: rule(),
        winner_reason: 'Blocked by release review',
        reason: 'Blocked by release review',
        precedence_reason: 'package_specificity',
        candidates: [
          {
            rule: rule(),
            specificity: { priority: 0, ecosystem: 2, package: 2, version: 1, action: 1, id: 7 },
            match_levels: { ecosystem: 'exact', package: 'exact', version: 'range' },
            matched: true,
            selected: true,
            explanation: 'The package and requested version satisfy this range.',
          },
          {
            rule: rule({ id: 2, package_name: '*', version: '*' }),
            specificity: { priority: 0, ecosystem: 2, package: 0, version: 0, action: 1, id: 2 },
            match_levels: { ecosystem: 'exact', package: 'wildcard', version: 'wildcard' },
            matched: true,
            selected: false,
            explanation: 'Package wildcard applies to every version.',
          },
        ],
      }
    },
  })
  await setUiPreferences(page, 'light', 'en')
  await page.setViewportSize({ width: 320, height: 844 })
  await page.goto('/admin/rules')

  await page.getByRole('button', { name: 'Test Rule' }).click()
  const dialog = page.getByRole('dialog', { name: 'Test Package Rule' })
  await dialog.getByLabel('Package Name').fill('requests')
  await dialog.getByLabel('Version').fill('2.31.0')
  await dialog.getByRole('button', { name: 'Test', exact: true }).click()

  await expect(dialog.locator('[data-rule-test-decision="deny"]')).toBeVisible()
  await expect(dialog.locator('[data-rule-test-winner]')).toContainText('pypi/requests@>= 2.31.0')
  await expect(dialog.locator('[data-rule-test-reason]')).toContainText('Blocked by release review')
  await expect(dialog.locator('[data-rule-test-precedence]')).toContainText('more specific package selector')
  await expect(dialog.locator('[data-rule-test-candidate]')).toHaveCount(2)
  await expect(dialog.locator('[data-rule-test-candidate]').nth(0)).toHaveAttribute('data-selected', 'true')
  await expect(dialog.locator('[data-rule-test-candidate]').nth(0)).toContainText('Exact package')
  await expect(dialog.locator('[data-rule-test-candidate]').nth(0)).toContainText('Version range')
  await expect(dialog.locator('[data-rule-test-candidate]').nth(1)).toContainText('Wildcard')
  await expect(dialog.locator('[data-rule-test-specificity]').nth(0)).toContainText('P0')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(320)
  expect(requestBody).toEqual({ ecosystem: 'pypi', package: 'requests', version: '2.31.0' })
})

test('test policy keeps the default allow explanation when no rule matches', async ({ page }) => {
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin/rules')

  await page.getByRole('button', { name: 'Test Rule' }).click()
  const dialog = page.getByRole('dialog', { name: 'Test Package Rule' })
  await dialog.getByLabel('Package Name').fill('unlisted-package')
  await dialog.getByLabel('Version').fill('1.0.0')
  await dialog.getByRole('button', { name: 'Test', exact: true }).click()

  await expect(dialog.locator('[data-rule-test-decision="allow"]')).toBeVisible()
  await expect(dialog.locator('[data-rule-test-no-match]')).toContainText('No rule matched')
  await expect(dialog.locator('[data-rule-test-candidates-empty]')).toContainText('No candidate rules')
  await expect(dialog.locator('[data-rule-test-candidate]')).toHaveCount(0)
})

test('readonly principals can test policy without mutation controls', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/auth/me': {
      id: 2,
      username: 'viewer',
      role: 'readonly',
      enabled: true,
      auth_method: 'jwt',
      token_permissions: null,
      can_write: false,
    },
  })
  await setUiPreferences(page, 'light', 'en')
  await page.goto('/admin/rules')

  await expect(page.getByRole('button', { name: 'Test Rule' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add Rule' })).toHaveCount(0)
})
