import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

interface BulkActionInput {
  kind: 'shortlist' | 'reject'
  ids: string[]
  reason?: string
}

interface BulkActionResult {
  succeeded: string[]
  failed: string[]
}

export function useBulkAction(intentId: string) {
  const qc = useQueryClient()

  return useMutation<BulkActionResult, Error, BulkActionInput>({
    mutationFn: async ({ kind, ids, reason }) => {
      const succeeded: string[] = []
      const failed: string[] = []

      for (const id of ids) {
        try {
          if (kind === 'shortlist') {
            await sourcingApi.shortlist(id)
          } else {
            await sourcingApi.reject(id, reason ?? 'Bulk reject (no reason)')
          }
          succeeded.push(id)
        } catch {
          failed.push(id)
        }
      }

      return { succeeded, failed }
    },

    onSettled: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
