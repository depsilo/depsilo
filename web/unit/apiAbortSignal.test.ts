import { AxiosError, type AxiosAdapter } from 'axios'
import { afterAll, beforeEach, describe, expect, it } from 'vitest'
import api, { adminApi, statsApi, webhookApi } from '../src/lib/api'

import { getReadiness } from '../src/lib/readiness'

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

describe('readiness transport', () => {
  it('uses the root readiness endpoint and keeps cancellation', async () => {
    const controller = new AbortController()
    api.defaults.adapter = async config => {
      expect(config.baseURL).toBe('')
      expect(config.url).toBe('/ready')
      expect(config.signal).toBe(controller.signal)
      return { data: {}, status: 200, statusText: 'OK', headers: {}, config }
    }
    await getReadiness({ signal: controller.signal })
  })

  it.each([
    { data: { status: 'not_ready', checks: { database: 'ready', storage: 'unavailable' } }, accepted: true },
    { data: '<html>upstream unavailable</html>', accepted: false },
    { data: { status: 'not_ready', checks: { database: 'ready', storage: 'ready' } }, accepted: false },
  ])('only accepts a measured not_ready 503 ($accepted)', async ({ data, accepted }) => {
    api.defaults.adapter = async config => {
      throw new AxiosError('unavailable', 'ERR_BAD_RESPONSE', config, undefined, {
        data, status: 503, statusText: 'Service Unavailable', headers: {}, config,
      })
    }
    if (accepted) {
      await expect(getReadiness()).resolves.toMatchObject({ status: 503, data })
    } else {
      await expect(getReadiness()).rejects.toMatchObject({ response: { status: 503 } })
    }
  })

  it('preserves other server errors', async () => {
    api.defaults.adapter = async config => {
      throw new AxiosError('failed', 'ERR_BAD_RESPONSE', config, undefined, {
        data: {}, status: 500, statusText: 'Internal Server Error', headers: {}, config,
      })
    }
    await expect(getReadiness()).rejects.toMatchObject({ response: { status: 500 } })
  })
})
