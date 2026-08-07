import type { AxiosAdapter } from 'axios'
import { afterAll, beforeEach, describe, expect, it } from 'vitest'
import api, { adminApi, statsApi, webhookApi } from '../src/lib/api'

const originalAdapter = api.defaults.adapter
let observedSignal: AbortSignal | undefined
let observedParams: unknown

beforeEach(() => {
  observedSignal = undefined
  observedParams = undefined
  const adapter: AxiosAdapter = async config => {
    observedSignal = config.signal
    observedParams = config.params
    return {
      data: {},
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
    }
  }
  api.defaults.adapter = adapter
})

afterAll(() => {
  api.defaults.adapter = originalAdapter
})

describe('GET request cancellation', () => {
  it('forwards the query signal through stats requests', async () => {
    const controller = new AbortController()
    await statsApi.getStats({ signal: controller.signal })
    expect(observedSignal).toBe(controller.signal)
  })

  it('forwards the signal without dropping admin query parameters', async () => {
    const controller = new AbortController()
    await adminApi.getDashboardTrends('24h', { signal: controller.signal })
    expect(observedSignal).toBe(controller.signal)
    expect(observedParams).toEqual({ range: '24h' })
  })

  it('forwards the query signal through webhook list requests', async () => {
    const controller = new AbortController()
    await webhookApi.list({ signal: controller.signal })
    expect(observedSignal).toBe(controller.signal)
  })
})
