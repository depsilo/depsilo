import { describe, expect, it } from 'vitest'

import { createUpstreamRuntimeModel } from '../../../src/admin/upstreams/runtimeModel'
import type { AdminUpstream, AdminUpstreamListResponse } from '../../../src/lib/adminApi.types'

const originalAlpha: AdminUpstream = {
  id: 1,
  adapter_type: 'pypi',
  name: 'alpha',
  url: 'https://alpha.example/simple',
  proxy: '',
  priority: 1,
  probe_mode: 'active',
  probe_interval: '30m',
  healthy: true,
  avg_latency_ms: 20,
  success_rate: 1,
  last_checked_at: null,
  worker_running: true,
  created_at: '2026-07-10T00:00:00Z',
  updated_at: '2026-07-10T00:00:00Z',
}

const initialList: AdminUpstreamListResponse = {
  items: [originalAlpha],
  total: 1,
}

describe('upstream runtime model', () => {
  it('does not let a late check overwrite a saved upstream', () => {
    const model = createUpstreamRuntimeModel()
    const ticket = model.beginCheck(originalAlpha)
    const editedAlpha: AdminUpstream = {
      ...originalAlpha,
      name: 'alpha-edited',
      url: 'https://edited.example/simple',
      updated_at: '2026-07-10T00:02:00Z',
    }
    const afterEdit = model.applyMutation(initialList, {
      kind: 'saved',
      upstream: editedAlpha,
    })

    const reconciled = model.applyChecks(afterEdit, [{
      status: 'fulfilled',
      ticket,
      result: {
        upstream: {
          ...originalAlpha,
          healthy: false,
          avg_latency_ms: 900,
          last_checked_at: '2026-07-10T00:01:00Z',
        },
        check: {
          healthy: false,
          latency_ms: 900,
          checked_at: '2026-07-10T00:01:00Z',
          error: 'timeout',
        },
      },
    }])

    expect(reconciled.data).toEqual({ items: [editedAlpha], total: 1 })
    expect(reconciled.accepted).toEqual([])
  })

  it('does not let a late check resurrect a deleted upstream', () => {
    const model = createUpstreamRuntimeModel()
    const ticket = model.beginCheck(originalAlpha)
    const afterDelete = model.applyMutation(initialList, {
      kind: 'deleted',
      upstreamId: originalAlpha.id,
    })

    const reconciled = model.applyChecks(afterDelete, [{
      status: 'fulfilled',
      ticket,
      result: {
        upstream: {
          ...originalAlpha,
          healthy: false,
          avg_latency_ms: 900,
          last_checked_at: '2026-07-10T00:01:00Z',
        },
        check: {
          healthy: false,
          latency_ms: 900,
          checked_at: '2026-07-10T00:01:00Z',
          error: 'timeout',
        },
      },
    }])

    expect(reconciled.data).toEqual({ items: [], total: 0 })
    expect(reconciled.accepted).toEqual([])
  })

  it('reports a failed check only while its ticket is current', () => {
    const model = createUpstreamRuntimeModel()
    const currentTicket = model.beginCheck(originalAlpha)
    const failure = new Error('network unavailable')

    const currentResult = model.applyChecks(initialList, [{
      status: 'rejected',
      ticket: currentTicket,
      reason: failure,
    }])
    expect(currentResult.failures).toEqual([{
      upstreamId: originalAlpha.id,
      reason: failure,
    }])

    const editedAlpha = {
      ...originalAlpha,
      updated_at: '2026-07-10T00:02:00Z',
    }
    const afterEdit = model.applyMutation(initialList, {
      kind: 'saved',
      upstream: editedAlpha,
    })
    const staleResult = model.applyChecks(afterEdit, [{
      status: 'rejected',
      ticket: currentTicket,
      reason: failure,
    }])
    expect(staleResult.failures).toEqual([])
  })

  it('keeps saved upstreams in canonical ecosystem order', () => {
    const model = createUpstreamRuntimeModel()
    const npmUpstream: AdminUpstream = {
      ...originalAlpha,
      id: 2,
      adapter_type: 'npm',
      name: 'npm-primary',
      url: 'https://registry.npmjs.org',
    }

    const result = model.applyMutation({ items: [npmUpstream], total: 1 }, {
      kind: 'saved',
      upstream: originalAlpha,
    })

    expect(result.items.map(upstream => upstream.id)).toEqual([originalAlpha.id, npmUpstream.id])
  })

  it('merges a current check result into the runtime list', () => {
    const model = createUpstreamRuntimeModel()
    const ticket = model.beginCheck(originalAlpha)
    const checkedAlpha: AdminUpstream = {
      ...originalAlpha,
      healthy: false,
      avg_latency_ms: 900,
      last_checked_at: '2026-07-10T00:01:00Z',
    }
    const result = {
      upstream: checkedAlpha,
      check: {
        healthy: false,
        latency_ms: 900,
        checked_at: '2026-07-10T00:01:00Z',
        error: 'timeout',
      },
    }

    const reconciled = model.applyChecks(initialList, [{
      status: 'fulfilled',
      ticket,
      result,
    }])

    expect(reconciled.data).toEqual({ items: [checkedAlpha], total: 1 })
    expect(reconciled.accepted).toEqual([result])
  })

  it('rejects a check when a newer server timestamp is already cached', () => {
    const model = createUpstreamRuntimeModel()
    const ticket = model.beginCheck(originalAlpha)
    const externallyEditedAlpha: AdminUpstream = {
      ...originalAlpha,
      name: 'edited-elsewhere',
      updated_at: '2026-07-10T00:02:00Z',
    }
    const staleResult = {
      upstream: {
        ...originalAlpha,
        healthy: false,
        avg_latency_ms: 900,
      },
      check: {
        healthy: false,
        latency_ms: 900,
        checked_at: '2026-07-10T00:01:00Z',
        error: 'timeout',
      },
    }

    const reconciled = model.applyChecks(
      { items: [externallyEditedAlpha], total: 1 },
      [{ status: 'fulfilled', ticket, result: staleResult }],
    )

    expect(reconciled.data).toEqual({ items: [externallyEditedAlpha], total: 1 })
    expect(reconciled.accepted).toEqual([])
  })
})
