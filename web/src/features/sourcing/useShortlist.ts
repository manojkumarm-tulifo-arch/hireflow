import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { Application, ApplicationListResponse } from '@/api/types'

interface MutationContext {
  prevData: Array<[unknown, unknown]>
}

export function useShortlist(intentId: string) {
  const qc = useQueryClient()

  return useMutation<Application, Error, string, MutationContext>({
    mutationFn: (applicationId) => sourcingApi.shortlist(applicationId),

    onMutate: async (applicationId) => {
      await qc.cancelQueries({ queryKey: ['applications', intentId] })

      const prevData = qc.getQueriesData<ApplicationListResponse>({
        queryKey: ['applications', intentId],
      }) as Array<[unknown, unknown]>

      qc.setQueriesData<ApplicationListResponse>(
        { queryKey: ['applications', intentId] },
        (old) => {
          if (!old) return old
          return {
            ...old,
            items: old.items.map((app) =>
              app.id === applicationId
                ? { ...app, status: 'Shortlisted' as const }
                : app,
            ),
          }
        },
      )

      return { prevData }
    },

    onError: (_err, _vars, ctx) => {
      if (!ctx) return
      for (const [queryKey, data] of ctx.prevData) {
        qc.setQueryData(queryKey as Parameters<typeof qc.setQueryData>[0], data)
      }
    },

    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
