import { useQuery } from '@tanstack/react-query'
import { authApi } from '@/lib/api'
import type { Principal } from '@/lib/adminApi.types'

export const principalQueryKey = ['auth', 'principal'] as const

export function canWrite(principal: Principal | undefined): boolean {
  return principal?.can_write === true
}

export function usePrincipal(enabled = true) {
  const query = useQuery({
    queryKey: principalQueryKey,
    queryFn: async ({ signal }) => (await authApi.me({ signal })).data,
    enabled,
    retry: false,
    staleTime: 30_000,
  })
  return { ...query, principal: query.data, canWrite: canWrite(query.data) }
}
