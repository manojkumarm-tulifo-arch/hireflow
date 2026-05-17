import { cn } from '@/lib/cn';
import { MapPin } from 'lucide-react';
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

function initials(name: string): string {
  return name
    .split(' ')
    .map((p) => p[0] ?? '')
    .join('')
    .slice(0, 2)
    .toUpperCase();
}

interface CandidateCardProps {
  application: Application;
  selected?: boolean;
  onSelect?: (id: string, checked: boolean) => void;
  onShortlist?: (id: string) => void;
  onHire?: (id: string) => void;
  onReject?: (id: string) => void;
}

export function CandidateCard({
  application,
  selected = false,
  onSelect,
  onShortlist,
  onHire,
  onReject,
}: CandidateCardProps) {
  const { id, status, overall_score, score_band, candidate } = application;
  const { full_name, headline, location, top_skills, judge_summary } = candidate;
  const isTerminal = TERMINAL.includes(status);

  return (
    <div
      className={cn(
        'bg-white rounded-xl border border-line shadow-sm p-4 flex gap-3',
        isTerminal && 'opacity-60',
      )}
    >
      {/* Checkbox */}
      <div className="pt-0.5">
        <input
          type="checkbox"
          disabled={isTerminal}
          checked={selected}
          onChange={(e) => onSelect?.(id, e.target.checked)}
          className="rounded border-line disabled:cursor-not-allowed"
          aria-label={`Select ${full_name}`}
        />
      </div>

      {/* Initials avatar */}
      <div className="shrink-0 w-9 h-9 rounded-full bg-accent-soft text-accent flex items-center justify-center text-xs font-bold">
        {initials(full_name)}
      </div>

      {/* Main content */}
      <div className="flex-1 min-w-0 space-y-1">
        {/* Name + headline + location row */}
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="font-semibold text-ink text-sm">{full_name}</span>
          {headline && (
            <span className="text-xs text-ink-sub truncate">{headline}</span>
          )}
          {location && (
            <span className="flex items-center gap-0.5 text-xs text-ink-sub">
              <MapPin className="w-3 h-3" />
              {location}
            </span>
          )}
        </div>

        {/* Top skills */}
        {top_skills.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {top_skills.map((skill) => (
              <span
                key={skill.name}
                className="text-[10px] bg-line-soft text-ink-sub rounded px-1.5 py-0.5"
              >
                {skill.name}
                {skill.years != null && (
                  <span className="ml-0.5 opacity-70">{skill.years}y</span>
                )}
              </span>
            ))}
          </div>
        )}

        {/* Judge summary */}
        {judge_summary && (
          <p className="text-xs text-ink-sub line-clamp-2">{judge_summary}</p>
        )}
      </div>

      {/* Right column: score + band + status + actions */}
      <div className="flex flex-col items-end gap-2 shrink-0">
        <div className="flex items-center gap-2">
          {overall_score != null && (
            <span className="text-sm font-bold text-ink">
              {Math.round(overall_score)}
            </span>
          )}
          <BandPill band={score_band} />
          <Pill variant={statusVariant(status)}>{status}</Pill>
        </div>

        <ApplicationActions
          application={application}
          onShortlist={onShortlist}
          onHire={onHire}
          onReject={onReject}
        />
      </div>
    </div>
  );
}
