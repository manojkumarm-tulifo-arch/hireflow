import { useMutation, useQueryClient } from '@tanstack/react-query'
import { sourcingApi } from '@/api/sourcing'
import type { BatchUploadResponse } from '@/api/types'

export function useUploadBatch(intentId: string) {
  const qc = useQueryClient()
  return useMutation<BatchUploadResponse, Error, File[]>({
    mutationFn: (files) => sourcingApi.uploadBatch(intentId, files),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['applications', intentId] })
    },
  })
}
