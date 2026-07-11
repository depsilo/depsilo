import axios from 'axios'

export interface ApiErrorInfo {
  status?: number
  code?: string
  message: string
}

export function getApiError(error: unknown): ApiErrorInfo {
  if (axios.isAxiosError<{ code?: string; message?: string }>(error)) {
    return {
      status: error.response?.status,
      code: error.response?.data?.code,
      message: error.response?.data?.message || error.message,
    }
  }
  return { message: error instanceof Error ? error.message : 'Unknown error' }
}
