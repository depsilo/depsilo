import axios from 'axios'
import api, { type ApiGetOptions } from './api'
import type { ReadinessResponse } from './adminApi.types'

// Only the operational Dashboard needs this probe; keep it in its lazy graph.
export const getReadiness = (options: ApiGetOptions = {}) =>
    api.get<ReadinessResponse>('/ready', { ...options, baseURL: '' }).catch((error: unknown) => {
      // A readiness failure is a measured service state. Other HTTP failures,
      // including an unrelated proxy's 503 page, remain query errors.
      if (axios.isAxiosError<ReadinessResponse>(error) && error.response?.status === 503) {
        const data = error.response.data
        const knownCheck = (value: unknown) => value === 'ready' || value === 'unavailable'
        if (data?.status === 'not_ready'
          && knownCheck(data.checks?.database)
          && knownCheck(data.checks?.storage)
          && (data.checks.database === 'unavailable' || data.checks.storage === 'unavailable')) {
          return error.response
        }
      }
      throw error
    })
