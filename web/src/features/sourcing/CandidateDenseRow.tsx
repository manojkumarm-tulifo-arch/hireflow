import { cn } from '@/lib/cn';
import { Pill, BandPill } from '@/components/ui/Pill';
import { ApplicationActions } from './ApplicationActions';
import type { Application, ApplicationStatus } from '@/api/types';

type PillVariant = 'strong' | 'moderate' | 'weak' | 'info' | 'warn' | 'error' | 'neutral';

const TERMINAL: ApplicationStatus[] = ['Hired', 'Rejected'];

function statusVariant(status: ApplicationStatus): PillVariant {
  switch (status) {
    case 'Shortlisted':
    case 'Interviewing':
      return 'warn';
    case 'Hired':
      return 'strong';
    case 'Rejected':
      return 'neutral';
    default:
      return 'neutral';
  }
}

interface CandidateDenseRowProps {
  application: Application;
  selected?: boolean;
  onSelect?: (id: string, checked: boolean) => void;
  onShortlist?: (id: string) => void;
  onHire?: (id: string) => void;
  onReject?: (id: string) => void;
}

export function CandidateDenseRow({
  application,
  selected = false,
  onSelect,
  onShortlist,
  onHire,
  onReject,
}: CandidateDenseRowProps) {
  const { id, status, overall_score, score_band, candidate } = application;
  const { full_name, headline, location } = candidate;
  const isTerminal = TERMINAL.includes(status);

  return (
    <tr
      className={cn(
        'border-b border-line last:border-0 hover:bg-line-soft/40 transition-colors',
        isTerminal && 'opacity-60',
      )}
    >
      {/* Checkbox */}
      <td className="px-3 py-2 w-8">
        <input
          type="checkbox"
          disabled={isTerminal}
          checked={selected}
          onChange={(e) => onSelect?.(id, e.target.checked)}
          className="rounded border-line disabled:cursor-not-allowed"
          aria-label={`Select ${full_name}`}
        />
      </td>

      {/* Name */}
      <td className="px-3 py-2">
        <span className="font-semibold text-sm text-ink">{full_name}</span>
      </td>

      {/* Headline */}
      <td className="px-3 py-2 max-w-[200px]">
        <span className="text-xs text-ink-sub truncate block">{headline}</span>
      </td>

      {/* Location */}
      <td className="px-3 py-2">
        <span className="text-xs text-ink-sub">{location}</span>
      </td>

      {/* Score */}
      <td className="px-3 py-2 text-right">
        {overall_score != null ? (
          <span className="text-sm font-bold text-ink">
            {Math.round(overall_score)}
          </span>
        ) : (
          <span className="text-xs text-ink-sub">—</span>
        )}
      </td>

      {/* Band */}
      <td className="px-3 py-2">
        <BandPill band={score_band} />
      </td>

      {/* Status */}
      <td className="px-3 py-2">
        <Pill variant={statusVariant(status)}>{status}</Pill>
      </td>

      {/* Actions */}
      <td className="px-3 py-2">
        <ApplicationActions
          application={application}
          onShortlist={onShortlist}
          onHire={onHire}
          onReject={onReject}
        />
      </td>
    </tr>
  );
}
