import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'

export function useRetryUpload(intentId: string) {
  const qc = useQueryClient()
  return useMutation<void, Error, string>({
    mutationFn: (uploadId) => sourcingApi.retryUpload(uploadId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
