import { UploadOutcomeRow } from './UploadOutcomeRow'
import { useRetryUpload } from './useRetryUpload'
import type { BatchUploadOutcome } from '@/api/types'

interface UploadOutcomesListProps {
  outcomes: BatchUploadOutcome[]
  intentId: string
}

export function UploadOutcomesList({ outcomes, intentId }: UploadOutcomesListProps) {
  const retry = useRetryUpload(intentId)

  if (outcomes.length === 0) return null

  // Group outcomes: ZIP parents have status === 'extracted_from_zip'.
  // Both parent and children share the same parent_item_id, so we distinguish
  // by status: parent has status 'extracted_from_zip', children do not.
  const topLevel: BatchUploadOutcome[] = []
  const childrenByParent = new Map<string, BatchUploadOutcome[]>()

  for (const o of outcomes) {
    if (o.parent_item_id && o.status !== 'extracted_from_zip') {
      // child of a ZIP
      const list = childrenByParent.get(o.parent_item_id) ?? []
      list.push(o)
      childrenByParent.set(o.parent_item_id, list)
    } else {
      // top-level: standalone file OR ZIP parent marker
      topLevel.push(o)
    }
  }

  return (
    <div className="space-y-1">
      {topLevel.map((o, idx) => {
        const key = o.upload_id ?? `${o.filename}-${idx}`
        const children = o.upload_id
          ? (childrenByParent.get(o.upload_id) ?? [])
          : []

        return (
          <div key={key}>
            <UploadOutcomeRow
              outcome={o}
              onRetry={retry.mutate}
              retrying={retry.isPending && retry.variables === o.upload_id}
            />
            {children.map((child, ci) => (
              <UploadOutcomeRow
                key={child.upload_id ?? `${child.filename}-${ci}`}
                outcome={child}
                indented
                onRetry={retry.mutate}
                retrying={retry.isPending && retry.variables === child.upload_id}
              />
            ))}
          </div>
        )
      })}
    </div>
  )
}
