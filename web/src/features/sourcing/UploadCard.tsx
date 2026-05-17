import { useState, useCallback } from 'react'
import { UploadCloud } from 'lucide-react'
import { Card, Button, Spinner } from '@/components/ui/primitives'
import { DropZone } from '@/components/ui/DropZone'
import { UploadOutcomesList } from './UploadOutcomesList'
import { useUploadBatch } from './useUploadBatch'
import { useBatchSSE } from './useBatchSSE'
import type { BatchUploadOutcome } from '@/api/types'

const ACCEPTED_MIME = [
  'application/pdf',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/msword',
  'application/vnd.oasis.opendocument.text',
  'application/zip',
].join(',')

interface UploadCardProps {
  intentId: string
}

export function UploadCard({ intentId }: UploadCardProps) {
  const [outcomes, setOutcomes] = useState<BatchUploadOutcome[]>([])
  const [batchId, setBatchId] = useState<string | null>(null)

  const upload = useUploadBatch(intentId)

  const handleFiles = useCallback(
    async (files: File[]) => {
      try {
        const response = await upload.mutateAsync(files)
        setBatchId(response.batch_id)
        setOutcomes((prev) => {
          // Merge: keep any existing items not in the new response, then append new
          const newFilenames = new Set(response.items.map((i) => i.filename))
          const kept = prev.filter((p) => !newFilenames.has(p.filename))
          return [...kept, ...response.items]
        })
      } catch {
        // Error surfaced via upload.error
      }
    },
    [upload],
  )

  useBatchSSE(batchId, intentId)

  const hasOutcomes = outcomes.length > 0

  const handleClear = useCallback(() => {
    setOutcomes([])
    setBatchId(null)
  }, [])

  return (
    <Card className="p-5 space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-bold text-ink">Upload resumes</h2>
        {hasOutcomes && (
          <Button variant="ghost" size="sm" onClick={handleClear}>
            Clear
          </Button>
        )}
      </div>

      <DropZone accept={ACCEPTED_MIME} multiple onFiles={handleFiles}>
        <div className="flex flex-col items-center gap-1 text-ink-sub">
          <UploadCloud className="w-8 h-8 text-ink-mute" />
          <p className="text-sm font-medium text-ink">
            Drop files here or{' '}
            <span className="text-accent">browse files</span>
          </p>
          <p className="text-xs text-ink-mute">
            PDF, DOC, DOCX, ODT, ZIP &middot; up to 10 MB each
          </p>
        </div>
      </DropZone>

      {upload.isPending && (
        <div className="flex items-center gap-2 text-sm text-ink-sub">
          <Spinner />
          <span>Uploading…</span>
        </div>
      )}

      {upload.isError && (
        <p className="text-sm text-red-600">{upload.error.message}</p>
      )}

      {hasOutcomes && (
        <UploadOutcomesList outcomes={outcomes} intentId={intentId} />
      )}
    </Card>
  )
}
