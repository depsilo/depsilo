import axios from 'axios'

export interface ApiErrorInfo {
  status?: number
  code?: string
  fieldErrors?: Record<string, string>
  message: string
}

export function getApiError(error: unknown): ApiErrorInfo {
  if (axios.isAxiosError<{ code?: string; message?: string; field_errors?: Record<string, string> }>(error)) {
    return {
      status: error.response?.status,
      code: error.response?.data?.code,
      fieldErrors: error.response?.data?.field_errors,
      message: error.response?.data?.message || error.message,
    }
  }
  return { message: error instanceof Error ? error.message : 'Unknown error' }
}
