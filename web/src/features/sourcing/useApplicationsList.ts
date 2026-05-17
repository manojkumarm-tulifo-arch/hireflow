import { useInfiniteQuery } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { Application, ApplicationStatus } from '@/api/types'

const PAGE_SIZE = 20

export interface AppListFilter {
  status?: ApplicationStatus
  limit?: number
  offset?: number
}

export function useApplicationsList(intentId: string, status?: string) {
  return useInfiniteQuery({
    queryKey: ['applications', intentId, { status: status ?? '' }],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      const filter: AppListFilter = { limit: PAGE_SIZE, offset: pageParam as number }
      if (status) filter.status = status as ApplicationStatus
      return sourcingApi.listApplications(intentId, filter)
    },
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((acc, p) => acc + p.items.length, 0)
      if (loaded >= lastPage.total) return undefined
      return loaded
    },
  })
}

export function flattenApplications(
  pages: Array<{ items: Application[] }> | undefined,
): Application[] {
  return pages?.flatMap((p) => p.items) ?? []
}
