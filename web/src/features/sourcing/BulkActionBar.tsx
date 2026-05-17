import { Button } from '@/components/ui/primitives'
import { useBulkAction } from './useBulkAction'

interface BulkActionBarProps {
  selectedIds: Set<string>
  intentId: string
  onClear: () => void
}

export function BulkActionBar({ selectedIds, intentId, onClear }: BulkActionBarProps) {
  const bulk = useBulkAction(intentId)
  const count = selectedIds.size

  if (count === 0) return null

  const ids = Array.from(selectedIds)

  async function handleShortlist() {
    await bulk.mutateAsync({ kind: 'shortlist', ids })
    onClear()
  }

  async function handleReject() {
    const reason = window.prompt(`Reject ${count} candidate(s) — enter a reason:`)
    if (reason === null || reason.trim() === '') return
    await bulk.mutateAsync({ kind: 'reject', ids, reason: reason.trim() })
    onClear()
  }

  return (
    <div className="sticky bottom-0 bg-amber-50 border-t border-amber-200 px-4 py-3 flex items-center gap-3 z-10">
      <span className="text-sm font-medium text-amber-900 flex-1">
        {count} selected
      </span>

      <Button
        size="sm"
        variant="primary"
        disabled={bulk.isPending}
        onClick={handleShortlist}
      >
        Shortlist {count}
      </Button>

      <Button
        size="sm"
        variant="danger"
        disabled={bulk.isPending}
        onClick={handleReject}
      >
        Reject {count}
      </Button>

      <Button
        size="sm"
        variant="ghost"
        disabled={bulk.isPending}
        onClick={onClear}
      >
        Clear
      </Button>
    </div>
  )
}
