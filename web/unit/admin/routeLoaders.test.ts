import { describe, expect, it, vi } from 'vitest'

import { createRoutePrefetcher } from '../../src/admin/routeLoaders'

const buildPrefetcher = () => {
  const calls: string[] = []
  const routes = {
    cache: { preload: () => calls.push('cache') },
    cacheIndexes: { preload: () => calls.push('cacheIndexes') },
  }
  return { calls, prefetch: createRoutePrefetcher(routes) }
}

describe('Admin route prefetcher', () => {
  it('warms a destination once no matter how often the pointer crosses it', () => {
    const { calls, prefetch } = buildPrefetcher()

    prefetch('cacheIndexes')
    prefetch('cacheIndexes')
    prefetch('cache')
    prefetch('cacheIndexes')

    expect(calls).toEqual(['cacheIndexes', 'cache'])
  })

  it('leaves each destination independent', () => {
    const { calls, prefetch } = buildPrefetcher()

    prefetch('cache')
    prefetch('cacheIndexes')

    expect(calls).toEqual(['cache', 'cacheIndexes'])
  })

  it('hands the destination to its own loader', () => {
    const preload = vi.fn()
    const prefetch = createRoutePrefetcher({ cache: { preload } })

    prefetch('cache')

    expect(preload).toHaveBeenCalledTimes(1)
  })
})
