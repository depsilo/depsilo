import type { Request } from '@playwright/test'

import { expect, mockAdminApi, setUiPreferences, test } from './fixtures/admin-api'

const enabledStatus = {
  enabled: true,
  endpoint: 'https://cache.example.test/ccache/v1/{namespace}',
  endpoints: {
    ccache: 'https://cache.example.test/ccache/v1/{namespace}',
    sccache: 'https://cache.example.test/sccache/v1/{namespace}',
  },
  stats: {
    size_bytes: 64 * 1024 * 1024,
    max_bytes: 1024 * 1024 * 1024,
    entries: 42,
    max_entries: 500000,
    hits: 314,
    namespace_count: 3,
  },
}

test('creates a scoped credential and clears both one-time client configurations on close', async ({ page }) => {
  let createPayload: unknown
  const remoteStorage = 'https://cache.example.test/ccache/v1/linux-ci|@bearer-token=depsilo_cc_one_time_secret'
  const sccacheConfig = '[cache.webdav]\n' +
    'endpoint = "https://cache.example.test/sccache/v1/linux-ci"\n' +
    'token = "depsilo_cc_one_time_secret"'
  await mockAdminApi(page, {
    'GET /api/v1/admin/compile-cache/status': enabledStatus,
    'GET /api/v1/admin/compile-cache/credentials': { items: [], total: 0 },
    'POST /api/v1/admin/compile-cache/credentials': async (request: Request) => {
      createPayload = request.postDataJSON()
      return {
        id: 7,
        name: 'Linux CI',
        namespace: 'linux-ci',
        permissions: 'readwrite',
        expires_at: '2026-08-20T00:00:00Z',
        token: 'depsilo_cc_one_time_secret',
        endpoints: {
          ccache: 'https://cache.example.test/ccache/v1/linux-ci',
          sccache: 'https://cache.example.test/sccache/v1/linux-ci',
        },
        ccache_remote_storage: remoteStorage,
        sccache_config: sccacheConfig,
        endpoint: 'https://cache.example.test/ccache/v1/linux-ci',
        remote_storage: remoteStorage,
        warning: 'save this token now',
      }
    },
  })
  await setUiPreferences(page, 'light', 'en')

  await page.goto('/admin/compile-cache')
  await expect(page.getByText('64.0 MB', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('42 / 500,000', { exact: true })).toBeVisible()
  await expect(page.getByText('314', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Create credential' }).click()
  const createDialog = page.getByRole('dialog', { name: 'Create credential' })
  await createDialog.getByLabel('Credential name').fill(' Linux CI ')
  await createDialog.getByLabel('Namespace').fill('linux-ci')
  await createDialog.getByLabel('Validity').selectOption('30')
  await createDialog.getByRole('button', { name: 'Create', exact: true }).click()

  const resultDialog = page.getByRole('dialog', { name: 'Credential created' })
  await expect(resultDialog.getByText('ccache', { exact: true })).toBeVisible()
  await expect(resultDialog.getByText(remoteStorage, { exact: true })).toBeVisible()
  await expect(resultDialog.getByText('sccache', { exact: true })).toBeVisible()
  await expect(resultDialog.getByText(sccacheConfig, { exact: true })).toBeVisible()
  const copyCCache = resultDialog.getByRole('button', { name: 'Copy ccache configuration' })
  await copyCCache.click()
  await expect(copyCCache).toContainText('Copied')
  const copySCCache = resultDialog.getByRole('button', { name: 'Copy sccache configuration' })
  await copySCCache.click()
  await expect(copySCCache).toContainText('Copied')
  expect(createPayload).toEqual({
    name: 'Linux CI',
    namespace: 'linux-ci',
    permissions: 'readwrite',
    ttl_days: 30,
  })

  await resultDialog.getByRole('button', { name: 'Close' }).last().click()
  await expect(page.getByText(remoteStorage, { exact: true })).toHaveCount(0)
  await expect(page.getByText(sccacheConfig, { exact: true })).toHaveCount(0)
})

test('confirms credential revocation and manual cleanup', async ({ page }) => {
  await mockAdminApi(page, {
    'GET /api/v1/admin/compile-cache/status': enabledStatus,
    'GET /api/v1/admin/compile-cache/credentials': {
      items: [{
        id: 9,
        name: 'macOS builders',
        namespace: 'macos',
        permissions: 'readonly',
        expires_at: null,
        last_used_at: null,
        created_at: '2026-07-21T00:00:00Z',
        updated_at: '2026-07-21T00:00:00Z',
      }],
      total: 1,
    },
    'DELETE /api/v1/admin/compile-cache/credentials/9': {},
    'POST /api/v1/admin/compile-cache/cleanup': {
      removed_entries: 4,
      reclaimed_bytes: 8 * 1024 * 1024,
      size_bytes: 56 * 1024 * 1024,
      entries: 38,
    },
  })
  await setUiPreferences(page, 'light', 'en')

  await page.goto('/admin/compile-cache')
  await page.getByRole('button', { name: 'Revoke credential macOS builders' }).click()
  const revokeDialog = page.getByRole('dialog', { name: 'Revoke build credential' })
  await revokeDialog.getByRole('button', { name: 'Revoke credential' }).click()
  await expect(page.getByText('Build credential revoked.')).toBeVisible()

  await page.getByRole('button', { name: 'Clean up', exact: true }).click()
  const cleanupDialog = page.getByRole('dialog', { name: 'Clean up compiler cache' })
  await cleanupDialog.getByRole('button', { name: 'Start cleanup' }).click()
  await expect(page.getByText('Cleanup complete: removed 4 entries and reclaimed 8.0 MB.')).toBeVisible()
})
