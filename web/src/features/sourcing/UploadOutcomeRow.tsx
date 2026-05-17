import { cn } from '@/lib/cn'
import { Pill } from '@/components/ui/Pill'
import { Button } from '@/components/ui/primitives'
import {
  FileText,
  Archive,
  AlertCircle,
  CheckCircle,
  XCircle,
} from 'lucide-react'
import type { BatchUploadOutcome } from '@/api/types'

interface UploadOutcomeRowProps {
  outcome: BatchUploadOutcome
  indented?: boolean
  onRetry?: (uploadId: string) => void
  retrying?: boolean
}

function statusMeta(outcome: BatchUploadOutcome): {
  icon: React.ReactNode
  pill: React.ReactNode
  rowBg: string
} {
  const { status, error } = outcome

  if (status === 'extracted_from_zip') {
    return {
      icon: <Archive className="w-4 h-4 text-ink-sub" />,
      pill: <Pill variant="info">ZIP</Pill>,
      rowBg: 'bg-blue-50/40',
    }
  }

  if (status === 'duplicate_in_intent' || status === 'deduplicated') {
    return {
      icon: <FileText className="w-4 h-4 text-ink-sub" />,
      pill: <Pill variant="warn">Duplicate</Pill>,
      rowBg: 'bg-amber-50/40',
    }
  }

  if (status === 'queued') {
    return {
      icon: <FileText className="w-4 h-4 text-accent" />,
      pill: <Pill variant="info">Queued</Pill>,
      rowBg: '',
    }
  }

  // status === '' means rejected / error
  if (!status || error) {
    return {
      icon: <XCircle className="w-4 h-4 text-red-500" />,
      pill: <Pill variant="error">Rejected</Pill>,
      rowBg: 'bg-red-50/40',
    }
  }

  // Fallback: accepted / unknown positive status
  return {
    icon: <CheckCircle className="w-4 h-4 text-green-600" />,
    pill: <Pill variant="strong">Accepted</Pill>,
    rowBg: '',
  }
}

export function UploadOutcomeRow({
  outcome,
  indented = false,
  onRetry,
  retrying = false,
}: UploadOutcomeRowProps) {
  const { icon, pill, rowBg } = statusMeta(outcome)
  const canRetry =
    (outcome.status === '' || !outcome.status) &&
    !!outcome.upload_id &&
    !!onRetry

  return (
    <div
      className={cn(
        'flex items-center gap-3 px-3 py-2 rounded-md text-sm',
        indented && 'ml-6 border-l border-line pl-4',
        rowBg,
      )}
    >
      <span className="shrink-0">{icon}</span>

      <span className="flex-1 min-w-0 truncate text-ink font-medium">
        {outcome.filename}
      </span>

      <span className="shrink-0">{pill}</span>

      {outcome.error && (
        <span
          className="flex items-center gap-1 text-xs text-red-600 shrink-0"
          title={outcome.error.detail ? JSON.stringify(outcome.error.detail) : undefined}
        >
          <AlertCircle className="w-3.5 h-3.5" />
          {outcome.error.message}
        </span>
      )}

      {canRetry && (
        <Button
          variant="secondary"
          size="sm"
          disabled={retrying}
          onClick={() => onRetry(outcome.upload_id!)}
        >
          {retrying ? 'Retrying…' : 'Retry'}
        </Button>
      )}
    </div>
  )
}
